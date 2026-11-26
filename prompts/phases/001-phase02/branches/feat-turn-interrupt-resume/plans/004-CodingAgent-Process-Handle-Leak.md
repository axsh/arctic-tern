# 004-CodingAgent-Process-Handle-Leak

> **Source Specification**: [003-CodingAgent-Process-Handle-Leak.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/003-CodingAgent-Process-Handle-Leak.md)

## Goal Description

`CodexAdapter` / `ClaudeCodeAdapter` が停止済み `ProcessManager` を `procs` に握り続ける参照リークを解消する。session `Close` で追跡から外し、`Stop` を冪等にし、adapter `Close` は残件だけを止める。

## User Review Required

None.

（`/execute-all-after-spec` による連続実行承認済み。Windows Cancel→taskkill 順序は backlog:e9a2417b-ef6b-491e-9f6c-a0e30c59774a のまま先送り。）

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 session Close で追跡除去（スレッドセーフ） | Proposed Changes > adapter track/untrack + session Close |
| M2 adapter Close は追跡中のみ Stop | Proposed Changes > `Close` が残件のみ |
| M3 `ProcessManager.Stop` 冪等 | Proposed Changes > `sync.Once` 化（codex / claudecode） |
| M4 両 adapter 対称 | Step-by-Step で両パッケージに同型変更 |
| M5 公開契約維持・既存経路 | Verification: build + `TestTurnInterrupt_` |
| N1 追跡表現の改善 | 任意: slice 維持 + `track`/`untrack` ヘルパ（map 必須にしない） |
| N2 debug ログ | 任意: untrack / Close 残件数を Debug |
| 先送り: Windows 孤児 | 実装しない（backlog:e9a2417b-ef6b-491e-9f6c-a0e30c59774a） |

## Proposed Changes

### codingagent/codex（TDD: テストを先に書く）

#### [NEW] [shared/libs/go/codingagent/codex/adapter_lifecycle_internal_test.go](shared/libs/go/codingagent/codex/adapter_lifecycle_internal_test.go)

*   **Description**: package `codex` の内部単体。実 CLI を起動せず、短い `exec.Cmd`（例: `sleep` / Windows `timeout`）または `Process==nil` の stub PM で追跡と Stop を検証する。
*   **Technical Design**:
    ```go
    // helper: start a short-lived OS process into *ProcessManager (cmd started, cancel no-op or real)
    func newTestPM(t *testing.T) *ProcessManager

    func TestProcessManager_StopIdempotent(t *testing.T)
    // pm := newTestPM; Stop(); Stop() again — no panic; second call does not re-enter kill path
    // Evidence: stopped flag / stopOnce; optional killCounter if injected, or ProcessState already exited

    func TestAdapter_SessionCloseUntracks(t *testing.T)
    // a := New(...); manually track 1 pm OR CreateSession with PATH stub if available
    // Prefer: a.track(pm); sess := &codexSession{pm: pm, onClose: ...} via CreateSession path
    // After session.Close(): a.trackedCount() == 0

    func TestAdapter_MultipleSessionsCloseClearsAll(t *testing.T)
    // track N>=3 PMs (or N CreateSession with fake binary); Close each; trackedCount()==0
    // then a.Close(); trackedCount()==0; no extra Stop on already-stopped (Once)

    func TestAdapter_CloseStopsOnlyTracked(t *testing.T)
    // track 1 live pm; do NOT session.Close; a.Close(); process exited; trackedCount()==0

    func TestAdapter_CloseAfterUntrackIsNoopKill(t *testing.T)
    // session.Close then a.Close: Stop called once for that PM (idempotent)
    ```
*   **Logic**: 仕様シナリオ 1〜4 を単体に落とす。`trackedCount()` はテスト用に同一パッケージから `len(a.procs)` を mu 保護で読むヘルパ、またはテスト内で `a.mu.Lock` して直接読む。

#### [MODIFY] [shared/libs/go/codingagent/codex/process.go](shared/libs/go/codingagent/codex/process.go)

*   **Description**: `Stop` を一度きりの終端操作にする。
*   **Technical Design**:
    ```go
    type ProcessManager struct {
        cmd         *exec.Cmd
        cancel      context.CancelFunc
        codexHome   string
        logger      logger.Logger
        stdinWriter io.WriteCloser
        stdinMu     sync.Mutex
        stopOnce    sync.Once
        stopErr     error
    }

    func (pm *ProcessManager) Stop() error {
        if pm == nil {
            return nil
        }
        pm.stopOnce.Do(func() {
            pm.stopErr = pm.stopOnceBody()
        })
        return pm.stopErr
    }

    // stopOnceBody: existing Stop body (closeStdin, windows taskkill / unix SIGTERM, RemoveAll codexHome)
    func (pm *ProcessManager) stopOnceBody() error { /* moved from current Stop */ }
    ```
*   **Logic**:
    1. 既存の `Stop` 本体を `stopOnceBody`（非公開）へ移す。
    2. 公開 `Stop` は `sync.Once` で本体を一度だけ実行し、結果を `stopErr` に保持して返す。
    3. `pm == nil` または既に Once 済みの再呼び出しは本体を走らせない（再 `taskkill` 禁止）。
    4. `cmd.Process == nil` の早期 return は本体側に残してよい。

#### [MODIFY] [shared/libs/go/codingagent/codex/adapter.go](shared/libs/go/codingagent/codex/adapter.go)

*   **Description**: track / untrack、session Close で除去、adapter Close は残件のみ。
*   **Technical Design**:
    ```go
    type CodexAdapter struct {
        config *codingagent.AdapterConfig
        mu     sync.Mutex
        procs  []*ProcessManager
        logger logger.Logger
    }

    func (a *CodexAdapter) track(pm *ProcessManager) {
        a.mu.Lock()
        defer a.mu.Unlock()
        a.procs = append(a.procs, pm)
    }

    func (a *CodexAdapter) untrack(pm *ProcessManager) {
        a.mu.Lock()
        defer a.mu.Unlock()
        for i, p := range a.procs {
            if p == pm {
                a.procs = append(a.procs[:i], a.procs[i+1:]...)
                break
            }
        }
        // optional: a.logger.Debug("untracked codex process", "remaining", len(a.procs))
    }

    type codexSession struct {
        id     string
        ch     <-chan codingagent.StreamEvent
        pm     *ProcessManager
        closed sync.Once
        // closeHook runs after Stop (untrack). Set in CreateSession.
        closeHook func()
    }

    func (s *codexSession) Close() error {
        var err error
        s.closed.Do(func() {
            err = s.pm.Stop()
            if s.closeHook != nil {
                s.closeHook()
            }
        })
        return err
    }

    // CreateSession: after StartProcess success:
    //   a.track(pm)
    //   return &codexSession{..., pm: pm, closeHook: func() { a.untrack(pm) }}

    func (a *CodexAdapter) Close() error {
        a.logger.Debug("closing all codex sessions")
        a.mu.Lock()
        pending := append([]*ProcessManager(nil), a.procs...)
        a.procs = nil
        a.mu.Unlock()
        // optional: a.logger.Debug("codex adapter close", "pending", len(pending))
        for _, pm := range pending {
            _ = pm.Stop()
        }
        return nil
    }
    ```
*   **Logic**:
    1. `CreateSession` の `a.procs = append(...)` を `a.track(pm)` に置き換える。
    2. 返却 session に `closeHook: func() { a.untrack(pm) }` を付ける。
    3. `codexSession.Close` は `Stop` の後に `untrack`（`sync.Once` で二重 Close も安全）。
    4. `CodexAdapter.Close` はスナップショットした残件だけ `Stop` し、リストを空にする。既に untrack 済みならループ 0 回。
    5. `Stop` の冪等により、session Close → adapter Close の順でも二重キルにならない。

### codingagent/claudecode（対称）

#### [NEW] [shared/libs/go/codingagent/claudecode/adapter_lifecycle_internal_test.go](shared/libs/go/codingagent/claudecode/adapter_lifecycle_internal_test.go)

*   **Description**: codex と同名のテスト関数群（`TestProcessManager_StopIdempotent`, `TestAdapter_SessionCloseUntracks`, `TestAdapter_MultipleSessionsCloseClearsAll`, `TestAdapter_CloseStopsOnlyTracked`, `TestAdapter_CloseAfterUntrackIsNoopKill`）。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/process.go](shared/libs/go/codingagent/claudecode/process.go)

*   **Description**: `ProcessManager` に `stopOnce` / `stopErr` を追加し、`Stop` を `sync.Once` 化（codex と同趣旨。既存の stderr debug printf は本体側に残す）。

#### [MODIFY] [shared/libs/go/codingagent/claudecode/adapter.go](shared/libs/go/codingagent/claudecode/adapter.go)

*   **Description**: `track` / `untrack`、`claudeSession` に `closed sync.Once` + `closeHook`、`Close` は残件スナップショットのみ。codex と対称。

### Documentation

#### 更新対象なし

*   `prompts/specifications` に該当ドキュメント無し。本変更は内部ライフサイクルのみで公開 API 文書の更新は不要。

## Step-by-Step Implementation Guide

1. **[x] レッド: codex ライフサイクルテストを追加**
    *   Create `shared/libs/go/codingagent/codex/adapter_lifecycle_internal_test.go` with the five tests above (they fail or do not compile until production code exists).
    *   Run via Verification Plan unit path; confirm RED for missing helpers / untrack behavior.
2. **[x] グリーン: codex `Stop` 冪等化**
    *   Edit `shared/libs/go/codingagent/codex/process.go`: add `stopOnce`/`stopErr`; wrap body.
    *   Commit: `fix: make codex ProcessManager.Stop idempotent`
3. **[x] グリーン: codex track/untrack + session Close**
    *   Edit `shared/libs/go/codingagent/codex/adapter.go` as in Proposed Changes.
    *   Make lifecycle tests PASS.
    *   Commit: `fix: untrack codex ProcessManager on session Close`
4. **[x] レッド→グリーン: claudecode 対称**
    *   Add `adapter_lifecycle_internal_test.go` for claudecode.
    *   Mirror `process.go` Stop Once and `adapter.go` track/untrack.
    *   Commit: `fix: untrack claude ProcessManager on session Close`（テストは同コミットまたは直前コミット）
5. **[x] Verification Plan を実行**
    *   `./scripts/process/build.sh` 成功。
    *   `./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'` 成功。
    *   総合判定を本計画末尾に記入。
    *   record-far-knowledge → pre-push-knowledge-check → `git push`。

## Verification Plan

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    *   Log Verification: `adapter_lifecycle_internal_test`（codex / claudecode）が PASS。既存 `process_repro` / `adapter` 試験が回帰しない。

2.  **Integration Tests** (interrupt 経路が引き続き `agentSess.Close` を呼ぶことの回帰):
    ```bash
    ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
    ```
    *   Log Verification: `TestTurnInterrupt_*` 全 PASS。fallback / native stub / idle 409 が維持される。

3.  **E2E Tests**:
    *   **不要（理由）**: 公開 API・エージェント意味論の変更ではなく、adapter 内部のプロセス参照管理の健全化である。仕様の合否根拠はパッケージ内単体（シナリオ 1〜4）と既存 interrupt 統合回帰（M5）とする。実 CLI 連続 interrupt の shutdown ログ目視は仕様シナリオ 5 の手動補助であり、本計画の必須自動検証には含めない。

### テスト項目の設計（testing-rules §8）

ボトムアップ順序:

| 順 | 項目 ID | 層 | 内容 | 証拠 |
| ---: | :--- | :--- | :--- | :--- |
| 1 | L1 | C: ProcessManager | `Stop` 二重呼び出しが安全・二回目 no-op | `TestProcessManager_StopIdempotent` |
| 2 | L2 | B: Adapter track | session Close 後 `procs` 件数 0 | `TestAdapter_SessionCloseUntracks` |
| 3 | L3 | B: Adapter multi | N 回 Close 後件数 0 | `TestAdapter_MultipleSessionsCloseClearsAll` |
| 4 | L4 | B: Adapter Close | 未 Close 残件のみ Stop、その後件数 0 | `TestAdapter_CloseStopsOnlyTracked` |
| 5 | L5 | B: Close 順序 | Close→adapter.Close で再キル無し | `TestAdapter_CloseAfterUntrackIsNoopKill` |
| 6 | L6 | A: 対称 | claudecode に L1〜L5 相当 | 同名テスト |
| 7 | L7 | A: 回帰 | interrupt 統合 | `TestTurnInterrupt_` |

#### 観点チェックリスト（testing-rules §8.3）

| # | 観点 | 本計画での扱い |
|---|------|----------------|
| 1 | 正常系 | L2〜L4: Close / adapter Close の主経路 |
| 2 | 異常系・境界 | L1 二重 Stop、L5 Close 後の adapter Close |
| 3 | 外部連携 | OS プロセス起動は L1/L4 で短い実プロセス。実 `codex`/`claude` CLI は必須にしない |
| 4 | データ一貫性 | 追跡集合の件数アサーション |
| 5 | 状態遷移 | tracked → untracked → empty |
| 6 | 設定・構成 | 対象外（adapter 選択は既存） |
| 7 | 副作用 | リーク解消そのものが観点。二重キル抑制 |

#### セルフレビュー（testing-rules §8.4）

1. **網羅性**: M1〜M4 は L1〜L6、M5 は L7。先送り Windows 孤児は意図的除外。十分。
2. **証拠**: 件数と冪等（Once）を直接断言。ログ目視だけにしない。
3. **迂回排除**: 同一パッケージから `procs` / `Stop` を直接検証し、別経路の偽成功を避ける。
4. **依存**: L1（Stop）→ L2〜L5（adapter）→ L6 対称 → L7 統合。

### 総合判定（testing-rules §9）

全テスト成功後、§9.2 の 7 項目を確かめ、下記「総合判定結果」に記入する。特に「untrack 忘れが無いこと」「Stop Once が実キル経路を二度走らせないこと」を見る。

#### 総合判定結果

**判定**: ✅ 動作確認完了

#### テスト結果サマリ
- 全テスト数: build.sh パイプライン PASS（codingagent lifecycle 単体含む）+ `TestTurnInterrupt_` 6 件
- 成功: 上記すべて
- 失敗: 0 件
- 事実上スキップ: 0 件

#### チェック項目の結果
| # | チェック項目 | 結果 | 備考 |
|---|------------|------|------|
| 1 | スキップされたテスト | ✅ | 該当なし |
| 2 | 部分的なエラー | ✅ | integration ログに WARN/ERROR の異常兆候なし（BuiltinAgents の起動ログのみ） |
| 3 | 迂回処理による偽成功 | ✅ | lifecycle 試験は同一パッケージで `procs` 件数と `Stop` Once を直接断言 |
| 4 | アダプタ・コンフィグの誤適用 | ✅ | codex / claudecode 双方に対称テストあり |
| 5 | テスト間の依存・順序問題 | ✅ | パッケージ内単体は独立。`TestTurnInterrupt_` も単独フィルタで PASS |
| 6 | カバレッジの妥当性 | ✅ | L1〜L7（Stop 冪等、untrack、複数 Close、adapter Close、対称、interrupt 回帰）を実施 |
| 7 | 外部システムの状態 | ✅ | 単体は短い OS プロセスのみ。interrupt 統合は stub/httptest 経路 |

#### 判定理由
session Close 後に追跡件数が 0 になること、および二重 Stop が安全であることを単体で証拠化した。既存 interrupt 統合も回帰していない。Windows Cancel→taskkill 順序による子プロセス孤児は本計画スコープ外（先送り backlog）であり、本判定の対象外とする。
