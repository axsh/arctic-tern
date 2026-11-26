package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestCawaBridge_CreateSessionPostsWorkspaceWrite(t *testing.T) {
	var sawBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &sawBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-sandbox"})
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/terminate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	b := newCawaBridge(ts.URL, "codex", t.TempDir(), t.TempDir())
	if err := b.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = b.End(context.Background()) })

	if sawBody["sandbox_mode"] != v1.SandboxModeWorkspaceWrite {
		t.Fatalf("sandbox_mode = %#v, want %q", sawBody["sandbox_mode"], v1.SandboxModeWorkspaceWrite)
	}
}
