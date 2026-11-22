# 000-Codex-Resume-Sandbox-Config-Override

> **Source Specification**: `prompts/phases/001-phase02/branches/bugfix-#71-overlay-config-copy/ideas/000-Codex-Resume-Sandbox-Config-Override.md`
>
> **Related Issue**: [#70](https://github.com/axsh/arctic-tern/issues/70)

## Goal Description

Codex の `BuildArgs` で、`exec resume` 直後に付く `-s read-only|workspace-write` をやめ、resume 時は `-c sandbox_mode="..."` で同等方針を渡す。fresh `exec` は既存の `-s` 互換を維持し、通常の 2 ターン目以降が clap 拒否 → self-heal に落ちないようにする。

## User Review Required

None.（仕様レビュー済み。本ワークフロー指示により計画作成後すぐ実装へ進む。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 resume で `-s` を付けない | Proposed Changes > `codex/process.go` `BuildArgs` |
| R2 resume で `-c sandbox_mode="..."` | 同上 |
| R3 resume の `danger-full-access` は bypass 維持 | 同上 |
| R4 fresh は `-s` / bypass 互換 | 同上 + 既存単体テスト維持 |
| R5 resume × sandbox 単体テスト | `codex/process_test.go` |
| 受け入れ: 2 ターン目で self-heal しない | `tests/common_sandbox_mode_test.go` resume 起動ログ検証 |
| Docs（任意だが本計画で実施） | `docs/reference-manual-web-apis.md` |

## Proposed Changes

### Unit tests first (TDD)

#### [MODIFY] [shared/libs/go/codingagent/codex/process_test.go](file://shared/libs/go/codingagent/codex/process_test.go)

*   **Description**: R5 の argv 固定テストを追加・既存 resume テストを強化。実装前に FAIL することを確認する。
*   **Technical Design**: ヘルパ（同ファイル内）:

```go
func hasFlagPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func hasExactArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
```

*   **Logic**（追加テスト）:

1. `TestCodexBuildArgs_ResumeReadOnlyUsesConfigOverride`
   - `BuildArgs("hi", nil, true, "abc-123", codingagent.SandboxModeReadOnly)`
   - `args[0]=="exec"`, `args[1]=="resume"`
   - `hasFlagPair(..., "-s", "read-only") == false`
   - `hasFlagPair(..., "-c", `sandbox_mode="read-only"`) == true`
   - `"--dangerously-bypass-approvals-and-sandbox"` なし

2. `TestCodexBuildArgs_ResumeWorkspaceWriteUsesConfigOverride`
   - resume id `"abc-123"`, mode `workspace-write`
   - `-s` ペアなし
   - `-c` + `sandbox_mode="workspace-write"` あり

3. `TestCodexBuildArgs_ResumeDangerFullAccessKeepsBypass`
   - 既存 `TestCodexBuildArgs_ResumeUsesExecResume` を拡張、または同内容を強化:
   - bypass フラグあり
   - `-s` なし
   - `-c` の値が `sandbox_mode=` で始まらない（sandbox_mode override を付けない）

4. `TestCodexBuildArgs_FreshReadOnlyKeepsDashS` / 既存 `TestCodexBuildArgs_SandboxEnforcedOmitsBypassFlag` / `TestCodexBuildArgs_WorkspaceWrite`
   - fresh で `-s read-only` / `-s workspace-write` を維持（リグレッション）

5. 空 sandboxMode + resume:
   - `BuildArgs("hi", nil, true, "sid", "")` → EffectiveSandboxMode により `sandbox_mode="read-only"` の `-c` が付く

### Codex BuildArgs

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](file://shared/libs/go/codingagent/codex/process.go)

*   **Description**: `BuildArgs` を resume / fresh で sandbox 表現を分岐する。
*   **Technical Design**: シグネチャは変更しない。

```go
func BuildArgs(prompt string, configOverrides []string, ignoreUserConfig bool, resumeSessionID string, sandboxMode string) []string
```

*   **Logic**（仕様書どおりの具体分岐）:

```go
func BuildArgs(prompt string, configOverrides []string, ignoreUserConfig bool, resumeSessionID string, sandboxMode string) []string {
	common := []string{"--json"}
	mode := codingagent.EffectiveSandboxMode(sandboxMode)
	resuming := resumeSessionID != ""

	switch mode {
	case codingagent.SandboxModeDangerFullAccess:
		common = append(common, "--dangerously-bypass-approvals-and-sandbox")
	case codingagent.SandboxModeWorkspaceWrite:
		if resuming {
			// Issue #70: codex exec resume rejects -s after resume; use -c instead.
			common = append(common, "-c", `sandbox_mode="workspace-write"`)
		} else {
			common = append(common, "-s", codingagent.SandboxModeWorkspaceWrite)
		}
	default: // read-only (including empty via EffectiveSandboxMode)
		if resuming {
			common = append(common, "-c", `sandbox_mode="read-only"`)
		} else {
			common = append(common, "-s", codingagent.SandboxModeReadOnly)
		}
	}

	if ignoreUserConfig {
		common = append(common, "--ignore-user-config")
	}
	common = append(common, configOverrides...)

	var args []string
	if resuming {
		args = append([]string{"exec", "resume"}, common...)
		args = append(args, resumeSessionID)
	} else {
		args = append([]string{"exec"}, common...)
	}
	if prompt != "" {
		args = append(args, "-")
	}
	return args
}
```

*   **GoDoc 更新**: sandboxMode 節に「resume 時は `-s` ではなく `-c sandbox_mode=...`（Issue #70）」を追記する。

### Integration / E2E（testfake）

#### [MODIFY] [tests/common_sandbox_mode_test.go](file://tests/common_sandbox_mode_test.go)

*   **Description**: 2 ターン目の launch 行が `resume` + `-c sandbox_mode=...` で、`-s` を含まないことを検証する。
*   **Technical Design**:
  - `startSandboxModeCodexServer` を拡張するか、resume 専用ヘルパを追加し、`ThreadIDByLaunch: map[int]string{1: "thr-sandbox-1"}` を渡す（2 ターン目が native resume になる）。
  - `ProcessRetry` は `MaxAttempts: 1` のままでよい（正常 resume ではリトライ不要）。
*   **Logic**:

```go
func TestSandboxMode_ResumeSecondTurnUsesConfigOverride(t *testing.T) {
	// Install testfake with ThreadIDByLaunch[1]="thr-ro"
	// Create session sandbox_mode omitted (read-only)
	// sendE2EMessage turn1, sendE2EMessage turn2
	// Parse launch.log lines "launch N args: ..."
	// launch 1: contains "exec", does NOT need "resume"; may contain "-s read-only"
	// launch 2: contains "resume" and `sandbox_mode="read-only"`; must NOT contain "-s "
	// launch count == 2 (no self-heal third launch)
}
```

追加ケース（同一ファイルで可）:

- `TestSandboxMode_ResumeSecondTurnWorkspaceWrite`: Create で `workspace-write` → launch 2 に `sandbox_mode="workspace-write"`、`-s` なし。

既存 `assertLaunchArgs` は「ログ全体」を見るため、resume 行専用に `assertLaunchLine` ヘルパを追加する。

```go
func launchArgsByNumber(t *testing.T, launchLog string, n int) string {
	// read file; find line with prefix fmt.Sprintf("launch %d args: ", n); return suffix
}
```

### Documentation

#### [MODIFY] [docs/reference-manual-web-apis.md](file://docs/reference-manual-web-apis.md)

*   **Description**: `sandbox_mode` 説明に Codex resume 時の渡し方を一文追加。
*   **Logic**: `read-only` / `workspace-write` の箇条書き付近に:

  - Fresh: `codex exec ... -s <mode>`
  - Resume: `codex exec resume ... -c sandbox_mode="<mode>"`（`-s` は resume 後に CLI が拒否するため。Issue #70）
  - `danger-full-access`: fresh / resume とも `--dangerously-bypass-approvals-and-sandbox`

## Step-by-Step Implementation Guide

1. [x] **Unit tests (FAIL first)**: `process_test.go` に R5 ケースを追加し、現行実装で FAIL することを確認する。
2. [x] **Implement BuildArgs**: `process.go` を上記 Logic どおり分岐し、GoDoc を更新する。
3. [x] **Unit tests PASS**: `./scripts/process/build.sh` で単体を含むビルドが通ることを確認する。
4. [x] **Integration test**: `tests/common_sandbox_mode_test.go` に resume 2 ターン目検証を追加する。
5. [x] **Docs**: `docs/reference-manual-web-apis.md` を更新する。
6. [x] **Verify**: build + `--specify TestSandboxMode` + `--specify TestStreamReconnect`。
7. [x] **Commit**: 意味単位で commit（テスト → 実装 → 統合/docs）。全成功後に push。

## Verification Plan

### Automated Verification

Windows（本環境）:

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration (sandbox)**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestSandboxMode"`
3. **Regression (stream/resume)**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnect"`

Linux / Remote-SSH の場合:

```bash
./scripts/process/build.sh --skip-etc
xvfb-run -a ./scripts/process/integration_test.sh --specify "TestSandboxMode"
xvfb-run -a ./scripts/process/integration_test.sh --specify "TestStreamReconnect"
```

### E2E Tests

- `tests/common_sandbox_mode_test.go` の `TestSandboxMode_ResumeSecondTurn*`（testfake + AgentService HTTP）。手動 CLI 確認は主検証にしない。

## Documentation

- `docs/reference-manual-web-apis.md`（sandbox_mode の fresh/resume 差）
- 本 plans ファイルが実装の正。仕様 ideas は変更しない。
