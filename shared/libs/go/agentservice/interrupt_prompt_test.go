package agentservice

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestSummarizeInterruptedTurn(t *testing.T) {
	tests := []struct {
		name        string
		events      []codingagent.StreamEvent
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "no events",
			events:      nil,
			wantContain: []string{interruptNoProgress},
		},
		{
			name: "heartbeat only is not progress",
			events: []codingagent.StreamEvent{
				{Type: codingagent.EventProgress, Content: "tool_still_running"},
			},
			wantContain: []string{interruptNoProgress},
			wantAbsent:  []string{"tool_still_running"},
		},
		{
			name: "assistant text",
			events: []codingagent.StreamEvent{
				{Type: codingagent.EventText, Content: "hello "},
				{Type: codingagent.EventText, Content: "world"},
			},
			wantContain: []string{"Assistant output (most recent part):", "hello world"},
			wantAbsent:  []string{"Tool calls:"},
		},
		{
			name: "tool still running",
			events: []codingagent.StreamEvent{
				{
					Type:     codingagent.EventToolUse,
					ToolName: "command_execution",
					ToolInput: map[string]any{"command": "sleep 60"},
				},
			},
			wantContain: []string{`- command_execution {"command":"sleep 60"} -> interrupted while running (no result)`},
		},
		{
			name: "tool completed",
			events: []codingagent.StreamEvent{
				{
					Type:      codingagent.EventToolUse,
					ToolName:  "Write",
					ToolInput: map[string]any{"file_path": "a.txt"},
				},
				{Type: codingagent.EventToolResult, Content: "ok"},
			},
			wantContain: []string{`- Write {"file_path":"a.txt"} -> completed: ok`},
		},
		{
			name: "tool_call_id matches the right call",
			events: []codingagent.StreamEvent{
				{Type: codingagent.EventToolUse, ToolName: "A", ToolCallID: "id1"},
				{Type: codingagent.EventToolUse, ToolName: "B", ToolCallID: "id2"},
				{Type: codingagent.EventToolResult, ToolCallID: "id1", Content: "r1"},
			},
			wantContain: []string{
				"- A -> completed: r1",
				"- B -> interrupted while running (no result)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeInterruptedTurn(tt.events)
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Fatalf("summary missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(got, absent) {
					t.Fatalf("summary unexpectedly contains %q:\n%s", absent, got)
				}
			}
		})
	}

	t.Run("many tools", func(t *testing.T) {
		events := make([]codingagent.StreamEvent, 0, 50)
		for i := 0; i < 25; i++ {
			name := fmt.Sprintf("T%d", i)
			events = append(events,
				codingagent.StreamEvent{Type: codingagent.EventToolUse, ToolName: name},
				codingagent.StreamEvent{Type: codingagent.EventToolResult, Content: "ok"},
			)
		}
		got := summarizeInterruptedTurn(events)
		if !strings.Contains(got, "- (5 earlier tool calls omitted)") {
			t.Fatalf("missing omit marker:\n%s", got)
		}
		toolLines := 0
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, "- T") {
				toolLines++
			}
		}
		if toolLines != 20 {
			t.Fatalf("tool lines = %d, want 20\n%s", toolLines, got)
		}
		if strings.Contains(got, "- T0 ") || strings.Contains(got, "- T0 ->") {
			t.Fatalf("earliest tools should be omitted:\n%s", got)
		}
		if !strings.Contains(got, "- T24 -> completed: ok") {
			t.Fatalf("latest tool missing:\n%s", got)
		}
	})
}

func TestSummarizeInterruptedTurn_TruncatesMultibyteSafely(t *testing.T) {
	text := strings.Repeat("あ", 3000)
	got := summarizeInterruptedTurn([]codingagent.StreamEvent{
		{Type: codingagent.EventText, Content: text},
	})
	if !utf8.ValidString(got) {
		t.Fatal("summary is not valid UTF-8")
	}
	if !strings.Contains(got, truncatedMarker) {
		t.Fatalf("missing truncated marker:\n%s", got)
	}
	max := interruptProgressMaxBytes + len("\n"+truncatedMarker)
	if len(got) > max {
		t.Fatalf("len=%d > max=%d", len(got), max)
	}
}

func TestBuildInterruptReplanPrompt_Layout(t *testing.T) {
	got := buildInterruptReplanPrompt(interruptPromptInput{
		InterruptedTurnID: "t-old",
		Progress:          "PROGRESS",
		Instruction:       "change of plan",
	})
	wantParts := []string{
		InterruptPromptHeader + "\n\n",
		"interrupted_turn_id: t-old\n\n",
		interruptPromptNotice + "\n\n",
		interruptPromptProgressLabel + "\nPROGRESS\n\n",
		interruptPromptFooter + "\n\n",
		"change of plan",
	}
	want := strings.Join(wantParts, "")
	if got != want {
		t.Fatalf("prompt mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if !strings.HasSuffix(got, "change of plan") {
		t.Fatal("instruction must be last")
	}
}

func TestFormatInterruptHistory(t *testing.T) {
	got := formatInterruptHistory("t-old", "fallback", "change of plan")
	want := "[Tern interrupt] interrupted_turn_id=t-old mode=fallback\nchange of plan"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
