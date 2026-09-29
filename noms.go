package main

import (
	"strings"
)

// NomParts és el resultat best-effort de partir un nom complet
// (sistema de 2 cognoms català/castellà). Es preserva l'ortografia
// original (accents, capitals); tot el matching passa per Normalize.
type NomParts struct {
	Nom     string
	Cognom1 string
	Cognom2 string
}

// SplitNom parteix un nom complet en nom + 2 cognoms (best-effort).
//
// Algorisme:
//  1. Trim + col·lapsar espais (Fields+Join). Esborrar segments
//     parentètics "(...)" (malnoms, "(Andorra)"); si queda un '('
//     sense tancar, tallar sufix des d'allà. Si queda buit, tot buit.
//  2. Format invertit "Cognoms, Nom": tot el darrere de la primera coma
//     és el Nom (join); el davant es reparteix en C1/C2 amb partícules.
//  3. Tokenitzar per Fields i filtrar connectors sols "i"/"y"/"e"
//     (comparats via Normalize). Si no queda res, tot buit.
//  4. n==1 -> Nom; n==2 -> Nom + C1.
//  5. n>=3 per defecte: C1/C2 són les 2 últimes unitats, Nom la resta.
//  6. Excepció n==3 amb nom compost (p. ex. "Joan Anton Rius"):
//     Nom = token1+token2, C1 = token3.
//  7. Les unitats de cognom absorbeixen partícules precedents (màx. 3).
//     Guionets i apòstrofs són un sol token (Fields no els parteix).
func SplitNom(full string) NomParts {
	// 1. Trim + col·lapsar, esborrar parentètics.
	cur := treuParentetics(full)
	if cur == "" {
		return NomParts{}
	}

	// 2. Format invertit amb coma.
	if i := strings.IndexByte(cur, ','); i >= 0 {
		nom := strings.Join(strings.Fields(cur[i+1:]), " ")
		bloc := strings.Join(strings.Fields(cur[:i]), " ")
		toks := filtraConnectors(strings.Fields(bloc))
		if len(toks) == 0 {
			return NomParts{Nom: nom}
		}
		c1, c2 := reparteixCognoms(toks)
		return NomParts{Nom: nom, Cognom1: c1, Cognom2: c2}
	}

	// 3. Tokenitzar + filtrar connectors.
	toks := filtraConnectors(strings.Fields(cur))
	if len(toks) == 0 {
		return NomParts{}
	}

	// 4. Casos curts.
	switch len(toks) {
	case 1:
		return NomParts{Nom: toks[0]}
	case 2:
		return NomParts{Nom: toks[0], Cognom1: toks[1]}
	}

	// 6. Excepció n==3 amb nom compost.
	if len(toks) == 3 && esIniciCompost(toks[0]) && esSegonCompost(toks[1]) {
		return NomParts{Nom: toks[0] + " " + toks[1], Cognom1: toks[2]}
	}

	// 5+7. Per defecte: 2 últimes unitats (amb partícules) són cognoms.
	iniciC2 := prenCognom(toks, len(toks))
	iniciC1 := prenCognom(toks, iniciC2)
	if iniciC1 == 0 {
		// Tot eren partícules: fallback sense perdre tokens.
		return NomParts{
			Nom:     toks[0],
			Cognom1: strings.Join(toks[1:len(toks)-1], " "),
			Cognom2: toks[len(toks)-1],
		}
	}
	return NomParts{
		Nom:     strings.Join(toks[:iniciC1], " "),
		Cognom1: strings.Join(toks[iniciC1:iniciC2], " "),
		Cognom2: strings.Join(toks[iniciC2:], " "),
	}
}

// treuParentetics esborra els segments "(...)" (malnoms, "(Andorra)") i
// col·lapsa espais. Si queda un '(' sense tancar, talla el sufix des
// d'allà (com ShortenAliasMax). Preserva la resta (cognoms darrere el
// parèntesi no es perden).
func treuParentetics(s string) string {
	cur := strings.Join(strings.Fields(s), " ")
	for {
		o := strings.IndexByte(cur, '(')
		if o < 0 {
			return cur
		}
		if c := strings.IndexByte(cur[o:], ')'); c >= 0 {
			cur = strings.Join(strings.Fields(cur[:o]+" "+cur[o+c+1:]), " ")
			continue
		}
		return strings.Join(strings.Fields(cur[:o]), " ")
	}
}

// filtraConnectors treu els tokens que són només "i"/"y"/"e"
// (comparats via Normalize, tolerant capitals/accents).
func filtraConnectors(toks []string) []string {
	out := toks[:0:0]
	for _, t := range toks {
		if connectorsSolts[Normalize(t)] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// connectorsSolts són les conjuncions soles que no fan de cognom.
var connectorsSolts = map[string]bool{
	"i": true, "y": true, "e": true,
}

// particulesCognom són les partícules que s'enganxen al cognom següent.
// Comparació via Normalize (tolerant capitals/accents: "De" -> "de").
var particulesCognom = map[string]bool{
	"de": true, "del": true, "dels": true,
	"la": true, "las": true, "el": true, "els": true, "les": true, "los": true,
	"d": true, "l": true, "al": true,
	"do": true, "da": true, "dos": true, "das": true, "ao": true, "aos": true,
	"van": true, "von": true, "den": true, "der": true, "di": true,
	"le": true, "du": true,
}

// esParticulaCognom diu si tok és una partícula de cognom (via Normalize).
func esParticulaCognom(tok string) bool {
	return particulesCognom[Normalize(tok)]
}

// prenCognom retorna l'inici del bloc de cognom que acaba a end (exclòs):
// comença a end-1 i absorbeix fins a 3 partícules precedents.
func prenCognom(toks []string, end int) int {
	if end <= 0 {
		return 0
	}
	start := end - 1
	for adjunts := 0; adjunts < 3 && start-1 >= 0 && esParticulaCognom(toks[start-1]); adjunts++ {
		start--
	}
	return start
}

// reparteixCognoms parteix un bloc de cognoms (format invertit) en C1/C2:
// C2 és l'última unitat (amb partícules), C1 la resta (join).
// Si tot el bloc és una sola unitat amb partícules, va a C1.
func reparteixCognoms(toks []string) (string, string) {
	if len(toks) == 1 {
		return toks[0], ""
	}
	iniciC2 := prenCognom(toks, len(toks))
	if iniciC2 == 0 {
		return strings.Join(toks, " "), ""
	}
	return strings.Join(toks[:iniciC2], " "), strings.Join(toks[iniciC2:], " ")
}

// iniciCompost són els primers tokens que poden formar nom compost.
var iniciCompost = map[string]bool{
	"maria": true, "mari": true, "anna": true, "ana": true,
	"joana": true, "joan": true, "josep": true, "jose": true,
	"juan": true, "pau": true, "pere": true,
}

// segonCompost són els segons tokens que tanquen un nom compost.
var segonCompost = map[string]bool{
	"anton": true, "antoni": true, "angels": true, "angeles": true,
	"pilar": true, "cruz": true, "rosa": true, "lluis": true, "luis": true,
	"oriol": true, "carme": true, "carmen": true, "dolors": true,
	"teresa": true, "jose": true, "josep": true, "maria": true,
	"anna": true, "de": true, "del": true, "la": true,
}

// esIniciCompost diu si tok pot obrir un nom compost (via Normalize).
func esIniciCompost(tok string) bool {
	return iniciCompost[Normalize(tok)]
}

// esSegonCompost diu si tok pot tancar un nom compost (via Normalize).
func esSegonCompost(tok string) bool {
	return segonCompost[Normalize(tok)]
}
