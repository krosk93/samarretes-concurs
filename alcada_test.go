package main

import "testing"

func TestNormalizeAlcada(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"1,55", 155, true},
		{"1.65", 165, true},
		{"1'62", 162, true},
		{"1’25", 125, true},
		{"1.25", 125, true},
		{"1,5", 150, true},
		{"1,4", 140, true},
		{"1,8", 180, true},
		{"1,7", 170, true},
		{"1.50m", 150, true},
		{"1,46m", 146, true},
		{"1'55mt", 155, true},
		{"1,55 m aprox", 155, true},
		{"1,40 aprox", 140, true},
		{"+- 1'55", 155, true},
		{"~145", 145, true},
		{"1,65 APROX", 165, true},
		{"1’40cm?", 140, true},
		{"1,30 cm", 130, true},
		{"1,58cm", 158, true},
		{"1'85cm", 185, true},
		{"1,33cm", 133, true},
		{"140cm", 140, true},
		{"155", 155, true},
		{"160 cm", 160, true},
		{"140cms", 140, true},
		{"132 cms", 132, true},
		{"165 cm", 165, true},
		{"175cm", 175, true},
		{"125cm", 125, true},
		{"120", 120, true},
		{"170", 170, true},
		{"Sóc un 14 (154cm)", 154, true},
		{"51", 151, true},
		{"60 i algo", 160, true},
		{"186 total", 156, true},
		{"Alçada \"total\" 1,83", 153, true},
		{"Mido 1,82 completo", 152, true},
		{"1,71m en total, fins les espatlles no ho sé...", 141, true},
		{"", 0, false},
		{"   ", 0, false},
		{"abc", 0, false},
		{"300", 0, false},
		{"95", 0, false},
		{"250", 0, false},
		{"1,30 m", 130, true},
	}
	for _, tt := range tests {
		got, ok := NormalizeAlcada(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("NormalizeAlcada(%q) = (%d,%v), want (%d,%v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
