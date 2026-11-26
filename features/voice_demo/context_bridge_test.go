package main

import (
	"strings"
	"testing"
)

func TestBuildContextBridgeNote(t *testing.T) {
	if _, ok := BuildContextBridgeNote("  "); ok {
		t.Fatal("empty should skip")
	}
	note, ok := BuildContextBridgeNote("Turn started (messages)")
	if !ok || !strings.HasPrefix(note, contextBridgePrefix) {
		t.Fatalf("note=%q ok=%v", note, ok)
	}
	if !strings.Contains(note, "Turn started") {
		t.Fatalf("missing summary: %q", note)
	}
	errNote, ok := BuildContextBridgeNote("upstream_error: boom")
	if !ok || !strings.Contains(errNote, "upstream_error") {
		t.Fatalf("error note=%q", errNote)
	}
}
