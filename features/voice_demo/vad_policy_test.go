package main

import "testing"

func TestResolveVAD(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got := ResolveVAD(VADInput{})
		if got.SilenceDurationMs != 900 || got.PrefixPaddingMs != 300 || got.Threshold != 0.5 {
			t.Fatalf("defaults = %+v", got)
		}
	})

	t.Run("passthrough", func(t *testing.T) {
		s, th, p := 1100, 0.7, 400
		got := ResolveVAD(VADInput{SilenceDurationMs: &s, Threshold: &th, PrefixPaddingMs: &p})
		if got.SilenceDurationMs != 1100 || got.Threshold != 0.7 || got.PrefixPaddingMs != 400 {
			t.Fatalf("passthrough = %+v", got)
		}
	})

	t.Run("clamp_low", func(t *testing.T) {
		s, th, p := 50, -0.2, -10
		got := ResolveVAD(VADInput{SilenceDurationMs: &s, Threshold: &th, PrefixPaddingMs: &p})
		if got.SilenceDurationMs != 200 || got.Threshold != 0 || got.PrefixPaddingMs != 0 {
			t.Fatalf("clamp_low = %+v", got)
		}
	})

	t.Run("clamp_high", func(t *testing.T) {
		s, th, p := 9000, 1.5, 5000
		got := ResolveVAD(VADInput{SilenceDurationMs: &s, Threshold: &th, PrefixPaddingMs: &p})
		if got.SilenceDurationMs != 2000 || got.Threshold != 1 || got.PrefixPaddingMs != 1000 {
			t.Fatalf("clamp_high = %+v", got)
		}
	})
}
