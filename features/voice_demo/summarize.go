package main

import "unicode/utf8"

// MaxSummaryRunes caps status lines we push to the UI / Realtime TTS.
// Reading full SSE dumps aloud makes the call unusable (investigation 015).
const MaxSummaryRunes = 120

// SummarizeEvent maps one CAWA SSE-ish field set into a short status line.
// Unknown / noisy events return "" so the demo does not speak them.
func SummarizeEvent(eventType, content, toolName, interruptMode string) string {
	var out string
	switch eventType {
	case "text":
		out = "Agent: " + content
	case "tool_use":
		name := toolName
		if name == "" {
			name = "tool"
		}
		out = "Running tool: " + name
	case "result":
		out = "Turn finished"
	case "system":
		if interruptMode != "" {
			out = "Interrupted (" + interruptMode + ")"
		} else if content == "turn interrupt" {
			out = "Interrupted"
		} else {
			return ""
		}
	default:
		return ""
	}
	return truncateRunes(out, MaxSummaryRunes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
