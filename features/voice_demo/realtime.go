package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// openAIRealtimeClientSecretsURL mints a short-lived ephemeral key (ek_...).
// Older docs used POST /v1/realtime/sessions; that path now 404s for many keys.
var openAIRealtimeClientSecretsURL = "https://api.openai.com/v1/realtime/client_secrets"

// createRealtimeEphemeralSession asks OpenAI for a short-lived Realtime client secret.
// barge controls whether assistant audio is hard-interrupted on user speech
// (abrupt) or soft (natural: interrupt_response=false).
// vad controls how long mid-utterance silence is tolerated before the model replies.
func createRealtimeEphemeralSession(ctx context.Context, apiKey, model, voice string, barge BargeInParams, vad VADParams) ([]byte, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is not set")
	}
	if model == "" {
		model = "gpt-realtime"
	}
	if voice == "" {
		voice = "marin"
	}
	if barge.Mode == "" {
		barge = ResolveBargeIn("natural")
	}
	if vad.SilenceDurationMs == 0 && vad.PrefixPaddingMs == 0 && vad.Threshold == 0 {
		vad = ResolveVAD(VADInput{})
	}

	payload := map[string]any{
		"expires_after": map[string]any{
			"anchor":  "created_at",
			"seconds": 600,
		},
		"session": map[string]any{
			"type":         "realtime",
			"model":        model,
			"instructions": "You are the voice conversation layer for a Tern coding demo. Keep replies short. Do not read out long code or logs. Coding work is executed by a separate Coding Agent behind Tern; acknowledge task updates briefly when the user is told a short status.",
			"audio": map[string]any{
				"input": map[string]any{
					"transcription": map[string]any{
						"model": "gpt-4o-mini-transcribe",
					},
					// turn_detection.interrupt_response: when true, user speech
					// immediately cancels assistant audio (abrupt). natural sets false.
					// silence_duration_ms: wait through mid-utterance pauses before end-of-turn.
					"turn_detection": map[string]any{
						"type":                 "server_vad",
						"interrupt_response":   barge.InterruptResponse,
						"silence_duration_ms":  vad.SilenceDurationMs,
						"threshold":            vad.Threshold,
						"prefix_padding_ms":    vad.PrefixPaddingMs,
					},
				},
				"output": map[string]any{
					"voice": voice,
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIRealtimeClientSecretsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("openai realtime client_secrets HTTP %d: %s", resp.StatusCode, truncateRunes(string(raw), 300))
	}
	return raw, nil
}
