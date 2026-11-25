package codingagent

import "context"

// CodingAgent is the common interface for coding agent backends.
// Both CLI wrapper types (Claude Code, Codex) and future direct API types
// can implement this interface.
type CodingAgent interface {
	// CreateSession starts a new agent session.
	// Internally launches a CLI subprocess.
	CreateSession(ctx context.Context, opts ...SessionOption) (Session, error)

	// Name returns the agent backend name ("claudecode", "codex").
	Name() string

	// Close releases agent resources.
	Close() error
}

// Session is an active agent session.
// Corresponds to the lifecycle of a CLI subprocess.
type Session interface {
	// Send sends a message and returns a streaming event channel.
	// The channel is closed when the agent response completes.
	Send(ctx context.Context, message string) (<-chan StreamEvent, error)

	// ID returns the session ID.
	ID() string

	// Close terminates the session and cleans up the subprocess.
	Close() error
}

// StdinWriter allows writing additional input to a running CLI session.
type StdinWriter interface {
	WriteStdin(text string) error
}

// InterruptResumeSupporter is an optional CodingAgent capability: sessions of this
// agent can natively interrupt the in-flight turn and continue it with an injected
// instruction. Agents that do not implement it are treated as not supporting it.
type InterruptResumeSupporter interface {
	SupportsInterruptResume() bool
}

// TurnInterrupter is implemented by sessions that support native interrupt resume.
// InterruptTurn stops the in-flight work inside the running agent, injects
// instruction, and returns a new channel that streams the continued turn until it
// completes. The implementation must close the previous event channel. An error
// means the agent did not take over; Tern then falls back to its own replan.
type TurnInterrupter interface {
	InterruptTurn(ctx context.Context, instruction string) (<-chan StreamEvent, error)
}
