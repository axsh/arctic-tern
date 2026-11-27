package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

// cawaBridge is a tiny wrapper around client/v1.
//
// Realtime (in the browser) handles low-latency voice.
// This bridge is the only place that talks to the Coding Agent, via Tern CAWA.
// Keeping those roles separate is the point of the demo.
type cawaBridge struct {
	client     *v1.Client
	session    *v1.Session
	agent      string
	workDir    string
	sessionDir string

	mu            sync.Mutex
	busy          bool
	lastAction    Action
	lastSummary   string
	interruptMode string
	// openQuestion is the human utterance (A) last sent to CAWA; kept for
	// turn-complete reconsider framing until a newer sent utterance replaces it.
	openQuestion string
}

func newCawaBridge(ternURL, agent, workDir, sessionDir string) *cawaBridge {
	return &cawaBridge{
		client:     v1.New(ternURL, v1.WithNoTimeout()),
		agent:      agent,
		workDir:    workDir,
		sessionDir: sessionDir,
	}
}

func (b *cawaBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.session != nil {
		return fmt.Errorf("session already started")
	}
	sess, err := b.client.CreateSession(ctx, v1.SessionRequest{
		Agent:       b.agent,
		WorkDir:     b.workDir,
		SessionDir:  b.sessionDir,
		SandboxMode: v1.SandboxModeWorkspaceWrite,
	})
	if err != nil {
		return err
	}
	b.session = sess
	b.busy = false
	b.lastAction = ActionNone
	b.lastSummary = ""
	b.interruptMode = ""
	b.openQuestion = ""
	log.Printf("cawa: CreateSession ok id=%s agent=%s", sess.ID, b.agent)
	return nil
}

func (b *cawaBridge) End(ctx context.Context) error {
	b.mu.Lock()
	sess := b.session
	b.session = nil
	b.busy = false
	b.openQuestion = ""
	b.mu.Unlock()
	if sess == nil {
		return nil
	}
	// Demo choice: terminate on hang-up so the next Start is a clean slate.
	if err := sess.Terminate(ctx); err != nil {
		return err
	}
	log.Printf("cawa: Terminate ok id=%s", sess.ID)
	return nil
}

func (b *cawaBridge) Busy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.busy
}

func (b *cawaBridge) Snapshot() (busy bool, lastAction Action, lastSummary, interruptMode, sessionID, openQuestion string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := ""
	if b.session != nil {
		id = b.session.ID
	}
	return b.busy, b.lastAction, b.lastSummary, b.interruptMode, id, b.openQuestion
}

// HandleUtterance applies routing (auto/always/never) then calls CAWA when send=true.
// The HTTP handler can return as soon as the stream is opened; we mark busy
// immediately so a second work utterance becomes an interrupt (barge-in).
func (b *cawaBridge) HandleUtterance(ctx context.Context, text, routeMode string) (sent bool, action Action, reason, summary string, err error) {
	b.mu.Lock()
	sess := b.session
	busy := b.busy
	b.mu.Unlock()
	if sess == nil {
		return false, ActionNone, "", "", fmt.Errorf("no CAWA session; call /api/call/start first")
	}

	send, action, reason := DecideUtteranceRoute(busy, text, routeMode)
	if !send {
		return false, ActionNone, reason, "", nil
	}

	// Detach from the HTTP request context. If we keep r.Context(), the
	// handler returning would cancel the SSE body and clear busy too early,
	// so the next utterance would incorrectly look "idle" and call messages.
	streamCtx := context.WithoutCancel(ctx)

	var stream *v1.Stream
	switch action {
	case ActionMessages:
		stream, err = sess.SendText(streamCtx, text)
	case ActionInterrupt:
		stream, err = sess.Interrupt(streamCtx, text)
	default:
		return false, ActionNone, reason, "", fmt.Errorf("unsupported action %s", action)
	}
	if err != nil {
		return true, action, reason, "", err
	}

	b.mu.Lock()
	b.busy = true
	b.lastAction = action
	b.lastSummary = "Turn started (" + string(action) + ")"
	b.openQuestion = strings.TrimSpace(text)
	b.mu.Unlock()

	go b.consumeStream(stream, action)
	return true, action, reason, b.summaryLockedCopy(), nil
}

func (b *cawaBridge) summaryLockedCopy() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSummary
}

func (b *cawaBridge) consumeStream(stream *v1.Stream, action Action) {
	defer func() {
		b.mu.Lock()
		b.busy = false
		if b.lastSummary == "" || b.lastSummary == "Turn started ("+string(action)+")" {
			b.lastSummary = "Turn finished"
		}
		b.mu.Unlock()
		log.Printf("cawa: stream done action=%s", action)
	}()

	for ev := range stream.Events() {
		mode := ev.InterruptMode
		sum := SummarizeEvent(string(ev.Type), ev.Text, ev.ToolName, mode)
		if ev.Type == v1.EventError && ev.Error != "" {
			sum = SummarizeEvent("text", ev.Error, "", "")
		}
		b.mu.Lock()
		if mode != "" {
			b.interruptMode = mode
		}
		if sum != "" {
			b.lastSummary = sum
		}
		b.mu.Unlock()
	}
}
