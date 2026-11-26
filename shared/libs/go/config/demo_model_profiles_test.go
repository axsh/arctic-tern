package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestDemoModelProfiles_CodingAgentsSingleShot(t *testing.T) {
	path := findRepoFile(t, filepath.Join("settings", "demo", "model_profiles.yaml"))
	cfg, err := LoadModelProfiles(path)
	if err != nil {
		t.Fatalf("LoadModelProfiles(%s): %v", path, err)
	}
	if cfg.CodingAgents == nil {
		t.Fatal("demo model_profiles.yaml missing coding_agents")
	}
	for _, name := range []string{"codex", "claudecode"} {
		raw, ok := cfg.CodingAgents[name]
		if !ok {
			t.Fatalf("coding_agents.%s missing", name)
		}
		got := raw.WithDefaults().ExecutionMode
		if got != codingagent.ExecutionModeSingleShot {
			t.Fatalf("coding_agents.%s.execution_mode = %q, want %q", name, got, codingagent.ExecutionModeSingleShot)
		}
	}
}

func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 12; i++ {
		cand := filepath.Join(dir, rel)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if abs, err := filepath.Abs(rel); err == nil {
		if st, err := os.Stat(abs); err == nil && !st.IsDir() {
			return abs
		}
	}
	t.Fatalf("could not find %s from %s", rel, thisFile)
	return ""
}
