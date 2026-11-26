package main

import "testing"

func TestResolveBargeIn(t *testing.T) {
	tests := []struct {
		in           string
		wantMode     BargeInMode
		wantIR       bool
		wantFade     int
		wantLabel    string
	}{
		{in: "natural", wantMode: BargeInNatural, wantIR: false, wantFade: 180, wantLabel: "soft_vad+fade"},
		{in: "", wantMode: BargeInNatural, wantIR: false, wantFade: 180, wantLabel: "soft_vad+fade"},
		{in: " NATURAL ", wantMode: BargeInNatural, wantIR: false, wantFade: 180, wantLabel: "soft_vad+fade"},
		{in: "abrupt", wantMode: BargeInAbrupt, wantIR: true, wantFade: 0, wantLabel: "abrupt_cut"},
		{in: "Abrupt", wantMode: BargeInAbrupt, wantIR: true, wantFade: 0, wantLabel: "abrupt_cut"},
		{in: "weird", wantMode: BargeInNatural, wantIR: false, wantFade: 180, wantLabel: "soft_vad+fade"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := ResolveBargeIn(tt.in)
			if got.Mode != tt.wantMode || got.InterruptResponse != tt.wantIR || got.FadeMs != tt.wantFade || got.StopLabel != tt.wantLabel {
				t.Fatalf("ResolveBargeIn(%q)=%+v", tt.in, got)
			}
		})
	}
}
