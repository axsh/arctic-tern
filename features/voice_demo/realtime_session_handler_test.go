package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleRealtimeSession_EchoesClampedVAD(t *testing.T) {
	var sawBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": "ek_test"})
	}))
	defer upstream.Close()

	prev := openAIRealtimeClientSecretsURL
	openAIRealtimeClientSecretsURL = upstream.URL + "/v1/realtime/client_secrets"
	defer func() { openAIRealtimeClientSecretsURL = prev }()

	staticDir := filepath.Join("static")
	mux := NewDemoMux(DemoConfig{
		TernURL:   "http://127.0.0.1:9",
		Agent:     "wayfinder",
		OpenAIKey: "sk-test",
		StaticDir: staticDir,
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"barge_in_mode":       "natural",
		"silence_duration_ms": 50,
		"threshold":           2.0,
		"prefix_padding_ms":   9000,
	})
	resp, err := http.Post(ts.URL+"/api/realtime/session", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("ok=%v err=%v", out["ok"], out["error"])
	}
	vad, _ := out["vad"].(map[string]any)
	if intFromAny(vad["silence_duration_ms"]) != 200 {
		t.Fatalf("echo silence=%v", vad["silence_duration_ms"])
	}
	if floatFromAny(vad["threshold"]) != 1 {
		t.Fatalf("echo threshold=%v", vad["threshold"])
	}
	if intFromAny(vad["prefix_padding_ms"]) != 1000 {
		t.Fatalf("echo prefix=%v", vad["prefix_padding_ms"])
	}
	td := turnDetectionFromBody(t, sawBody)
	if intFromAny(td["silence_duration_ms"]) != 200 {
		t.Fatalf("upstream silence=%v", td["silence_duration_ms"])
	}
}
