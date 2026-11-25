package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// imPlainAgent implements CodingAgent without declaring native interrupt resume.
type imPlainAgent struct{}

func (imPlainAgent) CreateSession(context.Context, ...codingagent.SessionOption) (codingagent.Session, error) {
	return nil, nil
}
func (imPlainAgent) Name() string  { return "plain" }
func (imPlainAgent) Close() error  { return nil }

type imFalseAgent struct{ imPlainAgent }

func (imFalseAgent) SupportsInterruptResume() bool { return false }

type imTrueAgent struct{ imPlainAgent }

func (imTrueAgent) SupportsInterruptResume() bool { return true }

// imPlainSession implements Session without TurnInterrupter.
type imPlainSession struct{}

func (imPlainSession) Send(context.Context, string) (<-chan codingagent.StreamEvent, error) {
	return nil, nil
}
func (imPlainSession) ID() string  { return "plain-session" }
func (imPlainSession) Close() error { return nil }

// imHookSession implements Session and TurnInterrupter.
type imHookSession struct{ imPlainSession }

func (imHookSession) InterruptTurn(context.Context, string) (<-chan codingagent.StreamEvent, error) {
	return nil, nil
}

func TestSelectInterruptMode(t *testing.T) {
	s := New()
	tests := []struct {
		name     string
		agent    codingagent.CodingAgent
		sess     codingagent.Session
		wantMode string
		wantHook bool
	}{
		{"agent does not declare", imPlainAgent{}, imHookSession{}, InterruptModeFallback, false},
		{"agent declares false", imFalseAgent{}, imHookSession{}, InterruptModeFallback, false},
		{"session lacks hook", imTrueAgent{}, imPlainSession{}, InterruptModeFallback, false},
		{"no agent session", imTrueAgent{}, nil, InterruptModeFallback, false},
		{"capable", imTrueAgent{}, imHookSession{}, InterruptModeNative, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &activeExecution{agentSess: tt.sess}
			mode, ti := s.selectInterruptMode(tt.agent, exec)
			if mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q", mode, tt.wantMode)
			}
			if (ti != nil) != tt.wantHook {
				t.Fatalf("hook present = %v, want %v", ti != nil, tt.wantHook)
			}
		})
	}
}

func TestFinishTurnRelay_AppendsTerminalOnce(t *testing.T) {
	s := New()
	src := make(chan codingagent.StreamEvent, 4)
	src <- codingagent.StreamEvent{Type: codingagent.EventToolUse, ToolName: "command_execution"}
	close(src)
	relay := newEventRelayWithHeartbeat(src, 0)
	exec := &activeExecution{sessionID: "s1", turnID: "t1", correlationID: "c1", relay: relay}

	terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}
	s.finishTurnRelay(exec, &terminal)
	s.finishTurnRelay(exec, &terminal)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !relay.isSourceDone() {
		time.Sleep(10 * time.Millisecond)
	}
	if !relay.isSourceDone() {
		t.Fatal("relay source not marked done")
	}

	var terminals int
	for _, ev := range relay.snapshot() {
		if ev.Type == codingagent.EventError && ev.Content == TurnInterruptedContent {
			terminals++
			if ev.TurnID != "t1" {
				t.Fatalf("terminal turn_id = %q, want t1", ev.TurnID)
			}
			if ev.CorrelationID != "c1" {
				t.Fatalf("terminal correlation_id = %q, want c1", ev.CorrelationID)
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("turn interrupted terminal count = %d, want 1", terminals)
	}
}

// detachProbeSession records whether Close was called.
type detachProbeSession struct {
	closed bool
}

func (p *detachProbeSession) Send(context.Context, string) (<-chan codingagent.StreamEvent, error) {
	return nil, nil
}
func (p *detachProbeSession) ID() string { return "detach-probe" }
func (p *detachProbeSession) Close() error {
	p.closed = true
	return nil
}

func TestDetachInterruptedTurn_KeepsProcessOwnership(t *testing.T) {
	s := New()
	sessionDir := t.TempDir()
	rec := &codingagent.SessionRecord{
		ID:         "s1",
		AgentName:  "codex",
		Status:     codingagent.StatusActive,
		SessionDir: sessionDir,
	}
	s.sessions.Create(rec)

	src := make(chan codingagent.StreamEvent, 2)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "partial"}
	close(src)
	relay := newEventRelayWithHeartbeat(src, 0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && relay.eventCount() < 1 {
		time.Sleep(10 * time.Millisecond)
	}

	execCtx, execCancel := context.WithCancel(context.Background())
	probe := &detachProbeSession{}
	exec := &activeExecution{
		sessionID:  "s1",
		turnID:     "t1",
		agentSess:  probe,
		relay:      relay,
		status:     codingagent.StatusActive,
		execCtx:    execCtx,
		execCancel: execCancel,
	}
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.RegisterExecCancel("s1", execCancel)
	s.RegisterActiveSession("s1", probe)

	events := s.detachInterruptedTurn(context.Background(), exec)
	if len(events) == 0 {
		t.Fatal("detach returned no events")
	}

	if _, ok := s.execRegistry.Get("s1"); ok {
		t.Fatal("exec should be unregistered from exec registry")
	}
	if execCtx.Err() != nil {
		t.Fatalf("exec ctx must not be cancelled: %v", execCtx.Err())
	}
	if probe.closed {
		t.Fatal("agent session must not be closed on detach")
	}
	if !s.CancelExecution("s1") {
		t.Fatal("exec cancel func must be kept for the continued turn")
	}

	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatalf("load canonical: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "partial") {
			found = true
		}
	}
	if !found {
		t.Fatalf("canonical history missing assistant 'partial': %+v", msgs)
	}

	execCancel()
}
