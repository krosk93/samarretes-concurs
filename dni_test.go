package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNormalizeDoc(t *testing.T) {
	cases := []struct{ in, want string }{
		{"O0123456 PASSAPORT", "o0123456"},
		{"(Passaport Andorra) 0011223", "0011223"},
		{"12345678-N", "12345678n"},
		{"12345678W", "12345678w"},
		{"", ""},
		{"   ", ""},
		{"PASSAPORT", ""},
		{"DNI 12345678Z", "12345678z"},
		{"NIF: 12345678-Z", "12345678z"},
		{"NIE X-1234567-A", "x1234567a"},
		{"Document d'identitat 99999999R", "d99999999r"},
		{"Documento de identidad 11111111H", "de11111111h"},
		{"Passport Andorra 0011223", "0011223"},
		{"Pasaporte  ABC-123 ", "abc123"},
		{"(DNI) 12345678 N", "12345678n"},
	}
	for _, c := range cases {
		if got := NormalizeDoc(c.in); got != c.want {
			t.Errorf("NormalizeDoc(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAppsistenciaDocSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":2,"rows":[{"dni":"12345678-N"},{"dni":"(Passaport Andorra) 0011223"}]}`))
	}))
	defer srv.Close()

	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "u", Password: "p"})
	u, _ := url.Parse(srv.URL)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "PHPSESSID", Value: "sess-logged", Path: "/"}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.FetchCastellers(ctx); err != nil {
		t.Fatalf("FetchCastellers: %v", err)
	}

	for _, in := range []string{"12345678N", "12345678-n", "0011223", "PASSAPORT andorra 0011223"} {
		if !c.HasDoc(in) {
			t.Errorf("HasDoc(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"99999999Z", ""} {
		if c.HasDoc(in) {
			t.Errorf("HasDoc(%q) = true, want false", in)
		}
	}
}
