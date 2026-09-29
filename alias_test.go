package main

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortenAlias(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Curts invariants (trim + col·lapsar espais).
		{"Coia", "Coia"},
		{"Pere", "Pere"},
		{"Baena", "Baena"},
		{"Andrea ", "Andrea"},
		{"Pàmies ", "Pàmies"},
		{"A.G.", "A.G."},
		{"+3", "+3"},

		// Articles inicials.
		{"El Guiri", "Guiri"},
		{"La Mari", "Mari"},

		// Inicial + cognom.
		{"Joan Lluís", "J.Lluís"},
		{"Xavi Vega", "X.Vega"},
		{"Dani Andorra", "D.Andorr"},
		{"Andreu Alba", "A.Alba"},

		// Stopwords interiors -> acrònim.
		{"PEP DE BOT", "P.BOT"},

		// Sufix parentètic.
		{"Medina (o Albi, si no está agafat)", "Medina"},
		{"Corti (ja estic a la app)", "Corti"},

		// Sense àlies.
		{"No en tinc...", ""},
		{"No en tinc", ""},
		{"no tinc", ""},
		{"", ""},
		{"...", ""},

		// Paraula única llarga.
		{"Marchante", "Marchant"},
		{"ElVendrell", "Vendrell"},

		// Últim token inicial.
		{"Manel C.", "Manel.C"},
		{"Dani C.", "Dani.C"},
	}
	for _, c := range cases {
		got := ShortenAlias(c.in)
		if got != c.want {
			t.Errorf("ShortenAlias(%q) = %q, want %q", c.in, got, c.want)
		}
		if utf8.RuneCountInString(got) > 8 {
			t.Errorf("ShortenAlias(%q) = %q supera 8 runes", c.in, got)
		}
	}
}

// Tots els àlies d'alias.txt han de quedar en <= 8 runes.
func TestShortenAlias_AllLinesFit8Runes(t *testing.T) {
	data, err := os.ReadFile("alias.txt")
	if err != nil {
		t.Fatalf("llegint alias.txt: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		got := ShortenAlias(line)
		if utf8.RuneCountInString(got) > 8 {
			t.Errorf("ShortenAlias(%q) = %q supera 8 runes", line, got)
		}
	}
}
