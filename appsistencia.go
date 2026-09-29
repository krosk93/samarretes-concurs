package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

const defaultAppsistenciaURL = "https://xiquetsdelserrallo.appsistencia.cat"

// castellersAPIPath is the only API path used so far (ordered list).
const castellersAPIPath = "/api/castellers?order=asc"

// appsistenciaUA mimics a real browser; the site may gate non-browser agents.
const appsistenciaUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6.2 Safari/605.1.15"

// AppsistenciaConfig holds env-driven credentials. Empty user/password = disabled.
type AppsistenciaConfig struct {
	BaseURL  string
	User     string
	Password string
}

func (c AppsistenciaConfig) Configured() bool {
	return strings.TrimSpace(c.User) != "" && strings.TrimSpace(c.Password) != ""
}

func loadAppsistenciaConfig() AppsistenciaConfig {
	base := strings.TrimSpace(os.Getenv("APPSISTENCIA_URL"))
	if base == "" {
		base = defaultAppsistenciaURL
	}
	base = strings.TrimRight(base, "/")
	user := strings.TrimSpace(os.Getenv("APPSISTENCIA_USER"))
	if user == "" {
		user = strings.TrimSpace(os.Getenv("APPSISTENCIA_USERNAME"))
	}
	return AppsistenciaConfig{
		BaseURL:  base,
		User:     user,
		Password: os.Getenv("APPSISTENCIA_PASSWORD"),
	}
}

// AppsistenciaClient keeps an authenticated session against Appsistència.
// Auth is a form POST on the base URL; session state lives in the
// PHPSESSID cookie stored in the jar. Reuse this client (or its helpers)
// for posterior API calls so the cookie is sent automatically.
type AppsistenciaClient struct {
	mu       sync.RWMutex
	baseURL  string
	user     string
	password string

	http     *http.Client
	loggedIn bool
	lastOK   time.Time

	// Cached castellers list (raw API payload + count), filled after login.
	castMu          sync.RWMutex
	castellersRaw   []byte
	castellersCount int
	castellersAt    time.Time

	// Normalized DNI set for Sheets↔Appsistència matching. Never exposed via API.
	docMu  sync.RWMutex
	docSet map[string]bool
}

func NewAppsistenciaClient(cfg AppsistenciaConfig) *AppsistenciaClient {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	// No redirect following on login POST: success is usually 302 to /go/...,
	// and we must not crawl the app, only keep the session cookie.
	httpClient := &http.Client{
		Timeout: 20 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &AppsistenciaClient{
		baseURL:  strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		user:     strings.TrimSpace(cfg.User),
		password: cfg.Password,
		http:     httpClient,
	}
}

func (a *AppsistenciaClient) BaseURL() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.baseURL
}

// Configured reports whether credentials exist (login can be attempted).
func (a *AppsistenciaClient) Configured() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.user != "" && a.password != ""
}

// LoggedIn reports last known login state (not a live probe).
func (a *AppsistenciaClient) LoggedIn() bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.loggedIn
}

// PHPSESSID returns the current session cookie value ("" when absent).
// Needed a posteriori for direct API calls sharing this session.
func (a *AppsistenciaClient) PHPSESSID() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	base, jar := a.baseURL, a.http.Jar
	a.mu.RUnlock()
	if base == "" || jar == nil {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, c := range jar.Cookies(u) {
		if strings.EqualFold(c.Name, "PHPSESSID") {
			return c.Value
		}
	}
	return ""
}

// HasSession is true when a PHPSESSID cookie exists in the jar.
func (a *AppsistenciaClient) HasSession() bool { return a.PHPSESSID() != "" }

// Do sends req with the session jar (caller builds absolute URL under base).
func (a *AppsistenciaClient) Do(req *http.Request) (*http.Response, error) {
	if a == nil || a.http == nil {
		return nil, fmt.Errorf("appsistencia: client not initialized")
	}
	return a.http.Do(req)
}

// Login performs GET (prime PHPSESSID) + form POST against the base URL only.
// Form fields match the observed login page: username, password,
// submitted=1 plus empty honeypots email_confirm/phone.
func (a *AppsistenciaClient) Login(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("appsistencia: client not initialized")
	}
	a.mu.RLock()
	base, user, pass := a.baseURL, a.user, a.password
	a.mu.RUnlock()
	if user == "" || pass == "" {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: APPSISTENCIA_USER/APPSISTENCIA_PASSWORD not set")
	}
	if base == "" {
		base = defaultAppsistenciaURL
	}
	if _, err := url.Parse(base); err != nil {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: bad base URL: %w", err)
	}

	// 1. GET base to obtain initial PHPSESSID.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: build GET: %w", err)
	}
	getReq.Header.Set("User-Agent", appsistenciaUA)
	getResp, err := a.http.Do(getReq)
	if err != nil {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: GET login page: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(getResp.Body, 1<<20))
	_ = getResp.Body.Close()

	// 2. POST credentials to the same base URL (form action="#").
	form := url.Values{}
	form.Set("username", user)
	form.Set("password", pass)
	form.Set("submitted", "1")
	form.Set("email_confirm", "")
	form.Set("phone", "")
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/", strings.NewReader(form.Encode()))
	if err != nil {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: build POST: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", appsistenciaUA)
	postReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	postReq.Header.Set("Referer", base+"/")
	postResp, err := a.http.Do(postReq)
	if err != nil {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: POST login: %w", err)
	}
	defer postResp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(postResp.Body, 2<<20))
	loc := postResp.Header.Get("Location")

	// Redirect after POST = logged in (session cookie already in jar).
	if postResp.StatusCode == http.StatusFound ||
		postResp.StatusCode == http.StatusSeeOther ||
		postResp.StatusCode == http.StatusTemporaryRedirect ||
		postResp.StatusCode == http.StatusPermanentRedirect {
		if strings.TrimSpace(loc) != "" {
			a.setLoggedIn(true)
			return nil
		}
	}
	// 200 with login form still present = rejected credentials.
	if looksLikeLoginPage(string(body)) {
		a.setLoggedIn(false)
		return fmt.Errorf("appsistencia: login rejected (check user/password)")
	}
	// 200 without login form = likely logged in (some setups render app shell).
	if postResp.StatusCode == http.StatusOK {
		a.setLoggedIn(true)
		return nil
	}
	a.setLoggedIn(false)
	return fmt.Errorf("appsistencia: unexpected login status %d", postResp.StatusCode)
}

// FetchCastellers GETs the ordered castellers list with the session jar
// (PHPSESSID cookie sent automatically) and caches the raw payload.
// Returns the number of castellers. Requires a prior Login.
func (a *AppsistenciaClient) FetchCastellers(ctx context.Context) (int, error) {
	if a == nil {
		return 0, fmt.Errorf("appsistencia: client not initialized")
	}
	a.mu.RLock()
	base := a.baseURL
	a.mu.RUnlock()
	if base == "" {
		base = defaultAppsistenciaURL
	}
	if !a.HasSession() {
		return 0, fmt.Errorf("appsistencia: no session, login first")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+castellersAPIPath, nil)
	if err != nil {
		return 0, fmt.Errorf("appsistencia: build castellers request: %w", err)
	}
	req.Header.Set("User-Agent", appsistenciaUA)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", base+"/")
	resp, err := a.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("appsistencia: GET castellers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		a.setLoggedIn(false)
		return 0, fmt.Errorf("appsistencia: castellers rejected (status %d, session invalid?)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusSeeOther {
		a.setLoggedIn(false)
		return 0, fmt.Errorf("appsistencia: castellers redirected to %q (session expired?)", resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("appsistencia: unexpected castellers status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	if err != nil {
		return 0, fmt.Errorf("appsistencia: read castellers: %w", err)
	}
	n, err := countCastellersBody(body)
	if err != nil {
		return 0, err
	}
	a.castMu.Lock()
	a.castellersRaw = body
	a.castellersCount = n
	a.castellersAt = time.Now()
	a.castMu.Unlock()
	a.docMu.Lock()
	a.docSet = buildDocSet(body)
	a.docMu.Unlock()
	return n, nil
}

// buildDocSet extracts rows[].dni values (object {"total","rows":[...]} or
// bare [...] array) into a normalized set via NormalizeDoc. Empty docs skipped.
func buildDocSet(body []byte) map[string]bool {
	set := map[string]bool{}
	add := func(v interface{}) {
		var raw string
		switch t := v.(type) {
		case string:
			raw = t
		case float64:
			if t == float64(int64(t)) {
				raw = fmt.Sprintf("%d", int64(t))
			} else {
				raw = fmt.Sprint(t)
			}
		case nil:
			return
		default:
			raw = fmt.Sprint(t)
		}
		if n := NormalizeDoc(raw); n != "" {
			set[n] = true
		}
	}
	var obj struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := json.Unmarshal(body, &obj); err == nil && obj.Rows != nil {
		for _, row := range obj.Rows {
			if v, ok := row["dni"]; ok {
				add(v)
			}
		}
		return set
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(body, &arr); err == nil {
		for _, row := range arr {
			if v, ok := row["dni"]; ok {
				add(v)
			}
		}
		return set
	}
	return set
}

// HasDoc reports whether doc (normalized via NormalizeDoc) is in the cached
// Appsistència set. Empty docs never match.
func (a *AppsistenciaClient) HasDoc(doc string) bool {
	if a == nil {
		return false
	}
	n := NormalizeDoc(doc)
	if n == "" {
		return false
	}
	a.docMu.RLock()
	defer a.docMu.RUnlock()
	return a.docSet[n]
}

// countCastellersBody counts entries in the castellers payload:
// either {"total":N,"rows":[...]} or a bare [...] array.
func countCastellersBody(body []byte) (int, error) {
	var obj struct {
		Total *int              `json:"total"`
		Rows  []json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(body, &obj); err == nil && (obj.Rows != nil || obj.Total != nil) {
		if obj.Rows != nil {
			return len(obj.Rows), nil
		}
		return *obj.Total, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(body, &arr); err == nil {
		return len(arr), nil
	}
	return 0, fmt.Errorf("appsistencia: unexpected castellers payload")
}

// CastellersCount returns the cached number of castellers (0 when not fetched).
func (a *AppsistenciaClient) CastellersCount() int {
	if a == nil {
		return 0
	}
	a.castMu.RLock()
	defer a.castMu.RUnlock()
	return a.castellersCount
}

// CastellersAt returns when the list was last fetched (zero when never).
func (a *AppsistenciaClient) CastellersAt() time.Time {
	if a == nil {
		return time.Time{}
	}
	a.castMu.RLock()
	defer a.castMu.RUnlock()
	return a.castellersAt
}

// CastellersRaw returns a copy of the cached payload (nil when not fetched).
func (a *AppsistenciaClient) CastellersRaw() []byte {
	if a == nil {
		return nil
	}
	a.castMu.RLock()
	defer a.castMu.RUnlock()
	out := make([]byte, len(a.castellersRaw))
	copy(out, a.castellersRaw)
	return out
}

func (a *AppsistenciaClient) setLoggedIn(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loggedIn = v
	if v {
		a.lastOK = time.Now()
	}
}

// looksLikeLoginPage detects the login form in a response body (hermetic, testable).
func looksLikeLoginPage(body string) bool {
	b := strings.ToLower(body)
	return strings.Contains(b, `name="password"`) ||
		strings.Contains(b, `id="password"`) ||
		strings.Contains(b, `class="form-signin"`)
}
