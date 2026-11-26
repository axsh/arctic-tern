package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRealtimeEphemeralSession_MissingKey(t *testing.T) {
	_, err := createRealtimeEphemeralSession(context.Background(), "", "gpt-4o-realtime-preview", "verse")
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is empty")
	}
	if !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("error should mention OPENAI_API_KEY, got %v", err)
	}
}

func TestCreateRealtimeEphemeralSession_PostsToOpenAI(t *testing.T) {
	var sawAuth string
	var sawPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "sess_test",
			"client_secret": map[string]any{
				"value":      "ek_test",
				"expires_at": 1,
			},
		})
	}))
	defer ts.Close()

	prev := openAIRealtimeSessionsURL
	openAIRealtimeSessionsURL = ts.URL + "/v1/realtime/sessions"
	defer func() { openAIRealtimeSessionsURL = prev }()

	raw, err := createRealtimeEphemeralSession(context.Background(), "sk-test", "gpt-4o-realtime-preview", "verse")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sawAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if !strings.HasSuffix(sawPath, "/v1/realtime/sessions") {
		t.Fatalf("path = %q", sawPath)
	}
	if !strings.Contains(string(raw), "ek_test") {
		t.Fatalf("body = %s", raw)
	}
}
