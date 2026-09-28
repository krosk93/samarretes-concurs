package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

//go:embed static
var staticFS embed.FS

// Person is one sheet row (Row = 1-based sheet row number, headers = row 1).
type Person struct {
	Row       int    `json:"row"`
	Nom       string `json:"nom"`
	Alies     string `json:"alies"`
	Colla     string `json:"colla"`
	Talla     string `json:"talla"`
	Recollida bool   `json:"recollida"`
	// Modalitat is the raw text of the "Vols fer pinya o només entrar a plaça?" cell.
	Modalitat string `json:"modalitat"`
	// Acompanya is true when the cell opts for "acompanyar" (sense pinya).
	Acompanya bool `json:"acompanya"`
}

type config struct {
	SpreadsheetID   string
	SheetName       string
	CredentialsFile string
	CredentialsJSON string
	Port            string
	RefreshInterval time.Duration
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() (config, error) {
	_ = godotenv.Load() // optional .env file
	cfg := config{
		SpreadsheetID:   strings.TrimSpace(os.Getenv("SPREADSHEET_ID")),
		SheetName:       strings.TrimSpace(getEnv("SHEET_NAME", "")),
		CredentialsFile: getEnv("GOOGLE_CREDENTIALS_FILE", "credentials.json"),
		CredentialsJSON: os.Getenv("GOOGLE_CREDENTIALS_JSON"),
		Port:            getEnv("PORT", "8080"),
	}
	if cfg.SpreadsheetID == "" {
		return cfg, fmt.Errorf("SPREADSHEET_ID is required")
	}
	secs := getEnv("REFRESH_INTERVAL_SECONDS", "60")
	n, err := strconv.Atoi(secs)
	if err != nil || n <= 0 {
		n = 60
	}
	cfg.RefreshInterval = time.Duration(n) * time.Second
	return cfg, nil
}

// saEmail extracts client_email from a service-account JSON key ("" if unknown).
func saEmail(creds []byte) string {
	var m struct {
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(creds, &m); err != nil {
		return ""
	}
	return strings.TrimSpace(m.ClientEmail)
}

func newSheetsService(ctx context.Context, cfg config) (*sheets.Service, string, error) {
	var creds []byte
	var err error
	if strings.TrimSpace(cfg.CredentialsJSON) != "" {
		raw := strings.TrimSpace(cfg.CredentialsJSON)
		if strings.HasPrefix(raw, "{") {
			creds = []byte(raw)
		} else {
			decoders := []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding}
			var decErr error
			for _, enc := range decoders {
				var d []byte
				d, decErr = enc.DecodeString(raw)
				if decErr == nil {
					creds = d
					break
				}
			}
			if creds == nil {
				return nil, "", fmt.Errorf("decode GOOGLE_CREDENTIALS_JSON as base64: %w", decErr)
			}
		}
	} else {
		creds, err = os.ReadFile(cfg.CredentialsFile)
		if err != nil {
			return nil, "", fmt.Errorf("read credentials file %q: %w", cfg.CredentialsFile, err)
		}
	}
	jwtCfg, err := google.JWTConfigFromJSON(creds, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, "", fmt.Errorf("parse service-account JSON: %w", err)
	}
	srv, err := sheets.NewService(ctx, option.WithHTTPClient(jwtCfg.Client(ctx)))
	if err != nil {
		return nil, "", fmt.Errorf("create sheets service: %w", err)
	}
	return srv, saEmail(creds), nil
}

// Store holds the cached people plus sheet metadata for writes.
type Store struct {
	mu           sync.RWMutex
	people       []Person
	sheetTitle   string
	recollidaCol string // A1-notation column letter, e.g. "F"
	maxRow       int    // last data row number (1-based)
}

func (s *Store) getAll() []Person {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Person, len(s.people))
	copy(out, s.people)
	return out
}

func (s *Store) find(row int) (Person, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.people {
		if p.Row == row {
			return p, true
		}
	}
	return Person{}, false
}

// colLetter converts a 0-based column index to A1-notation letters.
func colLetter(idx int) string {
	n := idx + 1
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

// findCol returns the first header index whose normalized text contains needle.
func findCol(headers []string, needle string) int {
	for i, h := range headers {
		if strings.Contains(Normalize(h), needle) {
			return i
		}
	}
	return -1
}

// findModalitatCol locates the sheet column holding the pinya/plaça choice.
// Tries "pinya", then "pla", then "vols fer". Returns -1 when absent.
func findModalitatCol(headers []string) int {
	for _, needle := range []string{"pinya", "pla", "vols fer"} {
		if i := findCol(headers, needle); i >= 0 {
			return i
		}
	}
	return -1
}

// isAcompanya reports whether the modalitat cell opts to accompany without pinya.
// Accent- and case-insensitive via Normalize.
func isAcompanya(v string) bool {
	return strings.Contains(Normalize(v), "acompanyar")
}

// parseCollected maps common truthy sheet values to true.
func parseCollected(v string) bool {
	switch Normalize(strings.TrimSpace(v)) {
	case "true", "1", "si", "x", "yes", "y", "ok", "fet", "recollida", "recollit", "si.", "fet.":
		return true
	}
	t := strings.TrimSpace(v)
	for _, mark := range []string{"✓", "✅", "✔", "☑"} {
		if strings.Contains(t, mark) {
			return true
		}
	}
	return false
}

func cellString(row []interface{}, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	switch v := row[idx].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "TRUE"
		}
		return "FALSE"
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// resolveSheetTitle returns cfg.SheetName or the first sheet's title.
func resolveSheetTitle(srv *sheets.Service, spreadsheetID, want string) (string, error) {
	if want != "" {
		return want, nil
	}
	meta, err := srv.Spreadsheets.Get(spreadsheetID).Fields("sheets.properties.title").Do()
	if err != nil {
		return "", err
	}
	if len(meta.Sheets) == 0 {
		return "", fmt.Errorf("spreadsheet has no sheets")
	}
	return meta.Sheets[0].Properties.Title, nil
}

// fetchAll reads the whole sheet and rebuilds the people cache.
func (s *Store) fetchAll(ctx context.Context, srv *sheets.Service, cfg config) error {
	title, err := resolveSheetTitle(srv, cfg.SpreadsheetID, cfg.SheetName)
	if err != nil {
		return fmt.Errorf("resolve sheet title: %w", err)
	}
	rng := fmt.Sprintf("%s!A:Z", title)
	resp, err := srv.Spreadsheets.Values.Get(cfg.SpreadsheetID, rng).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("read range %q: %w", rng, err)
	}
	if len(resp.Values) == 0 {
		s.mu.Lock()
		s.people = nil
		s.sheetTitle = title
		s.recollidaCol = ""
		s.maxRow = 1
		s.mu.Unlock()
		return nil
	}
	rawHeaders := make([]string, len(resp.Values[0]))
	for i, c := range resp.Values[0] {
		rawHeaders[i] = cellString([]interface{}{c}, 0)
	}
	iNom := findCol(rawHeaders, "nom")
	if iNom < 0 {
		// Fallback: first column if no "nom" header found.
		iNom = 0
	}
	iAlies := findCol(rawHeaders, "ali")
	iTalla := findCol(rawHeaders, "talla")
	iRecollida := findCol(rawHeaders, "recollida")
	iColla := findCol(rawHeaders, "colla")
	iModalitat := findModalitatCol(rawHeaders)

	var people []Person
	for r := 1; r < len(resp.Values); r++ {
		row := resp.Values[r]
		nom := cellString(row, iNom)
		alies := cellString(row, iAlies)
		if nom == "" && alies == "" {
			continue // skip blank rows
		}
		modalitat := cellString(row, iModalitat)
		people = append(people, Person{
			Row:       r + 1, // 1-based sheet row
			Nom:       nom,
			Alies:     alies,
			Colla:     cellString(row, iColla),
			Talla:     cellString(row, iTalla),
			Recollida: parseCollected(cellString(row, iRecollida)),
			Modalitat: modalitat,
			Acompanya: isAcompanya(modalitat),
		})
	}

	recCol := ""
	if iRecollida >= 0 {
		recCol = colLetter(iRecollida)
	}
	s.mu.Lock()
	s.people = people
	s.sheetTitle = title
	s.recollidaCol = recCol
	s.maxRow = len(resp.Values) // values are contiguous from row 1
	s.mu.Unlock()
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("bad config", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()
	srv, sa, err := newSheetsService(ctx, cfg)
	if err != nil {
		slog.Error("sheets auth failed", "err", err)
		os.Exit(1)
	}
	slog.Info("sheets client ready", "service_account", sa, "spreadsheet_id", cfg.SpreadsheetID, "sheet_name", cfg.SheetName)

	store := &Store{}
	refresh := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := store.fetchAll(ctx, srv, cfg); err != nil {
			slog.Error("refresh failed", "err", err, "hint", "share sheet with service_account as Editor")
			return
		}
		slog.Info("refreshed cache", "count", len(store.getAll()))
	}
	refresh()
	go func() {
		t := time.NewTicker(cfg.RefreshInterval)
		defer t.Stop()
		for range t.C {
			refresh()
		}
	}()

	mux := http.NewServeMux()

	// Embedded frontend.
	staticHandler := http.FileServer(http.FS(staticFS))
	mux.Handle("GET /static/", staticHandler)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "frontend missing", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/people", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.getAll())
	})

	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		writeJSON(w, http.StatusOK, SearchPeople(store.getAll(), q))
	})

	mux.HandleFunc("POST /api/toggle", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Row       int  `json:"row"`
			Recollida bool `json:"recollida"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		store.mu.RLock()
		title := store.sheetTitle
		recCol := store.recollidaCol
		maxRow := store.maxRow
		store.mu.RUnlock()

		if recCol == "" {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "recollida column not found in sheet headers"})
			return
		}
		if body.Row < 2 || (maxRow > 0 && body.Row > maxRow) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "row out of range"})
			return
		}
		val := "FALSE"
		if body.Recollida {
			val = "TRUE"
		}
		a1 := fmt.Sprintf("%s!%s%d", title, recCol, body.Row)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		_, err := srv.Spreadsheets.Values.Update(cfg.SpreadsheetID, a1,
			&sheets.ValueRange{Values: [][]interface{}{{val}}},
		).ValueInputOption("USER_ENTERED").Context(ctx).Do()
		if err != nil {
			slog.Error("toggle write failed", "range", a1, "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "sheets update failed"})
			return
		}
		// Update cache.
		store.mu.Lock()
		for i, p := range store.people {
			if p.Row == body.Row {
				store.people[i].Recollida = body.Recollida
				break
			}
		}
		store.mu.Unlock()
		updated, ok := store.find(body.Row)
		if !ok {
			// Row exists in sheet but was skipped from cache (blank name); return minimal object.
			writeJSON(w, http.StatusOK, Person{Row: body.Row, Recollida: body.Recollida})
			return
		}
		writeJSON(w, http.StatusOK, updated)
	})

	addr := ":" + cfg.Port
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
