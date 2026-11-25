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
	mode := s.selectInterruptMode(agent, old)
	newTurnID := s.generateID()
	stopCtx := context.WithoutCancel(r.Context())

	terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}
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

// selectInterruptMode chooses how to interrupt exec. Part 1 always returns InterruptModeFallback.
func (s *Server) selectInterruptMode(_ codingagent.CodingAgent, _ *activeExecution) string {
	return InterruptModeFallback
}
