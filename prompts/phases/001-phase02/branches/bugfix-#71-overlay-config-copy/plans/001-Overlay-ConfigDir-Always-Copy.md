# 001-Overlay-ConfigDir-Always-Copy

> **Source Specification**: `prompts/phases/001-phase02/branches/bugfix-#71-overlay-config-copy/ideas/001-Overlay-ConfigDir-Always-Copy.md`
>
> **Related Issue**: [#71](https://github.com/axsh/arctic-tern/issues/71)
>
> **Prerequisite**: [000-Codex-Resume-Sandbox-Config-Override.md](./000-Codex-Resume-Sandbox-Config-Override.md)（完了済み）

## Goal Description

`OverlayConfigDir` の `linkOrCopy` を symlink 優先から **常時 copy** に変え、Codex/Claude の書き込みが呼び出し元 `config_dir` に書き戻らないようにする。OS 間で overlay セマンティクスを揃える。

## User Review Required

None.（仕様レビュー済み。O1 警告は本計画では実装しない。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| R1 常時 copy（Symlink しない） | Proposed Changes > `config_overlay.go` |
| R2 Claude/Codex 同一セマンティクス | 共有 `OverlayConfigDir` のまま（ラッパ変更なし） |
| R3 src への書き戻し防止テスト | `config_overlay_test.go` |
| R4 dst が symlink でないテスト | `config_overlay_test.go`（+ 任意で adapter テスト） |
| R5 ドキュメント | `docs/reference-manual-web-apis.md` |
| O1/O2 | 本計画では実装しない |

## Proposed Changes

### Unit tests first (TDD)

#### [MODIFY] [shared/libs/go/codingagent/config_overlay_test.go](file://shared/libs/go/codingagent/config_overlay_test.go)

*   **Description**: R3/R4 を固定するサブテストを追加。実装前は現行 symlink 環境で R3 または R4 が FAIL し得る（symlink 成功時は dst 書き込みが src を汚す / `ModeSymlink` になる）。
*   **Logic**:

1. `t.Run("overlay destinations are not symlinks", ...)`
   - `config_dir` に `skills/demo/SKILL.md` と `settings.json` を用意
   - `OverlayConfigDir(session, config, []string{"skills", "settings.json"})`
   - 各 dst について:

```go
fi, err := os.Lstat(dst)
if err != nil { t.Fatal(err) }
if fi.Mode()&os.ModeSymlink != 0 {
	t.Fatalf("%s is a symlink; overlay must copy", dst)
}
```

2. `t.Run("writes through overlay do not mutate config_dir", ...)`
   - src `config.toml` 内容 `"original\n"`
   - overlay allowlist `[]string{"config.toml"}`
   - dst に `"mutated-by-agent\n"` を WriteFile
   - src を ReadFile → 依然 `"original\n"`

3. 既存 `re-apply updates allowlisted names` のコメントを「copy 再適用で v2」に更新（symlink 言及を削除）。

#### [MODIFY] [shared/libs/go/codingagent/codex/config_overlay_test.go](file://shared/libs/go/codingagent/codex/config_overlay_test.go)

*   **Description**: Apply 後の `config.toml` / `skills` が symlink でないことを 1 ケース追加（共有テストの補強）。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/config_overlay_test.go](file://shared/libs/go/codingagent/claudecode/config_overlay_test.go)

*   **Description**: 同様に `settings.json` または `skills` が symlink でないことを追加。

### Overlay implementation

#### [MODIFY] [shared/libs/go/codingagent/config_overlay.go](file://shared/libs/go/codingagent/config_overlay.go)

*   **Description**: `linkOrCopy` を copy 専用にし、コメント / `OverlayConfigDir` のドキュメントを更新。
*   **Technical Design**: 関数名は互換のため `linkOrCopy` のまま残すか、`replaceWithCopy` にリネームして呼び出しを更新する。本計画では **リネームして意図を明確化**する。

```go
// OverlayConfigDir copies allowlisted names from configDir into sessionDir.
// Only entries that exist under configDir are applied. Existing session-only
// data outside the allowlist is never removed. Protected names under
// sessionDir are never deleted even if listed in allowlist by mistake.
// Destinations are always plain copies (never symlinks) so writes under the
// vendor home cannot mutate the caller's config_dir (Issue #71).
func OverlayConfigDir(sessionDir, configDir string, allowlist []string) error {
	// ... existing validation and loop ...
	if err := replaceWithCopy(src, dst); err != nil {
		return fmt.Errorf("overlay config_dir: apply %s: %w", name, err)
	}
}

// replaceWithCopy removes dst if present, then recursively copies src to dst.
// Symlinks are never created.
func replaceWithCopy(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("remove existing dst: %w", err)
	}
	return copyPath(src, dst)
}
```

*   **Logic**:
  - `os.Symlink` 呼び出しを削除する。
  - `copyPath` / `copyDir` / `copyFile` は既存のまま利用する。
  - 保護 basename マップと allowlist スキップは変更しない。

### Documentation

#### [MODIFY] [docs/reference-manual-web-apis.md](file://docs/reference-manual-web-apis.md)

*   **Description**: `config_dir` 説明を copy セマンティクスに更新。
*   **Logic**: 現行「overlays allowlisted entries」を次の趣旨に変更:
  - Tern **copies** allowlisted entries into the agent vendor home (`{storage_root}/.codex` / `.claude` / Wayfinder session folder).
  - Caller `config_dir` is treated as read-only input; agent writes do not write back through the overlay (Issue #71).
  - Recommend placing `storage_root` and `config_dir` outside `work_dir` when the agent can write the work tree.

#### [MODIFY] [examples/config-dir-switch/README.md](file://examples/config-dir-switch/README.md)（存在し説明があれば）

*   **Logic**: symlink 言及があれば copy に合わせて一文修正。無ければスキップ。

### Integration / E2E

既存 `ConfigDir` 統合テスト（`tests/agentservice_integration_test.go` / e2e）を回帰実行する。新規 E2E は必須ではないが、単体で書き戻しを固定できないリスクがある場合のみ追加する。本計画の主検証は単体 R3/R4 + `--specify ConfigDir`。

任意の軽い統合追加（推奨）:

#### [MODIFY] [tests/agentservice_integration_test.go](file://tests/agentservice_integration_test.go)（または専用 `tests/common_config_dir_overlay_test.go`）

*   **Description**: mock / 実 overlay 後に vendor home 側ファイルが symlink でないことを 1 テストで確認（既存 ConfigDir ヘルパ再利用）。実装コストが高ければ単体のみで可。
*   **本計画の必須**: 単体 R3/R4 + `--specify ConfigDir` 通過。追加統合は時間があれば実施。

## Step-by-Step Implementation Guide

1. [x] **Unit tests (FAIL first)**: `config_overlay_test.go` に非 symlink / 非書き戻しケースを追加。
2. [x] **Implement always-copy**: `config_overlay.go` で `replaceWithCopy` に置換、GoDoc 更新。
3. [x] **Adapter unit tests**: codex/claudecode overlay テストに非 symlink 断言を追加。
4. [x] **Docs**: `reference-manual-web-apis.md`（+ example README 必要時）。
5. [x] **Verify**: `./scripts/process/build.sh` + `--specify ConfigDir`。
6. [x] **Branch regression**: `--specify "TestSandboxMode|TestStreamReconnect|ConfigDir"`（本環境の integration_test.sh は `--categories` 非対応のため同等フィルタで実施）。
7. [x] **Commit & push**.

## Verification Plan

### Automated Verification

Windows（本環境）:

1. **Build & Unit Tests**: `./scripts/process/build.sh`
2. **Integration**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "ConfigDir"`
3. **Branch regression**: `./scripts/process/build.sh && ./scripts/process/integration_test.sh --categories common,llm`

Linux / Remote-SSH の場合は `build.sh --skip-etc` と `xvfb-run -a` ラップを用いる。

### E2E Tests

- 単体 `TestOverlayConfigDir` の書き戻し / 非 symlink が主検証。
- 既存 ConfigDir 統合（`--specify ConfigDir`）でリグレッション確認。
- 手動確認は主検証にしない。

## Documentation

- `docs/reference-manual-web-apis.md`
- 本 plans ファイルが実装の正。
