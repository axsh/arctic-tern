package agentservice

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const (
	// InterruptPromptHeader opens the prompt of a Fallback replan turn.
	InterruptPromptHeader = "Tern turn interrupt"
	// InterruptHistoryMarker starts the canonical-history user message recorded for an interrupt.
	InterruptHistoryMarker = "[Tern interrupt]"

	interruptPromptNotice = "Your previous turn was interrupted by the user before it completed. " +
		"Tern stopped it and recorded the progress observed so far (best effort; it may be incomplete). " +
		"Reconcile that progress with the user's instruction below: continue work that is still valid, " +
		"re-plan work that the instruction changes, and do not repeat completed steps unless the instruction requires it."
	interruptPromptProgressLabel = "Progress observed before the interruption:"
	interruptPromptFooter        = "End of interrupt context"
	interruptNoProgress          = "(no progress was observed before the interruption)"
	truncatedMarker              = "... [TRUNCATED]"

	interruptProgressMaxBytes     = 4096
	interruptProgressTextMaxBytes = 1536
	interruptProgressMaxTools     = 20
	interruptProgressPreviewBytes = 160
)

type interruptPromptInput struct {
	InterruptedTurnID string
	Progress          string
	Instruction       string
}

type interruptToolStep struct {
	name    string
	callID  string
	input   string
	done    bool
	result  string
}

// summarizeInterruptedTurn builds a deterministic, truncated summary of the
// progress observed on a stopped turn. Heartbeats and system/error events are ignored.
func summarizeInterruptedTurn(events []codingagent.StreamEvent) string {
	var assistant strings.Builder
	var steps []interruptToolStep

	for _, ev := range events {
		switch ev.Type {
		case codingagent.EventText:
			assistant.WriteString(ev.Content)
		case codingagent.EventToolUse:
			name := ev.ToolName
			if name == "" {
				name = "tool"
			}
			input := ""
			if len(ev.ToolInput) > 0 {
				b, err := json.Marshal(ev.ToolInput)
				if err == nil {
					input = previewOneLine(string(b), interruptProgressPreviewBytes)
				}
			}
			steps = append(steps, interruptToolStep{
				name:   name,
				callID: ev.ToolCallID,
				input:  input,
			})
		case codingagent.EventToolResult:
			idx := -1
			if ev.ToolCallID != "" {
				for i := len(steps) - 1; i >= 0; i-- {
					if !steps[i].done && steps[i].callID == ev.ToolCallID {
						idx = i
						break
					}
				}
			}
			if idx < 0 {
				for i := len(steps) - 1; i >= 0; i-- {
					if !steps[i].done {
						idx = i
						break
					}
				}
			}
			if idx >= 0 {
				steps[idx].done = true
				steps[idx].result = previewOneLine(ev.Content, interruptProgressPreviewBytes)
			}
		}
	}

	text := strings.TrimSpace(assistant.String())
	if text == "" && len(steps) == 0 {
		return interruptNoProgress
	}

	var b strings.Builder
	if text != "" {
		b.WriteString("Assistant output (most recent part):\n")
		b.WriteString(truncateUTF8Tail(text, interruptProgressTextMaxBytes))
		b.WriteByte('\n')
	}
	if len(steps) > 0 {
		b.WriteString("Tool calls:\n")
		if len(steps) > interruptProgressMaxTools {
			omitted := len(steps) - interruptProgressMaxTools
			fmt.Fprintf(&b, "- (%d earlier tool calls omitted)\n", omitted)
			steps = steps[len(steps)-interruptProgressMaxTools:]
		}
		for _, st := range steps {
			line := "- " + st.name
			if st.input != "" {
				line += " " + st.input
			}
			if st.done {
				line += " -> completed"
				if st.result != "" {
					line += ": " + st.result
				}
			} else {
				line += " -> interrupted while running (no result)"
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	return truncateUTF8Head(strings.TrimRight(b.String(), "\n"), interruptProgressMaxBytes)
}

// buildInterruptReplanPrompt lays out the Fallback replan agent prompt with
// interrupt context first and the user instruction last.
func buildInterruptReplanPrompt(in interruptPromptInput) string {
	progress := in.Progress
	if progress == "" {
		progress = interruptNoProgress
	}
	var b strings.Builder
	b.WriteString(InterruptPromptHeader)
	b.WriteString("\n\n")
	b.WriteString("interrupted_turn_id: ")
	b.WriteString(in.InterruptedTurnID)
	b.WriteString("\n\n")
	b.WriteString(interruptPromptNotice)
	b.WriteString("\n\n")
	b.WriteString(interruptPromptProgressLabel)
	b.WriteByte('\n')
	b.WriteString(progress)
	b.WriteString("\n\n")
	b.WriteString(interruptPromptFooter)
	b.WriteString("\n\n")
	b.WriteString(in.Instruction)
	return b.String()
}

// formatInterruptHistory builds the canonical-history user message body for an interrupt.
func formatInterruptHistory(interruptedTurnID, mode, instruction string) string {
	return fmt.Sprintf("%s interrupted_turn_id=%s mode=%s\n%s",
		InterruptHistoryMarker, interruptedTurnID, mode, instruction)
}

func previewOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		cut = max
	}
	return s[:cut] + "..."
}

func truncateUTF8Head(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		cut = max
	}
	return s[:cut] + "\n" + truncatedMarker
}

func truncateUTF8Tail(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	start := len(s) - max
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return truncatedMarker + "\n" + s[start:]
}
