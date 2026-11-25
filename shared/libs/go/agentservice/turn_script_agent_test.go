package agentservice_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// turnScriptAgent runs scripted turns. A hanging turn emits system/text and a
// tool_use (or user_input_required) and then blocks until its context is
// cancelled. Other turns emit text "replanned" and result.
type turnScriptAgent struct {
	name          string
	hang          func(turn int) bool // 1-based turn number
	suspend       bool                // hanging turns emit user_input_required instead of tool_use
	blockCreate   chan struct{}       // when non-nil, CreateSession of turn >= 2 waits until closed
	createEntered chan struct{}       // buffered(1); signaled when a blocked CreateSession starts

	mu      sync.Mutex
	prompts []string // prompt passed to Send, per turn
	resumes []string // SessionConfig.AgentSessionID, per CreateSession
	creates atomic.Int32
	closes  atomic.Int32
}

func newTurnScriptAgent(hang func(turn int) bool) *turnScriptAgent {
	return &turnScriptAgent{
		name:          "codex",
		hang:          hang,
		createEntered: make(chan struct{}, 1),
	}
}

func (a *turnScriptAgent) Name() string { return a.name }

func (a *turnScriptAgent) Close() error { return nil }

func (a *turnScriptAgent) CreateSession(_ context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error) {
	n := a.creates.Add(1)
	cfg := codingagent.NewSessionConfig(opts...)
	a.mu.Lock()
	a.resumes = append(a.resumes, cfg.AgentSessionID)
	a.mu.Unlock()
	if n >= 2 && a.blockCreate != nil {
		select {
		case a.createEntered <- struct{}{}:
		default:
		}
		<-a.blockCreate
	}
	return &turnScriptSession{agent: a, turn: int(n)}, nil
}

func (a *turnScriptAgent) promptAt(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.prompts) {
		return ""
	}
	return a.prompts[i]
}

func (a *turnScriptAgent) resumeAt(i int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.resumes) {
		return ""
	}
	return a.resumes[i]
}

type turnScriptSession struct {
	agent  *turnScriptAgent
	turn   int
	closed atomic.Bool
}

func (s *turnScriptSession) ID() string { return "script-" + itoaScript(s.turn) }

func (s *turnScriptSession) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.agent.closes.Add(1)
	}
	return nil
}

func (s *turnScriptSession) Send(ctx context.Context, prompt string) (<-chan codingagent.StreamEvent, error) {
	s.agent.mu.Lock()
	s.agent.prompts = append(s.agent.prompts, prompt)
	s.agent.mu.Unlock()

	ch := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(ch)
		if s.agent.hang(s.turn) {
			ch <- codingagent.StreamEvent{Type: codingagent.EventSystem, SessionID: "native-thread-1"}
			ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "working on a.txt"}
			if s.agent.suspend {
				ch <- codingagent.StreamEvent{Type: codingagent.EventUserInputRequired, Content: "Proceed?"}
			} else {
				ch <- codingagent.StreamEvent{
					Type:      codingagent.EventToolUse,
					ToolName:  "command_execution",
					ToolInput: map[string]any{"command": "sleep 60"},
				}
			}
			<-ctx.Done()
			return
		}
		ch <- codingagent.StreamEvent{Type: codingagent.EventText, Content: "replanned"}
		ch <- codingagent.StreamEvent{Type: codingagent.EventResult}
	}()
	return ch, nil
}

func itoaScript(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// newScriptServer builds an httptest server fronting a turnScriptAgent.
func newScriptServer(t *testing.T, agent *turnScriptAgent, opts ...agentservice.ServerOption) (*agentservice.Server, *httptest.Server) {
	t.Helper()
	base := []agentservice.ServerOption{
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1}),
		agentservice.WithToolHeartbeatInterval(0),
	}
	base = append(base, opts...)
	srv := agentservice.New(base...)
	srv.RegisterAgent(agent)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func createScriptSession(t *testing.T, ts *httptest.Server) (sessionID, sessionDir string) {
	t.Helper()
	sessionDir = t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":       "codex",
		"session_dir": sessionDir,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status = %d", resp.StatusCode)
	}
	var created map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	return created["session_id"], sessionDir
}

type sseCapture struct {
	done   chan struct{}
	mu     sync.Mutex
	events []map[string]any
	sawDone bool
	status int
	raw    string
}

func startSSE(t *testing.T, ts *httptest.Server, path string, body any) *sseCapture {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new sse request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	c := &sseCapture{done: make(chan struct{})}
	c.status = resp.StatusCode
	go func() {
		defer close(c.done)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			c.mu.Lock()
			c.raw += line + "\n"
			c.mu.Unlock()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				c.mu.Lock()
				c.sawDone = true
				c.mu.Unlock()
				return
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				continue
			}
			c.mu.Lock()
			c.events = append(c.events, ev)
			c.mu.Unlock()
		}
	}()
	return c
}

func (c *sseCapture) waitFor(t *testing.T, pred func(ev map[string]any) bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range c.snapshot() {
			if pred(ev) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for SSE event; got %v", c.snapshot())
}

func (c *sseCapture) waitDone(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for SSE [DONE]; got %v", c.snapshot())
	}
}

func (c *sseCapture) snapshot() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.events...)
}

func postJSON(t *testing.T, ts *httptest.Server, path string, body any, accept string) (int, []byte) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func getSessionInfo(t *testing.T, ts *httptest.Server, sessionID string) map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/v1/sessions/" + sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func waitInFlightTurn(t *testing.T, ts *httptest.Server, sessionID string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info := getSessionInfo(t, ts, sessionID)
		if followable, _ := info["followable"].(bool); followable {
			if turnID, _ := info["turn_id"].(string); turnID != "" {
				return turnID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for in-flight turn on %s", sessionID)
	return ""
}

func loadCanonical(t *testing.T, sessionDir string) []session.Message {
	t.Helper()
	msgs, err := session.OpenCanonical(sessionDir).LoadRange(1, 0)
	if err != nil {
		t.Fatalf("load canonical: %v", err)
	}
	return msgs
}
