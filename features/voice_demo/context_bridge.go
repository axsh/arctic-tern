package main

import "strings"

const (
	contextBridgePrefix = "Tern coding agent status: "
	contextBridgeMaxRunes = 280
)

// BuildContextBridgeNote builds a short note for Realtime conversation.item.create.
// Empty or whitespace-only summaries are skipped.
func BuildContextBridgeNote(summary string) (note string, ok bool) {
	s := strings.TrimSpace(summary)
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(s, contextBridgePrefix) {
		s = strings.TrimSpace(strings.TrimPrefix(s, contextBridgePrefix))
	}
	s = truncateRunes(s, contextBridgeMaxRunes-len([]rune(contextBridgePrefix)))
	if s == "" {
		return "", false
	}
	return contextBridgePrefix + s, true
}
