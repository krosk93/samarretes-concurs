package main

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

//go:embed colles.json
var collesJSON []byte

type collaEntry struct {
	Nom    string `json:"nom"`
	Sigles string `json:"sigles"`

	norm      string   // normalized official name
	sigTokens []string // distinctive tokens (no stopwords, no generics)
}

var (
	collaEntries  []collaEntry
	collaNomCanon map[string]string // normalized official nom -> canonical sigles
	collaSigCanon map[string]string // lowercased sigles -> canonical sigles
	collaNomOf    map[string]string // canonical sigles -> official nom
)

var collaAposReplacer = strings.NewReplacer(
	"’", "'", "‘", "'", "´", "'", "`", "'", "ʼ", "'",
	"′", "'", "＇", "'", "‛", "'", "❛", "'", "❜", "'",
)

var collaStop = map[string]bool{
	"de": true, "del": true, "dels": true, "d": true,
	"el": true, "la": true, "els": true, "les": true, "l": true,
	"al": true, "als": true, "a": true, "es": true, "sa": true,
	"ses": true, "na": true, "en": true, "di": true, "i": true,
	"y": true, "los": true, "the": true, "of": true,
}

var collaGeneric = map[string]bool{
	"colla": true, "colles": true, "castellers": true, "castellera": true,
}

// normCollaText normalizes apostrophe variants, collapses repeated
// apostrophes, then applies Normalize (lowercase, accent strip, collapse).
func normCollaText(s string) string {
	s = collaAposReplacer.Replace(s)
	for strings.Contains(s, "''") {
		s = strings.ReplaceAll(s, "''", "'")
	}
	s = Normalize(s)
	s = strings.Trim(s, " .,;")
	return strings.Join(strings.Fields(s), " ")
}

// collaTokens splits on any non-letter/digit run (so l'esquerra -> l, esquerra
// and vila-seca -> vila, seca) and drops single-char tokens (d, l, ...).
func collaTokens(n string) []string {
	raw := strings.FieldsFunc(n, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if len([]rune(t)) <= 1 {
			continue
		}
		out = append(out, t)
	}
	return out
}

func collaSigTokens(n string) []string {
	var out []string
	for _, t := range collaTokens(n) {
		if collaStop[t] || collaGeneric[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

func isCapNorm(n string) bool {
	switch n {
	case "", "-", "cap", "caps", "cap colla", "no", "no colla",
		"sense colla", "ninguna":
		return true
	}
	return false
}

func init() {
	collaNomCanon = map[string]string{}
	collaSigCanon = map[string]string{}
	collaNomOf = map[string]string{}
	var raw []collaEntry
	if err := json.Unmarshal(collesJSON, &raw); err != nil {
		return
	}
	for _, e := range raw {
		n := normCollaText(e.Nom)
		collaEntries = append(collaEntries, collaEntry{
			Nom:       e.Nom,
			Sigles:    e.Sigles,
			norm:      n,
			sigTokens: collaSigTokens(n),
		})
		if _, ok := collaNomCanon[n]; !ok {
			collaNomCanon[n] = e.Sigles
		}
		lower := strings.ToLower(strings.TrimSpace(e.Sigles))
		if _, ok := collaSigCanon[lower]; !ok {
			collaSigCanon[lower] = e.Sigles
		}
		if _, ok := collaNomOf[e.Sigles]; !ok {
			collaNomOf[e.Sigles] = e.Nom
		}
	}
	// Most specific first: more distinctive tokens, then longer name.
	sort.SliceStable(collaEntries, func(a, b int) bool {
		if len(collaEntries[a].sigTokens) != len(collaEntries[b].sigTokens) {
			return len(collaEntries[a].sigTokens) > len(collaEntries[b].sigTokens)
		}
		return len(collaEntries[a].norm) > len(collaEntries[b].norm)
	})
}

// CollaNomOficial returns the official nom for known sigles (case-insensitive).
func CollaNomOficial(sigles string) string {
	canon, ok := collaSigCanon[strings.ToLower(strings.TrimSpace(sigles))]
	if !ok {
		return ""
	}
	return collaNomOf[canon]
}

func isKnownSigle(s string) bool {
	if s == "CAP" {
		return true
	}
	_, ok := collaSigCanon[strings.ToLower(s)]
	return ok
}

// collaSplitRe splits multi-colla answers on common separators.
var collaSplitRe = regexp.MustCompile(`(?i)(?:\s*(?:,|;|\+|/)\s*|\s+i\s+|\s+amb\s+|\s*antigament\s*|\s*anteriorment\s*)`)

// collaResolveSingle resolves one (already unsplit) colla text to sigles,
// trimmed original, or CAP.
func collaResolveSingle(input string) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	n := normCollaText(input)

	// a) sense colla
	if isCapNorm(n) {
		return "CAP"
	}
	// b) already known sigles (case-insensitive, ex cjxt -> CJXT)
	if canon, ok := collaSigCanon[strings.ToLower(n)]; ok {
		return canon
	}
	// c) Tarragona context (normalized contains)
	// CJXV first: "joves" contains "jove" as substring, so plural wins.
	if strings.Contains(n, "cjxv") || strings.Contains(n, "joves") {
		return "CJXV"
	}
	if strings.Contains(n, "vella") {
		return "CVXV"
	}
	if strings.Contains(n, "cjxt") ||
		(strings.Contains(n, "jove") && (strings.Contains(n, "tarragona") || strings.Contains(n, "tgn"))) ||
		n == "jove" {
		return "CJXT"
	}
	if n == "xiquets" {
		return "XdT"
	}
	if strings.Contains(n, "serrallo") {
		return "XdS"
	}
	// d) exact normalized match against colles.json nom
	if sig, ok := collaNomCanon[n]; ok {
		return sig
	}
	// e) contains: official contains input (or viceversa, guardat per curts).
	for _, e := range collaEntries {
		if e.norm == "" {
			continue
		}
		if strings.Contains(n, e.norm) {
			return e.Sigles
		}
		if len(n) >= 4 && strings.Contains(e.norm, n) {
			return e.Sigles
		}
	}
	// e2) distinctive-token containment: all official distinctive tokens
	// present in input (ex "Castellers Andorra" -> CdAN via {castellers,andorra}).
	inSet := map[string]bool{}
	for _, t := range collaTokens(n) {
		if collaStop[t] {
			continue
		}
		inSet[t] = true
	}
	for _, e := range collaEntries {
		if len(e.sigTokens) == 0 {
			continue
		}
		ok := true
		for _, t := range e.sigTokens {
			if !inSet[t] {
				ok = false
				break
			}
		}
		if ok {
			return e.Sigles
		}
	}
	// g) unknown: sigles-looking (<=5 letters, no spaces) -> upper,
	// else trimmed original so free-text search still matches.
	if trimmed != "" && !strings.Contains(trimmed, " ") && len([]rune(trimmed)) <= 5 {
		return strings.ToUpper(trimmed)
	}
	return trimmed
}

// CollaSiglesTotes resolves a raw colla cell to all its sigles.
// Multi-colla answers split on , ; + / i amb antigament anteriorment.
func CollaSiglesTotes(input string) []string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if trimmed == "" || isCapNorm(normCollaText(input)) {
		return []string{"CAP"}
	}
	rawParts := collaSplitRe.Split(trimmed, -1)
	var clean []string
	for _, p := range rawParts {
		if t := strings.Join(strings.Fields(strings.TrimSpace(p)), " "); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) > 1 {
		out := make([]string, 0, len(clean))
		allKnown := true
		for _, p := range clean {
			s := collaResolveSingle(p)
			if !isKnownSigle(s) || s == "" {
				allKnown = false
				break
			}
			out = append(out, s)
		}
		// Only trust the split when every part resolves to something
		// known; otherwise the separator was part of an official name
		// (ex "Xiqüelos i Xiqüeles del Delta", "Sant Pere i Sant Pau").
		if allKnown {
			seen := map[string]bool{}
			dedup := out[:0]
			for _, s := range out {
				if !seen[s] {
					seen[s] = true
					dedup = append(dedup, s)
				}
			}
			return dedup
		}
	}
	return []string{collaResolveSingle(trimmed)}
}

// CollaSigles resolves a raw colla cell to its primary sigles.
func CollaSigles(input string) string {
	return CollaSiglesTotes(input)[0]
}
