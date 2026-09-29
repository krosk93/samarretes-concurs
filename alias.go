package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ShortenAlias escurça un àlies de casteller a un màxim de 8 caràcters.
// És una drecera per a ShortenAliasMax(s, 8).
func ShortenAlias(s string) string {
	return ShortenAliasMax(s, 8)
}

// ShortenAliasMax escurça s a un màxim de max runes (no bytes: preserva
// accents/UTF-8 i mai trenca una rune a la meitat).
//
// Algorisme, ordenat i determinista:
//  1. TrimSpace + col·lapsar espais interiors (strings.Fields + Join).
//  2. Tallar sufix parentètic: tot des del primer '('.
//  3. Sense-àlies -> "": normalitzat (Normalize) buit, "no en tinc",
//     "no tinc", "no", "cap", o només punts/símbols (cap lletra ni dígit).
//  4. Si len(runes) <= max, retorna tal qual.
//  5. Treure articles inicials (El/La/Els/Les/L'/En/Na/Es/Sa), fins a 2 cops.
//  6. Multi-paraula: treure stopwords interiors i formar Inicial.Ultim.
//     Si l'últim token és una inicial (<=2 runes, p. ex. "C."),
//     forma "Primer.Lletra" sense espai (p. ex. "Manel C." -> "Manel.C").
//  7. Paraula única > max: prefix CamelCase (ElVendrell -> Vendrell),
//     després treure vocals de dreta a esquerra, si cal truncar dur.
//  8. Garanties finals: <= max runes, sense espais extrems ni punt/guió final
//     (només quan s'ha modificat; els invariants curts es preserven tal qual).
func ShortenAliasMax(s string, max int) string {
	if max <= 0 {
		return ""
	}

	// 1. Trim + col·lapsar espais interiors.
	cur := strings.Join(strings.Fields(s), " ")

	// 2. Tallar sufix parentètic des del primer '('.
	if i := strings.IndexByte(cur, '('); i >= 0 {
		cur = strings.Join(strings.Fields(cur[:i]), " ")
	}

	// 3. Detectar sense-àlies.
	if isNoAlias(cur) {
		return ""
	}

	// 4. Paraula única ja prou curta: invariant tal qual.
	// (Nota: el pas 4 de l'espec es fa aquí només per a paraula única:
	// els articles inicials i la forma Inicial.Cognom s'apliquen encara
	// que l'entrada ja càpiga en max, p. ex. "El Guiri" -> "Guiri",
	// "La Mari" -> "Mari", "Manel C." -> "Manel.C".)
	if !strings.Contains(cur, " ") && utf8.RuneCountInString(cur) <= max {
		return cur
	}

	// 5. Treure articles inicials (fins a 2 passades: "repetir un cop").
	for i := 0; i < 2; i++ {
		stripped, ok := stripLeadingArticle(cur)
		if !ok {
			break
		}
		cur = stripped
		if utf8.RuneCountInString(cur) <= max {
			return cur
		}
	}

	// 6. Multi-paraula (l'apòstrof no separa: només strings.Fields).
	if strings.Contains(cur, " ") {
		return shortenMultiword(cur, max)
	}

	// 7. Paraula única > max.
	return shortenSingleWord(cur, max)
}

// BuildFinalAlias construeix l'àlies final "Base (Sigles)" per a l'app.
// base = ShortenAlias(alies), amb fallback a ShortenAlias(nom) quan
// l'àlies és buit o negació (ShortenAlias retorna "" en aquests casos).
// La base es normalitza a minúscules amb inicial en majúscula per segment
// (normalizeAliasBase); les sigles mantenen la forma canònica (XdS, CJXT,
// CAP). Si la base queda buida, retorna "". Altrament afegeix " (" + sigles +
// ")" amb CollaSigles(colla) ("" -> base sola, si no base amb parèntesi).
// CollaSigles("") retorna "CAP", així que pràcticament sempre hi ha sufix.
// Multi-colla: CollaSigles ja agafa la primera.
func BuildFinalAlias(alies, nom, colla string) string {
	base := ShortenAlias(alies)
	if base == "" {
		base = ShortenAlias(nom)
	}
	if base == "" {
		return ""
	}
	base = normalizeAliasBase(base)
	sigles := CollaSigles(colla)
	if sigles == "" {
		return base
	}
	return base + " (" + sigles + ")"
}

// normalizeAliasBase passa cada segment de base separat per "." a minúscules
// amb la primera rune en majúscules ("GUIRI"->"Guiri", "J.PUIG"->"J.Puig",
// "Manel.C" inalterat). L'upper d'una no-lletra és no-op ("+3" queda "+3").
func normalizeAliasBase(base string) string {
	segs := strings.Split(base, ".")
	for i, s := range segs {
		r := []rune(strings.ToLower(s))
		if len(r) > 0 {
			r[0] = unicode.ToUpper(r[0])
		}
		segs[i] = string(r)
	}
	return strings.Join(segs, ".")
}

// isNoAlias diu si s no és un àlies real: buit, negació ("no en tinc",
// "no tinc", "no", "cap", tolerant punts finals com "No en tinc..."),
// o només punts/símbols sense cap lletra ni dígit.
func isNoAlias(s string) bool {
	n := Normalize(s)
	trimmed := strings.Trim(n, " .…!?,;-_")
	switch trimmed {
	case "", "no en tinc", "no tinc", "no", "cap":
		return true
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// leadingArticles són els articles inicials a treure (minúscules, amb
// espai o apòstrof final perquè el match és per prefix del normalitzat).
var leadingArticles = []string{
	"els ", "les ", "el ", "la ", "l'", "l’",
	"en ", "na ", "es ", "sa ",
}

// stripLeadingArticle treu un article inicial si n'hi ha.
// Compara case-insensitive via Normalize però talla l'original per runes
// (els articles són ASCII excepte l’/’, que té les mateixes runes en
// original i normalitzat).
func stripLeadingArticle(s string) (string, bool) {
	n := Normalize(s)
	for _, art := range leadingArticles {
		if strings.HasPrefix(n, art) {
			r := []rune(s)
			if len(r) <= len([]rune(art)) {
				return "", true
			}
			return strings.Join(strings.Fields(string(r[len([]rune(art)):])), " "), true
		}
	}
	return s, false
}

// interiorStopwords són els stopwords a eliminar de l'interior (i extrems
// no inicials ja tractats) en noms multi-paraula. Comparació via Normalize.
var interiorStopwords = map[string]bool{
	"de": true, "del": true, "dels": true, "d'": true,
	"al": true, "a": true, "i": true,
	"el": true, "la": true, "els": true, "les": true,
	"en": true, "na": true, "es": true, "sa": true, "l'": true,
}

// shortenMultiword forma "Inicial.UltimToken" a partir de tokens nets de
// stopwords. Cas especial: últim token inicial (<=2 runes, p. ex. "C." o
// "C") -> "Primer.Lletra" sense espai (p. ex. "Manel C." -> "Manel.C");
// si no hi cap, el primer token es trunca per l'esquerra? No: es trunca
// el primer token pel final fins a max-2 runes + "." + lletra.
func shortenMultiword(s string, max int) string {
	tokens := strings.Fields(s)
	kept := tokens[:0:0]
	for _, t := range tokens {
		if !interiorStopwords[Normalize(t)] {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		// Tot eren stopwords: tractar el primer token original com a únic.
		return shortenSingleWord(tokens[0], max)
	}
	if len(kept) == 1 {
		return shortenSingleWord(kept[0], max)
	}
	first, last := kept[0], kept[len(kept)-1]

	// Últim token inicial ("Manel C." -> [Manel, C.]): "Manel.C".
	if utf8.RuneCountInString(last) <= 2 {
		letter := strings.Trim(last, ".")
		if letter == "" {
			return finish(utf8truncate(first, max))
		}
		lr := []rune(letter)
		initial := string(lr[0])
		if utf8.RuneCountInString(first)+2 <= max {
			return first + "." + initial
		}
		// Truncar el primer token perquè "Truncat.X" càpiga en max.
		return finish(utf8truncate(first, max-2) + "." + initial)
	}

	initial := string([]rune(first)[0])
	if utf8.RuneCountInString(initial+"."+last) <= max {
		return initial + "." + last
	}
	// Truncar l'últim token perquè "I." + rest càpiga en max.
	return finish(initial + "." + utf8truncate(last, max-2))
}

// camelPrefixes per a paraula única enganxada ("ElVendrell" -> "Vendrell").
// Ordre: els més llargs primer.
var camelPrefixes = []string{"Els", "Les", "L'", "L’", "El", "La", "En", "Na", "Es", "Sa"}

// shortenSingleWord escurça una paraula única més llarga que max:
// prefix CamelCase + majúscula, després vocals de dreta a esquerra,
// finalment truncament dur.
func shortenSingleWord(w string, max int) string {
	if utf8.RuneCountInString(w) <= max {
		return w
	}
	// Prefix CamelCase: El|La|En|Na|... + majúscula sense espai.
	for _, p := range camelPrefixes {
		if strings.HasPrefix(w, p) {
			rr := []rune(w)
			pr := []rune(p)
			if len(rr) > len(pr) && unicode.IsUpper(rr[len(pr)]) {
				w = string(rr[len(pr):])
				break
			}
		}
	}
	if utf8.RuneCountInString(w) <= max {
		return finish(w)
	}
	// Treure vocals de dreta a esquerra fins a max.
	r := []rune(w)
	for len(r) > max {
		idx := -1
		for i := len(r) - 1; i >= 0; i-- {
			if isVowel(r[i]) {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		r = append(r[:idx], r[idx+1:]...)
	}
	if len(r) > max {
		r = r[:max]
	}
	return finish(string(r))
}

// isVowel detecta vocals catalanes (amb accent o sense), case-insensitive.
func isVowel(r rune) bool {
	switch unicode.ToLower(r) {
	case 'a', 'e', 'i', 'o', 'u',
		'à', 'è', 'é', 'í', 'ï', 'ò', 'ó', 'ú', 'ü':
		return true
	}
	return false
}

// utf8truncate talla a max runes (mai trenca una rune).
func utf8truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// finish aplica les garanties finals a un resultat modificat:
// <= max no es garanteix aquí (ho fa el caller), però sí: trim d'espais,
// col·lapsar espais interiors, i netejar punts/guions finals.
func finish(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.Trim(s, ".-–— ")
}
