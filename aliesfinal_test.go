package main

import "testing"

func TestBuildFinalAlias(t *testing.T) {
	cases := []struct {
		name  string
		alies string
		nom   string
		colla string
		want  string
	}{
		{
			name:  "alies normal amb colla",
			alies: "El Guiri",
			nom:   "Guillem Rius",
			colla: "Xiquets del Serrallo",
			want:  "Guiri (XdS)",
		},
		{
			name:  "alies buit fallback a nom",
			alies: "",
			nom:   "Manel C.",
			colla: "Xiquets del Serrallo",
			want:  "Manel.C (XdS)",
		},
		{
			name:  "no en tinc fallback a nom",
			alies: "no en tinc",
			nom:   "Jordi Puig",
			colla: "Colla Jove Xiquets de Tarragona",
			want:  "J.Puig (CJXT)",
		},
		{
			name:  "multi-colla agafa primera",
			alies: "El Guiri",
			nom:   "Guillem Rius",
			colla: "Xiquets del Serrallo, Colla Jove Xiquets de Tarragona",
			want:  "Guiri (XdS)",
		},
		{
			name:  "sense colla suffix CAP",
			alies: "El Guiri",
			nom:   "Guillem Rius",
			colla: "",
			want:  "Guiri (CAP)",
		},
		{
			name:  "sense colla explicita suffix CAP",
			alies: "La Mari",
			nom:   "Maria Roig",
			colla: "sense colla",
			want:  "Mari (CAP)",
		},
		{
			name:  "base buida total",
			alies: "",
			nom:   "",
			colla: "Xiquets del Serrallo",
			want:  "",
		},
		{
			name:  "nomes simbols buit total",
			alies: "---",
			nom:   "!!!",
			colla: "",
			want:  "",
		},
		{
			name:  "base majuscules normalitza caixa",
			alies: "EL GUIRI",
			nom:   "Guillem Rius",
			colla: "Xiquets del Serrallo",
			want:  "Guiri (XdS)",
		},
		{
			name:  "base amb punt majuscules",
			alies: "J.PUIG",
			nom:   "Jordi Puig",
			colla: "Xiquets del Serrallo",
			want:  "J.Puig (XdS)",
		},
		{
			name:  "base no-lletra preservada",
			alies: "+3",
			nom:   "Tmistocles",
			colla: "Brivalls de Cornudella",
			want:  "+3 (BdC)",
		},
		{
			name:  "fallback nom majuscules",
			alies: "",
			nom:   "MANEL C.",
			colla: "Xiquets del Serrallo",
			want:  "Manel.C (XdS)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildFinalAlias(tc.alies, tc.nom, tc.colla); got != tc.want {
				t.Errorf("BuildFinalAlias(%q, %q, %q) = %q, want %q",
					tc.alies, tc.nom, tc.colla, got, tc.want)
			}
		})
	}
}

func TestShouldFillAliesFinal(t *testing.T) {
	cases := []struct {
		name     string
		existent string
		computed string
		want     bool
	}{
		{name: "buida i computat", existent: "", computed: "Guiri (XdS)", want: true},
		{name: "ja plena", existent: "Guiri (XdS)", computed: "Guiri (XdS)", want: false},
		{name: "plena amb altre valor", existent: "Altre (CAP)", computed: "Guiri (XdS)", want: false},
		{name: "computat buit", existent: "", computed: "", want: false},
		{name: "tot buit", existent: "", computed: "", want: false},
		{name: "plena i computat buit", existent: "X (XdS)", computed: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFillAliesFinal(tc.existent, tc.computed); got != tc.want {
				t.Errorf("shouldFillAliesFinal(%q, %q) = %v, want %v",
					tc.existent, tc.computed, got, tc.want)
			}
		})
	}
}

func TestSheetsTextValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "leading plus formula-like", in: "+3 (BdC)", want: "'+3 (BdC)"},
		{name: "normal", in: "Guiri (XdS)", want: "'Guiri (XdS)"},
		{name: "ja prefixat inalterat", in: "'Guiri (XdS)", want: "'Guiri (XdS)"},
		{name: "buit", in: "", want: "'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sheetsTextValue(tc.in); got != tc.want {
				t.Errorf("sheetsTextValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
