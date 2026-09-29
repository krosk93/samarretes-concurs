package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	// DNI is the raw identity-document cell (sensitive, never serialized).
	DNI string `json:"-"`
	// EnAppsistencia is a per-request flag: DNI matches the Appsistència set.
	EnAppsistencia bool `json:"enAppsistencia"`
	// Modalitat is the raw text of the "Vols fer pinya o només entrar a plaça?" cell.
	Modalitat string `json:"modalitat"`
	// Acompanya is true when the cell opts for "acompanyar" (sense pinya).
	Acompanya bool `json:"acompanya"`
	// AliesFinal is the cached "Alies final" cell, or the computed
	// BuildFinalAlias value when the cell is empty.
	AliesFinal string `json:"aliesFinal"`
	// AltaApp is read-only for a future feature (no writes).
	AltaApp string `json:"altaApp"`
	// Raw contact/attribute cells for Appsistència creation (never serialized).
	Telefon       string `json:"-"`
	Email         string `json:"-"`
	DataNaixement string `json:"-"`
	AlcadaRaw    string `json:"-"`
	PosicioRaw    string `json:"-"`
	// AppsistenciaResult is transient per-toggle status, never cached.
	AppsistenciaResult string `json:"appsistencia,omitempty"`
}
type config struct {
	SpreadsheetID   string
	SheetName       string
	CredentialsFile string
	CredentialsJSON string
	Port            string
	RefreshInterval time.Duration
	Appsistencia    AppsistenciaConfig
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
	cfg.Appsistencia = loadAppsistenciaConfig()
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
	mu            sync.RWMutex
	people        []Person
	sheetTitle    string
	recollidaCol  string // A1-notation column letter, e.g. "F"
	tallaCol      string // A1-notation column letter, e.g. "E"; empty when header missing
	aliesFinalCol string // A1-notation column letter; empty when header missing
	altaAppCol    string // A1-notation column letter; empty when header missing
	maxRow        int    // last data row number (1-based)
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

// findFirstCol tries needles in order, returns first hit or -1.
func findFirstCol(headers []string, needles []string) int {
	for _, needle := range needles {
		if i := findCol(headers, needle); i >= 0 {
			return i
		}
	}
	return -1
}

// findPosicioCol locates the "Posició habitual a la pinya" column.
// Prefers "posicio"/"habitual" needles; "pinya" is only a fallback on a
// *different* column than modalitat (avoids colliding with the
// "Vols fer pinya...?" choice column).
func findPosicioCol(headers []string, modalitatIdx int) int {
	for _, needle := range []string{"posicio", "habitual"} {
		if i := findCol(headers, needle); i >= 0 {
			return i
		}
	}
	for i, h := range headers {
		if i == modalitatIdx {
			continue
		}
		if strings.Contains(Normalize(h), "pinya") {
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

// noShirtSize is the canonical stored value for "I don't want a t-shirt".
const noShirtSize = "Ja tinc una samarreta dels Xiquets del Serrallo"

// allowedSizes lists every value /api/size accepts (stored form).
var allowedSizes = []string{"S", "M", "L", "XL", "XXL", noShirtSize}

// normalizeSize maps a client-supplied size to its canonical stored value.
// Accepts any case and surrounding whitespace. Returns ok=false when not allowlisted.
func normalizeSize(v string) (string, bool) {
	n := Normalize(strings.TrimSpace(v))
	if n == "" {
		return "", false
	}
	switch n {
	case "s", "m", "l", "xl", "xxl":
		return strings.ToUpper(n), true
	}
	if n == Normalize(noShirtSize) {
		return noShirtSize, true
	}
	return "", false
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
		s.tallaCol = ""
		s.aliesFinalCol = ""
		s.altaAppCol = ""
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
	iAliesFinal := findCol(rawHeaders, "alies final")
	iAlies := -1
	for i, h := range rawHeaders {
		if i != iAliesFinal && strings.Contains(Normalize(h), "ali") {
			iAlies = i
			break
		}
	}
	iAltaApp := findCol(rawHeaders, "alta app")
	iTalla := findCol(rawHeaders, "talla")
	iRecollida := findCol(rawHeaders, "recollida")
	iColla := findCol(rawHeaders, "colla")
	iModalitat := findModalitatCol(rawHeaders)
	iDNI := -1
	for _, needle := range []string{"dni", "nif", "nie", "document", "passaport"} {
		if i := findCol(rawHeaders, needle); i >= 0 {
			iDNI = i
			break
		}
	}
	iTelefon := findFirstCol(rawHeaders, []string{"telefon", "mobil", "contacte"})
	iEmail := findFirstCol(rawHeaders, []string{"correu", "email", "mail"})
	iNaixement := findFirstCol(rawHeaders, []string{"naixement", "nacimiento", "birth", "data naix"})
	iAlcada := findFirstCol(rawHeaders, []string{"alcada", "espatlles", "altura", "height"})
	iPosicio := findPosicioCol(rawHeaders, iModalitat)

	var people []Person
	for r := 1; r < len(resp.Values); r++ {
		row := resp.Values[r]
		nom := cellString(row, iNom)
		alies := cellString(row, iAlies)
		if nom == "" && alies == "" {
			continue // skip blank rows
		}
		colla := cellString(row, iColla)
		modalitat := cellString(row, iModalitat)
		existent := cellString(row, iAliesFinal)
		final := existent
		if final == "" {
			final = BuildFinalAlias(alies, nom, colla)
		}
		people = append(people, Person{
			Row:           r + 1, // 1-based sheet row
			Nom:           nom,
			Alies:         alies,
			Colla:         colla,
			Talla:         cellString(row, iTalla),
			Recollida:     parseCollected(cellString(row, iRecollida)),
			Modalitat:     modalitat,
			Acompanya:     isAcompanya(modalitat),
			AliesFinal:    final,
			AltaApp:       cellString(row, iAltaApp),
			DNI:           cellString(row, iDNI),
			Telefon:       cellString(row, iTelefon),
			Email:         cellString(row, iEmail),
			DataNaixement: cellString(row, iNaixement),
			AlcadaRaw:     cellString(row, iAlcada),
			PosicioRaw:    cellString(row, iPosicio),
		})
	}

	recCol := ""
	if iRecollida >= 0 {
		recCol = colLetter(iRecollida)
	}
	sizeCol := ""
	if iTalla >= 0 {
		sizeCol = colLetter(iTalla)
	}
	finalCol := ""
	if iAliesFinal >= 0 {
		finalCol = colLetter(iAliesFinal)
	}
	altaCol := ""
	if iAltaApp >= 0 {
		altaCol = colLetter(iAltaApp)
	}
	s.mu.Lock()
	s.people = people
	s.sheetTitle = title
	s.recollidaCol = recCol
	s.tallaCol = sizeCol
	s.aliesFinalCol = finalCol
	s.altaAppCol = altaCol
	s.maxRow = len(resp.Values) // values are contiguous from row 1
	s.mu.Unlock()
	return nil
}

// appsFlagClient is the Appsistència client used by applyAppsistenciaFlag.
// Set once in main; per-request flagging keeps Store cache free of this state.
var appsFlagClient *AppsistenciaClient

// applyAppsistenciaFlag marks EnAppsistencia = HasDoc(DNI) per person
// (empty DNI → false). Always fresh, no extra Store state.
func applyAppsistenciaFlag(people []Person) []Person {
	for i := range people {
		people[i].EnAppsistencia = appsFlagClient.HasDoc(people[i].DNI)
	}
	return people
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// shouldFillAliesFinal is the lazy-fill decision for POST /api/toggle:
// write the computed final alias only when the sheet cell is empty and
// we computed something non-empty. Pure helper, no I/O (hermetic).
func shouldFillAliesFinal(existent, computed string) bool {
	return computed != "" && existent == ""
}

// sheetsTextValue força format text a Sheets amb prefix apòstrof (marcador
// que l'API no retorna en lectura): evita que USER_ENTERED intenti parsejar
// com a fórmula valors amb leading +,-,=,@ (ex "+3 (BdC)"). Idempotent.
func sheetsTextValue(s string) string {
	if strings.HasPrefix(s, "'") {
		return s
	}
	return "'" + s
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

	// Appsistència session (PHPSESSID) + castellers cache. Optional:
	// active only when user+password are set; failures never block.
	appsClient := NewAppsistenciaClient(cfg.Appsistencia)
	appsFlagClient = appsClient
	refreshAppsistencia := func() {
		if !appsClient.Configured() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if !appsClient.LoggedIn() || !appsClient.HasSession() {
			if err := appsClient.Login(ctx); err != nil {
				slog.Warn("appsistencia login failed", "err", err, "base_url", cfg.Appsistencia.BaseURL)
				return
			}
			slog.Info("appsistencia login ok", "base_url", cfg.Appsistencia.BaseURL, "has_session", appsClient.HasSession())
		}
		if n, err := appsClient.FetchCastellers(ctx); err != nil {
			// Session may have expired (fetch clears loggedIn then): one re-login + retry.
			if !appsClient.LoggedIn() {
				if lerr := appsClient.Login(ctx); lerr == nil {
					if n, err := appsClient.FetchCastellers(ctx); err == nil {
						slog.Info("appsistencia castellers loaded", "count", n)
						return
					}
				}
			}
			slog.Warn("appsistencia castellers fetch failed", "err", err)
			return
		} else {
			slog.Info("appsistencia castellers loaded", "count", n)
		}
	}
	if appsClient.Configured() {
		refreshAppsistencia()
	} else {
		slog.Info("appsistencia disabled", "hint", "set APPSISTENCIA_USER + APPSISTENCIA_PASSWORD to enable")
	}

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
			refreshAppsistencia() // same REFRESH_INTERVAL_SECONDS as Sheets
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

	mux.HandleFunc("GET /api/appsistencia/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured":  appsClient.Configured(),
			"loggedIn":    appsClient.LoggedIn(),
			"hasSession":  appsClient.HasSession(),
			"baseURL":     cfg.Appsistencia.BaseURL,
			"user":        cfg.Appsistencia.User,
			"castellers":  appsClient.CastellersCount(),
			"fetchedAt":   appsClient.CastellersAt().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("POST /api/appsistencia/login", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		if err := appsClient.Login(ctx); err != nil {
			slog.Warn("appsistencia login failed", "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		n, err := appsClient.FetchCastellers(ctx)
		if err != nil {
			slog.Warn("appsistencia castellers fetch failed", "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{
				"loggedIn":   true,
				"hasSession": appsClient.HasSession(),
				"error":      err.Error(),
			})
			return
		}
		slog.Info("appsistencia castellers loaded", "count", n)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"loggedIn":   true,
			"hasSession": appsClient.HasSession(),
			"castellers": n,
		})
	})

	mux.HandleFunc("GET /api/appsistencia/castellers", func(w http.ResponseWriter, r *http.Request) {
		raw := appsClient.CastellersRaw()
		if len(raw) == 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no cached castellers; POST /api/appsistencia/login first"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	})

	mux.HandleFunc("GET /api/people", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, applyAppsistenciaFlag(store.getAll()))
	})

	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		writeJSON(w, http.StatusOK, applyAppsistenciaFlag(SearchPeople(store.getAll(), q)))
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
		finalCol := store.aliesFinalCol
		altaCol := store.altaAppCol
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

		// Snapshot cached per alies/DNI/alta (la creació necessita raws).
		cached, hasCached := store.find(body.Row)

		// Lazy fill d'"Alies final": només quan cal omplir la cel·la buida
		// d'aquesta fila. La decisió requereix llegir la cel·la (la cache
		// guarda el fallback computat, no l'estat buit/ple del sheet).
		fill := ""
		fRange := ""
		if hasCached && finalCol != "" {
			if computed := BuildFinalAlias(cached.Alies, cached.Nom, cached.Colla); computed != "" {
				fRange = fmt.Sprintf("%s!%s%d", title, finalCol, body.Row)
				getResp, getErr := srv.Spreadsheets.Values.Get(cfg.SpreadsheetID, fRange).Context(ctx).Do()
				existent := ""
				if getErr != nil {
					// Read fallat: no bloquejar la marca, escriure només recollida.
					slog.Warn("toggle alies final read failed, writing recollida only", "range", fRange, "err", getErr)
					fRange = ""
				} else {
					if len(getResp.Values) > 0 {
						existent = cellString(getResp.Values[0], 0)
					}
					if shouldFillAliesFinal(existent, computed) {
						fill = computed
					} else {
						fRange = ""
					}
				}
			}
		}

		// Creació automàtica a Appsistència: només amb recollida=true.
		// Mai falla el toggle: skip silenciós o error loguejat.
		appsResult := ""
		needAlta := false
		if body.Recollida {
			appsResult = AppsResultSkipped
			effectiveAlias := ""
			if fill != "" {
				effectiveAlias = fill
			} else if hasCached {
				effectiveAlias = cached.AliesFinal
			}
			if appsFlagClient == nil || !appsFlagClient.Configured() {
				// skip silenciós: feature desactivada.
			} else if !hasCached {
				slog.Warn("appsistencia create skipped: row not in cache", "row", body.Row)
			} else if res, ok := classifyAppsCreate(true,
				appsFlagClient.HasDoc(cached.DNI),
				NormalizeDoc(cached.DNI) == "",
				parseCollected(cached.AltaApp)); !ok {
				appsResult = res
				if NormalizeDoc(cached.DNI) == "" && !appsFlagClient.HasDoc(cached.DNI) && !parseCollected(cached.AltaApp) {
					slog.Warn("appsistencia create skipped: empty DNI", "row", body.Row)
				}
			} else {
				payload := BuildCastellerPayload(cached, effectiveAlias)
				err := appsFlagClient.CreateCasteller(ctx, payload)
				if err != nil && errors.Is(err, errAppsistenciaAuth) {
					// Sessió caducada: re-login + 1 retry.
					if lerr := appsFlagClient.Login(ctx); lerr == nil {
						err = appsFlagClient.CreateCasteller(ctx, payload)
					} else {
						slog.Warn("appsistencia re-login failed", "row", body.Row, "err", lerr)
					}
				}
				if err != nil {
					slog.Warn("appsistencia create failed, recollida kept", "row", body.Row, "err", err)
					appsResult = AppsResultError
				} else {
					appsResult = AppsResultCreated
					needAlta = true
				}
			}
		}

		// UN sol BatchUpdate amb recollida + alies final (si cal) + alta app
		// (si creada). Quota 60 writes/min: mai 2 Updates separats.
		altaRange := ""
		if needAlta {
			if altaCol == "" {
				slog.Warn("appsistencia created but alta app column missing, sheet not updated", "row", body.Row)
				needAlta = false
			} else {
				altaRange = fmt.Sprintf("%s!%s%d", title, altaCol, body.Row)
			}
		}
		data := []*sheets.ValueRange{
			{Range: a1, Values: [][]interface{}{{val}}},
		}
		if fill != "" && fRange != "" {
			data = append(data, &sheets.ValueRange{Range: fRange, Values: [][]interface{}{{sheetsTextValue(fill)}}})
		}
		if altaRange != "" {
			data = append(data, &sheets.ValueRange{Range: altaRange, Values: [][]interface{}{{"TRUE"}}})
		}
		if len(data) == 1 {
			_, err := srv.Spreadsheets.Values.Update(cfg.SpreadsheetID, a1,
				&sheets.ValueRange{Values: [][]interface{}{{val}}},
			).ValueInputOption("USER_ENTERED").Context(ctx).Do()
			if err != nil {
				slog.Error("toggle write failed", "range", a1, "err", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "sheets update failed"})
				return
			}
		} else {
			_, err := srv.Spreadsheets.Values.BatchUpdate(cfg.SpreadsheetID,
				&sheets.BatchUpdateValuesRequest{
					ValueInputOption: "USER_ENTERED",
					Data:             data,
				},
			).Context(ctx).Do()
			if err != nil {
				ranges := a1
				if fRange != "" {
					ranges += "." + fRange
				}
				if altaRange != "" {
					ranges += "." + altaRange
				}
				slog.Error("toggle write failed", "range", ranges, "err", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": "sheets update failed"})
				return
			}
		}
		// Update cache.
		store.mu.Lock()
		for i, p := range store.people {
			if p.Row == body.Row {
				store.people[i].Recollida = body.Recollida
				if fill != "" {
					store.people[i].AliesFinal = fill
				}
				if needAlta {
					store.people[i].AltaApp = "TRUE"
				}
				break
			}
		}
		store.mu.Unlock()
		updated, ok := store.find(body.Row)
		if !ok {
			// Row exists in sheet but was skipped from cache (blank name); return minimal object.
			min := applyAppsistenciaFlag([]Person{{Row: body.Row, Recollida: body.Recollida}})[0]
			min.AppsistenciaResult = appsResult
			writeJSON(w, http.StatusOK, min)
			return
		}
		out := applyAppsistenciaFlag([]Person{updated})[0]
		out.AppsistenciaResult = appsResult
		writeJSON(w, http.StatusOK, out)
	})

	// POST /api/size changes a person's t-shirt size in the sheet.
	mux.HandleFunc("POST /api/size", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Row   int    `json:"row"`
			Talla string `json:"talla"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cos JSON no vàlid"})
			return
		}
		canonical, ok := normalizeSize(body.Talla)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":   "talla no permesa; tria una opció de la llista",
				"allowed": allowedSizes,
			})
			return
		}
		store.mu.RLock()
		title := store.sheetTitle
		sizeCol := store.tallaCol
		maxRow := store.maxRow
		store.mu.RUnlock()

		if sizeCol == "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "columna de talla no trobada a la fulla"})
			return
		}
		if body.Row < 2 || (maxRow > 0 && body.Row > maxRow) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fila fora de rang"})
			return
		}
		a1 := fmt.Sprintf("%s!%s%d", title, sizeCol, body.Row)
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		_, err := srv.Spreadsheets.Values.Update(cfg.SpreadsheetID, a1,
			&sheets.ValueRange{Values: [][]interface{}{{canonical}}},
		).ValueInputOption("USER_ENTERED").Context(ctx).Do()
		if err != nil {
			slog.Error("size write failed", "range", a1, "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "no s'ha pogut actualitzar la fulla"})
			return
		}
		// Update cache.
		store.mu.Lock()
		for i, p := range store.people {
			if p.Row == body.Row {
				store.people[i].Talla = canonical
				break
			}
		}
		store.mu.Unlock()
		updated, found := store.find(body.Row)
		if !found {
			// Row exists in sheet but was skipped from cache (blank name); return minimal object.
			writeJSON(w, http.StatusOK, applyAppsistenciaFlag([]Person{{Row: body.Row, Talla: canonical}})[0])
			return
		}
		writeJSON(w, http.StatusOK, applyAppsistenciaFlag([]Person{updated})[0])
	})

	addr := ":" + cfg.Port
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
