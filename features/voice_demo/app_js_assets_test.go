package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppJSResumesRemoteAudioAfterBargeIn(t *testing.T) {
	path := filepath.Join("static", "app.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	js := string(raw)

	if strings.Contains(js, "fadeAndPauseRemoteAudio") {
		t.Fatal("app.js still references fadeAndPauseRemoteAudio; rename to softenRemoteAudioOnBargeIn")
	}
	if !strings.Contains(js, "function softenRemoteAudioOnBargeIn") {
		t.Fatal("app.js missing softenRemoteAudioOnBargeIn")
	}
	if !strings.Contains(js, "function ensureRemoteAudioPlaying") {
		t.Fatal("app.js missing ensureRemoteAudioPlaying")
	}

	softenBody := functionBody(t, js, "softenRemoteAudioOnBargeIn")
	if strings.Contains(softenBody, ".pause(") {
		t.Fatal("softenRemoteAudioOnBargeIn must not call .pause( (leaves remote audio silent)")
	}

	ensureBody := functionBody(t, js, "ensureRemoteAudioPlaying")
	if !strings.Contains(ensureBody, ".play(") {
		t.Fatal("ensureRemoteAudioPlaying must call .play(")
	}
	if !strings.Contains(ensureBody, ".volume") {
		t.Fatal("ensureRemoteAudioPlaying must restore .volume")
	}
	if !strings.Contains(ensureBody, "resumeLoggedForResponse") {
		t.Fatal("ensureRemoteAudioPlaying must gate resume playback log with resumeLoggedForResponse")
	}

	if !strings.Contains(js, `input_audio_buffer.speech_started`) ||
		!strings.Contains(js, "softenRemoteAudioOnBargeIn()") {
		t.Fatal("speech_started must call softenRemoteAudioOnBargeIn()")
	}
	if !strings.Contains(js, "ensureRemoteAudioPlaying(") {
		t.Fatal("assistant audio start must call ensureRemoteAudioPlaying(")
	}
	if !strings.Contains(js, "response.created") && !strings.Contains(js, "output_audio_buffer.started") {
		t.Fatal("app.js must hook response.created or output_audio_buffer.started")
	}
}

func TestAppJSActiveResponseCancelAndBridge(t *testing.T) {
	path := filepath.Join("static", "app.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	js := string(raw)

	if !strings.Contains(js, "function cancelActiveRealtimeResponse") {
		t.Fatal("app.js missing cancelActiveRealtimeResponse")
	}
	cancelBody := functionBody(t, js, "cancelActiveRealtimeResponse")
	if !strings.Contains(cancelBody, "response.cancel") {
		t.Fatal("cancelActiveRealtimeResponse must send response.cancel")
	}
	if !strings.Contains(js, "realtimeResponseActive") {
		t.Fatal("app.js must track realtimeResponseActive")
	}
	if !strings.Contains(js, `input_audio_buffer.speech_started`) ||
		!strings.Contains(js, "cancelActiveRealtimeResponse()") {
		t.Fatal("speech_started must call cancelActiveRealtimeResponse()")
	}
	if !strings.Contains(js, "conversation_already_has_active_response") {
		t.Fatal("app.js must handle conversation_already_has_active_response")
	}
	if !strings.Contains(js, "function maybeBridgeSummary") {
		t.Fatal("app.js missing maybeBridgeSummary")
	}
	bridgeBody := functionBody(t, js, "maybeBridgeSummary")
	if !strings.Contains(bridgeBody, "conversation.item.create") {
		t.Fatal("maybeBridgeSummary must create conversation.item")
	}
	if strings.Contains(bridgeBody, "response.create") {
		t.Fatal("maybeBridgeSummary must not auto response.create")
	}
	if !strings.Contains(js, "route_mode") {
		t.Fatal("app.js must post route_mode on utterance")
	}
	if !strings.Contains(js, "/api/realtime/voices") {
		t.Fatal("app.js must load voices from /api/realtime/voices")
	}
}

// functionBody returns the source between "function name" and the next top-level "function ".
func functionBody(t *testing.T, src, name string) string {
	t.Helper()
	startMarker := "function " + name
	start := strings.Index(src, startMarker)
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	rest := src[start:]
	// Skip past the opening brace of this function.
	brace := strings.Index(rest, "{")
	if brace < 0 {
		t.Fatalf("function %s has no opening brace", name)
	}
	after := rest[brace+1:]
	next := strings.Index(after, "\nfunction ")
	if next < 0 {
		return after
	}
	return after[:next]
}
