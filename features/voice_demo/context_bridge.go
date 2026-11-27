package main

import (
	"fmt"
	"strings"
)

const (
	contextBridgePrefix       = "Tern coding agent status: "
	contextBridgeMaxRunes     = 280
	reconsiderQuestionMaxRunes = 400
	reconsiderSummaryMaxRunes  = 400
	noOpenQuestionPlaceholder  = "(no open question recorded)"
)

// BuildContextBridgeNote builds a short progress note for Realtime conversation.item.create.
// Empty or whitespace-only summaries are skipped. Progress notes must not trigger speak.
func BuildContextBridgeNote(summary string) (note string, ok bool) {
	s := strings.TrimSpace(summary)
	if s == "" {
		return "", false
	}
	if strings.HasPrefix(s, contextBridgePrefix) {
		s = strings.TrimSpace(strings.TrimPrefix(s, contextBridgePrefix))
	}
	s = truncateRunes(s, contextBridgeMaxRunes-len([]rune(contextBridgePrefix)))
	if s == "" {
		return "", false
	}
	return contextBridgePrefix + s, true
}

// BuildTurnCompleteReconsiderNote frames coding-agent result (C) as optional evidence
// for answering the human question (A). It must not force a spoken readout or agent ack.
func BuildTurnCompleteReconsiderNote(question, summary string) (note string, ok bool) {
	s := strings.TrimSpace(summary)
	if s == "" {
		return "", false
	}
	q := strings.TrimSpace(question)
	if q == "" {
		q = noOpenQuestionPlaceholder
	}
	q = truncateRunes(q, reconsiderQuestionMaxRunes)
	s = truncateRunes(s, reconsiderSummaryMaxRunes)
	note = fmt.Sprintf(
		"Open question from the human (still unanswered until you decide otherwise):\n%s\n\n"+
			"Coding agent result (optional evidence; you may ignore details irrelevant to A):\n%s\n\n"+
			"Reconsider (B): Does this suffice to answer A?\n"+
			"- If yes and you should speak to the human: say D as the answer to A (not an ack to the agent).\n"+
			"- If not enough: request more work via the usual coding-agent path; do not narrate status to the human.\n"+
			"- If you should stay silent for now: say nothing.\n"+
			"Do not say that results came back or that you understood the agent.",
		q, s,
	)
	return note, true
}
