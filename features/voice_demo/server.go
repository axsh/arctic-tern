package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// demoApp holds process-wide demo state. One concurrent call is enough for an Example.
type demoApp struct {
	mu         sync.Mutex
	bridge     *cawaBridge
	ternURL    string
	agent      string
	workDir    string
	openaiKey  string
	staticDir  string
}

// DemoConfig is used by main and by tests.
type DemoConfig struct {
	TernURL   string
	Agent     string
	WorkDir   string
	OpenAIKey string
	StaticDir string
}

// NewDemoMux builds the HTTP handlers for the voice demo.
// Exported so tests/ can mount the same routes against a mock CAWA URL.
func NewDemoMux(cfg DemoConfig) http.Handler {
	if cfg.Agent == "" {
		cfg.Agent = "wayfinder"
	}
	app := &demoApp{
		ternURL:   cfg.TernURL,
		agent:     cfg.Agent,
		workDir:   cfg.WorkDir,
		openaiKey: cfg.OpenAIKey,
		staticDir: cfg.StaticDir,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(cfg.StaticDir))))
	mux.HandleFunc("POST /api/call/start", app.handleCallStart)
	mux.HandleFunc("POST /api/call/end", app.handleCallEnd)
	mux.HandleFunc("POST /api/realtime/session", app.handleRealtimeSession)
	mux.HandleFunc("POST /api/utterance", app.handleUtterance)
	mux.HandleFunc("GET /api/status", app.handleStatus)
	return mux
}

func (a *demoApp) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(a.staticDir, "index.html"))
}

func (a *demoApp) handleCallStart(w http.ResponseWriter, r *http.Request) {
	workDir := a.workDir
	if workDir == "" {
		dir, err := os.MkdirTemp("", "voice-demo-work-*")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		workDir = dir
	}
	sessionDir, err := os.MkdirTemp("", "voice-demo-session-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	bridge := newCawaBridge(a.ternURL, a.agent, workDir, sessionDir)
	if err := bridge.Start(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	a.mu.Lock()
	if a.bridge != nil {
		_ = a.bridge.End(r.Context())
	}
	a.bridge = bridge
	a.mu.Unlock()

	_, _, _, _, sessionID := bridge.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID})
}

func (a *demoApp) handleCallEnd(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	bridge := a.bridge
	a.bridge = nil
	a.mu.Unlock()
	if bridge != nil {
		if err := bridge.End(r.Context()); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *demoApp) handleRealtimeSession(w http.ResponseWriter, r *http.Request) {
	raw, err := createRealtimeEphemeralSession(r.Context(), a.openaiKey, "", "")
	if err != nil {
		status := http.StatusBadGateway
		if err.Error() == "OPENAI_API_KEY is not set" {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (a *demoApp) handleUtterance(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
		return
	}

	a.mu.Lock()
	bridge := a.bridge
	a.mu.Unlock()
	if bridge == nil {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "call not started"})
		return
	}

	action, summary, err := bridge.HandleUtterance(r.Context(), req.Text)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":     false,
			"error":  err.Error(),
			"action": string(action),
		})
		return
	}
	busy, lastAction, lastSummary, interruptMode, _ := bridge.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"action":         string(action),
		"summary":        summary,
		"busy":           busy,
		"last_action":    string(lastAction),
		"last_summary":   lastSummary,
		"interrupt_mode": interruptMode,
	})
}

func (a *demoApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	bridge := a.bridge
	a.mu.Unlock()
	if bridge == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "busy": false, "last_action": "", "last_summary": "", "interrupt_mode": "", "session_id": "",
		})
		return
	}
	busy, lastAction, lastSummary, interruptMode, sessionID := bridge.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"busy":           busy,
		"last_action":    string(lastAction),
		"last_summary":   lastSummary,
		"interrupt_mode": interruptMode,
		"session_id":     sessionID,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
