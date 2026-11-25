package llm_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// liveInterruptInstruction is the replan instruction sent to a real agent while
// a blocking tool is in flight. It asks the agent to stop and reply with a
// fixed marker so the replan turn is observable and deterministic.
const liveInterruptInstruction = "Stop waiting immediately. Do not run any more commands or tools. Reply with exactly: INTERRUPT_OK"

// postInterruptSSE posts POST /interrupt as SSE and returns the response
// (caller must close the body).
func postInterruptSSE(t *testing.T, baseURL, sessionID, content string, timeout time.Duration) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"content": content})
	req, err := http.NewRequest(http.MethodPost,
		baseURL+"/api/v1/sessions/"+sessionID+"/interrupt",
		bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new interrupt request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("interrupt request: %v", err)
	}
	return resp
}

// assertInterruptHistoryRecorded verifies the canonical history contains the
// interrupt user record for interruptedTurnID with the replan instruction.
func assertInterruptHistoryRecorded(t *testing.T, sessionDir, interruptedTurnID string) {
	t.Helper()
	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatalf("load canonical history: %v", err)
	}
	prefix := agentservice.InterruptHistoryMarker + " interrupted_turn_id=" + interruptedTurnID
	for _, m := range msgs {
		if m.Role == "user" && strings.HasPrefix(m.Content, prefix) &&
			strings.Contains(m.Content, liveInterruptInstruction) {
			return
		}
	}
	t.Fatalf("canonical history missing interrupt record with prefix %q; msgs=%d", prefix, len(msgs))
}

// runLiveInterruptReplanOnce drives one real interrupt+replan cycle. It returns
// ok=false (retryable) only when the blocking tool never started before the
// interrupt could be issued. Any protocol/contract violation is a Fatal.
func runLiveInterruptReplanOnce(t *testing.T, baseURL, agent, model, workDir string, sleepSec int) (ok bool, reason string) {
	t.Helper()
	sessionID := createE2ESessionWithModel(t, baseURL, agent, model, workDir)
	stream := startCancelableToolSSE(t, baseURL, sessionID, sleepToolPrompt(sleepSec), 180*time.Second)

	select {
	case <-stream.toolInFlight:
		// Tool is running; interrupt while in-flight.
	case res := <-stream.finished:
		if res.err != nil {
			return false, fmt.Sprintf("SSE failed before tool started: %v", res.err)
		}
		return false, fmt.Sprintf("SSE finished before tool started; events=%v", summarizeEvents(res.events))
	case <-time.After(90 * time.Second):
		return false, "timed out waiting for the blocking tool to start"
	}

	info := getE2ESession(t, baseURL, sessionID)
	oldTurn, _ := info["turn_id"].(string)
	sessionDir, _ := info["session_dir"].(string)
	if oldTurn == "" {
		t.Fatalf("empty turn_id while tool is in flight; info=%v", info)
	}

	resp := postInterruptSSE(t, baseURL, sessionID, liveInterruptInstruction, 120*time.Second)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("interrupt status=%d body=%s", resp.StatusCode, b)
	}
	newEvents, newDone := parseE2ESSEEvents(t, resp)

	var sawFallbackSystem bool
	for _, ev := range newEvents {
		if ev.Type == codingagent.EventSystem && ev.Content == agentservice.InterruptEventContent {
			sawFallbackSystem = true
			if ev.InterruptMode != agentservice.InterruptModeFallback {
				t.Fatalf("interrupt_mode = %q, want %q", ev.InterruptMode, agentservice.InterruptModeFallback)
			}
			if ev.InterruptedTurnID != oldTurn {
				t.Fatalf("interrupted_turn_id = %q, want %q", ev.InterruptedTurnID, oldTurn)
			}
			if ev.TurnID == "" || ev.TurnID == oldTurn {
				t.Fatalf("replan turn_id = %q, old = %q", ev.TurnID, oldTurn)
			}
		}
	}
	if !sawFallbackSystem {
		t.Fatalf("replan stream missing fallback system event; events=%v", summarizeEvents(newEvents))
	}
	assertTurnTerminal(t, newEvents, newDone)
	if hasEventType(newEvents, codingagent.EventError) {
		t.Fatalf("replan stream carried an error; events=%v", summarizeEvents(newEvents))
	}
	if !hasEventType(newEvents, codingagent.EventResult) {
		t.Fatalf("replan stream missing result; events=%v", summarizeEvents(newEvents))
	}

	select {
	case res := <-stream.finished:
		if res.err != nil {
			t.Fatalf("old SSE after interrupt: %v", res.err)
		}
		var sawInterrupted bool
		for _, ev := range res.events {
			if ev.Type == codingagent.EventError && ev.Content == agentservice.TurnInterruptedContent {
				if ev.TurnID != oldTurn {
					t.Fatalf("old error turn_id = %q, want %q", ev.TurnID, oldTurn)
				}
				sawInterrupted = true
			}
		}
		if !sawInterrupted {
			t.Fatalf("old stream missing %q terminal; events=%v",
				agentservice.TurnInterruptedContent, summarizeEvents(res.events))
		}
		if !res.done {
			t.Fatal("old stream missing [DONE]")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("old SSE did not finish within 30s after interrupt")
	}

	final := getE2ESession(t, baseURL, sessionID)
	if status, _ := final["status"].(string); status != codingagent.StatusCompleted {
		t.Fatalf("final status = %q, want %q", status, codingagent.StatusCompleted)
	}
	if errField, _ := final["error"].(string); errField != "" {
		t.Fatalf("final error = %q, want empty", errField)
	}
	if followable, _ := final["followable"].(bool); followable {
		t.Fatal("session should not be followable after the replan completes")
	}

	assertInterruptHistoryRecorded(t, sessionDir, oldTurn)
	return true, ""
}

// runLiveInterruptReplan runs the interrupt replan cycle, retrying once with a
// longer tool duration when the tool failed to start in time.
func runLiveInterruptReplan(t *testing.T, baseURL, agent, model string) {
	t.Helper()
	ok, reason := runLiveInterruptReplanOnce(t, baseURL, agent, model, liveWorkDir(t), 45)
	if !ok {
		t.Logf("first interrupt attempt aborted early: %s; retrying once", reason)
		ok, reason = runLiveInterruptReplanOnce(t, baseURL, agent, model, liveWorkDir(t), 60)
	}
	if !ok {
		t.Fatalf("interrupt replan requires an in-flight tool before interrupt: %s", reason)
	}
}

// TestLiveTurnInterrupt_CodexReplan interrupts a real in-flight Codex tool and
// verifies the Fallback replan turn runs on the same session.
func TestLiveTurnInterrupt_CodexReplan(t *testing.T) {
	requireLiveCodexCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()
	runLiveInterruptReplan(t, baseURL, "codex", liveCodexHeartbeatModel)
}

// TestLiveTurnInterrupt_ClaudeCodeReplan verifies the same contract with Claude Code.
func TestLiveTurnInterrupt_ClaudeCodeReplan(t *testing.T) {
	requireLiveClaudeCLI(t)
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()
	runLiveInterruptReplan(t, baseURL, "claudecode", e2eDefaultModel)
}

// TestLiveTurnInterrupt_WayfinderReplan verifies the same contract with Wayfinder
// (no external CLI; uses the LLM Gateway directly).
func TestLiveTurnInterrupt_WayfinderReplan(t *testing.T) {
	baseURL, cleanup := startHeartbeatCancelLiveServer(t)
	defer cleanup()
	runLiveInterruptReplan(t, baseURL, "wayfinder", e2eDefaultModel)
}
