package agentservice

import (
	"context"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

const (
	// TurnInterruptedContent is the terminal error content on the interrupted turn's stream.
	TurnInterruptedContent = "turn interrupted"
	// sessionTerminatedContent is the terminal content reported for a terminated turn.
	sessionTerminatedContent = "session terminated"
)

// stopOptions selects the extra work stopInFlightTurn performs for its caller.
type stopOptions struct {
	reason   string                   // stopReason* constant
	terminal *codingagent.StreamEvent // appended to the stopped turn's relay so subscribers see an explicit terminal
	ingest   bool                     // ingest the stopped turn's partial facts into canonical history
}

// stopInFlightTurn stops exec on behalf of an API call. The caller must hold the
// session turn gate. After it returns, exec makes no further session-record writes;
// the caller writes the final session status. Returns the stopped turn's relay snapshot.
//
// The step order is itself an invariant: markStopped is set before CancelExecution so
// late writes are suppressed, and the registry entries are released only after the
// reconcile and ingest have read the still-registered relay.
func (s *Server) stopInFlightTurn(ctx context.Context, exec *activeExecution, opts stopOptions) []codingagent.StreamEvent {
	if exec == nil {
		return nil
	}
	// 1. Stop late writes before cancelling the process.
	exec.markStopped(opts.reason)
	// 2. Stop any reattach timer.
	exec.stopReattachTimer()
	// 3. Cancel the CLI process / Wayfinder Run.
	s.CancelExecution(exec.sessionID)
	// 4. Best-effort close of the agent session.
	if exec.agentSess != nil {
		_ = exec.agentSess.Close()
	}
	// 5-6. Append an explicit terminal (at most once) and let subscribers drain.
	s.finishTurnRelay(exec, opts.terminal)
	// 7. Reconcile while the relay is still registered (turn_files flush reads it).
	if s.artifactStore != nil {
		s.reconcileSessionArtifacts(ctx, exec.sessionID, exec.turnID, exec.correlationID)
	}
	// 8. Snapshot the relay for the caller and ingest.
	events := exec.relay.snapshot()
	// 9. Optionally ingest the partial facts into canonical history.
	if opts.ingest {
		s.ingestTurnEvents(exec.sessionID, events)
	}
	// 10. Release the busy-state registries owned by exec.
	s.execRegistry.UnregisterIf(exec.sessionID, exec)
	s.UnregisterActiveSession(exec.sessionID)
	s.UnregisterExecCancel(exec.sessionID)
	// 11. Return the stopped turn's relay snapshot.
	return events
}

// finishTurnRelay ends exec's relay for its subscribers: it appends terminal at most
// once (with turn/correlation ids and the turn's usage finalized) and marks the source
// done. A nil terminal only marks the source done. Safe to call more than once; the
// terminal is appended only on the first call (guarded by stopMu via terminalAppended).
func (s *Server) finishTurnRelay(exec *activeExecution, terminal *codingagent.StreamEvent) {
	if exec == nil {
		return
	}
	if terminal != nil && exec.relay != nil {
		exec.stopMu.Lock()
		first := !exec.terminalAppended
		exec.terminalAppended = true
		exec.stopMu.Unlock()
		if first {
			ev := *terminal
			ev.TurnID = exec.turnID
			ev.CorrelationID = exec.correlationID
			exec.relay.appendEvent(ev)
			s.applyUsageSideEffects(exec.sessionID, exec, &ev)
		}
	}
	if exec.relay != nil {
		exec.relay.markSourceDone()
	}
}

// detachInterruptedTurn finalizes a turn whose agent process continues under a new
// turn (native interrupt): it reconciles and ingests the old turn and unregisters it
// from the exec registry, but keeps the process, its cancel func and its active-session
// entry for the successor. The caller must hold the session turn gate and have marked
// exec stopped (and already finished its relay). Returns the stopped turn's snapshot.
func (s *Server) detachInterruptedTurn(ctx context.Context, exec *activeExecution) []codingagent.StreamEvent {
	if exec == nil {
		return nil
	}
	exec.stopReattachTimer()
	if s.artifactStore != nil {
		s.reconcileSessionArtifacts(ctx, exec.sessionID, exec.turnID, exec.correlationID)
	}
	events := exec.relay.snapshot()
	s.ingestTurnEvents(exec.sessionID, events)
	s.execRegistry.UnregisterIf(exec.sessionID, exec)
	return events
}

// stoppedTerminalContent maps a stop reason to the terminal content a detached wait reports.
func stoppedTerminalContent(reason string) string {
	switch reason {
	case stopReasonCancelled:
		return turnCancelledError
	case stopReasonInterrupted:
		return TurnInterruptedContent
	case stopReasonTerminated:
		return sessionTerminatedContent
	default:
		return drainTimeoutTerminalContent
	}
}
