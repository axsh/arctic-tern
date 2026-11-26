package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAIRealtimeSessionsURL is the Sessions API used to mint an ephemeral key
// so the browser can speak to OpenAI Realtime without seeing our long-lived key.
// Tests may override this to point at httptest.
var openAIRealtimeSessionsURL = "https://api.openai.com/v1/realtime/sessions"

// createRealtimeEphemeralSession asks OpenAI for a short-lived Realtime session.
// The browser uses the returned client_secret for WebRTC; the Coding Agent is
// still driven only through Tern/CAWA on this demo server.
func createRealtimeEphemeralSession(ctx context.Context, apiKey, model, voice string) ([]byte, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	if model == "" {
		model = "gpt-4o-realtime-preview"
	}
	if voice == "" {
		voice = "verse"
	}

	// Instructions keep Realtime in the "conversation UX" role.
	// File edits and shell commands must go through CAWA, not through this model.
	body := fmt.Sprintf(`{
  "model": %q,
  "voice": %q,
  "modalities": ["audio", "text"],
  "instructions": "You are the voice conversation layer for a Tern coding demo. Keep replies short. Do not read out long code or logs. Coding work is executed by a separate Coding Agent behind Tern; acknowledge task updates briefly when the user is told a short status."
}`, model, voice)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIRealtimeSessionsURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	// OpenAI Realtime session create historically wanted this beta header.
	req.Header.Set("OpenAI-Beta", "realtime=v1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai realtime sessions HTTP %d: %s", resp.StatusCode, truncateRunes(string(raw), 300))
	}
	return raw, nil
}
