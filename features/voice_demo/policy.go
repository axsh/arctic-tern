package main

import (
	"fmt"
	"strings"
)

// Action is which CAWA API the demo should call for one finalized user utterance.
// We intentionally do NOT define a cancel action here: barge-in must use interrupt
// (stop + inject + continue/replan), not cancel-then-messages.
type Action string

const (
	ActionNone      Action = "none"
	ActionMessages  Action = "messages"
	ActionInterrupt Action = "interrupt"
)

// DecideUtteranceAction chooses the CAWA API for a finalized user utterance.
//
// Why this split exists:
//   - Realtime voice is the conversation layer (fast back-and-forth).
//   - CAWA / Coding Agent is the execution authority (may run for a long time).
//   - When the agent is idle, a new utterance starts a normal turn via messages.
//   - When the agent is already busy, a new utterance is a barge-in: we must call
//     interrupt so Tern stops the in-flight turn and continues with the new text.
//     Using cancel + messages would drop the "interrupted and continue" semantics
//     that this demo exists to show.
func DecideUtteranceAction(busy bool, text string) (Action, error) {
	if strings.TrimSpace(text) == "" {
		return ActionNone, fmt.Errorf("empty utterance")
	}
	if busy {
		return ActionInterrupt, nil
	}
	return ActionMessages, nil
}
