package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Resultats de POST /api/toggle al camp `appsistencia` (omitempty).
const (
	AppsResultSkipped = "skipped"
	AppsResultExists  = "exists"
	AppsResultCreated = "created"
	AppsResultError   = "error"
)

// errAppsistenciaAuth marca fallades d'autenticació (401/403/redirect)
// perquè el caller faci re-login + 1 retry.
var errAppsistenciaAuth = errors.New("appsistencia: auth")

// NormalizeTelefon conserva només dígits, preservant un '+' inicial.
// Elimina espais, guions, punts i parèntesis. Retorna "" quan hi ha
// menys de 9 dígits (número incomplet, no enviable).
func NormalizeTelefon(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	plus := strings.HasPrefix(t, "+")
	var b strings.Builder
	for _, r := range t {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 9 {
		return ""
	}
	if plus {
		return "+" + d
	}
	return d
}

// NormalizeEmail fa trim + lower i validació mínima (@ amb punt al domini).
// Retorna "" quan és buit o invàlid.
func NormalizeEmail(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	if strings.ContainsAny(t, " \t\n\r") {
		return ""
	}
	t = strings.ToLower(t)
	parts := strings.Split(t, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	dom := parts[1]
	if !strings.Contains(dom, ".") {
		return ""
	}
	if strings.HasPrefix(dom, ".") || strings.HasSuffix(dom, ".") {
		return ""
	}
	if strings.HasPrefix(parts[0], ".") || strings.HasSuffix(parts[0], ".") {
		return ""
	}
	return t
}

// NormalizeDataNaixement accepta DD/MM/YYYY, DD-MM-YYYY, YYYY-MM-DD i
// DD.MM.YYYY (també YYYY/MM/DD i YYYY.MM.DD) i retorna DD/MM/YYYY.
// Retorna "" quan és buit o data invàlida (dia/mes fora de rang,
// 29/02 en no-traspàs, any fora de [1900,2100]).
func NormalizeDataNaixement(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	t = strings.NewReplacer("/", "-", ".", "-").Replace(t)
	t = strings.ReplaceAll(t, " ", "")
	parts := strings.Split(t, "-")
	// Elimina segments buits per dobles separadors.
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	parts = kept
	if len(parts) != 3 {
		return ""
	}
	var y, m, d int
	var err error
	if len(parts[0]) == 4 {
		// YYYY-MM-DD
		if y, err = strconv.Atoi(parts[0]); err != nil {
			return ""
		}
		if m, err = strconv.Atoi(parts[1]); err != nil {
			return ""
		}
		if d, err = strconv.Atoi(parts[2]); err != nil {
			return ""
		}
	} else {
		// DD-MM-YYYY (any ha de tenir 4 dígits)
		if len(parts[2]) != 4 {
			return ""
		}
		if d, err = strconv.Atoi(parts[0]); err != nil {
			return ""
		}
		if m, err = strconv.Atoi(parts[1]); err != nil {
			return ""
		}
		if y, err = strconv.Atoi(parts[2]); err != nil {
			return ""
		}
	}
	if y < 1900 || y > 2100 || m < 1 || m > 12 || d < 1 || d > 31 {
		return ""
	}
	// Valida dia real (incl. traspàs) via roundtrip de time.Date.
	rt := time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
	if rt.Year() != y || int(rt.Month()) != m || rt.Day() != d {
		return ""
	}
	return fmt.Sprintf("%02d/%02d/%04d", d, m, y)
}

// NormalizePosicio neteja text lliure de posició habitual.
//
// DECISIÓ: no hi ha mapping conegut text català -> codi Appsistència.
// L'exemple demanat usa posicio:"6" però les dades reals observades
// (mock/castellers.json, API) són textuals: "baix", "contrafort",
// "lateral"... Inventar una taula seria incorrecte, així que es passa
// el text net (trim + col·lapse d'espais). Si ja ve un codi numèric,
// es preserva tal qual.
func NormalizePosicio(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// classifyAppsCreate és la decisió hermètica de creació (testejable):
// retorna (resultatFinal, intentaCrear). "" + true vol dir "cal intentar".
//
// DECISIÓ DNI buit: skip (no crear) per no generar duplicats orfes sense
// clau de matching (HasDoc/NormalizeDoc). La recollida queda marcada
// igual; quan l'operador ompli el DNI, un nou toggle=true reintenta.
func classifyAppsCreate(configured, hasDoc, dniEmpty, altaTrue bool) (string, bool) {
	if !configured {
		return AppsResultSkipped, false
	}
	if hasDoc {
		return AppsResultExists, false
	}
	if dniEmpty {
		return AppsResultSkipped, false
	}
	if altaTrue {
		return AppsResultSkipped, false
	}
	return "", true
}

// BuildCastellerPayload omple el mapa per POST /api/castellers.
// mote = aliesFinal (o BuildFinalAlias si cal). dni = trimmed original
// (no NormalizeDoc: només per matching). alcada via NormalizeAlcada
// (string int o ""). Fixos demanats: posicio 6, revisat 0, soci 0,
// pot_votar 0, propi 0, tecnica 2.
func BuildCastellerPayload(p Person, aliesFinal string) map[string]string {
	np := SplitNom(p.Nom)
	mote := strings.TrimSpace(aliesFinal)
	if mote == "" {
		mote = BuildFinalAlias(p.Alies, p.Nom, p.Colla)
	}
	alc := ""
	if v, ok := NormalizeAlcada(p.AlcadaRaw); ok {
		alc = strconv.Itoa(v)
	}
	return map[string]string{
		"nom":              np.Nom,
		"cognom1":          np.Cognom1,
		"cognom2":          np.Cognom2,
		"mote":             mote,
		"posicio":          "6",
		"email":            NormalizeEmail(p.Email),
		"data_naixement":   NormalizeDataNaixement(p.DataNaixement),
		"telefon":          NormalizeTelefon(p.Telefon),
		"direccio":         "",
		"poblacio":         "",
		"codi_postal":      "",
		"dni":              strings.TrimSpace(p.DNI),
		"soci":             "NO",
		"data_alta":        "",
		"instant_camisa":   "",
		"alcada_espatlles": alc,
		"revisat":          "0",
		"estat_acollida":   "no_aplica",
		"llistes":          "0",
		"tecnica":          "2",
		"propi":            "0",
		"pot_votar":        "0",
		"lesionat":         "0",
		"ultima_renovacio": "",
	}
}

// castellersCreatePath és el path de creació (sense query order).
const castellersCreatePath = "/api/castellers"

// isAppsAuthStatus diu si l'status HTTP indica sessió invàlida.
func isAppsAuthStatus(code int) bool {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden,
		http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// CreateCasteller fa POST base+/api/castellers amb headers exactes del curl
// (Content-Type form-urlencoded; charset=UTF-8, Accept */*) i body JSON cru.
// Accepta 200/201. Si no hi ha sessió, fa Login primer. En 401/403/redirect
// marca loggedIn=false i retorna errAppsistenciaAuth (wrappat) perquè el
// caller faci re-login + 1 retry. En èxit, afegeix el DNI al docSet.
func (a *AppsistenciaClient) CreateCasteller(ctx context.Context, payload map[string]string) error {
	if a == nil {
		return fmt.Errorf("appsistencia: client not initialized")
	}
	a.mu.RLock()
	base := a.baseURL
	a.mu.RUnlock()
	if base == "" {
		base = defaultAppsistenciaURL
	}
	if !a.HasSession() {
		if err := a.Login(ctx); err != nil {
			return fmt.Errorf("appsistencia: login before create: %w", err)
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("appsistencia: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+castellersCreatePath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("appsistencia: build create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", appsistenciaUA)
	req.Header.Set("Referer", base+"/")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("appsistencia: POST castellers: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		a.AddDoc(payload["dni"])
		return nil
	}
	if isAppsAuthStatus(resp.StatusCode) {
		a.setLoggedIn(false)
		return fmt.Errorf("%w: create rejected (status %d)", errAppsistenciaAuth, resp.StatusCode)
	}
	return fmt.Errorf("appsistencia: unexpected create status %d", resp.StatusCode)
}

// AddDoc afegeix un document al set normalitzat (post-creació, sense refetch).
func (a *AppsistenciaClient) AddDoc(doc string) {
	if a == nil {
		return
	}
	n := NormalizeDoc(doc)
	if n == "" {
		return
	}
	a.docMu.Lock()
	if a.docSet == nil {
		a.docSet = map[string]bool{}
	}
	a.docSet[n] = true
	a.docMu.Unlock()
}
