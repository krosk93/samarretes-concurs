package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const fakeLoginForm = `<form class="form-signin" method="POST"><input id="password" name="password"></form>`

// fakeAppsistencia mimics the real flow: GET sets PHPSESSID, POST checks
// credentials and 302-redirects on success, re-renders form on failure.
func fakeAppsistencia(t *testing.T, wantUser, wantPass string, seenForm *urlValuesCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "sess123", Path: "/"})
			_, _ = w.Write([]byte(fakeLoginForm))
			return
		}
		_ = r.ParseForm()
		seenForm.Set(r.Form)
		if r.Form.Get("username") == wantUser && r.Form.Get("password") == wantPass &&
			r.Form.Get("submitted") == "1" {
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "sess-logged", Path: "/"})
			w.Header().Set("Location", "/go/?q=actuacio/3/")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(fakeLoginForm))
	}))
}

type urlValuesCapture struct {
	v map[string][]string
}

func (c *urlValuesCapture) Set(v map[string][]string) { c.v = v }

func TestAppsistenciaLoginSuccess(t *testing.T) {
	var seen urlValuesCapture
	srv := fakeAppsistencia(t, "joan", "secret", &seen)
	defer srv.Close()

	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "joan", Password: "secret"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !c.LoggedIn() {
		t.Fatal("LoggedIn = false, want true")
	}
	if got := c.PHPSESSID(); got != "sess-logged" {
		t.Fatalf("PHPSESSID = %q, want sess-logged", got)
	}
	if !c.HasSession() {
		t.Fatal("HasSession = false, want true")
	}
	// Honeypot fields must be posted (empty) + submitted=1.
	if seen.v["submitted"][0] != "1" {
		t.Fatalf("submitted = %v, want 1", seen.v["submitted"])
	}
	if seen.v["email_confirm"][0] != "" || seen.v["phone"][0] != "" {
		t.Fatalf("honeypots not empty: %v", seen.v)
	}
}

func TestAppsistenciaLoginRejected(t *testing.T) {
	var seen urlValuesCapture
	srv := fakeAppsistencia(t, "joan", "secret", &seen)
	defer srv.Close()

	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "joan", Password: "wrong"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Login(ctx); err == nil {
		t.Fatal("Login with wrong password: want error, got nil")
	}
	if c.LoggedIn() {
		t.Fatal("LoggedIn = true after rejection, want false")
	}
}

func TestAppsistenciaLoginMissingCreds(t *testing.T) {
	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: "https://x.example", User: "", Password: ""})
	if c.Configured() {
		t.Fatal("Configured = true with empty creds, want false")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Login(ctx); err == nil {
		t.Fatal("Login without creds: want error, got nil")
	}
}

func TestLooksLikeLoginPage(t *testing.T) {
	if !looksLikeLoginPage(fakeLoginForm) {
		t.Fatal("login form not detected")
	}
	if looksLikeLoginPage("<html><body>APP dashboard</body></html>") {
		t.Fatal("dashboard misdetected as login")
	}
	if !strings.Contains(defaultAppsistenciaURL, "appsistencia.cat") {
		t.Fatal("default URL changed unexpectedly")
	}
}

func TestCountCastellersBody(t *testing.T) {
	n, err := countCastellersBody([]byte(`{"total":839,"totalNotFiltered":839,"rows":[{"id":"1"},{"id":"2"}]}`))
	if err != nil || n != 2 {
		t.Fatalf("object rows: got %d,%v want 2,nil", n, err)
	}
	n, err = countCastellersBody([]byte(`[{"id":"1"},{"id":"2"},{"id":"3"}]`))
	if err != nil || n != 3 {
		t.Fatalf("bare array: got %d,%v want 3,nil", n, err)
	}
	if _, err := countCastellersBody([]byte(`<html>login</html>`)); err == nil {
		t.Fatal("invalid payload: want error, got nil")
	}
}

func TestFetchCastellersSendsSession(t *testing.T) {
	var gotCookie, gotOrder string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/castellers" {
			http.NotFound(w, r)
			return
		}
		gotCookie = r.Header.Get("Cookie")
		gotOrder = r.URL.Query().Get("order")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":2,"rows":[{"id":"1"},{"id":"2"}]}`))
	}))
	defer srv.Close()

	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "u", Password: "p"})
	u, _ := url.Parse(srv.URL)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "PHPSESSID", Value: "sess-logged", Path: "/"}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n, err := c.FetchCastellers(ctx)
	if err != nil {
		t.Fatalf("FetchCastellers: %v", err)
	}
	if n != 2 || c.CastellersCount() != 2 {
		t.Fatalf("count = %d cached %d, want 2", n, c.CastellersCount())
	}
	if !strings.Contains(gotCookie, "PHPSESSID=sess-logged") {
		t.Fatalf("Cookie header = %q, want PHPSESSID", gotCookie)
	}
	if gotOrder != "asc" {
		t.Fatalf("order = %q, want asc", gotOrder)
	}
	if len(c.CastellersRaw()) == 0 || c.CastellersAt().IsZero() {
		t.Fatal("cache empty after fetch")
	}
}

func TestFetchCastellersNoSession(t *testing.T) {
	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: "https://x.example", User: "u", Password: "p"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.FetchCastellers(ctx); err == nil {
		t.Fatal("fetch without session: want error, got nil")
	}
}
