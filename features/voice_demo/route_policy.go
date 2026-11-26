package main

import (
	"strings"
	"unicode/utf8"
)

// RouteMode controls whether finalized transcripts are sent to CAWA.
type RouteMode string

const (
	RouteAlways RouteMode = "always"
	RouteNever  RouteMode = "never"
	RouteAuto   RouteMode = "auto"
)

// DecideUtteranceRoute decides whether to call CAWA and which action to use.
func DecideUtteranceRoute(busy bool, text, mode string) (send bool, action Action, reason string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return false, ActionNone, "empty"
	}
	m := RouteMode(strings.ToLower(strings.TrimSpace(mode)))
	if m == "" {
		m = RouteAuto
	}
	switch m {
	case RouteNever:
		return false, ActionNone, "never"
	case RouteAlways:
		a, err := DecideUtteranceAction(busy, text)
		if err != nil {
			return false, ActionNone, "empty"
		}
		return true, a, "always"
	default: // auto
		if busy {
			if looksLikeWorkChange(text) {
				return true, ActionInterrupt, "auto_work_change"
			}
			return false, ActionNone, "auto_skip_busy_chat"
		}
		if looksLikeWork(text) {
			return true, ActionMessages, "auto_work"
		}
		return false, ActionNone, "auto_skip_chat"
	}
}

func looksLikeWork(text string) bool {
	if utf8.RuneCountInString(text) >= 40 {
		return true
	}
	lower := strings.ToLower(text)
	needles := []string{
		"please", "create", "file", "run", "sleep", "shell", "command",
		"write", "implement", "fix", "refactor", "test",
		"作って", "実行", "ファイル", "スリープ", "コマンド", "実装", "修正",
	}
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) || strings.Contains(text, n) {
			return true
		}
	}
	return false
}

func looksLikeWorkChange(text string) bool {
	if looksLikeWork(text) {
		return true
	}
	lower := strings.ToLower(text)
	needles := []string{
		"stop", "cancel", "interrupt", "abort", "instead",
		"やめて", "中止", "変更", "違う", "やり直し",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) || strings.Contains(text, n) {
			return true
		}
	}
	return false
}
