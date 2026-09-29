// Mock local d'Appsistència per a desenvolupament.
//
// Imita el flux que usa appsistencia.go:
//   GET base+/ (prime PHPSESSID) -> POST base+/ form login (302 = èxit)
//   GET base+/api/castellers?order=asc amb cookie PHPSESSID.
//
// Només stdlib. Estat en memòria carregat de castellers.json.
package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

//go:embed castellers.json
var fixtureEmbed []byte

const maxPostBody = 2 << 20 // 2MB

const loginHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Appsistencia mock - login</title></head>
<body>
<form class="form-signin" method="post" action="#">
  <h2>Login (mock)</h2>
  <input type="text" name="username" placeholder="username">
  <input type="password" name="password" id="password" placeholder="password">
  <input type="hidden" name="submitted" value="1">
  <input type="hidden" name="email_confirm" value="">
  <input type="hidden" name="phone" value="">
  <button type="submit">Entra</button>
</form>
</body></html>`

type Server struct {
	mu          sync.Mutex
	user        string
	password    string
	sessions    map[string]bool // PHPSESSID -> authenticated?
	castellers  []map[string]any
	lastPostRaw string
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback molt improbable: no falla mai a la pràctica.
		return fmt.Sprintf("fallback-%d", os.Getpid())
	}
	return hex.EncodeToString(b)
}

// loadFixture accepta array nu [...] o objecte {"rows":[...]}.
func loadFixture(data []byte) ([]map[string]any, error) {
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil && arr != nil {
		return arr, nil
	}
	var obj struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj.Rows == nil {
		return nil, fmt.Errorf("fixture sense rows")
	}
	return obj.Rows, nil
}

func (s *Server) reloadFixture() error {
	data := fixtureEmbed
	// Si hi ha castellers.json al costat del binari/directori de treball, té prioritat.
	if b, err := os.ReadFile("castellers.json"); err == nil && len(bytes.TrimSpace(b)) > 0 {
		data = b
	}
	rows, err := loadFixture(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.castellers = rows
	s.mu.Unlock()
	return nil
}

func (s *Server) validSession(r *http.Request) bool {
	c, err := r.Cookie("PHPSESSID")
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[c.Value]
}

func (s *Server) ensureAnonCookie(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("PHPSESSID"); err == nil && c.Value != "" {
		return
	}
	id := newSessionID()
	s.mu.Lock()
	s.sessions[id] = false
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: id, Path: "/"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// statusRecorder registra el codi per al log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rw *statusRecorder) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (s *Server) withLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next(rw, r)
		log.Printf("%s %s -> %d", r.Method, r.URL.RequestURI(), rw.status)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.ensureAnonCookie(w, r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, loginHTML)
	case http.MethodPost:
		_ = r.ParseForm()
		user := r.FormValue("username")
		pass := r.FormValue("password")
		if user == s.user && pass == s.password {
			id := newSessionID()
			s.mu.Lock()
			s.sessions[id] = true
			s.mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: id, Path: "/"})
			w.Header().Set("Location", "/go/panel")
			w.WriteHeader(http.StatusFound)
			return
		}
		// Credencials KO: 200 amb mateix HTML login.
		s.ensureAnonCookie(w, r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, loginHTML)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	if !s.validSession(r) {
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "<html><body><h1>Mock panel</h1><p>Sessió OK.</p></body></html>")
}

func (s *Server) handleCastellers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.validSession(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no session"})
			return
		}
		s.mu.Lock()
		rows := make([]map[string]any, len(s.castellers))
		copy(rows, s.castellers)
		s.mu.Unlock()
		if rows == nil {
			rows = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"total": len(rows), "rows": rows})
	case http.MethodPost:
		s.handleCastellersPost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// parsePostBody intenta json.Unmarshal directe a map; si falla, fa ParseForm
// i prova cada valor/clau com JSON (cobreix JSON cru amb
// Content-Type: application/x-www-form-urlencoded).
func parsePostBody(raw []byte) (map[string]any, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(trimmed, &m); err == nil && m != nil {
		return m, true
	}
	// Pot ser form-urlencoded que encapsula JSON en un camp o clau.
	if vals, err := url.ParseQuery(string(raw)); err == nil {
		candidates := []string{}
		for k, vs := range vals {
			candidates = append(candidates, k)
			candidates = append(candidates, vs...)
		}
		for _, c := range candidates {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			var m2 map[string]any
			if err := json.Unmarshal([]byte(c), &m2); err == nil && m2 != nil {
				return m2, true
			}
		}
	}
	return nil, false
}

func maxID(rows []map[string]any) int {
	max := 0
	for _, row := range rows {
		switch v := row["id"].(type) {
		case float64:
			if int(v) > max {
				max = int(v)
			}
		case int:
			if v > max {
				max = v
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && int(n) > max {
				max = int(n)
			}
		}
	}
	return max
}

func (s *Server) handleCastellersPost(w http.ResponseWriter, r *http.Request) {
	if !s.validSession(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no session"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxPostBody+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read body"})
		return
	}
	if len(raw) > maxPostBody {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body too large"})
		return
	}
	m, ok := parsePostBody(raw)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	s.mu.Lock()
	m["id"] = maxID(s.castellers) + 1
	s.castellers = append(s.castellers, m)
	s.lastPostRaw = string(raw)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active := 0
	for _, authed := range s.sessions {
		if authed {
			active++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       s.user,
		"sessions":   active,
		"total":      len(s.castellers),
		"castellers": len(s.castellers),
		"last_post":  s.lastPostRaw,
	})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.reloadFixture(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.lastPostRaw = ""
	n := len(s.castellers)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": n})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func main() {
	port := getenv("PORT", "8081")
	s := &Server{
		user:     getenv("MOCK_USER", "test"),
		password: getenv("MOCK_PASSWORD", "test"),
		sessions: map[string]bool{},
	}
	if err := s.reloadFixture(); err != nil {
		log.Fatalf("carrega fixture: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.withLog(s.handleRoot))
	mux.HandleFunc("/go/panel", s.withLog(s.handlePanel))
	mux.HandleFunc("/api/castellers", s.withLog(s.handleCastellers))
	mux.HandleFunc("/__mock/info", s.withLog(s.handleInfo))
	mux.HandleFunc("/__mock/reset", s.withLog(s.handleReset))
	mux.HandleFunc("/health", s.withLog(s.handleHealth))

	addr := ":" + strings.TrimPrefix(port, ":")
	log.Printf("mock-appsistencia escoltant a %s (user=%q, castellers=%d)", addr, s.user, len(s.castellers))
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
