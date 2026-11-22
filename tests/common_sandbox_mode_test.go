package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex"
	"github.com/axsh/arctic-tern/shared/libs/go/codingagent/codex/testfake"
	"github.com/axsh/arctic-tern/shared/libs/go/config"
	"github.com/axsh/arctic-tern/shared/libs/go/logger"
)

func TestSandboxMode_OmitDefaultsReadOnly(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, "")
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_ExplicitReadOnly(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeReadOnly)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_WorkspaceWrite(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeWorkspaceWrite)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeWorkspaceWrite)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeWorkspaceWrite)
}

func TestSandboxMode_DangerFullAccess(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeDangerFullAccess)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeDangerFullAccess)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, true, "")
}

func TestSandboxMode_ServerDisableSandboxFallback(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, true)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, "")
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeDangerFullAccess)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, true, "")
}

func TestSandboxMode_ExplicitReadOnlyBeatsServerDisable(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServer(t, true)
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeReadOnly)
	assertSessionSandboxMode(t, baseURL, sessionID, codingagent.SandboxModeReadOnly)

	sendE2EMessage(t, baseURL, sessionID, "hello", 30*time.Second).Body.Close()
	assertLaunchArgs(t, launchLog, false, codingagent.SandboxModeReadOnly)
}

func TestSandboxMode_InvalidValue(t *testing.T) {
	_, baseURL, cleanup := startSandboxModeCodexServer(t, false)
	defer cleanup()

	workDir := t.TempDir()
	body, _ := json.Marshal(map[string]string{
		"agent":        "codex",
		"work_dir":     workDir,
		"sandbox_mode": "full-auto",
	})
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, b)
	}
}

func TestSandboxMode_ResumeSecondTurnUsesConfigOverride(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServerWithThread(t, false, map[int]string{1: "thr-ro"})
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, "")
	drainSandboxModeTurn(t, baseURL, sessionID, "turn1")
	drainSandboxModeTurn(t, baseURL, sessionID, "turn2")

	if got := testfake.LaunchCount(t, launchLog); got != 2 {
		t.Fatalf("launch count = %d, want 2 (no self-heal third launch); log=\n%s", got, readLaunchLog(t, launchLog))
	}
	line1 := launchArgsByNumber(t, launchLog, 1)
	if strings.Contains(line1, "resume") {
		t.Fatalf("launch 1 must be fresh exec, got %q", line1)
	}
	if !strings.Contains(line1, "-s "+codingagent.SandboxModeReadOnly) {
		t.Fatalf("launch 1 expected -s read-only, got %q", line1)
	}
	line2 := launchArgsByNumber(t, launchLog, 2)
	if !strings.Contains(line2, "resume") {
		t.Fatalf("launch 2 expected resume, got %q", line2)
	}
	if strings.Contains(line2, "-s ") {
		t.Fatalf("launch 2 must not pass -s (Issue #70), got %q", line2)
	}
	if !strings.Contains(line2, `sandbox_mode="read-only"`) {
		t.Fatalf("launch 2 expected -c sandbox_mode=\"read-only\", got %q", line2)
	}
}

func TestSandboxMode_ResumeSecondTurnWorkspaceWrite(t *testing.T) {
	launchLog, baseURL, cleanup := startSandboxModeCodexServerWithThread(t, false, map[int]string{1: "thr-ww"})
	defer cleanup()

	workDir := t.TempDir()
	sessionID := createSandboxModeSession(t, baseURL, workDir, codingagent.SandboxModeWorkspaceWrite)
	drainSandboxModeTurn(t, baseURL, sessionID, "turn1")
	drainSandboxModeTurn(t, baseURL, sessionID, "turn2")

	if got := testfake.LaunchCount(t, launchLog); got != 2 {
		t.Fatalf("launch count = %d, want 2; log=\n%s", got, readLaunchLog(t, launchLog))
	}
	line2 := launchArgsByNumber(t, launchLog, 2)
	if !strings.Contains(line2, "resume") {
		t.Fatalf("launch 2 expected resume, got %q", line2)
	}
	if strings.Contains(line2, "-s ") {
		t.Fatalf("launch 2 must not pass -s, got %q", line2)
	}
	if !strings.Contains(line2, `sandbox_mode="workspace-write"`) {
		t.Fatalf("launch 2 expected sandbox_mode=\"workspace-write\", got %q", line2)
	}
}

func drainSandboxModeTurn(t *testing.T, baseURL, sessionID, message string) {
	t.Helper()
	resp := sendE2EMessage(t, baseURL, sessionID, message, 30*time.Second)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send %q: status=%d body=%s", message, resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"type":"result"`) {
		t.Fatalf("send %q: missing result event, body=%s", message, body)
	}
}

func startSandboxModeCodexServer(t *testing.T, disableSandbox bool) (launchLog, baseURL string, cleanup func()) {
	t.Helper()
	return startSandboxModeCodexServerWithThread(t, disableSandbox, nil)
}

func startSandboxModeCodexServerWithThread(t *testing.T, disableSandbox bool, threadByLaunch map[int]string) (launchLog, baseURL string, cleanup func()) {
	t.Helper()
	dir := t.TempDir()
	launchLog = filepath.Join(dir, "launch.log")
	testfake.Install(t, dir, testfake.Options{
		Lines: []string{
			`{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`,
			`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		LaunchLogPath:    launchLog,
		ThreadIDByLaunch: threadByLaunch,
	})

	log := logger.NewDefault(logger.LevelInfo)
	opts := []agentservice.ServerOption{
		agentservice.WithLogger(log),
		agentservice.WithProcessRetry(config.ProcessRetryConfig{MaxAttempts: 1, IntervalSeconds: 0}),
		agentservice.WithSandboxDisabled(disableSandbox),
	}
	srv := agentservice.New(opts...)
	srv.SetModelProfiles(&config.ModelProfilesConfig{
		CodingAgents: map[string]config.AgentConfig{
			"codex": {ExecutionMode: codingagent.ExecutionModeSingleShot},
		},
	})
	srv.RegisterAgent(codex.New(&codingagent.AdapterConfig{Logger: log}))

	ctx := context.Background()
	port := freePort(t)
	if err := srv.Launch(ctx, port); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	baseURL = "http://localhost:" + strconv.Itoa(port)
	cleanup = func() { _ = srv.Shutdown(context.Background()) }
	return launchLog, baseURL, cleanup
}

func readLaunchLog(t *testing.T, launchLog string) string {
	t.Helper()
	data, err := os.ReadFile(launchLog)
	if err != nil {
		t.Fatalf("read launch log: %v", err)
	}
	return string(data)
}

func launchArgsByNumber(t *testing.T, launchLog string, n int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	prefix := "launch " + strconv.Itoa(n) + " args: "
	var text string
	for time.Now().Before(deadline) {
		text = readLaunchLog(t, launchLog)
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimPrefix(line, prefix)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("launch %d args not found in log:\n%s", n, text)
	return ""
}

func createSandboxModeSession(t *testing.T, baseURL, workDir, sandboxMode string) string {
	t.Helper()
	payload := map[string]string{
		"agent":    "codex",
		"work_dir": workDir,
	}
	if sandboxMode != "" {
		payload["sandbox_mode"] = sandboxMode
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/api/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create session: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["session_id"] == "" {
		t.Fatal("empty session_id")
	}
	return result["session_id"]
}

func assertSessionSandboxMode(t *testing.T, baseURL, sessionID, want string) {
	t.Helper()
	info := getE2ESession(t, baseURL, sessionID)
	got, _ := info["sandbox_mode"].(string)
	if got != want {
		t.Fatalf("sandbox_mode = %q, want %q", got, want)
	}
}

func assertLaunchArgs(t *testing.T, launchLog string, wantBypass bool, wantSandboxFlag string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(launchLog)
		if err == nil && len(bytes.TrimSpace(data)) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		t.Fatalf("launch log empty or unreadable: %v", err)
	}
	text := string(data)
	hasBypass := strings.Contains(text, "dangerously-bypass-approvals-and-sandbox")
	if hasBypass != wantBypass {
		t.Fatalf("bypass present=%v want=%v; log=%s", hasBypass, wantBypass, text)
	}
	if wantSandboxFlag != "" {
		needle := "-s " + wantSandboxFlag
		if !strings.Contains(text, needle) {
			t.Fatalf("expected %q in launch log: %s", needle, text)
		}
	}
}
