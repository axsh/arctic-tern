package agentservice_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

func TestInterrupt_FallbackReplacesInFlightTurn(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	_, ts := newScriptServer(t, agent)
	sessionID, sessionDir := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool {
		return ev["type"] == "tool_use"
	}, 3*time.Second)
	oldTurn := waitInFlightTurn(t, ts, sessionID)

	content := "change of plan: create b.txt only"
	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content":        content,
		"correlation_id": "corr-int",
	})
	if status := intSSE.waitStatus(t, 5*time.Second); status != http.StatusOK {
		t.Fatalf("interrupt status=%d raw=%s", status, intSSE.raw)
	}
	intSSE.waitDone(t, 5*time.Second)
	oldSSE.waitDone(t, 3*time.Second)

	intEvs := intSSE.snapshot()
	var newTurn string
	var sawInterrupt, sawReplanned, sawResult bool
	for _, ev := range intEvs {
		switch ev["type"] {
		case "system":
			if ev["content"] == "turn context" {
				newTurn, _ = ev["turn_id"].(string)
				if newTurn == "" || newTurn == oldTurn {
					t.Fatalf("new turn_id=%q old=%q", newTurn, oldTurn)
				}
				if ev["correlation_id"] != "corr-int" {
					t.Fatalf("correlation_id = %v", ev["correlation_id"])
				}
			}
			if ev["content"] == agentservice.InterruptEventContent {
				sawInterrupt = true
				if ev["interrupt_mode"] != agentservice.InterruptModeFallback {
					t.Fatalf("interrupt_mode = %v", ev["interrupt_mode"])
				}
				if ev["interrupted_turn_id"] != oldTurn {
					t.Fatalf("interrupted_turn_id = %v, want %s", ev["interrupted_turn_id"], oldTurn)
				}
			}
		case "text":
			if ev["content"] == "replanned" {
				sawReplanned = true
			}
		case "result":
			sawResult = true
		}
	}
	if !sawInterrupt || !sawReplanned || !sawResult {
		t.Fatalf("interrupt stream incomplete: interrupt=%v replanned=%v result=%v evs=%v",
			sawInterrupt, sawReplanned, sawResult, intEvs)
	}

	var oldInterrupted bool
	for _, ev := range oldSSE.snapshot() {
		if ev["type"] == "error" && ev["content"] == agentservice.TurnInterruptedContent {
			if ev["turn_id"] != oldTurn {
				t.Fatalf("old error turn_id = %v", ev["turn_id"])
			}
			oldInterrupted = true
		}
	}
	if !oldInterrupted {
		t.Fatalf("old stream missing turn interrupted: %v", oldSSE.snapshot())
	}

	prompt := agent.promptAt(1)
	for _, want := range []string{
		agentservice.InterruptPromptHeader,
		"interrupted_turn_id: " + oldTurn,
		"working on a.txt",
		`command_execution {"command":"sleep 60"} -> interrupted while running`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(prompt), content) {
		t.Fatalf("prompt does not end with instruction:\n%s", prompt)
	}

	info := getSessionInfo(t, ts, sessionID)
	if info["id"] != sessionID {
		t.Fatalf("session id changed: %v", info["id"])
	}
	if info["status"] != codingagent.StatusCompleted {
		t.Fatalf("status = %v, want completed", info["status"])
	}
	if errStr, _ := info["error"].(string); errStr != "" {
		t.Fatalf("error = %q, want empty", errStr)
	}
	if followable, _ := info["followable"].(bool); followable {
		t.Fatal("followable should be false")
	}
	if agent.closes.Load() != 2 {
		t.Fatalf("closes = %d, want 2", agent.closes.Load())
	}

	msgs := loadCanonical(t, sessionDir)
	var roles []string
	var contents []string
	for _, m := range msgs {
		roles = append(roles, m.Role)
		contents = append(contents, m.Content)
	}
	join := strings.Join(contents, "\n---\n")
	if !strings.Contains(join, "run long tool") {
		t.Fatalf("missing original user: %v", contents)
	}
	if !strings.Contains(join, "working on a.txt") {
		t.Fatalf("missing partial assistant: %v", contents)
	}
	wantHist := agentservice.InterruptHistoryMarker + " interrupted_turn_id=" + oldTurn + " mode=fallback\n" + content
	if !strings.Contains(join, wantHist) {
		t.Fatalf("missing interrupt history record:\n%v", contents)
	}
	if !strings.Contains(join, "replanned") {
		t.Fatalf("missing replanned assistant: %v", contents)
	}
	idxUser := indexOfSubstring(contents, "run long tool")
	idxAsst := indexOfSubstring(contents, "working on a.txt")
	idxInt := indexOfSubstring(contents, agentservice.InterruptHistoryMarker)
	idxRep := indexOfSubstring(contents, "replanned")
	if !(idxUser < idxAsst && idxAsst < idxInt && idxInt < idxRep) {
		t.Fatalf("canonical order wrong: user=%d asst=%d int=%d rep=%d contents=%v",
			idxUser, idxAsst, idxInt, idxRep, contents)
	}
}

func TestInterrupt_FallbackResumesNativeConversation(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "continue",
	})
	intSSE.waitDone(t, 5*time.Second)
	if agent.resumeAt(1) != "native-thread-1" {
		t.Fatalf("resumeAt(1) = %q, want native-thread-1", agent.resumeAt(1))
	}
}

func TestInterrupt_ValidationDoesNotStopInFlightTurn(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	srv, ts := newScriptServer(t, agent)
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"codex": {MaxPromptBytes: 10000},
		},
	})
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	oldTurn := waitInFlightTurn(t, ts, sessionID)

	cases := []struct {
		name string
		body map[string]any
		code int
	}{
		{"empty content", map[string]any{"content": ""}, http.StatusBadRequest},
		{"unknown algorithm", map[string]any{"content": "x", "supplement": map[string]any{"algorithm": "nope"}}, http.StatusBadRequest},
		{"too large", map[string]any{"content": strings.Repeat("x", 2000)}, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", tc.body, "text/event-stream")
			if code != tc.code {
				t.Fatalf("status=%d, want %d", code, tc.code)
			}
			info := getSessionInfo(t, ts, sessionID)
			if followable, _ := info["followable"].(bool); !followable {
				t.Fatal("turn should still be followable")
			}
			if turnID, _ := info["turn_id"].(string); turnID != oldTurn {
				t.Fatalf("turn_id changed to %q", turnID)
			}
			if agent.closes.Load() != 0 {
				t.Fatalf("closes = %d, want 0", agent.closes.Load())
			}
			select {
			case <-oldSSE.done:
				t.Fatal("old stream should not be done")
			default:
			}
		})
	}
	postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/cancel", nil, "")
}

func TestInterrupt_IdleSessionRejected(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return false })
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "x",
	}, "")
	if code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !strings.Contains(string(body), "no active turn") {
		t.Fatalf("body=%s", body)
	}
	if agent.creates.Load() != 0 {
		t.Fatalf("creates = %d, want 0", agent.creates.Load())
	}

	msg := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "hello"}},
	})
	msg.waitDone(t, 5*time.Second)
	if agent.creates.Load() != 1 {
		t.Fatalf("creates after message = %d", agent.creates.Load())
	}

	code, body = postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "x",
	}, "")
	if code != http.StatusConflict {
		t.Fatalf("after completed: status=%d body=%s", code, body)
	}
	if agent.creates.Load() != 1 {
		t.Fatalf("creates should stay 1, got %d", agent.creates.Load())
	}
}

func TestInterrupt_SessionClosedRejected(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return false })
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)
	code, _ := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/terminate", nil, "")
	if code != http.StatusOK {
		t.Fatalf("terminate status=%d", code)
	}
	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "x",
	}, "")
	if code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !strings.Contains(string(body), "session closed") {
		t.Fatalf("body=%s", body)
	}
}

func TestInterrupt_ConcurrentInterruptRejected(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	agent.blockCreate = make(chan struct{})
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intA := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "first",
	})
	select {
	case <-agent.createEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("CreateSession did not block")
	}

	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "second",
	}, "text/event-stream")
	if code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !strings.Contains(string(body), "turn transition in progress") {
		t.Fatalf("body=%s", body)
	}

	close(agent.blockCreate)
	intA.waitDone(t, 5*time.Second)
	if agent.creates.Load() != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates.Load())
	}
	info := getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusCompleted {
		t.Fatalf("status = %v", info["status"])
	}
}

func TestInterrupt_CancelWaitsForTransitionAndCancelsReplanTurn(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return true })
	agent.blockCreate = make(chan struct{})
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "redirect",
	})
	select {
	case <-agent.createEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("CreateSession did not block")
	}

	var cancelCode int
	var cancelBody []byte
	var cancelWG sync.WaitGroup
	cancelWG.Add(1)
	cancelStarted := make(chan struct{})
	go func() {
		defer cancelWG.Done()
		close(cancelStarted)
		cancelCode, cancelBody = postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/cancel", nil, "")
	}()
	<-cancelStarted
	time.Sleep(150 * time.Millisecond)
	select {
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() { cancelWG.Wait(); close(done) }()
		return done
	}():
		t.Fatal("cancel returned before transition unlocked")
	default:
	}

	close(agent.blockCreate)
	cancelDone := make(chan struct{})
	go func() { cancelWG.Wait(); close(cancelDone) }()
	select {
	case <-cancelDone:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return")
	}
	if cancelCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelCode, cancelBody)
	}
	intSSE.waitDone(t, 3*time.Second)
	var sawInterrupt bool
	for _, ev := range intSSE.snapshot() {
		if ev["type"] == "system" && ev["content"] == agentservice.InterruptEventContent {
			sawInterrupt = true
		}
	}
	if !sawInterrupt {
		t.Fatalf("interrupt stream missing turn interrupt: %v", intSSE.snapshot())
	}
	info := getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusError {
		t.Fatalf("status = %v", info["status"])
	}
	if info["error"] != "turn cancelled" {
		t.Fatalf("error = %v", info["error"])
	}
	if agent.closes.Load() != 2 {
		t.Fatalf("closes = %d, want 2", agent.closes.Load())
	}
}

func TestInterrupt_SuspendedTurnCanBeInterrupted(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	agent.suspend = true
	_, ts := newScriptServer(t, agent)
	sessionID, sessionDir := createScriptSession(t, ts)

	msg := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "ask me"}},
	})
	msg.waitDone(t, 5*time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if getSessionInfo(t, ts, sessionID)["status"] == codingagent.StatusSuspended {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if getSessionInfo(t, ts, sessionID)["status"] != codingagent.StatusSuspended {
		t.Fatalf("status = %v, want suspended", getSessionInfo(t, ts, sessionID)["status"])
	}

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "never mind, just finish",
	})
	intSSE.waitDone(t, 5*time.Second)
	var sawReplanned bool
	for _, ev := range intSSE.snapshot() {
		if ev["type"] == "text" && ev["content"] == "replanned" {
			sawReplanned = true
		}
	}
	if !sawReplanned {
		t.Fatalf("missing replanned: %v", intSSE.snapshot())
	}
	info := getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusCompleted {
		t.Fatalf("status = %v", info["status"])
	}
	if agent.closes.Load() != 2 {
		t.Fatalf("closes = %d", agent.closes.Load())
	}
	found := false
	for _, m := range loadCanonical(t, sessionDir) {
		if strings.Contains(m.Content, agentservice.InterruptHistoryMarker) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("canonical history missing interrupt record")
	}
}

func TestInterrupt_MessageDuringTransitionGetsBusy(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	agent.blockCreate = make(chan struct{})
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "redirect",
	})
	select {
	case <-agent.createEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("CreateSession did not block")
	}

	var msgCode int
	var msgBody []byte
	var msgWG sync.WaitGroup
	msgWG.Add(1)
	started := make(chan struct{})
	go func() {
		defer msgWG.Done()
		close(started)
		msgCode, msgBody = postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
			"content": []map[string]string{{"type": "text", "text": "sneak"}},
		}, "text/event-stream")
	}()
	<-started
	time.Sleep(150 * time.Millisecond)
	select {
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() { msgWG.Wait(); close(done) }()
		return done
	}():
		t.Fatal("message returned before transition unlocked")
	default:
	}

	close(agent.blockCreate)
	msgDone := make(chan struct{})
	go func() { msgWG.Wait(); close(msgDone) }()
	select {
	case <-msgDone:
	case <-time.After(3 * time.Second):
		t.Fatal("message did not return")
	}
	if msgCode != http.StatusConflict {
		t.Fatalf("message status=%d body=%s", msgCode, msgBody)
	}
	if !strings.Contains(string(msgBody), "session busy") {
		t.Fatalf("body=%s", msgBody)
	}
	intSSE.waitDone(t, 5*time.Second)
	if agent.creates.Load() != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates.Load())
	}
}

func TestInterrupt_JSONModeReturnsEventArray(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "json mode",
	}, "application/json")
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%s", code, body)
	}
	var events []map[string]any
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	var sawInterrupt, sawResult bool
	for _, ev := range events {
		if ev["type"] == "system" && ev["content"] == agentservice.InterruptEventContent {
			sawInterrupt = true
			if ev["interrupt_mode"] != agentservice.InterruptModeFallback {
				t.Fatalf("interrupt_mode = %v", ev["interrupt_mode"])
			}
		}
		if ev["type"] == "result" {
			sawResult = true
		}
	}
	if !sawInterrupt || !sawResult {
		t.Fatalf("events = %v", events)
	}
}

func TestSendMessage_BusyHintMentionsInterrupt(t *testing.T) {
	agent := newTurnScriptAgent(func(turn int) bool { return turn == 1 })
	_, ts := newScriptServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "busy?"}},
	}, "text/event-stream")
	if code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !strings.Contains(string(body), "interrupt") {
		t.Fatalf("hint missing interrupt: %s", body)
	}
	postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/cancel", nil, "")
}

func indexOfSubstring(ss []string, sub string) int {
	for i, s := range ss {
		if strings.Contains(s, sub) {
			return i
		}
	}
	return -1
}
