package llm_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/tasklog"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// turnInterruptAgent scripts a two-phase conversation for interrupt integration
// tests. Turn 1 emits a native session id, assistant text "working" and a
// tool_use, then blocks until its context is cancelled (the interrupt stop).
// Later turns wait replanDelay, emit text "replanned" and a result.
type turnInterruptAgent struct {
	name        string
	replanDelay time.Duration
	creates     atomic.Int32

	mu      sync.Mutex
	prompts []string // prompt passed to Send, per turn
}

func (a *turnInterruptAgent) Name() string { return a.name }
func (a *turnInterruptAgent) Close() error { return nil }

func (a *turnInterruptAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	n := a.creates.Add(1)
	return &turnInterruptSession{agent: a, turn: int(n)}, nil
}

func (a *turnInterruptAgent) promptAt(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.prompts) {
		return ""
	}
	return a.prompts[i]
}

type turnInterruptSession struct {
	agent *turnInterruptAgent
	turn  int
}

func (s *turnInterruptSession) ID() string { return fmt.Sprintf("ti-native-%d", s.turn) }
func (s *turnInterruptSession) Close() error { return nil }

func (s *turnInterruptSession) Send(ctx context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	s.agent.mu.Lock()
	s.agent.prompts = append(s.agent.prompts, prompt)
	s.agent.mu.Unlock()

	ch := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(ch)
		if s.turn == 1 {
			ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "ti-thread-1"}
			ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "working"}
			ch <- codingagent.StreamEvent{
				Type:      codingagent.EventToolUse,
				ToolName:  "command_execution",
				ToolInput: map[string]any{"command": "sleep 60"},
			}
			<-ctx.Done()
			return
		}
		if s.agent.replanDelay > 0 {
			select {
			case <-time.After(s.agent.replanDelay):
			case <-ctx.Done():
				return
			}
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "replanned"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

// newTurnInterruptHTTP builds an httptest server fronting a turnInterruptAgent,
// mirroring the deterministic mock-agent setup used by other integration tests.
func newTurnInterruptHTTP(t *testing.T, agent *turnInterruptAgent) *httptest.Server {
	t.Helper()
	srv := agentservice.New(
		agentservice.WithTaskLog(tasklog.New()),
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithToolHeartbeatInterval(0),
	)
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return ts
}

// readStreamUntilToolUse consumes events from ch until a tool_use event is seen.
// It fails the test if the channel closes first.
func readStreamUntilToolUse(t *testing.T, ch <-chan v1.Event) {
	t.Helper()
	for ev := range ch {
		if ev.Type == v1.EventToolUse {
			return
		}
		if ev.Type == v1.EventError {
			t.Fatalf("unexpected error before tool_use: %s", ev.Error)
		}
	}
	t.Fatal("stream ended before tool_use")
}

func TestTurnInterrupt_ClientV1RedirectsInFlightTurn(t *testing.T) {
	agent := &turnInterruptAgent{name: "codex"}
	ts := newTurnInterruptHTTP(t, agent)
	client := v1.New(ts.URL, v1.WithNoTimeout())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent: "codex", WorkDir: t.TempDir(), SessionDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	oldStream, err := sess.SendText(ctx, "run long tool")
	if err != nil {
		t.Fatalf("send text: %v", err)
	}
	oldEvents := oldStream.Events()
	readStreamUntilToolUse(t, oldEvents)
	oldTurn := oldStream.TurnID()
	if oldTurn == "" {
		t.Fatal("old turn id empty")
	}

	newStream, err := sess.Interrupt(ctx, "Stop. Only create b.txt.")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	var sawInterruptSystem, sawReplanned, sawResult, sawNewError bool
	for ev := range newStream.Events() {
		switch ev.Type {
		case v1.EventSystem:
			if ev.InterruptMode != "" {
				sawInterruptSystem = true
				if ev.InterruptMode != v1.InterruptModeFallback {
					t.Fatalf("interrupt_mode = %q, want %q", ev.InterruptMode, v1.InterruptModeFallback)
				}
				if ev.InterruptedTurnID != oldTurn {
					t.Fatalf("interrupted_turn_id = %q, want %q", ev.InterruptedTurnID, oldTurn)
				}
			}
		case v1.EventText:
			if ev.Text == "replanned" {
				sawReplanned = true
			}
		case v1.EventResult:
			sawResult = true
		case v1.EventError:
			sawNewError = true
		}
	}
	if sawNewError {
		t.Fatal("new (replan) stream must not carry an error")
	}
	if !sawInterruptSystem || !sawReplanned || !sawResult {
		t.Fatalf("replan stream incomplete: system=%v replanned=%v result=%v",
			sawInterruptSystem, sawReplanned, sawResult)
	}
	if newStream.TurnID() == "" || newStream.TurnID() == oldTurn {
		t.Fatalf("new turn id = %q, old = %q", newStream.TurnID(), oldTurn)
	}

	// The old turn's subscriber must receive the terminal interrupt error.
	var sawOldInterrupted bool
	for ev := range oldEvents {
		if ev.Type == v1.EventError && ev.Error == v1.TurnInterruptedContent {
			sawOldInterrupted = true
		}
	}
	if !sawOldInterrupted {
		t.Fatal("old stream missing turn interrupted error")
	}

	info, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if info.Status != codingagent.StatusCompleted {
		t.Fatalf("status = %q, want completed", info.Status)
	}
	if info.Error != "" {
		t.Fatalf("error = %q, want empty", info.Error)
	}
	if info.Followable {
		t.Fatal("followable should be false")
	}
}

func TestTurnInterrupt_CanonicalHistoryOrder(t *testing.T) {
	agent := &turnInterruptAgent{name: "codex"}
	ts := newTurnInterruptHTTP(t, agent)
	client := v1.New(ts.URL, v1.WithNoTimeout())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent: "codex", WorkDir: t.TempDir(), SessionDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	oldStream, err := sess.SendText(ctx, "run long tool")
	if err != nil {
		t.Fatalf("send text: %v", err)
	}
	oldEvents := oldStream.Events()
	readStreamUntilToolUse(t, oldEvents)
	oldTurn := oldStream.TurnID()

	newStream, err := sess.Interrupt(ctx, "Stop. Only create b.txt.")
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	for range newStream.Events() {
	}
	for range oldEvents {
	}

	info, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	msgs, err := session.OpenCanonical(info.SessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatalf("load canonical: %v", err)
	}

	idxUser, idxWorking, idxTool, idxInterrupt, idxReplan := -1, -1, -1, -1, -1
	wantHist := agentservice.InterruptHistoryMarker + " interrupted_turn_id=" + oldTurn + " mode=fallback"
	for i, m := range msgs {
		switch {
		case m.Role == "user" && strings.Contains(m.Content, "run long tool"):
			idxUser = i
		case m.Role == "assistant" && strings.Contains(m.Content, "working"):
			idxWorking = i
		case m.Role == "assistant" && len(m.ToolCalls) > 0:
			idxTool = i
		case m.Role == "user" && strings.Contains(m.Content, agentservice.InterruptHistoryMarker):
			idxInterrupt = i
			if !strings.Contains(m.Content, wantHist) {
				t.Fatalf("interrupt record = %q, want to contain %q", m.Content, wantHist)
			}
		case m.Role == "assistant" && strings.Contains(m.Content, "replanned"):
			idxReplan = i
		}
	}
	if idxUser < 0 || idxWorking < 0 || idxTool < 0 || idxInterrupt < 0 || idxReplan < 0 {
		t.Fatalf("missing canonical messages: user=%d working=%d tool=%d interrupt=%d replan=%d msgs=%+v",
			idxUser, idxWorking, idxTool, idxInterrupt, idxReplan, msgs)
	}
	if !(idxUser < idxWorking && idxWorking < idxTool && idxTool < idxInterrupt && idxInterrupt < idxReplan) {
		t.Fatalf("canonical order wrong: user=%d working=%d tool=%d interrupt=%d replan=%d",
			idxUser, idxWorking, idxTool, idxInterrupt, idxReplan)
	}
}

func TestTurnInterrupt_IdleSessionClientError(t *testing.T) {
	agent := &turnInterruptAgent{name: "codex"}
	ts := newTurnInterruptHTTP(t, agent)
	client := v1.New(ts.URL, v1.WithNoTimeout())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent: "codex", WorkDir: t.TempDir(), SessionDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = sess.Interrupt(ctx, "change of plan")
	if err == nil {
		t.Fatal("expected interrupt error on idle session")
	}
	if !strings.Contains(err.Error(), "409") || !strings.Contains(err.Error(), "no active turn") {
		t.Fatalf("error = %v, want 409 and 'no active turn'", err)
	}
	if agent.creates.Load() != 0 {
		t.Fatalf("creates = %d, want 0", agent.creates.Load())
	}
}

func TestTurnInterrupt_FollowSeesReplanTurn(t *testing.T) {
	agent := &turnInterruptAgent{name: "codex", replanDelay: 300 * time.Millisecond}
	ts := newTurnInterruptHTTP(t, agent)
	client := v1.New(ts.URL, v1.WithNoTimeout())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent: "codex", WorkDir: t.TempDir(), SessionDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	oldStream, err := sess.SendText(ctx, "run long tool")
	if err != nil {
		t.Fatalf("send text: %v", err)
	}
	oldEvents := oldStream.Events()
	readStreamUntilToolUse(t, oldEvents)
	oldTurn := oldStream.TurnID()

	// Interrupt over raw HTTP, read only the first event, then drop the body.
	// The replan turn detaches and keeps running for replanDelay.
	newTurn := startInterruptAndReadFirstTurn(t, ts, sess.ID, "redirect to follow")
	if newTurn == "" || newTurn == oldTurn {
		t.Fatalf("new turn id = %q, old = %q", newTurn, oldTurn)
	}

	follow, err := sess.Follow(ctx)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	var sawInterruptSystem, sawResult bool
	for ev := range follow.Events() {
		if ev.Type == v1.EventSystem && ev.InterruptMode == v1.InterruptModeFallback {
			sawInterruptSystem = true
		}
		if ev.Type == v1.EventResult {
			sawResult = true
		}
	}
	if follow.TurnID() != newTurn {
		t.Fatalf("follow turn id = %q, want %q", follow.TurnID(), newTurn)
	}
	if !sawInterruptSystem {
		t.Fatal("follow did not replay the turn interrupt system event")
	}
	if !sawResult {
		t.Fatal("follow did not end with a result")
	}

	// Drain the old subscriber so the goroutine exits cleanly.
	for range oldEvents {
	}
}

// startInterruptAndReadFirstTurn posts an interrupt as SSE, reads events until a
// turn_id appears (the replan turn's opening system event), then closes the body.
func startInterruptAndReadFirstTurn(t *testing.T, ts *httptest.Server, sessionID, content string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"content": content})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sessionID+"/interrupt", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new interrupt request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("interrupt request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("interrupt status = %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var ev codingagent.StreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.TurnID != "" {
			return ev.TurnID
		}
	}
	t.Fatal("interrupt stream ended before a turn_id was seen")
	return ""
}
