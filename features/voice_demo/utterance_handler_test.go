package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockCAWA counts messages / interrupt / cancel and can hang a messages SSE
// until interrupt is observed so the demo bridge stays busy.
func startMockCAWA(t *testing.T) (baseURL string, messages, interrupts, cancels *atomic.Int32) {
	t.Helper()
	messages = &atomic.Int32{}
	interrupts = &atomic.Int32{}
	cancels = &atomic.Int32{}

	var mu sync.Mutex
	hangCh := make(chan struct{})
	hangOnce := sync.Once{}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"session_id": "sess-demo"})
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		messages.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"type\":\"text\",\"content\":\"working\"}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		select {
		case <-hangCh:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"result\"}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/interrupt", func(w http.ResponseWriter, r *http.Request) {
		interrupts.Add(1)
		hangOnce.Do(func() {
			mu.Lock()
			close(hangCh)
			mu.Unlock()
		})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"system\",\"content\":\"turn interrupt\",\"interrupt_mode\":\"fallback\",\"interrupted_turn_id\":\"old\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"text\",\"content\":\"replanned\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"result\"}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		cancels.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/sessions/{id}/terminate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL, messages, interrupts, cancels
}

func TestUtteranceHandler_IdleMessagesBusyInterrupt(t *testing.T) {
	cawaURL, messages, interrupts, cancels := startMockCAWA(t)
	staticDir := filepath.Join("static")
	demo := httptest.NewServer(NewDemoMux(DemoConfig{
		TernURL:   cawaURL,
		Agent:     "wayfinder",
		WorkDir:   t.TempDir(),
		OpenAIKey: "",
		StaticDir: staticDir,
	}))
	t.Cleanup(demo.Close)

	postJSON := func(path string, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		resp, err := http.Post(demo.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			t.Fatalf("POST %s status=%d body=%s", path, resp.StatusCode, b)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("decode %s: %v body=%s", path, err, b)
		}
		return out
	}

	start := postJSON("/api/call/start", map[string]any{})
	if start["ok"] != true {
		t.Fatalf("start=%v", start)
	}

	u1 := postJSON("/api/utterance", map[string]string{"text": "run a long task"})
	if u1["action"] != "messages" {
		t.Fatalf("first action=%v want messages", u1["action"])
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if messages.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if messages.Load() != 1 {
		t.Fatalf("messages=%d want 1", messages.Load())
	}

	// Wait until bridge marks busy (stream opened).
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, err := http.Get(demo.URL + "/api/status")
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		var status map[string]any
		_ = json.NewDecoder(st.Body).Decode(&status)
		st.Body.Close()
		if status["busy"] == true {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	u2 := postJSON("/api/utterance", map[string]string{"text": "change of plan: only create b.txt"})
	if u2["action"] != "interrupt" {
		t.Fatalf("second action=%v want interrupt body=%v", u2["action"], u2)
	}
	if interrupts.Load() != 1 {
		t.Fatalf("interrupts=%d want 1", interrupts.Load())
	}
	if cancels.Load() != 0 {
		t.Fatalf("cancels=%d want 0 (barge-in must not cancel)", cancels.Load())
	}
	if messages.Load() != 1 {
		t.Fatalf("messages=%d want 1 (no second messages on barge-in)", messages.Load())
	}

	// Missing key path for realtime.
	resp, err := http.Post(demo.URL+"/api/realtime/session", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("realtime: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("realtime status=%d want 503", resp.StatusCode)
	}
}
