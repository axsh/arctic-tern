package main

import (
	"strings"
	"testing"
)

func TestSummarizeEvent(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		content       string
		toolName      string
		interruptMode string
		wantContain   string
		wantEmpty     bool
		maxLen        bool
	}{
		{name: "text", eventType: "text", content: "hello world", wantContain: "Agent: hello world"},
		{name: "tool", eventType: "tool_use", toolName: "Bash", wantContain: "Running tool: Bash"},
		{name: "tool empty name", eventType: "tool_use", wantContain: "Running tool: tool"},
		{name: "result", eventType: "result", wantContain: "Turn finished"},
		{name: "interrupt fallback", eventType: "system", content: "turn interrupt", interruptMode: "fallback", wantContain: "Interrupted (fallback)"},
		{name: "interrupt native", eventType: "system", interruptMode: "native", wantContain: "Interrupted (native)"},
		{name: "ignored system", eventType: "system", content: "turn context", wantEmpty: true},
		{name: "huge text truncated", eventType: "text", content: strings.Repeat("x", 500), wantContain: "Agent:", maxLen: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeEvent(tt.eventType, tt.content, tt.toolName, tt.interruptMode)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("want empty, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContain) {
				t.Fatalf("got %q, want to contain %q", got, tt.wantContain)
			}
			if tt.maxLen {
				// + len("Agent: ") headroom; SummarizeEvent caps runes of the whole line.
				if len([]rune(got)) > MaxSummaryRunes {
					t.Fatalf("summary too long: %d runes", len([]rune(got)))
				}
				if strings.Contains(got, strings.Repeat("x", 200)) {
					t.Fatal("summary dumped too much of the original text")
				}
			}
		})
	}
}
