package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTelefon(t *testing.T) {
	cases := []struct{ in, want string }{
		{"600 111 222", "600111222"},
		{"600-111-222", "600111222"},
		{"600.111.222", "600111222"},
		{"(600) 111 222", "600111222"},
		{"+34 600 111 222", "+34600111222"},
		{"", ""},
		{"123", ""},       // <9 dígits
		{"60011122", ""},  // 8 dígits
		{"600111222", "600111222"},
		{"  +34-600-111-222  ", "+34600111222"},
	}
	for _, tc := range cases {
		if got := NormalizeTelefon(tc.in); got != tc.want {
			t.Errorf("NormalizeTelefon(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Joan@Exemple.CAT ", "joan@exemple.cat"},
		{"a@b.cd", "a@b.cd"},
		{"", ""},
		{"sense-arroba", ""},
		{"a@b", ""},
		{"a@.cd", ""},
		{"@b.cd", ""},
		{"a b@c.cd", ""},
		{"a@@b.cd", ""},
	}
	for _, tc := range cases {
		if got := NormalizeEmail(tc.in); got != tc.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeDataNaixement(t *testing.T) {
	cases := []struct{ in, want string }{
		{"15/01/1990", "15/01/1990"},
		{"15-01-1990", "15/01/1990"},
		{"1990-01-15", "15/01/1990"},
		{"15.01.1990", "15/01/1990"},
		{"1990/01/15", "15/01/1990"},
		{" 15/01/1990 ", "15/01/1990"},
		{"", ""},
		{"1990-13-01", ""},
		{"32/01/1990", ""},
		{"30/02/2020", ""},
		{"29/02/2020", "29/02/2020"},
		{"29/02/2019", ""},
		{"15/01/90", ""},
		{"notadate", ""},
		{"15/01/1800", ""},
	}
	for _, tc := range cases {
		if got := NormalizeDataNaixement(tc.in); got != tc.want {
			t.Errorf("NormalizeDataNaixement(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizePosicioPassthrough(t *testing.T) {
	// Documenta decisió: sense mapping conegut, passa net.
	cases := []struct{ in, want string }{
		{"baix", "baix"},
		{"  Contrafort  ", "Contrafort"},
		{"6", "6"},
		{"", ""},
		{"lateral  dret", "lateral dret"},
	}
	for _, tc := range cases {
		if got := NormalizePosicio(tc.in); got != tc.want {
			t.Errorf("NormalizePosicio(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildCastellerPayload(t *testing.T) {
	p := Person{
		Nom:           "Joan Anton Rius Ferrer",
		Alies:         "",
		Colla:         "Xiquets del Serrallo",
		DNI:           " 12345678A ",
		Telefon:       "600 111 222",
		Email:         "JOAN@Exemple.CAT",
		DataNaixement: "15/01/1990",
		AlcadaRaw:     "150",
		PosicioRaw:    "baix",
	}
	got := BuildCastellerPayload(p, "")
	np := SplitNom(p.Nom)
	if got["nom"] != np.Nom || got["cognom1"] != np.Cognom1 || got["cognom2"] != np.Cognom2 {
		t.Fatalf("split mismatch: %+v vs SplitNom=%+v", got, np)
	}
	if got["mote"] != BuildFinalAlias(p.Alies, p.Nom, p.Colla) {
		t.Fatalf("mote = %q, want computed final", got["mote"])
	}
	if got["telefon"] != "600111222" || got["email"] != "joan@exemple.cat" {
		t.Fatalf("contacte: %+v", got)
	}
	if got["data_naixement"] != "15/01/1990" {
		t.Fatalf("data_naixement = %q", got["data_naixement"])
	}
	if got["alcada_espatlles"] != "150" {
		t.Fatalf("alcada = %q", got["alcada_espatlles"])
	}
	if got["dni"] != "12345678A" {
		t.Fatalf("dni = %q", got["dni"])
	}
	// Defaults exactes.
	for k, want := range map[string]string{
		"posicio": "6", "soci": "NO", "revisat": "0", "estat_acollida": "no_aplica",
		"llistes": "0", "tecnica": "2", "propi": "0", "pot_votar": "0",
		"lesionat": "0", "ultima_renovacio": "",
		"direccio": "", "poblacio": "", "codi_postal": "",
		"data_alta": "", "instant_camisa": "",
	} {
		if got[k] != want {
			t.Errorf("payload[%q] = %q, want %q", k, got[k], want)
		}
	}
	// mote explícit preval.
	got2 := BuildCastellerPayload(p, "Guiri (XdS)")
	if got2["mote"] != "Guiri (XdS)" {
		t.Fatalf("mote explicit = %q", got2["mote"])
	}
	// Alçada invàlida -> "".
	p.AlcadaRaw = "sense dades"
	if got3 := BuildCastellerPayload(p, "X"); got3["alcada_espatlles"] != "" {
		t.Fatalf("alcada invalida = %q, want empty", got3["alcada_espatlles"])
	}
}

func TestClassifyAppsCreate(t *testing.T) {
	if res, ok := classifyAppsCreate(false, false, false, false); ok || res != AppsResultSkipped {
		t.Fatalf("disabled = %q,%v want skipped,false", res, ok)
	}
	if res, ok := classifyAppsCreate(true, true, false, false); ok || res != AppsResultExists {
		t.Fatalf("exists = %q,%v", res, ok)
	}
	if res, ok := classifyAppsCreate(true, false, true, false); ok || res != AppsResultSkipped {
		t.Fatalf("empty dni = %q,%v want skipped,false", res, ok)
	}
	if res, ok := classifyAppsCreate(true, false, false, true); ok || res != AppsResultSkipped {
		t.Fatalf("alta true = %q,%v", res, ok)
	}
	if res, ok := classifyAppsCreate(true, false, false, false); !ok || res != "" {
		t.Fatalf("attempt = %q,%v want '',true", res, ok)
	}
}

func TestCreateCastellerHeadersAndBody(t *testing.T) {
	var gotCT, gotAccept string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/castellers" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		gotCT = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			http.Error(w, "not object", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id": 9}`))
	}))
	defer srv.Close()

	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "u", Password: "p"})
	u, _ := url.Parse(srv.URL)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "PHPSESSID", Value: "sess", Path: "/"}})

	payload := BuildCastellerPayload(Person{Nom: "A B C", DNI: "1A"}, "M (XdS)")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.CreateCasteller(ctx, payload); err != nil {
		t.Fatalf("CreateCasteller: %v", err)
	}
	if gotCT != "application/x-www-form-urlencoded; charset=UTF-8" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	if gotAccept != "*/*" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotBody["mote"] != "M (XdS)" || gotBody["soci"] != "NO" || gotBody["tecnica"] != "2" || gotBody["posicio"] != "6" || gotBody["revisat"] != "0" || gotBody["propi"] != "0" || gotBody["pot_votar"] != "0" {
		t.Fatalf("body = %+v", gotBody)
	}
	// DNI afegit al set sense refetch.
	if !c.HasDoc("1A") {
		t.Fatal("HasDoc after create = false, want true")
	}
}

func TestCreateCastellerAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no session", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: srv.URL, User: "u", Password: "p"})
	u, _ := url.Parse(srv.URL)
	c.http.Jar.SetCookies(u, []*http.Cookie{{Name: "PHPSESSID", Value: "sess", Path: "/"}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.CreateCasteller(ctx, map[string]string{"nom": "A"})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want status 401", err)
	}
}

func TestAddDocEmptyNoMatch(t *testing.T) {
	c := NewAppsistenciaClient(AppsistenciaConfig{BaseURL: "https://x.example", User: "u", Password: "p"})
	c.AddDoc("")
	if c.HasDoc("") {
		t.Fatal("HasDoc('') = true, want false")
	}
	c.AddDoc(" 12345678A ")
	if !c.HasDoc("12345678a") {
		t.Fatal("HasDoc after AddDoc = false")
	}
}
