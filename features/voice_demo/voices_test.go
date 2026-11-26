package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestVoicesHandler_ListsCuratedVoices(t *testing.T) {
	mux := NewDemoMux(DemoConfig{
		TernURL:   "http://127.0.0.1:9",
		StaticDir: filepath.Join("static"),
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/realtime/voices")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["ok"] != true || out["default"] != "marin" || out["source"] != "curated" {
		t.Fatalf("meta=%v", out)
	}
	voices, _ := out["voices"].([]any)
	if len(voices) < 10 {
		t.Fatalf("voices len=%d", len(voices))
	}
	need := map[string]bool{"marin": false, "cedar": false, "alloy": false}
	for _, raw := range voices {
		m, _ := raw.(map[string]any)
		id, _ := m["id"].(string)
		if _, ok := need[id]; ok {
			need[id] = true
		}
	}
	for id, ok := range need {
		if !ok {
			t.Fatalf("missing voice %s", id)
		}
	}
}
