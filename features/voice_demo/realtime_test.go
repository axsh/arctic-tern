package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRealtimeEphemeralSession_MissingKey(t *testing.T) {
	_, err := createRealtimeEphemeralSession(context.Background(), "", "gpt-realtime", "marin", 1.0, ResolveBargeIn("natural"), ResolveVAD(VADInput{}))
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is empty")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("error should mention OPENAI_API_KEY, got %v", err)
	}
}

func TestCreateRealtimeEphemeralSession_PostsNaturalInterruptResponse(t *testing.T) {
	var sawPath string
	var sawBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "ek_test"})
	}))
	defer ts.Close()

	prev := openAIRealtimeClientSecretsURL
	openAIRealtimeClientSecretsURL = ts.URL + "/v1/realtime/client_secrets"
	defer func() { openAIRealtimeClientSecretsURL = prev }()

	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "marin", 1.0, ResolveBargeIn("natural"), ResolveVAD(VADInput{}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasSuffix(sawPath, "/v1/realtime/client_secrets") {
		t.Fatalf("path = %q", sawPath)
	}
	ir := interruptResponseFromBody(t, sawBody)
	if ir != false {
		t.Fatalf("natural interrupt_response=%v want false", ir)
	}
}

func TestCreateRealtimeEphemeralSession_PostsAbruptInterruptResponse(t *testing.T) {
	var sawBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "ek_test"})
	}))
	defer ts.Close()

	prev := openAIRealtimeClientSecretsURL
	openAIRealtimeClientSecretsURL = ts.URL + "/v1/realtime/client_secrets"
	defer func() { openAIRealtimeClientSecretsURL = prev }()

	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "marin", 1.0, ResolveBargeIn("abrupt"), ResolveVAD(VADInput{}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ir := interruptResponseFromBody(t, sawBody)
	if ir != true {
		t.Fatalf("abrupt interrupt_response=%v want true", ir)
	}
}

func TestCreateRealtimeEphemeralSession_PostsVoiceAndSpeed(t *testing.T) {
	var sawBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "ek_test"})
	}))
	defer ts.Close()

	prev := openAIRealtimeClientSecretsURL
	openAIRealtimeClientSecretsURL = ts.URL + "/v1/realtime/client_secrets"
	defer func() { openAIRealtimeClientSecretsURL = prev }()

	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "cedar", 1.25, ResolveBargeIn("natural"), ResolveVAD(VADInput{}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	session, _ := sawBody["session"].(map[string]any)
	audio, _ := session["audio"].(map[string]any)
	out, _ := audio["output"].(map[string]any)
	if out["voice"] != "cedar" {
		t.Fatalf("voice=%v", out["voice"])
	}
	if floatFromAny(out["speed"]) != 1.25 {
		t.Fatalf("speed=%v", out["speed"])
	}
}

func TestCreateRealtimeEphemeralSession_PostsVADParams(t *testing.T) {
	var sawBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "ek_test"})
	}))
	defer ts.Close()

	prev := openAIRealtimeClientSecretsURL
	openAIRealtimeClientSecretsURL = ts.URL + "/v1/realtime/client_secrets"
	defer func() { openAIRealtimeClientSecretsURL = prev }()

	s, th, p := 1200, 0.6, 250
	vad := ResolveVAD(VADInput{SilenceDurationMs: &s, Threshold: &th, PrefixPaddingMs: &p})
	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "marin", 1.0, ResolveBargeIn("natural"), vad)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	td := turnDetectionFromBody(t, sawBody)
	if intFromAny(td["silence_duration_ms"]) != 1200 {
		t.Fatalf("silence_duration_ms=%v", td["silence_duration_ms"])
	}
	if floatFromAny(td["threshold"]) != 0.6 {
		t.Fatalf("threshold=%v", td["threshold"])
	}
	if intFromAny(td["prefix_padding_ms"]) != 250 {
		t.Fatalf("prefix_padding_ms=%v", td["prefix_padding_ms"])
	}
}

func interruptResponseFromBody(t *testing.T, body map[string]any) bool {
	t.Helper()
	td := turnDetectionFromBody(t, body)
	v, ok := td["interrupt_response"].(bool)
	if !ok {
		t.Fatalf("interrupt_response missing: %#v", body)
	}
	return v
}

func turnDetectionFromBody(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	session, _ := body["session"].(map[string]any)
	audio, _ := session["audio"].(map[string]any)
	input, _ := audio["input"].(map[string]any)
	td, _ := input["turn_detection"].(map[string]any)
	if td == nil {
		t.Fatalf("turn_detection missing: %#v", body)
	}
	return td
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return -1
	}
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return -1
	}
}
