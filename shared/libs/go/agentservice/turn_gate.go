package agentservice

import "sync"

// turnGates serializes, per session, the requests that start a turn or change the
// in-flight turn (messages, interrupt, respond, cancel, terminate, PATCH). A gate is
// held from the request's busy check until its new execution is registered or the
// request fails, never while a turn streams.
type turnGates struct {
	mu    sync.Mutex
	gates map[string]*sync.Mutex
}

// gate lazily creates the map and the per-session mutex.
func (g *turnGates) gate(sessionID string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.gates == nil {
		g.gates = make(map[string]*sync.Mutex)
	}
	m, ok := g.gates[sessionID]
	if !ok {
		m = &sync.Mutex{}
		g.gates[sessionID] = m
	}
	return m
}

// Lock blocks until the session gate is free. The returned unlock is idempotent.
func (g *turnGates) Lock(sessionID string) func() {
	m := g.gate(sessionID)
	m.Lock()
	return sync.OnceFunc(m.Unlock)
}

// TryLock acquires the gate only if it is free. The returned unlock is idempotent.
func (g *turnGates) TryLock(sessionID string) (func(), bool) {
	m := g.gate(sessionID)
	if !m.TryLock() {
		return func() {}, false
	}
	return sync.OnceFunc(m.Unlock), true
}

// Forget drops the gate of a deleted session.
func (g *turnGates) Forget(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.gates, sessionID)
}
