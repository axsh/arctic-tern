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
	_, err := createRealtimeEphemeralSession(context.Background(), "", "gpt-realtime", "marin", ResolveBargeIn("natural"))
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

	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "marin", ResolveBargeIn("natural"))
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

	_, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-realtime", "marin", ResolveBargeIn("abrupt"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	ir := interruptResponseFromBody(t, sawBody)
	if ir != true {
		t.Fatalf("abrupt interrupt_response=%v want true", ir)
	}
}

func interruptResponseFromBody(t *testing.T, body map[string]any) bool {
	t.Helper()
	session, _ := body["session"].(map[string]any)
	audio, _ := session["audio"].(map[string]any)
	input, _ := audio["input"].(map[string]any)
	td, _ := input["turn_detection"].(map[string]any)
	v, ok := td["interrupt_response"].(bool)
	if !ok {
		t.Fatalf("interrupt_response missing: %#v", body)
	}
	return v
}
