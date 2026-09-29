package main

import (
	"testing"
)

func TestSplitNom(t *testing.T) {
	cases := []struct {
		in   string
		want NomParts
	}{
		// Buit.
		{"", NomParts{}},
		{"   ", NomParts{}},
		{"(Nuri)", NomParts{}},

		// 1 token.
		{"Jordi", NomParts{Nom: "Jordi"}},

		// 2 tokens.
		{"Jordi Pons", NomParts{Nom: "Jordi", Cognom1: "Pons"}},

		// 3 tokens simple.
		{"Jordi Pons Bofill", NomParts{Nom: "Jordi", Cognom1: "Pons", Cognom2: "Bofill"}},

		// n==3 compost: Anton no parteix com a cognom.
		{"Joan Anton Rius", NomParts{Nom: "Joan Anton", Cognom1: "Rius"}},

		// n==3 no-compost: torrell no és a la llista, Nom simple.
		{"Anna Torrell Puig", NomParts{Nom: "Anna", Cognom1: "Torrell", Cognom2: "Puig"}},

		// Compost llarg: Nom agafa la resta.
		{"Maria del Pilar Sanz Ruiz", NomParts{Nom: "Maria del Pilar", Cognom1: "Sanz", Cognom2: "Ruiz"}},
		{"María de la Cruz Segura Huertas", NomParts{Nom: "María de la Cruz", Cognom1: "Segura", Cognom2: "Huertas"}},

		// Partícula a C1.
		{"Marta de Mingo Garcia", NomParts{Nom: "Marta", Cognom1: "de Mingo", Cognom2: "Garcia"}},

		// Multipartícula.
		{"Elena De La Rosa Parra", NomParts{Nom: "Elena", Cognom1: "De La Rosa", Cognom2: "Parra"}},
		{"Pau De los Rios Blanch", NomParts{Nom: "Pau", Cognom1: "De los Rios", Cognom2: "Blanch"}},

		// Conjuncions i/y filtrades.
		{"Jordi Pons i Bofill", NomParts{Nom: "Jordi", Cognom1: "Pons", Cognom2: "Bofill"}},
		{"Anna Gasco y Navarro", NomParts{Nom: "Anna", Cognom1: "Gasco", Cognom2: "Navarro"}},

		// Guionet i apòstrof: un sol token.
		{"Laia Llort-Fronzes Puig", NomParts{Nom: "Laia", Cognom1: "Llort-Fronzes", Cognom2: "Puig"}},
		{"Anna d'Alba Puig", NomParts{Nom: "Anna", Cognom1: "d'Alba", Cognom2: "Puig"}},

		// Parèntesi: s'esborra el segment "(...)", la resta es parteix.
		{"Núria (Nuri) Coll Martí", NomParts{Nom: "Núria", Cognom1: "Coll", Cognom2: "Martí"}},
		{"Daniel Marquez (Andorra)", NomParts{Nom: "Daniel", Cognom1: "Marquez"}},

		// Coma invertida.
		{"Soto Moya, Elena", NomParts{Nom: "Elena", Cognom1: "Soto", Cognom2: "Moya"}},
		{"De La Rosa Parra, Elena", NomParts{Nom: "Elena", Cognom1: "De La Rosa", Cognom2: "Parra"}},

		// Espais múltiples.
		{"  Jordi   Pons   Bofill  ", NomParts{Nom: "Jordi", Cognom1: "Pons", Cognom2: "Bofill"}},

		// Preserva accents i capitals.
		{"Núria Coll Martí", NomParts{Nom: "Núria", Cognom1: "Coll", Cognom2: "Martí"}},
	}
	for _, c := range cases {
		got := SplitNom(c.in)
		if got != c.want {
			t.Errorf("SplitNom(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
