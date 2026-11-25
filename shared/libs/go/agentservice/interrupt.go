package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// InterruptRequest is the request body for POST /api/v1/sessions/:id/interrupt.
type InterruptRequest struct {
	Content       string                      `json:"content"`
	CorrelationID string                      `json:"correlation_id,omitempty"`
	Supplement    *session.SupplementStrategy `json:"supplement,omitempty"`
}

// Interrupt modes reported on the new turn's stream and in server logs.
const (
	InterruptModeFallback = "fallback"
	InterruptModeNative   = "native"
)

const (
	// InterruptEventContent is the content of the system event that opens a turn started by an interrupt.
	InterruptEventContent = "turn interrupt"
	// interruptPromptReserveBytes is the prompt budget kept for the interrupt notice and progress summary.
	interruptPromptReserveBytes = 8 * 1024

	errNoActiveTurn             = "no active turn"
	errTurnTransitionInProgress = "turn transition in progress"
	errSessionClosed            = "session closed"
	hintInterruptNoActiveTurn   = "send a message to start a new turn"
	hintTurnTransition          = "retry after the current turn transition completes"
	logTurnInterrupted          = "turn interrupted"
	logNativeInterruptFailed    = "native interrupt failed; falling back"
)

// handleInterrupt handles POST /api/v1/sessions/:id/interrupt.
func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
	if len(parts) < 2 || parts[1] != "interrupt" || parts[0] == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	sessionID := parts[0]

	record, err := s.sessions.Get(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if record.Status == codingagent.StatusClosed {
		writeJSONError(w, http.StatusConflict, errSessionClosed, "")
		return
	}

	var req InterruptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if req.Supplement != nil && !portable.KnownAlgorithm(req.Supplement.Algorithm) {
		http.Error(w, "unknown supplement algorithm: "+req.Supplement.Algorithm, http.StatusBadRequest)
		return
	}

	if _, ok := s.agents[record.AgentName]; !ok {
		http.Error(w, "agent not available", http.StatusInternalServerError)
		return
	}

	maxBytes := s.resolveAgentConfig(record.AgentName).MaxPromptBytes
	if len(req.Content)+interruptPromptReserveBytes > maxBytes {
		limit := maxBytes - interruptPromptReserveBytes
		if limit < 0 {
			limit = 0
		}
		http.Error(w, fmt.Sprintf("interrupt content (%d bytes) exceeds the limit (%d bytes)",
			len(req.Content), limit), http.StatusRequestEntityTooLarge)
		return
	}

	release, ok := s.turnGates.TryLock(sessionID)
	if !ok {
		writeJSONError(w, http.StatusConflict, errTurnTransitionInProgress, hintTurnTransition)
		return
	}
	defer release()

	old, ok := s.execRegistry.Get(sessionID)
	if !ok {
		writeJSONError(w, http.StatusConflict, errNoActiveTurn, hintInterruptNoActiveTurn)
		return
	}

	if latest, err := s.sessions.Get(sessionID); err == nil {
		record = latest
	}
	if record.Status == codingagent.StatusClosed {
		writeJSONError(w, http.StatusConflict, errSessionClosed, "")
		return
	}
	s.applySessionModelDefault(record)

	agent := s.agents[record.AgentName]
	mode, ti := s.selectInterruptMode(agent, old)
	newTurnID := s.generateID()
	stopCtx := context.WithoutCancel(r.Context())

	terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}
	if mode == InterruptModeNative {
		// End the old turn for its subscribers once, then hand the interrupt to the
		// running agent. The process and its execution context move to the new turn.
		old.markStopped(stopReasonInterrupted)
		s.finishTurnRelay(old, &terminal)
		if s.serveNativeInterrupt(w, r, stopCtx, record, old, ti, req, newTurnID, release) {
			return
		}
		// The agent rejected the interrupt; fall back within the same request.
		mode = InterruptModeFallback
	}
	events := s.stopInFlightTurn(stopCtx, old, stopOptions{
		reason:   stopReasonInterrupted,
		terminal: &terminal,
		ingest:   true,
	})

	if latest, err := s.sessions.Get(sessionID); err == nil {
		record = latest
	}
	record.Status = codingagent.StatusError
	record.Error = TurnInterruptedContent
	if err := s.sessions.Update(record); err != nil && s.logger != nil {
		s.logger.Warn("failed to persist interrupt session state", "session_id", sessionID, "error", err)
	}

	historyText := formatInterruptHistory(old.turnID, mode, req.Content)
	if err := AppendSessionMessage(record.SessionDir, session.Message{
		Role: "user",
		Content: historyText,
		ContentParts: []session.ContentPart{{
			Type: "text",
			Text: historyText,
		}},
		Timestamp: time.Now(),
		Origin:    record.AgentName,
	}); err != nil && s.logger != nil {
		s.logger.Warn("failed to append interrupt history", "session_id", sessionID, "error", err)
	}

	if s.logger != nil {
		s.logger.Info(logTurnInterrupted,
			"session_id", sessionID,
			"agent", record.AgentName,
			"interrupted_turn_id", old.turnID,
			"turn_id", newTurnID,
			"mode", mode,
			"correlation_id", req.CorrelationID,
		)
	}

	prompt := buildInterruptReplanPrompt(interruptPromptInput{
		InterruptedTurnID: old.turnID,
		Progress:          summarizeInterruptedTurn(events),
		Instruction:       req.Content,
	})

	s.launchTurn(w, r, record, turnLaunch{
		turnID:        newTurnID,
		agentPrompt:   prompt,
		correlationID: req.CorrelationID,
		supplement:    req.Supplement,
		preamble: []codingagent.StreamEvent{{
			Type:              codingagent.EventSystem,
			Content:           InterruptEventContent,
			InterruptMode:     mode,
			InterruptedTurnID: old.turnID,
		}},
		onRegistered: release,
	})
}

// selectInterruptMode returns InterruptModeNative and the session hook only when the
// agent declares native interrupt resume and the running session implements it;
// otherwise InterruptModeFallback and a nil hook.
func (s *Server) selectInterruptMode(agent codingagent.CodingAgent, exec *activeExecution) (string, codingagent.TurnInterrupter) {
	sup, ok := agent.(codingagent.InterruptResumeSupporter)
	if !ok || !sup.SupportsInterruptResume() {
		return InterruptModeFallback, nil
	}
	if exec == nil || exec.agentSess == nil {
		return InterruptModeFallback, nil
	}
	ti, ok := exec.agentSess.(codingagent.TurnInterrupter)
	if !ok {
		return InterruptModeFallback, nil
	}
	return InterruptModeNative, ti
}

// serveNativeInterrupt hands the interrupt to the running agent session and streams the
// continued turn as a new turn (new turn_id). It returns false, without writing to w,
// when the agent rejects the interrupt; the caller then falls back to Tern's replan.
// The caller must hold the session turn gate and have marked old stopped and finished
// its relay. On success the process ownership and execution context move to the new turn.
func (s *Server) serveNativeInterrupt(w http.ResponseWriter, r *http.Request, stopCtx context.Context,
	record *codingagent.SessionRecord, old *activeExecution, ti codingagent.TurnInterrupter,
	req InterruptRequest, newTurnID string, release func()) bool {

	sessionID := old.sessionID

	// 1. Hand the interrupt to the running agent session (reuse the old process lifetime).
	ctx := old.execCtx
	if ctx == nil {
		ctx = context.Background()
	}
	ch, err := ti.InterruptTurn(ctx, req.Content)
	if err != nil {
		// 2. The agent did not take over; the caller falls back. Nothing is written to w.
		if s.logger != nil {
			s.logger.Warn(logNativeInterruptFailed,
				"session_id", sessionID,
				"agent", record.AgentName,
				"interrupted_turn_id", old.turnID,
				"error", err.Error(),
			)
		}
		return false
	}

	// 3. Detach the old turn, keeping the process / cancel func / active-session entry.
	s.detachInterruptedTurn(stopCtx, old)

	// 4. Record the interrupt into canonical history (mode=native).
	if latest, err := s.sessions.Get(sessionID); err == nil {
		record = latest
	}
	historyText := formatInterruptHistory(old.turnID, InterruptModeNative, req.Content)
	if err := AppendSessionMessage(record.SessionDir, session.Message{
		Role:    "user",
		Content: historyText,
		ContentParts: []session.ContentPart{{
			Type: "text",
			Text: historyText,
		}},
		Timestamp: time.Now(),
		Origin:    record.AgentName,
	}); err != nil && s.logger != nil {
		s.logger.Warn("failed to append interrupt history", "session_id", sessionID, "error", err)
	}
	if s.logger != nil {
		s.logger.Info(logTurnInterrupted,
			"session_id", sessionID,
			"agent", record.AgentName,
			"interrupted_turn_id", old.turnID,
			"turn_id", newTurnID,
			"mode", InterruptModeNative,
			"correlation_id", req.CorrelationID,
		)
	}

	// 5. Snapshot the workdir for the continued turn.
	s.captureTurnSnapshot(sessionID, newTurnID, record.WorkDir)

	// 6. Build the continued execution; the process ownership moves from old to it.
	preamble := codingagent.StreamEvent{
		Type:              codingagent.EventSystem,
		Content:           InterruptEventContent,
		InterruptMode:     InterruptModeNative,
		InterruptedTurnID: old.turnID,
	}
	exec := &activeExecution{
		sessionID:     sessionID,
		turnID:        newTurnID,
		sessionModel:  record.Model,
		correlationID: req.CorrelationID,
		agentSess:     old.agentSess,
		stdin:         old.stdin,
		relay:         newEventRelayWithHeartbeat(prependEvents(ch, preamble), s.resolvedToolHeartbeatInterval()),
		status:        codingagent.StatusActive,
		usageAgg:      newTurnUsageAggregator(newTurnID),
		execCtx:       old.execCtx,
		execCancel:    old.execCancel,
	}

	// 7. Register the continued execution (should not fail under the held gate).
	if err := s.execRegistry.Register(sessionID, exec); err != nil {
		if exec.execCancel != nil {
			exec.execCancel()
		}
		if exec.agentSess != nil {
			_ = exec.agentSess.Close()
		}
		writeSessionBusy(w, codingagent.StatusActive)
		return true
	}

	// 8. Transfer process ownership and release the turn gate before streaming.
	if exec.execCancel != nil {
		s.RegisterExecCancel(sessionID, exec.execCancel)
	}
	s.RegisterActiveSession(sessionID, exec.agentSess)
	s.markSessionActive(sessionID)
	release()

	// 9/10. Stream the continued turn to the interrupt request.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		s.startSideEffectPump(exec)
		term, suspended := s.streamSSERelay(r.Context(), w, exec, true)
		if suspended {
			return true
		}
		if !exec.isStopped() && term.retryable {
			content, overloaded := s.classifiedTerminal(term.content)
			s.emitClassifiedSSE(w, exec, content, overloaded)
		}
		s.finishExecution(exec, sessionID, exec.agentSess, nil)
		if r.Context().Err() == nil {
			s.writeSSEDone(w)
		}
		return true
	}

	term, events, suspended := s.respondJSONRelay(r.Context(), w, exec, true)
	if suspended {
		return true
	}
	if !exec.isStopped() && term.retryable {
		content, _ := s.classifiedTerminal(term.content)
		events = append(events, codingagent.StreamEvent{Type: codingagent.EventError, Content: content})
		s.updateSessionStatusOnTerminal(sessionID,
			codingagent.StreamEvent{Type: codingagent.EventError, Content: content}, true, content)
	}
	writeJSONEvents(w, events)
	s.finishExecution(exec, sessionID, exec.agentSess, nil)
	return true
}
