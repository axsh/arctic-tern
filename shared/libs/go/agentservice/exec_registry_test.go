package agentservice

import (
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestExecRegistry_RegisterRejectsDuplicate(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	if err := reg.Register("s1", exec); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.Register("s1", exec); err == nil {
		t.Fatal("expected ErrSessionBusy")
	}
}

func TestExecRegistry_SetStatus(t *testing.T) {
	reg := newExecRegistry()
	exec := &activeExecution{sessionID: "s1", status: codingagent.StatusActive}
	_ = reg.Register("s1", exec)
	reg.SetStatus("s1", codingagent.StatusSuspended)
	got, ok := reg.Get("s1")
	if !ok || got.status != codingagent.StatusSuspended {
		t.Fatalf("status = %q, ok=%v", got.status, ok)
	}
}

func TestExecRegistry_UnregisterIfOnlyRemovesSameExecution(t *testing.T) {
	reg := newExecRegistry()
	a := &activeExecution{sessionID: "s1"}
	b := &activeExecution{sessionID: "s1"}
	if err := reg.Register("s1", a); err != nil {
		t.Fatal(err)
	}
	if reg.UnregisterIf("s1", b) {
		t.Fatal("UnregisterIf removed a different execution")
	}
	if _, ok := reg.Get("s1"); !ok {
		t.Fatal("a should still be registered")
	}
	if !reg.UnregisterIf("s1", a) {
		t.Fatal("UnregisterIf should remove the same execution")
	}
	if _, ok := reg.Get("s1"); ok {
		t.Fatal("a should be gone after UnregisterIf")
	}
}

func TestActiveExecution_MarkStoppedOnce(t *testing.T) {
	e := &activeExecution{sessionID: "s1"}
	if !e.markStopped(stopReasonInterrupted) {
		t.Fatal("first markStopped should return true")
	}
	if e.markStopped(stopReasonCancelled) {
		t.Fatal("second markStopped should return false")
	}
	if e.stoppedReason() != stopReasonInterrupted {
		t.Fatalf("stoppedReason = %q, want interrupted", e.stoppedReason())
	}
	if !e.isStopped() {
		t.Fatal("isStopped should be true")
	}
	var nilExec *activeExecution
	if nilExec.isStopped() {
		t.Fatal("nil exec isStopped should be false")
	}
}

func TestActiveExecution_SuppressesSessionWrites(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{"", false},
		{stopReasonCancelled, true},
		{stopReasonInterrupted, true},
		{stopReasonTerminated, true},
		{stopReasonDrainTimeout, false},
	}
	for _, tt := range tests {
		e := &activeExecution{sessionID: "s1"}
		if tt.reason != "" {
			e.markStopped(tt.reason)
		}
		if got := e.suppressesSessionWrites(); got != tt.want {
			t.Errorf("reason %q: suppressesSessionWrites = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

func TestPrependEvents_YieldsPreambleThenSource(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 3)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "a"}
	src <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(src)
	out := prependEvents(src, codingagent.StreamEvent{Type: codingagent.EventSystem, Content: "p"})
	var got []codingagent.StreamEvent
	for ev := range out {
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	if got[0].Type != codingagent.EventSystem || got[0].Content != "p" {
		t.Fatalf("first event = %+v, want preamble", got[0])
	}
	if got[1].Content != "a" || got[2].Type != codingagent.EventResult {
		t.Fatalf("source events not forwarded in order: %+v", got)
	}
}

func TestEventRelay_IsSourceDone(t *testing.T) {
	ch := make(chan codingagent.StreamEvent)
	relay := newEventRelay(ch)
	if relay.isSourceDone() {
		t.Fatal("expected sourceDone=false before close")
	}
	close(ch)
	deadline := time.Now().Add(2 * time.Second)
	for !relay.isSourceDone() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for sourceDone")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !relay.isSourceDone() {
		t.Fatal("expected sourceDone=true after close")
	}
}

func TestEventRelay_StreamDrainsAfterSourceClose(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelay(src)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "a"}
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "b"}
	src <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(src)

	out := relay.stream(0, false)
	var got []codingagent.StreamEvent
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-out:
			if !ok {
				if len(got) != 3 {
					t.Fatalf("got %d events, want 3", len(got))
				}
				return
			}
			got = append(got, ev)
		case <-deadline:
			t.Fatalf("timed out draining stream, got %d events", len(got))
		}
	}
}

func TestEventRelay_NoHangWhenDoneRacesWithWait(t *testing.T) {
	const rounds = 100
	for i := 0; i < rounds; i++ {
		src := make(chan codingagent.StreamEvent)
		relay := newEventRelay(src)
		out := relay.stream(0, false)

		done := make(chan struct{})
		go func() {
			defer close(done)
			for range out {
			}
		}()

		// Let the subscriber reach the wait path with an empty buffer.
		time.Sleep(time.Millisecond)
		close(src)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("round %d: stream hung after source close", i)
		}
	}
}

func TestEventRelay_StopOnUserInputStillCloses(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelay(src)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "before"}
	src <- codingagent.StreamEvent{Type: codingagent.EventUserInputRequired, Content: "ask"}
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "after"}
	close(src)

	out := relay.stream(0, true)
	var got []codingagent.StreamEvent
	for ev := range out {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (stop on user input)", len(got))
	}
	if got[1].Type != codingagent.EventUserInputRequired {
		t.Fatalf("last event = %v, want user_input_required", got[1].Type)
	}
}

func TestEventRelay_ToolHeartbeatInjectsProgress(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelayWithHeartbeat(src, 40*time.Millisecond)
	src <- codingagent.StreamEvent{Type: codingagent.EventToolUse, ToolName: "command_execution"}

	deadline := time.Now().Add(500 * time.Millisecond)
	var sawHeartbeat bool
	for time.Now().Before(deadline) {
		for _, ev := range relay.EventsSnapshot() {
			if ev.Type == codingagent.EventProgress && ev.Content == toolStillRunningContent {
				if ev.ToolName != "command_execution" {
					t.Fatalf("tool_name = %q, want command_execution", ev.ToolName)
				}
				sawHeartbeat = true
				break
			}
		}
		if sawHeartbeat {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !sawHeartbeat {
		t.Fatalf("expected tool_still_running progress within interval; events=%v", relay.EventsSnapshot())
	}

	src <- codingagent.StreamEvent{Type: codingagent.EventToolResult, Content: "done"}
	src <- codingagent.StreamEvent{Type: codingagent.EventResult}
	close(src)

	waitDone := time.Now().Add(2 * time.Second)
	for !relay.isSourceDone() {
		if time.Now().After(waitDone) {
			t.Fatal("timed out waiting for sourceDone")
		}
		time.Sleep(5 * time.Millisecond)
	}

	nAfter := len(relay.EventsSnapshot())
	time.Sleep(120 * time.Millisecond)
	if len(relay.EventsSnapshot()) != nAfter {
		t.Fatalf("heartbeat continued after tool_result: before=%d after=%d", nAfter, len(relay.EventsSnapshot()))
	}
}

func TestEventRelay_NoHeartbeatWithoutTool(t *testing.T) {
	src := make(chan codingagent.StreamEvent, 4)
	relay := newEventRelayWithHeartbeat(src, 30*time.Millisecond)
	src <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "thinking"}
	time.Sleep(100 * time.Millisecond)
	for _, ev := range relay.EventsSnapshot() {
		if ev.Type == codingagent.EventProgress {
			t.Fatalf("unexpected progress without tool_use: %+v", ev)
		}
	}
	close(src)
}
