package main

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var alcadaNumRe = regexp.MustCompile(`\d+(?:[.,'’‘´]\d+)?`)
var alcadaParenRe = regexp.MustCompile(`\(([^)]*)\)`)

// NormalizeAlcada normalitza l'alçada fins les espatlles a cm enters.
// Retorna ok=false si no hi ha cap valor vàlid en [100,210].
// Si el text marca estatura total ("total", "complet..."), resta 30 cm
// com a estimació de cap+coll per obtenir les espatlles.
func NormalizeAlcada(s string) (int, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}

	// Marca d'estatura total (cobreix "total", "completo/a") via Normalize.
	n := Normalize(s)
	esTotal := strings.Contains(n, "total") || strings.Contains(n, "complet")

	// Candidats ordenats: primer els de dins de parèntesis, després tots.
	type cand struct {
		num   string
		after string
	}
	var ordenats []cand
	for _, m := range alcadaParenRe.FindAllStringSubmatch(t, -1) {
		frag := m[1]
		for _, loc := range alcadaNumRe.FindAllStringIndex(frag, -1) {
			ordenats = append(ordenats, cand{frag[loc[0]:loc[1]], frag[loc[1]:]})
		}
	}
	for _, loc := range alcadaNumRe.FindAllStringIndex(t, -1) {
		ordenats = append(ordenats, cand{t[loc[0]:loc[1]], t[loc[1]:]})
	}
	if len(ordenats) == 0 {
		return 0, false
	}

	total := 0
	trobat := false
	for _, c := range ordenats {
		cm, ok := alcadaValora(c.num, c.after)
		if !ok {
			continue
		}
		if cm < 100 || cm > 210 {
			continue
		}
		total = cm
		trobat = true
		break
	}
	if !trobat {
		return 0, false
	}

	// Estatura total: espatlles = total - 30 (estimació cap+coll).
	if esTotal {
		total -= 30
		if total < 100 || total > 210 {
			return 0, false
		}
	}
	if total < 100 || total > 210 {
		return 0, false
	}
	return total, true
}

// alcadaValora converteix un candidat de text a cm.
// after és el text posterior (per detectar sufix m/cm/cms/mt).
func alcadaValora(num, after string) (int, bool) {
	if strings.ContainsAny(num, ",.'’‘´") {
		// Amb separador decimal: sempre metres si v<10, sufix ignorat.
		norm := strings.NewReplacer(",", ".", "'", ".", "’", ".", "‘", ".", "´", ".").Replace(num)
		v, err := strconv.ParseFloat(norm, 64)
		if err != nil {
			return 0, false
		}
		if v < 10 {
			return int(math.Round(v * 100)), true
		}
		return int(math.Round(v)), true
	}
	v, err := strconv.Atoi(num)
	if err != nil {
		return 0, false
	}
	cm := v
	// Enter sense separador: si v<10 amb sufix de metres, metres; si no, cm.
	if v < 10 && strings.Contains(alcadaSufix(after), "m") {
		cm = v * 100
	}
	// Dígits de centenars omesos ("51"→151).
	if cm < 90 {
		cm += 100
	}
	return cm, true
}

// alcadaSufix recull les lletres darrere del número (tolerant amb espais).
func alcadaSufix(after string) string {
	i := 0
	for i < len(after) && (after[i] == ' ' || after[i] == '\t') {
		i++
	}
	j := i
	for j < len(after) && ((after[j] >= 'a' && after[j] <= 'z') || (after[j] >= 'A' && after[j] <= 'Z')) {
		j++
	}
	return strings.ToLower(after[i:j])
}
