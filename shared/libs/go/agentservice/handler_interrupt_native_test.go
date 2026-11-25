package agentservice_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

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
		if a.continueHangs {
			<-ctx.Done()
			return
		}
		cont <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "native-continued"}
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
