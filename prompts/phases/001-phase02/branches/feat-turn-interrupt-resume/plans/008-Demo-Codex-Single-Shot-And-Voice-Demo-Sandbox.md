# 008-Demo-Codex-Single-Shot-And-Voice-Demo-Sandbox

> **Source Specification**: [007-Demo-Codex-Single-Shot-And-Voice-Demo-Sandbox.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/007-Demo-Codex-Single-Shot-And-Voice-Demo-Sandbox.md)

## Goal Description

demo の `model_profiles.yaml` に `coding_agents`（`execution_mode: single_shot`）を追加し、voice_demo の CreateSession に `sandbox_mode=workspace-write` を明示する。README / UI hint で再起動と非 Temp work_dir を案内する。

## User Review Required

None.

## Requirement Traceability

| Requirement | Implementation Point |
| :--- | :--- |
| M1 coding_agents single_shot | `settings/demo/model_profiles.yaml` + config load test |
| M2 workspace-write | `features/voice_demo/cawa.go` + CreateSession body test |
| M3 README | `features/voice_demo/README.md` |
| M4 automated | Verification Plan |
| S1 UI hint | `features/voice_demo/static/index.html` |
| S2 default work_dir | README 必須化のみ（Temp 既定は残し、案内で回避） |

## Proposed Changes

### settings/demo

#### [MODIFY] settings/demo/model_profiles.yaml

ファイル末尾（`providers` の後）に次を追加する（example と同等）:

```yaml
coding_agents:
  codex:
    max_prompt_bytes: 1048576
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: single_shot
  claudecode:
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: single_shot
```

#### [NEW] shared/libs/go/config/demo_model_profiles_test.go（または既存 loader 試験の拡張）

* `LoadModelProfiles` でリポジトリの `settings/demo/model_profiles.yaml` を読み、
  * `CodingAgents["codex"].WithDefaults().ExecutionMode == single_shot`
  * `CodingAgents["claudecode"].WithDefaults().ExecutionMode == single_shot`
* パスは `filepath.Join` でリポジトリ相対を解決（テスト cwd に依存しすぎないよう、既知の相対探索または `settings/demo/...` を複数候補で探す）。

### features/voice_demo

#### [MODIFY] features/voice_demo/cawa.go

`CreateSession` の `v1.SessionRequest` に:

```go
SandboxMode: v1.SandboxModeWorkspaceWrite, // "workspace-write"
```

を追加する。

#### [MODIFY] features/voice_demo のセッション開始試験

既存の utterance / call-start 試験で CreateSession をモックしている場合は body に `sandbox_mode=workspace-write` を断言する。無ければ httptest で `newCawaBridge`→`Start` 相当を検証する新規 `TestCawaBridge_CreateSessionPostsWorkspaceWrite` を追加。

#### [MODIFY] features/voice_demo/README.md

* Tern demo 再起動が必要なこと
* `coding_agents.execution_mode: single_shot` が demo に入ったこと
* `TERN_WORK_DIR` に非 Temp パス推奨（例: `tmp/voice-demo-work`）

#### [MODIFY] features/voice_demo/static/index.html

hint に一行: demo Tern は single_shot 設定が必要／work_dir は Temp 以外を推奨（詳細は README）。

## Step-by-Step Implementation Guide

1. **[ ]** RED: demo profiles load test（single_shot 未設定で失敗することを確認）→ YAML 追加で GREEN
2. **[ ]** RED: CreateSession sandbox_mode 試験 → `cawa.go` で GREEN
3. **[ ]** README + index hint
4. **[ ]** Verification; 総合判定; far-knowledge; push

## Verification Plan

### テスト項目

| 段 | テスト |
| :--- | :--- |
| L1 | Demo model_profiles CodingAgents single_shot |
| L2 | voice_demo CreateSession sandbox_mode |
| L3 | `go test -C features/voice_demo ./...` |
| L4 | `TestTurnInterrupt_` |

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 実際 |
| :--- | :--- | :--- |
| Demo CodingAgents single_shot | coding_agents 無し | （実装時） |
| CreateSession sandbox_mode | フィールド無し | （実装時） |

### Automated Verification

```bash
./scripts/process/build.sh
go test -C features/voice_demo -count=1 ./...
./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

E2E Playwright 不要: 設定と CreateSession body の契約試験で足りる。実マイク手動は再現手順レポートへ。

### テスト項目設計セルフレビュー (§11.4)

* 網羅性: M1/M2 を単体でカバー。M3 は文書。
* 証拠: YAML 読込値と JSON body。
* 迂回排除: 文字列だけの偽陽性を避け、`LoadModelProfiles` + `WithDefaults` を使う。
* 依存: build → feature unit → TurnInterrupt。

### 総合判定結果

（実装後記入）

## Documentation

#### [MODIFY] features/voice_demo/README.md

上記 M3。
