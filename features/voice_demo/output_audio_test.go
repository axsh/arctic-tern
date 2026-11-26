package main

import "testing"

func TestResolveOutputAudio(t *testing.T) {
	v, s := ResolveOutputAudio("", 0)
	if v != "marin" || s != 1.0 {
		t.Fatalf("defaults voice=%s speed=%v", v, s)
	}
	v, s = ResolveOutputAudio("cedar", 1.25)
	if v != "cedar" || s != 1.25 {
		t.Fatalf("passthrough voice=%s speed=%v", v, s)
	}
	v, s = ResolveOutputAudio("nope", 9)
	if v != "marin" || s != 1.5 {
		t.Fatalf("clamp voice=%s speed=%v", v, s)
	}
	_, s = ResolveOutputAudio("alloy", 0.1)
	if s != 0.25 {
		t.Fatalf("min speed=%v", s)
	}
}
