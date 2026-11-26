package claudecode

import (
	"context"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func newTestAdapter() *ClaudeCodeAdapter {
	return New(&codingagent.AdapterConfig{})
}

func newTestPM(t *testing.T) *ProcessManager {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "60", "127.0.0.1")
	} else {
		cmd = exec.Command("sleep", "60")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start test process: %v", err)
	}
	pm := &ProcessManager{
		cmd:    cmd,
		cancel: func() {},
		logger: logger.NewDefault(logger.LevelInfo),
	}
	t.Cleanup(func() { _ = pm.Stop() })
	return pm
}

func (a *ClaudeCodeAdapter) trackedCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.procs)
}

func TestProcessManager_StopIdempotent(t *testing.T) {
	pm := newTestPM(t)
	if err := pm.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := pm.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if err := pm.Stop(); err != nil {
		t.Fatalf("third Stop: %v", err)
	}
}

func TestAdapter_SessionCloseUntracks(t *testing.T) {
	a := newTestAdapter()
	pm := newTestPM(t)
	a.track(pm)
	if a.trackedCount() != 1 {
		t.Fatalf("trackedCount = %d, want 1", a.trackedCount())
	}
	s := &claudeSession{
		id:        "t1",
		pm:        pm,
		closeHook: func() { a.untrack(pm) },
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if a.trackedCount() != 0 {
		t.Fatalf("trackedCount after Close = %d, want 0", a.trackedCount())
	}
}

func TestAdapter_MultipleSessionsCloseClearsAll(t *testing.T) {
	a := newTestAdapter()
	const n = 3
	sessions := make([]*claudeSession, 0, n)
	for i := 0; i < n; i++ {
		pm := newTestPM(t)
		a.track(pm)
		captured := pm
		sessions = append(sessions, &claudeSession{
			id:        "t",
			pm:        captured,
			closeHook: func() { a.untrack(captured) },
		})
	}
	if a.trackedCount() != n {
		t.Fatalf("trackedCount = %d, want %d", a.trackedCount(), n)
	}
	for _, s := range sessions {
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if a.trackedCount() != 0 {
		t.Fatalf("trackedCount after closes = %d, want 0", a.trackedCount())
	}
	if err := a.Close(); err != nil {
		t.Fatalf("adapter Close: %v", err)
	}
	if a.trackedCount() != 0 {
		t.Fatalf("trackedCount after adapter Close = %d, want 0", a.trackedCount())
	}
}

func TestAdapter_CloseStopsOnlyTracked(t *testing.T) {
	a := newTestAdapter()
	pm := newTestPM(t)
	a.track(pm)
	if err := a.Close(); err != nil {
		t.Fatalf("adapter Close: %v", err)
	}
	if a.trackedCount() != 0 {
		t.Fatalf("trackedCount = %d, want 0", a.trackedCount())
	}
	if err := pm.Stop(); err != nil {
		t.Fatalf("Stop after adapter Close: %v", err)
	}
}

func TestAdapter_CloseAfterUntrackIsNoopKill(t *testing.T) {
	a := newTestAdapter()
	pm := newTestPM(t)
	a.track(pm)
	s := &claudeSession{
		id:        "t1",
		pm:        pm,
		closeHook: func() { a.untrack(pm) },
	}
	if err := s.Close(); err != nil {
		t.Fatalf("session Close: %v", err)
	}
	if a.trackedCount() != 0 {
		t.Fatalf("trackedCount = %d, want 0", a.trackedCount())
	}
	if err := a.Close(); err != nil {
		t.Fatalf("adapter Close: %v", err)
	}
	if err := pm.Stop(); err != nil {
		t.Fatalf("extra Stop: %v", err)
	}
}

func TestAdapter_ConcurrentTrackUntrack(t *testing.T) {
	a := newTestAdapter()
	var started atomic.Int32
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer close(done)
		for ctx.Err() == nil {
			pm := &ProcessManager{
				cmd:    &exec.Cmd{},
				cancel: func() {},
				logger: logger.NewDefault(logger.LevelInfo),
			}
			a.track(pm)
			started.Add(1)
			a.untrack(pm)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if started.Load() == 0 {
		t.Fatal("expected concurrent track/untrack activity")
	}
	_ = a.Close()
}
