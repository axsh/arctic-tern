package agentservice_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
)

// nativeStubAgent declares native interrupt resume when capable is true.
// Turn 1 hangs after tool_use until its context is cancelled or InterruptTurn takes
// over. InterruptTurn closes the previous channel and returns a continued stream with
// text "native-continued" and result (or blocks until ctx is cancelled when
// continueHangs). Turns started by Fallback emit text "replanned" and result.
type nativeStubAgent struct {
	name          string
	capable       bool
	withoutHook   bool  // sessions do not implement TurnInterrupter
	hookErr       error // InterruptTurn returns this error
	continueHangs bool

	mu                sync.Mutex
	instructions      []string
	prompts           []string
	continuedCtx      context.Context // ctx passed to InterruptTurn
	closesAtInterrupt atomic.Int32    // agent-session closes observed when InterruptTurn ran
	creates           atomic.Int32
	closes            atomic.Int32
	interruptCalls    atomic.Int32
}

func (a *nativeStubAgent) Name() string                 { return a.name }
func (a *nativeStubAgent) Close() error                 { return nil }
func (a *nativeStubAgent) SupportsInterruptResume() bool { return a.capable }

func (a *nativeStubAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	n := a.creates.Add(1)
	if a.withoutHook {
		return &plainNativeStubSession{agent: a, turn: int(n)}, nil
	}
	return &nativeStubSession{agent: a, turn: int(n)}, nil
}

func (a *nativeStubAgent) recordPrompt(p string) {
	a.mu.Lock()
	a.prompts = append(a.prompts, p)
	a.mu.Unlock()
}

func (a *nativeStubAgent) promptAt(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.prompts) {
		return ""
	}
	return a.prompts[i]
}

func (a *nativeStubAgent) instructionAt(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.instructions) {
		return ""
	}
	return a.instructions[i]
}

func (a *nativeStubAgent) continuedContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.continuedCtx
}

// nativeStubSession implements TurnInterrupter.
type nativeStubSession struct {
	agent     *nativeStubAgent
	turn      int
	mu        sync.Mutex // guards sends to cur and closing it
	cur       chan codingagent.StreamEvent
	takeover  chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
}

func (s *nativeStubSession) ID() string { return "native-" + itoaScript(s.turn) }

func (s *nativeStubSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.agent.closes.Add(1)
	}
	return nil
}

func (s *nativeStubSession) closeCur() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		close(s.cur)
		s.mu.Unlock()
	})
}

func (s *nativeStubSession) Send(ctx context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	s.agent.recordPrompt(prompt)
	ch := make(chan codingagent.StreamEvent, 8)
	if s.turn == 1 {
		s.mu.Lock()
		s.cur = ch
		s.takeover = make(chan struct{})
		s.mu.Unlock()
		go func() {
			s.mu.Lock()
			ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "native-thread-1"}
			ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "working"}
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventToolUse,
				ToolName:  "command_execution",
				ToolInput: map[string]any{"command": "sleep 60"},
			}
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				s.closeCur()
			case <-s.takeover:
			}
		}()
		return ch, nil
	}
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "replanned"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

func (s *nativeStubSession) InterruptTurn(ctx context.Context, instruction string) (<-chan codingagent.StreamEvent, error) {
	a := s.agent
	a.interruptCalls.Add(1)
	a.closesAtInterrupt.Store(a.closes.Load())
	a.mu.Lock()
	a.instructions = append(a.instructions, instruction)
	a.continuedCtx = ctx
	a.mu.Unlock()
	if a.hookErr != nil {
		return nil, a.hookErr
	}
	s.mu.Lock()
	if s.takeover != nil {
		close(s.takeover)
	}
	s.mu.Unlock()
	s.closeCur()
	cont := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(cont)
		cont <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "native-continued"}
		if a.continueHangs {
			<-ctx.Done()
			return
		}
		cont <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return cont, nil
}

// plainNativeStubSession is the same session without InterruptTurn (withoutHook).
type plainNativeStubSession struct {
	agent     *nativeStubAgent
	turn      int
	mu        sync.Mutex
	cur       chan codingagent.StreamEvent
	closeOnce sync.Once
	closed    atomic.Bool
}

func (s *plainNativeStubSession) ID() string { return "plain-native-" + itoaScript(s.turn) }

func (s *plainNativeStubSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.agent.closes.Add(1)
	}
	return nil
}

func (s *plainNativeStubSession) closeCur() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		close(s.cur)
		s.mu.Unlock()
	})
}

func (s *plainNativeStubSession) Send(ctx context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	s.agent.recordPrompt(prompt)
	ch := make(chan codingagent.StreamEvent, 8)
	if s.turn == 1 {
		s.mu.Lock()
		s.cur = ch
		s.mu.Unlock()
		go func() {
			s.mu.Lock()
			ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "native-thread-1"}
			ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "working"}
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventToolUse,
				ToolName:  "command_execution",
				ToolInput: map[string]any{"command": "sleep 60"},
			}
			s.mu.Unlock()
			<-ctx.Done()
			s.closeCur()
		}()
		return ch, nil
	}
	go func() {
		defer close(ch)
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "replanned"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

// newNativeServer builds an httptest server fronting the given agents.
func newNativeServer(t *testing.T, agents ...codingagent.CodingAgent) (*agentservice.Server, *httptest.Server) {
	t.Helper()
	srv := agentservice.New(
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithToolHeartbeatInterval(0),
	)
	for _, a := range agents {
		srv.RegisterAgent(a)
	}
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestHandleListAgents_ReportsInterruptResume(t *testing.T) {
	_, ts := newNativeServer(t,
		&mockCodingAgent{name: "claudecode"},
		&nativeStubAgent{name: "codex", capable: true},
	)
	resp, err := http.Get(ts.URL + "/api/v1/agents")
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var agents []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		t.Fatalf("decode: %v", err)
	}
	seen := map[string]map[string]any{}
	for _, a := range agents {
		name, _ := a["name"].(string)
		seen[name] = a
	}
	codex, ok := seen["codex"]
	if !ok {
		t.Fatalf("codex missing: %v", agents)
	}
	if codex["supports_interrupt_resume"] != true {
		t.Fatalf("codex supports_interrupt_resume = %v, want true", codex["supports_interrupt_resume"])
	}
	claude, ok := seen["claudecode"]
	if !ok {
		t.Fatalf("claudecode missing: %v", agents)
	}
	val, present := claude["supports_interrupt_resume"]
	if !present {
		t.Fatalf("claudecode missing supports_interrupt_resume key: %v", claude)
	}
	if val != false {
		t.Fatalf("claudecode supports_interrupt_resume = %v, want false", val)
	}
}

func TestInterrupt_NativeContinuesInSameAgentSession(t *testing.T) {
	agent := &nativeStubAgent{name: "codex", capable: true}
	_, ts := newNativeServer(t, agent)
	sessionID, sessionDir := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	oldTurn := waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "switch to b.txt",
	})
	if status := intSSE.waitStatus(t, 5*time.Second); status != http.StatusOK {
		t.Fatalf("interrupt status=%d raw=%s", status, intSSE.raw)
	}
	intSSE.waitDone(t, 5*time.Second)
	oldSSE.waitDone(t, 3*time.Second)

	var newTurn string
	var sawInterrupt, sawContinued, sawResult bool
	for _, ev := range intSSE.snapshot() {
		switch ev["type"] {
		case "system":
			if ev["content"] == "turn context" {
				newTurn, _ = ev["turn_id"].(string)
			}
			if ev["content"] == agentservice.InterruptEventContent {
				sawInterrupt = true
				if ev["interrupt_mode"] != agentservice.InterruptModeNative {
					t.Fatalf("interrupt_mode = %v, want native", ev["interrupt_mode"])
				}
				if ev["interrupted_turn_id"] != oldTurn {
					t.Fatalf("interrupted_turn_id = %v, want %s", ev["interrupted_turn_id"], oldTurn)
				}
			}
		case "text":
			if ev["content"] == "native-continued" {
				sawContinued = true
			}
		case "result":
			sawResult = true
		}
	}
	if !sawInterrupt || !sawContinued || !sawResult {
		t.Fatalf("native stream incomplete: interrupt=%v continued=%v result=%v evs=%v",
			sawInterrupt, sawContinued, sawResult, intSSE.snapshot())
	}
	if newTurn == "" || newTurn == oldTurn {
		t.Fatalf("new turn_id=%q old=%q", newTurn, oldTurn)
	}

	if agent.interruptCalls.Load() != 1 {
		t.Fatalf("interruptCalls = %d, want 1", agent.interruptCalls.Load())
	}
	if got := agent.instructionAt(0); got != "switch to b.txt" {
		t.Fatalf("instruction[0] = %q, want 'switch to b.txt'", got)
	}
	if agent.creates.Load() != 1 {
		t.Fatalf("creates = %d, want 1 (no new process)", agent.creates.Load())
	}
	if agent.closesAtInterrupt.Load() != 0 {
		t.Fatalf("closesAtInterrupt = %d, want 0", agent.closesAtInterrupt.Load())
	}
	if agent.closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1 after the turn", agent.closes.Load())
	}

	var oldInterrupted int
	for _, ev := range oldSSE.snapshot() {
		if ev["type"] == "error" && ev["content"] == agentservice.TurnInterruptedContent {
			if ev["turn_id"] != oldTurn {
				t.Fatalf("old error turn_id = %v", ev["turn_id"])
			}
			oldInterrupted++
		}
	}
	if oldInterrupted != 1 {
		t.Fatalf("old interrupted count = %d, want 1", oldInterrupted)
	}

	info := getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusCompleted {
		t.Fatalf("status = %v, want completed", info["status"])
	}

	msgs := loadCanonical(t, sessionDir)
	var contents []string
	for _, m := range msgs {
		contents = append(contents, m.Content)
	}
	join := strings.Join(contents, "\n---\n")
	wantHist := agentservice.InterruptHistoryMarker + " interrupted_turn_id=" + oldTurn + " mode=native\nswitch to b.txt"
	if !strings.Contains(join, wantHist) {
		t.Fatalf("missing native interrupt record %q:\n%v", wantHist, contents)
	}
	if !strings.Contains(join, "native-continued") {
		t.Fatalf("missing native-continued assistant: %v", contents)
	}
	idxInt := indexOfSubstring(contents, agentservice.InterruptHistoryMarker)
	idxCont := indexOfSubstring(contents, "native-continued")
	if !(idxInt >= 0 && idxCont >= 0 && idxInt < idxCont) {
		t.Fatalf("canonical order wrong: int=%d cont=%d contents=%v", idxInt, idxCont, contents)
	}
}

func TestInterrupt_NativeFailureFallsBackInSameRequest(t *testing.T) {
	agent := &nativeStubAgent{name: "codex", capable: true, hookErr: errors.New("agent rejected interrupt")}
	_, ts := newNativeServer(t, agent)
	sessionID, sessionDir := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	oldTurn := waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "fallback please",
	})
	intSSE.waitDone(t, 5*time.Second)
	oldSSE.waitDone(t, 3*time.Second)

	var sawInterrupt, sawReplanned, sawResult bool
	for _, ev := range intSSE.snapshot() {
		switch ev["type"] {
		case "system":
			if ev["content"] == agentservice.InterruptEventContent {
				sawInterrupt = true
				if ev["interrupt_mode"] != agentservice.InterruptModeFallback {
					t.Fatalf("interrupt_mode = %v, want fallback", ev["interrupt_mode"])
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
		t.Fatalf("fallback stream incomplete: interrupt=%v replanned=%v result=%v",
			sawInterrupt, sawReplanned, sawResult)
	}

	if agent.interruptCalls.Load() != 1 {
		t.Fatalf("interruptCalls = %d, want 1", agent.interruptCalls.Load())
	}
	if agent.creates.Load() != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates.Load())
	}
	if prompt := agent.promptAt(1); !strings.HasPrefix(prompt, agentservice.InterruptPromptHeader) {
		t.Fatalf("prompts[1] does not start with replan header:\n%s", prompt)
	}

	var oldInterrupted int
	for _, ev := range oldSSE.snapshot() {
		if ev["type"] == "error" && ev["content"] == agentservice.TurnInterruptedContent {
			oldInterrupted++
		}
	}
	if oldInterrupted != 1 {
		t.Fatalf("old interrupted count = %d, want 1", oldInterrupted)
	}

	found := false
	for _, m := range loadCanonical(t, sessionDir) {
		if strings.Contains(m.Content, agentservice.InterruptHistoryMarker+" interrupted_turn_id="+oldTurn+" mode=fallback") {
			found = true
		}
	}
	if !found {
		t.Fatal("canonical history missing fallback interrupt record")
	}

	info := getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusCompleted {
		t.Fatalf("status = %v, want completed", info["status"])
	}
}

func TestInterrupt_CapabilityFalseNeverCallsNativeHook(t *testing.T) {
	agent := &nativeStubAgent{name: "codex", capable: false}
	_, ts := newNativeServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "redirect",
	})
	intSSE.waitDone(t, 5*time.Second)
	oldSSE.waitDone(t, 3*time.Second)

	var sawFallback bool
	for _, ev := range intSSE.snapshot() {
		if ev["type"] == "system" && ev["content"] == agentservice.InterruptEventContent {
			if ev["interrupt_mode"] != agentservice.InterruptModeFallback {
				t.Fatalf("interrupt_mode = %v, want fallback", ev["interrupt_mode"])
			}
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatalf("missing fallback interrupt event: %v", intSSE.snapshot())
	}
	if agent.interruptCalls.Load() != 0 {
		t.Fatalf("interruptCalls = %d, want 0", agent.interruptCalls.Load())
	}
	if agent.creates.Load() != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates.Load())
	}
}

func TestInterrupt_SessionWithoutHookUsesFallback(t *testing.T) {
	agent := &nativeStubAgent{name: "codex", capable: true, withoutHook: true}
	_, ts := newNativeServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	_ = waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "redirect",
	})
	intSSE.waitDone(t, 5*time.Second)
	oldSSE.waitDone(t, 3*time.Second)

	var sawFallback bool
	for _, ev := range intSSE.snapshot() {
		if ev["type"] == "system" && ev["content"] == agentservice.InterruptEventContent {
			if ev["interrupt_mode"] != agentservice.InterruptModeFallback {
				t.Fatalf("interrupt_mode = %v, want fallback", ev["interrupt_mode"])
			}
			sawFallback = true
		}
	}
	if !sawFallback {
		t.Fatalf("missing fallback interrupt event: %v", intSSE.snapshot())
	}
	if agent.creates.Load() != 2 {
		t.Fatalf("creates = %d, want 2", agent.creates.Load())
	}
}

func TestInterrupt_NativeTurnCancelStopsTransferredContext(t *testing.T) {
	agent := &nativeStubAgent{name: "codex", capable: true, continueHangs: true}
	_, ts := newNativeServer(t, agent)
	sessionID, _ := createScriptSession(t, ts)

	oldSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{
		"content": []map[string]string{{"type": "text", "text": "run long tool"}},
	})
	oldSSE.waitFor(t, func(ev map[string]any) bool { return ev["type"] == "tool_use" }, 3*time.Second)
	oldTurn := waitInFlightTurn(t, ts, sessionID)

	intSSE := startSSE(t, ts, "/api/v1/sessions/"+sessionID+"/interrupt", map[string]any{
		"content": "keep going",
	})
	intSSE.waitFor(t, func(ev map[string]any) bool {
		return ev["type"] == "text" && ev["content"] == "native-continued"
	}, 5*time.Second)

	var newTurn string
	for _, ev := range intSSE.snapshot() {
		if ev["type"] == "system" && ev["content"] == "turn context" {
			newTurn, _ = ev["turn_id"].(string)
		}
	}
	if newTurn == "" || newTurn == oldTurn {
		t.Fatalf("new turn_id=%q old=%q", newTurn, oldTurn)
	}
	info := getSessionInfo(t, ts, sessionID)
	if followable, _ := info["followable"].(bool); !followable {
		t.Fatal("continued turn should be followable")
	}
	if turnID, _ := info["turn_id"].(string); turnID != newTurn {
		t.Fatalf("in-flight turn_id = %q, want %q", turnID, newTurn)
	}

	code, body := postJSON(t, ts, "/api/v1/sessions/"+sessionID+"/cancel", nil, "")
	if code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", code, body)
	}

	select {
	case <-agent.continuedContext().Done():
	case <-time.After(1 * time.Second):
		t.Fatal("transferred context was not cancelled within 1s")
	}

	intSSE.waitDone(t, 5*time.Second)

	if agent.closes.Load() != 1 {
		t.Fatalf("closes = %d, want 1", agent.closes.Load())
	}
	info = getSessionInfo(t, ts, sessionID)
	if info["status"] != codingagent.StatusError {
		t.Fatalf("status = %v, want error", info["status"])
	}
	if info["error"] != "turn cancelled" {
		t.Fatalf("error = %v, want turn cancelled", info["error"])
	}
}
