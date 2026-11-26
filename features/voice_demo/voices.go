package main

import "net/http"

// curatedRealtimeVoices matches OpenAI Realtime built-in voice ids (docs).
var curatedRealtimeVoices = []map[string]any{
	{"id": "marin", "label": "Marin", "recommended": true},
	{"id": "cedar", "label": "Cedar", "recommended": true},
	{"id": "alloy", "label": "Alloy"},
	{"id": "ash", "label": "Ash"},
	{"id": "ballad", "label": "Ballad"},
	{"id": "coral", "label": "Coral"},
	{"id": "echo", "label": "Echo"},
	{"id": "sage", "label": "Sage"},
	{"id": "shimmer", "label": "Shimmer"},
	{"id": "verse", "label": "Verse"},
}

const defaultRealtimeVoice = "marin"

func (a *demoApp) handleVoices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"default": defaultRealtimeVoice,
		"voices":  curatedRealtimeVoices,
		"source":  "curated",
	})
}

func isCuratedVoice(id string) bool {
	for _, v := range curatedRealtimeVoices {
		if v["id"] == id {
			return true
		}
	}
	return false
}
