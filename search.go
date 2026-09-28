package main

import (
	"sort"
	"strings"
	"unicode"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Normalize lowercases, strips Catalan/Spanish accents, trims and collapses spaces.
func Normalize(s string) string {
	s = strings.ToLower(s)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, _ := transform.String(t, s)
	// ç/ñ survive NFD removal in some forms; map explicitly.
	out = strings.NewReplacer("ç", "c", "ñ", "n").Replace(out)
	out = strings.Join(strings.Fields(out), " ")
	return out
}

type scored struct {
	p    Person
	tier int // 0 exact substring, 1 token-prefix, 2 all-tokens, 3 fuzzy
	pos  int // substring index or fuzzy distance (lower = better)
}

// SearchPeople returns up to 50 best matches for query over people.
// Empty query returns first 50 (in sheet order).
func SearchPeople(people []Person, query string) []Person {
	if strings.TrimSpace(query) == "" {
		if len(people) > 50 {
			out := make([]Person, 50)
			copy(out, people[:50])
			return out
		}
		out := make([]Person, len(people))
		copy(out, people)
		return out
	}
	q := Normalize(query)
	qTokens := strings.Fields(q)

	var scoredList []scored
	for _, p := range people {
		combined := Normalize(p.Nom + " " + p.Alies)
		nameTokens := strings.Fields(combined)

		// Tier 0: exact substring of combined "nom alies".
		if idx := strings.Index(combined, q); idx >= 0 {
			scoredList = append(scoredList, scored{p, 0, idx})
			continue
		}
		// Tier 1: every query token is a prefix of some name token.
		if len(qTokens) > 0 {
			ok := true
			for _, qt := range qTokens {
				found := false
				for _, nt := range nameTokens {
					if strings.HasPrefix(nt, qt) {
						found = true
						break
					}
				}
				if !found {
					ok = false
					break
				}
			}
			if ok {
				scoredList = append(scoredList, scored{p, 1, len(combined)})
				continue
			}
		}
		// Tier 2: all query tokens present as substrings.
		if len(qTokens) > 0 {
			ok := true
			for _, qt := range qTokens {
				if !strings.Contains(combined, qt) {
					ok = false
					break
				}
			}
			if ok {
				scoredList = append(scoredList, scored{p, 2, len(combined)})
				continue
			}
		}
	}

	// Tier 3 fallback: fuzzy ranking on "nom + alies" for the rest.
	if len(scoredList) == 0 && len(people) > 0 {
		targets := make([]string, len(people))
		normTargets := make([]string, len(people))
		for i, p := range people {
			targets[i] = p.Nom + " " + p.Alies
			normTargets[i] = Normalize(targets[i])
		}
		ranks := fuzzy.RankFind(q, normTargets)
		// Map fuzzy hits back to people (targets are unique-ish; match by index).
		byTarget := map[string][]int{}
		for i, t := range normTargets {
			byTarget[t] = append(byTarget[t], i)
		}
		used := map[int]bool{}
		for _, r := range ranks {
			for _, i := range byTarget[r.Target] {
				if !used[i] {
					used[i] = true
					scoredList = append(scoredList, scored{people[i], 3, r.Distance})
					break
				}
			}
			if len(scoredList) >= 50 {
				break
			}
		}
	}

	sort.SliceStable(scoredList, func(i, j int) bool {
		if scoredList[i].tier != scoredList[j].tier {
			return scoredList[i].tier < scoredList[j].tier
		}
		return scoredList[i].pos < scoredList[j].pos
	})
	if len(scoredList) > 50 {
		scoredList = scoredList[:50]
	}
	out := make([]Person, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.p
	}
	return out
}
