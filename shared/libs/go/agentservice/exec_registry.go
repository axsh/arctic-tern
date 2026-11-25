package agentservice

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// ErrSessionBusy is returned when a session already has an active execution.
var ErrSessionBusy = errors.New("session busy")

const hintSessionBusy = "follow, respond, interrupt, cancel or terminate"

// Stop reasons recorded on an execution stopped outside its own runTurn loop.
const (
	stopReasonCancelled    = "cancelled"
	stopReasonInterrupted  = "interrupted"
	stopReasonTerminated   = "terminated"
	stopReasonDrainTimeout = "drain_timeout"
)

// DefaultToolHeartbeatInterval is the SSE progress heartbeat period while a
// tool is in-flight (EventToolUse until matching EventToolResult / turn end).
const DefaultToolHeartbeatInterval = 30 * time.Second

// toolStillRunningContent is the EventProgress content used as tool liveness.
// Distinct from WBS progress payloads (e.g. "2/5"); clients may treat this
// string as a stall-reset heartbeat.
const toolStillRunningContent = "tool_still_running"

type activeExecution struct {
	sessionID        string
	turnID           string
	sessionModel     string
	correlationID    string
	agentSess        codingagent.Session
	stdin            codingagent.StdinWriter
	relay            *eventRelay
	status           string
	streamOffset     int
	sideEffectOffset int
	savedFiles       []string
	usageAgg         *turnUsageAggregator

	subMu         sync.Mutex
	subCancel     context.CancelFunc
	subscriberGen int
	reattachTimer *time.Timer

	execCtx    context.Context    // agent process lifetime; transferred on native interrupt
	execCancel context.CancelFunc // cancels execCtx; transferred on native interrupt

	stopMu           sync.Mutex
	stopReason       string
	terminalAppended bool // guarded by stopMu; ensures the turn's terminal is appended at most once
}

// markStopped records why exec was stopped. It returns false when exec was already stopped.
func (e *activeExecution) markStopped(reason string) bool {
	if e == nil {
		return false
	}
	e.stopMu.Lock()
	defer e.stopMu.Unlock()
	if e.stopReason != "" {
		return false
	}
	e.stopReason = reason
	return true
}

// stoppedReason returns why exec was stopped, or "" when it is still running. nil-safe.
func (e *activeExecution) stoppedReason() string {
	if e == nil {
		return ""
	}
	e.stopMu.Lock()
	defer e.stopMu.Unlock()
	return e.stopReason
}

// isStopped reports whether exec was stopped outside its own runTurn loop. nil-safe.
func (e *activeExecution) isStopped() bool {
	return e.stoppedReason() != ""
}

// suppressesSessionWrites reports whether late writes from exec to the session record
// (status, error, agent_session_id, usage) must be skipped: the API call that stopped it
// (cancel / interrupt / terminate) writes the final state itself. Drain timeouts keep
// today's late status write.
func (e *activeExecution) suppressesSessionWrites() bool {
	switch e.stoppedReason() {
	case stopReasonCancelled, stopReasonInterrupted, stopReasonTerminated:
		return true
	default:
		return false
	}
}

type execRegistry struct {
	mu   sync.Mutex
	exec map[string]*activeExecution
}

func newExecRegistry() *execRegistry {
	return &execRegistry{exec: make(map[string]*activeExecution)}
}

func (r *execRegistry) Register(id string, exec *activeExecution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exec[id]; ok {
		return ErrSessionBusy
	}
	r.exec[id] = exec
	return nil
}

func (r *execRegistry) Get(id string) (*activeExecution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	exec, ok := r.exec[id]
	return exec, ok
}

func (r *execRegistry) SetStatus(id, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if exec, ok := r.exec[id]; ok {
		exec.status = status
	}
}

func (r *execRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.exec, id)
}

// UnregisterIf removes id only when it still maps to exec.
func (r *execRegistry) UnregisterIf(id string, exec *activeExecution) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.exec[id]; ok && cur == exec {
		delete(r.exec, id)
		return true
	}
	return false
}

// prependEvents yields preamble before forwarding every event of src; the
// returned channel closes when src closes.
func prependEvents(src <-chan codingagent.StreamEvent, preamble ...codingagent.StreamEvent) <-chan codingagent.StreamEvent {
	out := make(chan codingagent.StreamEvent, len(preamble)+8)
	go func() {
		defer close(out)
		for _, ev := range preamble {
			out <- ev
		}
		for ev := range src {
			out <- ev
		}
	}()
	return out
}

func (e *activeExecution) stopReattachTimer() {
	if e == nil {
		return
	}
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if e.reattachTimer != nil {
		e.reattachTimer.Stop()
		e.reattachTimer = nil
	}
}

func (e *activeExecution) stealSubscriber() (gen int, subCtx context.Context) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if e.reattachTimer != nil {
		e.reattachTimer.Stop()
		e.reattachTimer = nil
	}
	if e.subCancel != nil {
		e.subCancel()
		e.subCancel = nil
	}
	subCtx, e.subCancel = context.WithCancel(context.Background())
	e.subscriberGen++
	return e.subscriberGen, subCtx
}

func (e *activeExecution) clearSubscriber(gen int) bool {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if gen != e.subscriberGen {
		return false
	}
	e.subCancel = nil
	return true
}

func (e *activeExecution) hasSubscriber() bool {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	return e.subCancel != nil
}

// eventRelay buffers events from a single agent channel for sequential SSE consumers.
// While a tool is in-flight it also injects EventProgress heartbeats so subscribers
// see liveness even when the agent emits no stdout.
type eventRelay struct {
	mu         sync.Mutex
	events     []codingagent.StreamEvent
	notify     chan struct{} // capacity 1; event wakeups (may drop)
	doneCh     chan struct{} // closed exactly once when source exhausts
	sourceDone bool
	doneOnce   sync.Once
}

func newEventRelay(source <-chan codingagent.StreamEvent) *eventRelay {
	return newEventRelayWithHeartbeat(source, DefaultToolHeartbeatInterval)
}

func newEventRelayWithHeartbeat(source <-chan codingagent.StreamEvent, interval time.Duration) *eventRelay {
	r := &eventRelay{
		notify: make(chan struct{}, 1),
		doneCh: make(chan struct{}),
	}
	go r.pump(source, interval)
	return r
}

func (r *eventRelay) appendEvent(ev codingagent.StreamEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *eventRelay) markSourceDone() {
	r.mu.Lock()
	r.sourceDone = true
	r.mu.Unlock()
	r.doneOnce.Do(func() { close(r.doneCh) })
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *eventRelay) pump(source <-chan codingagent.StreamEvent, interval time.Duration) {
	var (
		toolDepth int
		toolName  string
		ticker    *time.Ticker
		tickC     <-chan time.Time
	)
	stopTicker := func() {
		if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickC = nil
		}
	}
	startOrResetTicker := func(name string) {
		if name != "" {
			toolName = name
		}
		if interval <= 0 {
			return
		}
		stopTicker()
		ticker = time.NewTicker(interval)
		tickC = ticker.C
	}
	defer stopTicker()

	for {
		select {
		case ev, ok := <-source:
			if !ok {
				r.markSourceDone()
				return
			}
			switch ev.Type {
			case codingagent.EventToolUse:
				toolDepth++
				startOrResetTicker(ev.ToolName)
			case codingagent.EventToolResult:
				if toolDepth > 0 {
					toolDepth--
				}
				if toolDepth == 0 {
					stopTicker()
					toolName = ""
				}
			case codingagent.EventResult, codingagent.EventError:
				stopTicker()
				toolDepth = 0
				toolName = ""
			}
			r.appendEvent(ev)
		case <-tickC:
			if toolDepth <= 0 {
				continue
			}
			r.appendEvent(codingagent.StreamEvent{
				Type:     codingagent.EventProgress,
				Content:  toolStillRunningContent,
				ToolName: toolName,
			})
		}
	}
}

// stream returns events starting at startIdx. When stopOnUserInput is true,
// the channel closes after delivering EventUserInputRequired.
func (r *eventRelay) stream(startIdx int, stopOnUserInput bool) <-chan codingagent.StreamEvent {
	ch := make(chan codingagent.StreamEvent, 8)
	go func() {
		defer close(ch)
		idx := startIdx
		for {
			r.mu.Lock()
			for idx < len(r.events) {
				ev := r.events[idx]
				idx++
				r.mu.Unlock()
				ch <- ev
				if stopOnUserInput && ev.Type == codingagent.EventUserInputRequired {
					return
				}
				r.mu.Lock()
			}
			done := r.sourceDone
			r.mu.Unlock()
			if done {
				return
			}
			select {
			case <-r.notify:
			case <-r.doneCh:
			}
		}
	}()
	return ch
}

func (r *eventRelay) eventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// isSourceDone reports whether the upstream agent event channel has closed.
func (r *eventRelay) isSourceDone() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sourceDone
}

// EventsSnapshot returns a copy of buffered relay events.
func (r *eventRelay) EventsSnapshot() []codingagent.StreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]codingagent.StreamEvent(nil), r.events...)
}

func (r *eventRelay) snapshot() []codingagent.StreamEvent {
	if r == nil {
		return nil
	}
	return r.EventsSnapshot()
}
