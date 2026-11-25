package agentservice

import (
	"testing"
	"time"
)

func TestTurnGates_TryLockFailsWhileHeld(t *testing.T) {
	var g turnGates
	unlock := g.Lock("s1")
	if _, ok := g.TryLock("s1"); ok {
		t.Fatal("TryLock should fail while the gate is held")
	}
	unlock()
	rel, ok := g.TryLock("s1")
	if !ok {
		t.Fatal("TryLock should succeed after unlock")
	}
	rel()
}

func TestTurnGates_LockWaitsForUnlock(t *testing.T) {
	var g turnGates
	unlock := g.Lock("s1")
	acquired := make(chan struct{})
	go func() {
		rel := g.Lock("s1")
		close(acquired)
		rel()
	}()
	select {
	case <-acquired:
		t.Fatal("second Lock acquired while the gate is held")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second Lock did not acquire after unlock")
	}
}

func TestTurnGates_UnlockIsIdempotent(t *testing.T) {
	var g turnGates
	u1 := g.Lock("s1")
	u1()
	u2 := g.Lock("s1")
	u1() // second call must be a no-op and must not release u2
	if _, ok := g.TryLock("s1"); ok {
		t.Fatal("TryLock should fail; u2 is still held")
	}
	u2()
}

func TestTurnGates_SessionsAreIndependent(t *testing.T) {
	var g turnGates
	unlock := g.Lock("s1")
	defer unlock()
	rel, ok := g.TryLock("s2")
	if !ok {
		t.Fatal("a different session should not be blocked by s1's gate")
	}
	rel()
}
