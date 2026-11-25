package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

func TestFinishExecution_StoppedExecLeavesSuccessorRegistries(t *testing.T) {
	s := New()
	old := &activeExecution{sessionID: "s1"}
	old.markStopped(stopReasonInterrupted)
	successor := &activeExecution{sessionID: "s1"}
	if err := s.execRegistry.Register("s1", successor); err != nil {
		t.Fatal(err)
	}
	newProbe := &closeProbe{}
	s.RegisterActiveSession("s1", newProbe)
	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterExecCancel("s1", cancel)

	oldProbe := &closeProbe{}
	s.finishExecution(old, "s1", oldProbe, nil)

	if got, ok := s.execRegistry.Get("s1"); !ok || got != successor {
		t.Fatal("successor registration was removed by a stopped exec finish")
	}
	if !s.CancelExecution("s1") {
		t.Fatal("successor cancel func was removed")
	}
	if ctx.Err() == nil {
		t.Fatal("successor ctx not cancelled by CancelExecution")
	}
	if oldProbe.closed {
		t.Fatal("stopped exec must not close its agent session (the stopper owns closing)")
	}
	if newProbe.closed {
		t.Fatal("finishExecution closed the successor's active session")
	}
}

func TestFinishExecution_SupersededExecClosesOnlyItsOwnSession(t *testing.T) {
	s := New()
	old := &activeExecution{sessionID: "s1"}
	successor := &activeExecution{sessionID: "s1"}
	if err := s.execRegistry.Register("s1", successor); err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	s.RegisterExecCancel("s1", cancel)

	oldProbe := &closeProbe{}
	s.finishExecution(old, "s1", oldProbe, nil)

	if !oldProbe.closed {
		t.Fatal("superseded exec should close its own agent session")
	}
	if got, ok := s.execRegistry.Get("s1"); !ok || got != successor {
		t.Fatal("superseded finish removed the successor registration")
	}
	if !s.CancelExecution("s1") {
		t.Fatal("superseded finish removed the successor cancel func")
	}
}

func TestFinishExecution_OwnExecReleasesRegistries(t *testing.T) {
	s := New()
	exec := &activeExecution{sessionID: "s1"}
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	s.RegisterExecCancel("s1", cancel)
	probe := &closeProbe{}
	s.RegisterActiveSession("s1", probe)

	s.finishExecution(exec, "s1", probe, nil)

	if _, ok := s.execRegistry.Get("s1"); ok {
		t.Fatal("own exec still registered after finish")
	}
	if s.CancelExecution("s1") {
		t.Fatal("own exec cancel func not released")
	}
	if !probe.closed {
		t.Fatal("own exec agent session not closed")
	}
}

func waitRelayEvents(t *testing.T, relay *eventRelay, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for relay.eventCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("relay did not buffer %d events (got %d)", n, relay.eventCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopInFlightTurn_InterruptFinalizesTurnOnce(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	dir := t.TempDir()
	record := &codingagent.SessionRecord{ID: "s1", AgentName: "codex", SessionDir: dir, Status: codingagent.StatusActive}
	if err := s.sessions.Create(record); err != nil {
		t.Fatal(err)
	}
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelayWithHeartbeat(src, 0)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "partial"}
	src <- codingagent.StreamEvent{Type: codingagent.EventToolUse, ToolName: "command_execution", ToolInput: map[string]any{"command": "sleep 60"}}
	waitRelayEvents(t, relay, 2)

	probe := &closeProbe{}
	exec := &activeExecution{
		sessionID: "s1",
		turnID:    "t-old",
		agentSess: probe,
		relay:     relay,
		usageAgg:  newTurnUsageAggregator("t-old"),
	}
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterExecCancel("s1", cancel)

	terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}
	events := s.stopInFlightTurn(context.Background(), exec, stopOptions{
		reason:   stopReasonInterrupted,
		terminal: &terminal,
		ingest:   true,
	})

	if ctx.Err() == nil {
		t.Fatal("exec ctx not cancelled")
	}
	if !probe.closed {
		t.Fatal("agent session not closed by stopInFlightTurn")
	}
	if _, ok := s.execRegistry.Get("s1"); ok {
		t.Fatal("exec still registered after stop")
	}
	if s.CancelExecution("s1") {
		t.Fatal("exec cancel func not released")
	}
	if !relay.isSourceDone() {
		t.Fatal("relay source not marked done")
	}
	if !exec.suppressesSessionWrites() {
		t.Fatal("suppressesSessionWrites should be true after an interrupt stop")
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	last := events[len(events)-1]
	if last.Type != codingagent.EventError || last.Content != TurnInterruptedContent || last.TurnID != "t-old" {
		t.Fatalf("last event = %+v, want error turn interrupted with turn_id t-old", last)
	}
	msgs, err := session.OpenCanonical(dir).LoadRange(1, 0)
	if err != nil {
		t.Fatalf("load canonical: %v", err)
	}
	var hasPartial, hasToolCall bool
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "partial") {
			hasPartial = true
		}
		for _, tc := range m.ToolCalls {
			if tc.Name == "command_execution" {
				hasToolCall = true
			}
		}
	}
	if !hasPartial || !hasToolCall {
		t.Fatalf("canonical missing ingested facts: %+v", msgs)
	}
}

func TestStopInFlightTurn_CancelDoesNotIngestOrAppend(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	dir := t.TempDir()
	record := &codingagent.SessionRecord{ID: "s1", AgentName: "codex", SessionDir: dir, Status: codingagent.StatusActive}
	if err := s.sessions.Create(record); err != nil {
		t.Fatal(err)
	}
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelayWithHeartbeat(src, 0)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "partial"}
	src <- codingagent.StreamEvent{Type: codingagent.EventToolUse, ToolName: "command_execution"}
	waitRelayEvents(t, relay, 2)

	exec := &activeExecution{
		sessionID: "s1",
		turnID:    "t-old",
		agentSess: &closeProbe{},
		relay:     relay,
		usageAgg:  newTurnUsageAggregator("t-old"),
	}
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.RegisterExecCancel("s1", cancel)
	_ = ctx

	events := s.stopInFlightTurn(context.Background(), exec, stopOptions{reason: stopReasonCancelled})
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (no terminal appended)", len(events))
	}
	if last := events[len(events)-1]; last.Type != codingagent.EventToolUse {
		t.Fatalf("last event = %+v, want tool_use", last)
	}
	msgs, _ := session.OpenCanonical(dir).LoadRange(1, 0)
	if len(msgs) != 0 {
		t.Fatalf("canonical should stay empty on cancel, got %+v", msgs)
	}
}

func TestMarkSessionActive_ClearsStaleError(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	rec := &codingagent.SessionRecord{ID: "s1", Status: codingagent.StatusError, Error: turnCancelledError}
	if err := s.sessions.Create(rec); err != nil {
		t.Fatal(err)
	}
	s.markSessionActive("s1")
	got, err := s.sessions.Get("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != codingagent.StatusActive {
		t.Fatalf("status = %q, want active", got.Status)
	}
	if got.Error != "" {
		t.Fatalf("error = %q, want empty", got.Error)
	}
}

func TestHandleRelaySideEffects_StoppedExecWritesNothing(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	rec := &codingagent.SessionRecord{ID: "s1", AgentName: "codex", Status: codingagent.StatusActive, AgentSessionID: "orig"}
	if err := s.sessions.Create(rec); err != nil {
		t.Fatal(err)
	}
	exec := &activeExecution{sessionID: "s1", turnID: "t-old", usageAgg: newTurnUsageAggregator("t-old")}
	exec.markStopped(stopReasonInterrupted)
	for _, ev := range []codingagent.StreamEvent{
		{Type: codingagent.EventSystem, SessionID: "late"},
		{Type: codingagent.EventUserInputRequired},
		{Type: codingagent.EventResult},
	} {
		if _, err := s.handleRelaySideEffects("s1", exec, ev, false, nil, nil); err != nil {
			t.Fatalf("handleRelaySideEffects: %v", err)
		}
	}
	got, err := s.sessions.Get("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "orig" {
		t.Fatalf("AgentSessionID = %q, want orig (stopped exec must not write late session id)", got.AgentSessionID)
	}
	if got.Status != codingagent.StatusActive {
		t.Fatalf("status = %q, want active (stopped exec must not write status)", got.Status)
	}
}

func TestWaitDetached_StoppedExecReturnsStopReason(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	exec := &activeExecution{sessionID: "s1"}
	exec.markStopped(stopReasonInterrupted)
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	term, suspended := s.waitDetached(ctx, exec)
	if suspended {
		t.Fatal("stopped exec should not be reported as suspended")
	}
	if term.kind != codingagent.EventError || term.content != TurnInterruptedContent {
		t.Fatalf("term = %+v, want error turn interrupted", term)
	}
}

func TestArmReattachTimer_SkipsStoppedExec(t *testing.T) {
	s := NewWithStore(NewMemorySessionStore())
	exec := &activeExecution{sessionID: "s1"}
	exec.markStopped(stopReasonInterrupted)
	s.armReattachTimer(exec)
	exec.subMu.Lock()
	defer exec.subMu.Unlock()
	if exec.reattachTimer != nil {
		t.Fatal("reattach timer armed for a stopped exec")
	}
}

func TestStopExecOnDrainTimeout_MarksStoppedWithoutSuppressingWrites(t *testing.T) {
	s := New()
	probe := &closeProbe{}
	exec := &activeExecution{sessionID: "s1", agentSess: probe}
	if err := s.execRegistry.Register("s1", exec); err != nil {
		t.Fatal(err)
	}
	term := s.stopExecOnDrainTimeout("s1", exec)
	if !exec.isStopped() {
		t.Fatal("drain timeout should mark the exec stopped")
	}
	if exec.suppressesSessionWrites() {
		t.Fatal("drain timeout must not suppress session writes")
	}
	if term.content != drainTimeoutTerminalContent {
		t.Fatalf("content = %q, want drain timeout", term.content)
	}
}
