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

func TestBuildTurnCompleteReconsiderNote(t *testing.T) {
	if _, ok := BuildTurnCompleteReconsiderNote("q", "  "); ok {
		t.Fatal("empty summary should skip")
	}
	note, ok := BuildTurnCompleteReconsiderNote(
		"Summarize the README in the top folder",
		"README summary: Arctic Tern is a demo harness.",
	)
	if !ok {
		t.Fatal("expected ok")
	}
	for _, want := range []string{
		"Open question from the human",
		"Summarize the README in the top folder",
		"Coding agent result",
		"README summary: Arctic Tern is a demo harness.",
		"Reconsider (B)",
		"Do not say that results came back",
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("missing %q in note=%q", want, note)
		}
	}
	if strings.Contains(note, "Speak the final answer to the human now") {
		t.Fatal("must not force immediate spoken readout")
	}
	emptyQ, ok := BuildTurnCompleteReconsiderNote("", "Turn finished")
	if !ok || !strings.Contains(emptyQ, "(no open question recorded)") {
		t.Fatalf("empty question placeholder: ok=%v note=%q", ok, emptyQ)
	}
}
