package main

import (
	"strings"
)

var docStopwords = map[string]bool{
	"passaport": true,
	"passport":  true,
	"pasaporte": true,
	"andorra":   true,
	"dni":       true,
	"nif":       true,
	"nie":       true,
	"document":  true,
	"documento": true,
	"identitat": true,
	"identidad": true,
}

// NormalizeDoc normalizes an identity document for Sheets↔Appsistència matching:
// lowercases (via Normalize, reusing accent handling), tokenizes into [a-z0-9]
// sequences, drops stopwords, concatenates the rest.
func NormalizeDoc(s string) string {
	n := Normalize(s)
	var b strings.Builder
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tok := cur.String()
		cur.Reset()
		if docStopwords[tok] {
			return
		}
		b.WriteString(tok)
	}
	for _, r := range n {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return b.String()
}
