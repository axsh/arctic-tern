package llm_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDocsIndexLayout runs scripts/utils/check_docs_index.sh to ensure the
// top-level README remains an index and docs/ uses the expected layout.
func TestDocsIndexLayout(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	projectRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), ".."))
	script := filepath.Join(projectRoot, "scripts", "utils", "check_docs_index.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("check script missing: %v", err)
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = projectRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check_docs_index.sh failed: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
