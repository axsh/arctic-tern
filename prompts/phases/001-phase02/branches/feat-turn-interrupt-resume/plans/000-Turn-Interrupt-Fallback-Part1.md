# 000-Turn-Interrupt-Fallback-Part1

> **Source Specification**: [000-Turn-Interrupt-And-Resume.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/000-Turn-Interrupt-And-Resume.md)
> **Part**: 1 / 2（Part 2: [001-Turn-Interrupt-Native-Seam-Part2.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/plans/001-Turn-Interrupt-Native-Seam-Part2.md)）

## Goal Description

busy 中のセッションに対する統一割り込み API `POST /api/v1/sessions/:id/interrupt` を追加し、MVP 既定の **Fallback replan** を実装する。Fallback replan は、進行中ターンを止め、途中成果と割り込み内容を正本 history に残し、同一 session id で「継続できることは継続し、必要なことは計画しなおす」再計画ターンを始める。

割り込みは「旧ターンの停止」と「新ターンの開始」をほぼ同時に行う。現行の実行ライフサイクルには、この使い方で必ず表面化する不備が 4 つあるため、本 Part で先に直す。

1. **後片付けの取り違え**: 停止済みの旧ターンの処理（`runTurn` の終了処理）が、後から session id だけを手掛かりに後片付けする。後継ターンが既に登録されていると、後継の実行登録・cancel 関数・アクティブセッションを消してしまう。消されると、後継ターンは cancel できず、セッションは実行中なのに idle に見える。
2. **状態の上書き**: 停止済みの旧ターンが、後から session record（status / error / agent_session_id / usage）を書き換える。後継ターンの `active` が旧ターンの `error` で上書きされうる。
3. **cancel 後の次ターンが completed にならない（現行バグ）**: cancel は session に `error = "turn cancelled"` を残す。次ターン開始時にこれを消さないため、SSE の次ターンは完了しても `updateSessionStatusOnTerminal` と `streamSSERelay` がマーカーを見て状態更新を飛ばし、status が `active` のまま残る。割り込みは cancel 相当の停止の直後に次ターンを始めるので必ずこれを踏む。
4. **順序の保証が無い**: ターン開始・割り込み・cancel・terminate・PATCH・respond が同一セッションで重なったとき、どれが先に効くかが決まっていない。

Native resume の宣言口・経路選択・`GET /api/v1/agents` への露出は Part 2 で扱う。本 Part の割り込みは常に `fallback` モードで動き、経路選択関数は形だけ用意する。

## User Review Required

仕様が「実装計画で決める」とした点と、計画で新たに決めた点を挙げる。

1. **公開 API 名**: 仮決定どおり `POST /api/v1/sessions/:id/interrupt`、client は `Session.Interrupt` / `Session.InterruptWithOpts`（backlog:acf44bcc-f2f9-48df-8969-c9e975f1aeb2）。
2. **応答ストリームの固定（M4.4）**: interrupt の応答は「新ターン（再計画ターン）のストリーム」とする。content negotiation は `POST /messages` と同一（`Accept: text/event-stream` で SSE、それ以外は JSON 配列）。切断時は既存の Follow（`GET .../events`）で新ターンに再接続する。旧ターンの購読者（元の `POST /messages` の SSE や Follow）には `{"type":"error","content":"turn interrupted"}` と `[DONE]` を返す。
3. **停止粒度**: 即時停止（cancel と同じ停止）を既定とする（backlog:f627eae3-e0a1-4923-9ddc-cbc531851166 の仮決定をこの計画で採用）。
4. **suspended（ask_user 待ち）のターンも interrupt を受け付ける**: 回答ではなく方針変更として扱う。Wayfinder は stdin が無く respond できないが、interrupt による方針変更はできるようになる。
5. **セッション単位のターンゲート**: ターン開始と、実行中ターンを変える API を同一セッション内で直列化する。interrupt が旧ターンを止めて新ターンを登録するまでの間に来た 2 本目の interrupt は `409 {"error":"turn transition in progress"}`。messages / PATCH / respond / cancel / terminate はゲートが空くまで待ち、空いた後の状態（＝新ターン）に作用する。たとえば割り込み直後の cancel は再計画ターンを止める。
6. **前提修正の同梱**: (a) 新ターン登録時に session の `error` を空にする（上記 3 の修正）。(b) cancel / interrupt / terminate で止めた実行は、以後 session record に書かない（上記 2 の修正）。(c) (b) により、terminate 中の旧ターンが `closed` を `error` で上書きしうる問題も解消する。(d) 旧ターンの終了処理は、自分の実行登録が今も自分のものであるときだけ後片付けする（上記 1 の修正）。
7. **正本 history の割り込みレコード形式**: role `user`、本文 1 行目が `[Tern interrupt] interrupted_turn_id=<旧turn_id> mode=<mode>`、2 行目以降が割り込み内容。正本のスキーマ（`session.Message`）は変えない（Wayfinder 形式の互換を保つ）。中断されたターンの途中の assistant / tool の事実は、このレコードの直前に正本へ取り込む（現行の cancel は取り込まない。cancel の挙動は変えない）。
8. **統合テストの実行コマンド**: 本リポジトリの `scripts/process/integration_test.sh` は `--categories` を持たず `--specify` のみを受け付ける。Verification Plan は `--specify` で絞り込む。
9. **ライブ E2E**: Codex を必須ゲートとし、仕様 M6（全 Agent で Fallback が動くこと）に従い Claude Code と Wayfinder でも同じシナリオを必須とする。各 CLI と LLM 認証が無い環境では Fatal で落ちる（Skip しない）。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 統一割り込みエントリ `POST .../interrupt`、`content` 必須、`correlation_id` 任意 | Proposed Changes > agentservice 割り込み > `interrupt.go`（`InterruptRequest` / `handleInterrupt`）、`service.go`（ルーティング）、client/v1 > `session.go`（`Interrupt`） |
| M1 インフライトが無いセッションへの interrupt は 409 | `handleInterrupt` の `no active turn`。`TestInterrupt_IdleSessionRejected` |
| M1 idle へのメッセージを interrupt に統合しない | `handleInterrupt` は旧ターンが無いとき新ターンを始めない（同上のテストで `creates == 0` を確認） |
| M2 Suspend → Inject → Reconcile → Resume/Replan | `handleInterrupt` の手順（Suspend: `stopInFlightTurn`、Inject: `AppendSessionMessage` + 途中成果の ingest、Reconcile: `summarizeInterruptedTurn` / `buildInterruptReplanPrompt`、Replan: `launchTurn`） |
| M2 cancel と区別する | `handleCancel` は再計画しない。`TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes` |
| M3 Native 優先・Fallback 既定 | 本 Part は `selectInterruptMode` が常に `fallback` を返す（経路選択の差し込み口のみ）。Native 本体は Part 2 |
| M4.1 停止（cancel 相当、`closed` にしない） | `turn_stop.go`（`stopInFlightTurn`）。`TestStopInFlightTurn_InterruptFinalizesTurnOnce` |
| M4.2 中断レコード（turn_id / 理由 / content / 進捗要約） | 途中成果の ingest（`stopOptions.ingest`）+ `formatInterruptHistory` + 再計画プロンプトの進捗要約。`TestInterrupt_FallbackReplacesInFlightTurn`、`TestTurnInterrupt_CanonicalHistoryOrder` |
| M4.3 同一 session id で新ターン、中断事実と途中成果を踏まえた指示 | `launchTurn`（`agentPrompt`）+ `buildInterruptReplanPrompt`。ネイティブの会話 id での resume は既存の `sessionOptsWithResume` で維持。`TestInterrupt_FallbackResumesNativeConversation` |
| M4.4 応答ストリームを固定 | interrupt の応答＝新ターンのストリーム（User Review Required 2）。docs `reference-manual-web-apis.md` §9.1 |
| M5 Capability 宣言口 | Part 2 |
| M6 通常の二重 `messages` は 409 のまま、hint に interrupt | `exec_registry.go`（`hintSessionBusy`）。`TestSendMessage_BusyHintMentionsInterrupt` |
| M6 respond / terminate / cancel との共存 | ターンゲート（`turn_gate.go`）+ 停止ヘルパ。`TestInterrupt_CancelWaitsForTransitionAndCancelsReplanTurn`、`TestInterrupt_MessageDuringTransitionGetsBusy`、`TestHandleTerminate_InFlightTurnStaysClosed`、既存の respond 系テスト |
| M6 Wayfinder / Codex / Claude で Fallback が動く | 実装はエージェント名で分岐しない。ライブ `TestLiveTurnInterrupt_CodexReplan` / `_ClaudeCodeReplan` / `_WayfinderReplan` |
| M7 docs / client / examples | client/v1（`Interrupt` / `InterruptWithOpts` / Event のフィールド）、Documentation 節 |
| M8 採用経路の観測（ログ必須・SSE 推奨） | サーバログ `turn interrupted`（`mode`）+ 新ターン先頭の SSE system イベント（`interrupt_mode` / `interrupted_turn_id`） |
| N1 utterance hint | 対象外。進捗要約はプロンプト内に留める（仕様で任意・ブロッカーにしないとされている） |
| N2 interrupt の multimodal | 対象外。`content` は文字列のみ（仕様 MVP） |
| N3 キューイング | 対象外。遷移中の 2 本目は 409（仕様 MVP） |
| 検証シナリオ 1（busy 中 messages は 409、hint に interrupt） | `TestSendMessage_BusyHintMentionsInterrupt`、`TestHandlePatchSession_BusyRejected`（hint 期待値を更新） |
| 検証シナリオ 2（Fallback ハッピーパス (1)〜(6)） | `TestInterrupt_FallbackReplacesInFlightTurn`、`TestTurnInterrupt_ClientV1RedirectsInFlightTurn`、`TestTurnInterrupt_CanonicalHistoryOrder`、ライブ 3 種 |
| 検証シナリオ 3（idle は拒否、新ターンを始めない） | `TestInterrupt_IdleSessionRejected`、`TestTurnInterrupt_IdleSessionClientError` |
| 検証シナリオ 4（cancel は再計画しない、次 messages が通る） | `TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes`、ライブ `TestLiveTurnCancel_CodexResume`（状態確認を追加） |
| 検証シナリオ 5（連続 interrupt は 409、二重実行しない） | `TestInterrupt_ConcurrentInterruptRejected` |
| 検証シナリオ 6（capability false で Native フックを呼ばない） | 本 Part は Native フック自体が無く常に fallback（`TestInterrupt_FallbackReplacesInFlightTurn` で `interrupt_mode=fallback` を確認）。フックの有無を伴う確認は Part 2 |
| 検証シナリオ 7（capability true の stub） | Part 2 |
| テスト項目（ビルド・統合・ライブのコマンド） | Verification Plan |

## Proposed Changes

依存の向き（インタフェース → 構造体 → ロジック）の順に並べ、各コンポーネントでは `_test.go` を先に記す。

### codingagent（SSE の新フィールド）

#### [MODIFY] [shared/libs/go/codingagent/event_test.go](shared/libs/go/codingagent/event_test.go)

*   **Description**: `TestStreamEventJSONMarshal` のテーブルに 2 ケースを足す。
*   **Technical Design**:
    ```go
    {
        name: "interrupt system event carries mode and interrupted turn",
        event: codingagent.StreamEvent{
            Type: codingagent.EventSystem, Content: "turn interrupt",
            InterruptMode: "fallback", InterruptedTurnID: "t-old",
        },
        checks: func(t *testing.T, data map[string]any) {
            if data["interrupt_mode"] != "fallback" {
                t.Errorf("interrupt_mode = %v", data["interrupt_mode"])
            }
            if data["interrupted_turn_id"] != "t-old" {
                t.Errorf("interrupted_turn_id = %v", data["interrupted_turn_id"])
            }
        },
    },
    {
        name:  "interrupt fields are omitted when empty",
        event: codingagent.StreamEvent{Type: codingagent.EventText, Content: "x"},
        checks: func(t *testing.T, data map[string]any) {
            for _, k := range []string{"interrupt_mode", "interrupted_turn_id"} {
                if _, ok := data[k]; ok {
                    t.Errorf("%s must be omitted", k)
                }
            }
        },
    },
    ```

#### [MODIFY] [shared/libs/go/codingagent/event.go](shared/libs/go/codingagent/event.go)

*   **Description**: `StreamEvent` に割り込みの観測用フィールドを足す（omitempty なので既存のワイヤ形式は変わらない）。
*   **Technical Design**:
    ```go
    type StreamEvent struct {
        // ... existing fields ...
        Usage             *TokenUsage `json:"usage,omitempty"`
        // InterruptMode is set on the system event that opens a turn started by
        // POST /interrupt ("fallback" or "native").
        InterruptMode string `json:"interrupt_mode,omitempty"`
        // InterruptedTurnID is the turn_id that the interrupt stopped.
        InterruptedTurnID string `json:"interrupted_turn_id,omitempty"`
        Error             error  `json:"-"`
        Retryable         bool   `json:"-"`
    }
    ```
*   **Logic**: `SplitStreamEventForSSE` は `tool_result` 以外を素通しするので追加の対応は不要。

---

### agentservice 実行ライフサイクルの安全化（前提修正）

#### [NEW] [shared/libs/go/agentservice/turn_script_agent_test.go](shared/libs/go/agentservice/turn_script_agent_test.go)

*   **Description**: cancel / terminate / interrupt の HTTP テストで共有する台本付きエージェントと HTTP ヘルパ（package `agentservice_test`）。
*   **Technical Design**:
    ```go
    // turnScriptAgent runs scripted turns. A hanging turn emits system/text and a
    // tool_use (or user_input_required) and then blocks until its context is
    // cancelled. Other turns emit text "replanned" and result.
    type turnScriptAgent struct {
        name          string
        hang          func(turn int) bool // 1-based turn number
        suspend       bool                // hanging turns emit user_input_required instead of tool_use
        blockCreate   chan struct{}       // when non-nil, CreateSession of turn >= 2 waits until closed
        createEntered chan struct{}       // buffered(1); signaled when a blocked CreateSession starts

        mu      sync.Mutex
        prompts []string // prompt passed to Send, per turn
        resumes []string // SessionConfig.AgentSessionID, per CreateSession
        creates atomic.Int32
        closes  atomic.Int32
    }

    func newTurnScriptAgent(hang func(turn int) bool) *turnScriptAgent // name "codex", createEntered buffered(1)
    func (a *turnScriptAgent) Name() string
    func (a *turnScriptAgent) Close() error
    func (a *turnScriptAgent) CreateSession(ctx context.Context, opts ...codingagent.SessionOption) (codingagent.Session, error)
    func (a *turnScriptAgent) promptAt(i int) string // 0-based; "" when absent
    func (a *turnScriptAgent) resumeAt(i int) string

    type turnScriptSession struct {
        agent  *turnScriptAgent
        turn   int
        closed atomic.Bool
    }
    func (s *turnScriptSession) ID() string    // "script-<turn>"
    func (s *turnScriptSession) Close() error  // counts once per session (CompareAndSwap)
    func (s *turnScriptSession) Send(ctx context.Context, prompt string) (<-chan codingagent.StreamEvent, error)
    ```
*   **Logic**:
    *   `CreateSession`: `n := a.creates.Add(1)`。`cfg := codingagent.NewSessionConfig(opts...)` の `AgentSessionID` を `resumes` に追記する。`n >= 2 && a.blockCreate != nil` なら `createEntered` へ非ブロッキング送信してから `<-a.blockCreate` で待つ。
    *   `Send`: prompt を `prompts` に追記し、バッファ 8 のチャネルを返す。`a.hang(s.turn)` が真なら順に `{system, SessionID:"native-thread-1"}`、`{text, "working on a.txt"}`、`suspend` なら `{user_input_required, "Proceed?"}`、そうでなければ `{tool_use, ToolName:"command_execution", ToolInput:{"command":"sleep 60"}}` を送り、`<-ctx.Done()` で閉じる。偽なら `{text, "replanned"}`、`{result}` を送って閉じる。
    *   HTTP ヘルパ（同ファイル）:
    ```go
    func newScriptServer(t *testing.T, agent *turnScriptAgent, opts ...agentservice.ServerOption) (*agentservice.Server, *httptest.Server)
    // base opts: WithProcessRetry(MaxAttempts:1), WithToolHeartbeatInterval(0)
    func createScriptSession(t *testing.T, ts *httptest.Server) (sessionID, sessionDir string) // agent "codex", session_dir = t.TempDir()
    type sseCapture struct { /* done chan; mu; events []map[string]any; sawDone bool; status int; raw string */ }
    func startSSE(t *testing.T, ts *httptest.Server, path string, body any) *sseCapture // POST, Accept: text/event-stream, reads in a goroutine until [DONE] or EOF
    func (c *sseCapture) waitFor(t *testing.T, pred func(ev map[string]any) bool, timeout time.Duration)
    func (c *sseCapture) waitDone(t *testing.T, timeout time.Duration)
    func (c *sseCapture) snapshot() []map[string]any
    func postJSON(t *testing.T, ts *httptest.Server, path string, body any, accept string) (int, []byte)
    func getSessionInfo(t *testing.T, ts *httptest.Server, sessionID string) map[string]any
    func waitInFlightTurn(t *testing.T, ts *httptest.Server, sessionID string) string // polls until followable, returns turn_id (timeout 3s)
    func loadCanonical(t *testing.T, sessionDir string) []session.Message         // session.OpenCanonical(dir).LoadRange(1, 0)
    ```

#### [MODIFY] [shared/libs/go/agentservice/exec_registry_test.go](shared/libs/go/agentservice/exec_registry_test.go)

*   **Description**: 実行登録の同一性確認、停止理由、先頭イベント付与の単体テストを足す。
*   **Technical Design**:
    ```go
    func TestExecRegistry_UnregisterIfOnlyRemovesSameExecution(t *testing.T)
    // register a; UnregisterIf("s1", b) == false and a stays; UnregisterIf("s1", a) == true and Get fails.

    func TestActiveExecution_MarkStoppedOnce(t *testing.T)
    // markStopped(stopReasonInterrupted) == true; second markStopped(stopReasonCancelled) == false;
    // stoppedReason() == stopReasonInterrupted; isStopped() == true; nil exec: isStopped() == false.

    func TestActiveExecution_SuppressesSessionWrites(t *testing.T)
    // table: "" -> false, cancelled/interrupted/terminated -> true, drain_timeout -> false.

    func TestPrependEvents_YieldsPreambleThenSource(t *testing.T)
    // src gets text "a", result, then close; prependEvents(src, system "p") yields p, a, result and closes.
    ```

#### [NEW] [shared/libs/go/agentservice/turn_gate_test.go](shared/libs/go/agentservice/turn_gate_test.go)

*   **Description**: ターンゲートの単体テスト（package `agentservice`、待ちはケース A で 200ms 以内）。
*   **Technical Design**:
    ```go
    func TestTurnGates_TryLockFailsWhileHeld(t *testing.T)   // Lock("s1"); TryLock("s1") fails; unlock; TryLock succeeds
    func TestTurnGates_LockWaitsForUnlock(t *testing.T)      // second Lock in goroutine stays blocked 100ms, returns after unlock
    func TestTurnGates_UnlockIsIdempotent(t *testing.T)      // u1 := Lock; u1(); u2 := Lock; u1() again; TryLock still fails (u2 held)
    func TestTurnGates_SessionsAreIndependent(t *testing.T)  // Lock("s1") does not block TryLock("s2")
    ```

#### [MODIFY] [shared/libs/go/agentservice/usage_aggregator_test.go](shared/libs/go/agentservice/usage_aggregator_test.go)

*   **Description**: 停止ヘルパが使用量を確定させる処理と、旧ターンの SSE 処理が同時に集計器へ触れても壊れないことを確かめる。
*   **Technical Design**:
    ```go
    func TestTurnUsageAggregator_ConcurrentObserveAndMerge(t *testing.T) {
        a := newTurnUsageAggregator("t1")
        var wg sync.WaitGroup
        for i := 0; i < 8; i++ {
            wg.Add(2)
            go func(i int) {
                defer wg.Done()
                for j := 0; j < 200; j++ {
                    a.Observe(codingagent.StreamEvent{Type: codingagent.EventText,
                        Usage: &codingagent.TokenUsage{CallID: fmt.Sprintf("o-%d-%d", i, j), InputTokens: 1}})
                }
            }(i)
            go func(i int) {
                defer wg.Done()
                for j := 0; j < 200; j++ {
                    a.MergeCalls([]codingagent.TokenUsage{{CallID: fmt.Sprintf("m-%d-%d", i, j), InputTokens: 1}})
                }
            }(i)
        }
        wg.Wait()
        rec, ok := a.Finalize()
        if !ok || len(rec.Calls) != 3200 {
            t.Fatalf("ok=%v calls=%d, want 3200", ok, len(rec.Calls))
        }
    }
    ```
*   **Logic**: 修正前は `seenCall` マップへの同時書き込みで Go ランタイムが `concurrent map writes` で異常終了する（`-race` 無しでも検出される）。

#### [NEW] [shared/libs/go/agentservice/turn_lifecycle_internal_test.go](shared/libs/go/agentservice/turn_lifecycle_internal_test.go)

*   **Description**: HTTP を通さず、実際の `Server` と登録簿で後片付けと停止の不変条件を確かめる（package `agentservice`）。既存の `closeProbe`（`handler_retry_unexported_test.go`）を再利用する。
*   **Technical Design**:
    ```go
    func TestFinishExecution_StoppedExecLeavesSuccessorRegistries(t *testing.T)
    // old stopped (interrupted); successor registered with its cancel func and active session.
    // finishExecution(old, "s1", oldProbe, nil): successor still registered; CancelExecution("s1") == true
    // and successor ctx is cancelled; oldProbe.closed == false (the stopper owns closing); newProbe.closed == false.

    func TestFinishExecution_SupersededExecClosesOnlyItsOwnSession(t *testing.T)
    // old not stopped but superseded: oldProbe closed; successor registration and cancel func kept.

    func TestFinishExecution_OwnExecReleasesRegistries(t *testing.T)
    // own exec: unregistered; CancelExecution("s1") == false; probe closed.

    func TestStopInFlightTurn_InterruptFinalizesTurnOnce(t *testing.T)
    // NewWithStore(NewMemorySessionStore()); record{ID:"s1", AgentName:"codex", SessionDir:t.TempDir()}.
    // relay over src (heartbeat 0) with text "partial" and tool_use command_execution; wait until 2 events.
    // exec registered with RegisterExecCancel; stopInFlightTurn(ctx, exec, {interrupted, terminal: error "turn interrupted", ingest: true}).
    // assert: exec ctx cancelled; probe closed; not registered; CancelExecution false; relay source done;
    // last relay event is error "turn interrupted" with TurnID "t-old"; suppressesSessionWrites() true;
    // canonical history has assistant "partial" and a tool call "command_execution"; returned len(events) == 3.

    func TestStopInFlightTurn_CancelDoesNotIngestOrAppend(t *testing.T)
    // reason cancelled, no terminal, ingest false: canonical history stays empty; last relay event is the tool_use.

    func TestMarkSessionActive_ClearsStaleError(t *testing.T)
    // record Status error / Error "turn cancelled" -> markSessionActive -> active / "".

    func TestHandleRelaySideEffects_StoppedExecWritesNothing(t *testing.T)
    // record active, AgentSessionID "orig"; exec stopped (interrupted).
    // EventSystem{SessionID:"late"}, EventUserInputRequired, EventResult -> AgentSessionID "orig", Status active.

    func TestWaitDetached_StoppedExecReturnsStopReason(t *testing.T)
    // exec stopped (interrupted) -> waitDetached returns error "turn interrupted" without waiting (ctx timeout 1s guards hangs).

    func TestArmReattachTimer_SkipsStoppedExec(t *testing.T)
    // exec stopped -> armReattachTimer leaves reattachTimer nil.

    func TestStopExecOnDrainTimeout_MarksStoppedWithoutSuppressingWrites(t *testing.T)
    // own exec -> isStopped() true, suppressesSessionWrites() false, content is drain timeout.
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler_cancel_test.go](shared/libs/go/agentservice/handler_cancel_test.go)

*   **Description**: cancel / terminate の回帰テストを足す（`turnScriptAgent` を使用）。
*   **Technical Design**:
    ```go
    func TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes(t *testing.T)
    // hang(1)=true, hang(2)=false. messages SSE in flight (wait tool_use) -> POST cancel 200 "cancelled".
    // old SSE done within 3s; session status error, error "turn cancelled", followable false; creates == 1.
    // next POST /messages (SSE) -> 200 with result; then status "completed" and error "".  <- red before fix

    func TestHandleTerminate_InFlightTurnStaysClosed(t *testing.T)
    // hang(1)=true. messages SSE in flight -> POST terminate 200; old SSE done within 3s;
    // status "closed" after the old stream finished; closes == 1.
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler_session_test.go](shared/libs/go/agentservice/handler_session_test.go)

*   **Description**: `TestHandlePatchSession_BusyRejected` の hint 期待値を `follow, respond, interrupt, cancel or terminate` に更新する。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](shared/libs/go/agentservice/exec_registry.go)

*   **Description**: 停止理由、同一性付きの登録解除、先頭イベント付与、busy hint の更新。
*   **Technical Design**:
    ```go
    const hintSessionBusy = "follow, respond, interrupt, cancel or terminate"

    // Stop reasons recorded on an execution stopped outside its own runTurn loop.
    const (
        stopReasonCancelled    = "cancelled"
        stopReasonInterrupted  = "interrupted"
        stopReasonTerminated   = "terminated"
        stopReasonDrainTimeout = "drain_timeout"
    )

    type activeExecution struct {
        // ... existing fields ...
        stopMu     sync.Mutex
        stopReason string
    }

    // markStopped records why exec was stopped. It returns false when exec was already stopped.
    func (e *activeExecution) markStopped(reason string) bool
    func (e *activeExecution) stoppedReason() string // "" when running; nil-safe
    func (e *activeExecution) isStopped() bool       // nil-safe
    // suppressesSessionWrites reports whether late writes from exec to the session record
    // (status, error, agent_session_id, usage) must be skipped: the API call that stopped it
    // (cancel / interrupt / terminate) writes the final state itself. Drain timeouts keep
    // today's late status write.
    func (e *activeExecution) suppressesSessionWrites() bool

    // UnregisterIf removes id only when it still maps to exec.
    func (r *execRegistry) UnregisterIf(id string, exec *activeExecution) bool

    // prependEvents yields preamble before forwarding every event of src; the
    // returned channel closes when src closes.
    func prependEvents(src <-chan codingagent.StreamEvent, preamble ...codingagent.StreamEvent) <-chan codingagent.StreamEvent
    ```
*   **Logic**:
    *   `markStopped`: `stopMu` 下で `stopReason != ""` なら false、そうでなければ代入して true。
    *   `suppressesSessionWrites`: `stoppedReason()` が cancelled / interrupted / terminated のとき true。
    *   `prependEvents`: `out := make(chan codingagent.StreamEvent, len(preamble)+8)`。goroutine で preamble を順に送り、`for ev := range src { out <- ev }` の後に `close(out)`。

#### [NEW] [shared/libs/go/agentservice/turn_gate.go](shared/libs/go/agentservice/turn_gate.go)

*   **Description**: セッション単位のターンゲート。
*   **Technical Design**:
    ```go
    // turnGates serializes, per session, the requests that start a turn or change the
    // in-flight turn (messages, interrupt, respond, cancel, terminate, PATCH). A gate is
    // held from the request's busy check until its new execution is registered or the
    // request fails, never while a turn streams.
    type turnGates struct {
        mu    sync.Mutex
        gates map[string]*sync.Mutex
    }

    func (g *turnGates) gate(sessionID string) *sync.Mutex // lazily creates the map and the entry
    // Lock blocks until the session gate is free. The returned unlock is idempotent.
    func (g *turnGates) Lock(sessionID string) func()          // m.Lock(); return sync.OnceFunc(m.Unlock)
    // TryLock acquires the gate only if it is free.
    func (g *turnGates) TryLock(sessionID string) (func(), bool) // m.TryLock(); return sync.OnceFunc(m.Unlock), true
    // Forget drops the gate of a deleted session.
    func (g *turnGates) Forget(sessionID string)
    ```
*   **Logic**: ゼロ値で使える（`New` / `NewWithStore` の変更は `Server` へのフィールド追加のみ）。

#### [MODIFY] [shared/libs/go/agentservice/usage_aggregator.go](shared/libs/go/agentservice/usage_aggregator.go)

*   **Description**: `turnUsageAggregator` に `mu sync.Mutex` を足し、`Observe` / `MergeCalls` / `Finalize` の本体をロック下で実行する（公開挙動は不変、`Finalize` の冪等性も維持）。

#### [NEW] [shared/libs/go/agentservice/turn_stop.go](shared/libs/go/agentservice/turn_stop.go)

*   **Description**: API 呼び出し（cancel / interrupt / terminate）が実行中ターンを止める共通処理と、停止に関する定数。
*   **Technical Design**:
    ```go
    const (
        // TurnInterruptedContent is the terminal error content on the interrupted turn's stream.
        TurnInterruptedContent = "turn interrupted"
        // sessionTerminatedContent is the terminal content reported for a terminated turn.
        sessionTerminatedContent = "session terminated"
    )

    // stopOptions selects the extra work stopInFlightTurn performs for its caller.
    type stopOptions struct {
        reason   string                   // stopReason* constant
        terminal *codingagent.StreamEvent // appended to the stopped turn's relay so subscribers see an explicit terminal
        ingest   bool                     // ingest the stopped turn's partial facts into canonical history
    }

    // stopInFlightTurn stops exec on behalf of an API call. The caller must hold the
    // session turn gate. After it returns, exec makes no further session-record writes;
    // the caller writes the final session status. Returns the stopped turn's relay snapshot.
    func (s *Server) stopInFlightTurn(ctx context.Context, exec *activeExecution, opts stopOptions) []codingagent.StreamEvent

    // stoppedTerminalContent maps a stop reason to the terminal content a detached wait reports.
    func stoppedTerminalContent(reason string) string
    ```
*   **Logic**（`stopInFlightTurn` の順序。順序自体が不変条件）:
    1. `exec.markStopped(opts.reason)`（以後の遅れた書き込みを止める。CancelExecution より前に行う）
    2. `exec.stopReattachTimer()`
    3. `s.CancelExecution(exec.sessionID)`（CLI プロセスや Wayfinder の Run を止める）
    4. `exec.agentSess != nil` なら `Close()`
    5. `opts.terminal != nil && exec.relay != nil` なら、写しに `TurnID = exec.turnID`、`CorrelationID = exec.correlationID` を入れて `exec.relay.appendEvent(ev)` し、`s.applyUsageSideEffects(exec.sessionID, exec, &ev)` で旧ターンの使用量を一度だけ確定する
    6. `exec.relay.markSourceDone()`（nil ならしない）
    7. `s.artifactStore != nil` なら `s.reconcileSessionArtifacts(ctx, exec.sessionID, exec.turnID, exec.correlationID)`（旧ターンがまだ登録されている間に行う。turn_files の flush は登録中の relay を読むため）
    8. `events := exec.relay.snapshot()`
    9. `opts.ingest` なら `s.ingestTurnEvents(exec.sessionID, events)`
    10. `s.execRegistry.UnregisterIf(exec.sessionID, exec)`、`s.UnregisterActiveSession(exec.sessionID)`、`s.UnregisterExecCancel(exec.sessionID)`
    11. `return events`
    *   `stoppedTerminalContent`: cancelled → `turnCancelledError`、interrupted → `TurnInterruptedContent`、terminated → `sessionTerminatedContent`、それ以外 → `drainTimeoutTerminalContent`。

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](shared/libs/go/agentservice/handler_retry.go)

*   **Description**: 遅れた書き込みの抑止、置き換え済み実行の検出、`runTurn` のフック、新ターン開始時の error クリア。
*   **Technical Design**:
    ```go
    // runTurnHooks lets the caller of runTurn observe turn admission.
    type runTurnHooks struct {
        preamble     []codingagent.StreamEvent // emitted at the head of the first relay
        onRegistered func()                    // idempotent; called after Register, or when runTurn returns without registering
    }

    func (s *Server) runTurn(r *http.Request, w http.ResponseWriter, execCtx context.Context, execCancel func(),
        record *codingagent.SessionRecord, sessionID, turnID, correlationID, promptText, rawUserPrompt, fallbackResume string,
        baseOpts []codingagent.SessionOption, savedFiles []string, hooks runTurnHooks)
    ```
*   **Logic**:
    *   `markSessionActive`: `rec.Status = StatusActive` に加えて `rec.Error = ""`（前ターンの cancel / interrupt マーカーや失敗理由は新ターンの状態ではない）。
    *   `stopExecOnDrainTimeout`: 既存の同一性確認の直後に `exec.markStopped(stopReasonDrainTimeout)`。`s.execRegistry.Unregister(sessionID)` を `UnregisterIf(sessionID, exec)` に置き換える（`exec == nil` のときは従来どおり `Unregister`）。
    *   `runTurn`:
        1. `finish := func() { s.finishExecution(active, sessionID, agentSess, savedFiles) }`（`active` は登録前 nil）。
        2. 冒頭に `released := false; release := func() { if !released { released = true; if hooks.onRegistered != nil { hooks.onRegistered() } } }; defer release()`。
        3. `sess.Send` 成功後、`!registered && len(hooks.preamble) > 0` なら `ch = prependEvents(ch, hooks.preamble...)` としてから relay を作る（リトライの 2 本目以降の relay には付けない）。
        4. `Register` 成功時: `registered = true; s.markSessionActive(sessionID); release()`、続けて従来どおり `wantSSE` なら `startSideEffectPump`。
        5. SSE 分岐: `term, suspended := s.streamSSERelay(...)` と `if suspended { return }` の直後に
           ```go
           if active.isStopped() {
               s.finishExecution(active, sessionID, agentSess, savedFiles)
               if r.Context().Err() == nil {
                   s.writeSSEDone(w)
               }
               return
           }
           ```
           を置く。停止済みの実行はリトライ・self-heal（`clearPersistedAgentSessionID` を含む）に進ませない。
        6. JSON 分岐: `respondJSONRelay` と `if suspended { return }` の直後に `if active.isStopped() { writeJSONEvents(w, events); s.finishExecution(active, sessionID, agentSess, savedFiles); return }`。
        7. 残りの `s.finishActiveExecution(sessionID, X, savedFiles)` はすべて `s.finishExecution(active, sessionID, X, savedFiles)` に置き換える。
    *   `emitClassifiedSSE`: `if exec != nil && !exec.suppressesSessionWrites() { s.updateSessionStatusOnTerminal(...) }`。
    *   `handleRelaySideEffects`: 先頭で `if exec.suppressesSessionWrites() { if s.taskLog != nil && !exec.hasSubscriber() { s.taskLog.Add(toAgentLogEntry(ev, sessionID, exec.turnID, exec.correlationID)) }; return false, nil }`。
    *   `pumpExecSideEffects`: ループ先頭の条件を `cur, ok := s.execRegistry.Get(sessionID); if !ok || cur != exec || exec.isStopped() { return }` にする。
    *   `armReattachTimer`: 早期 return の条件に `|| exec.isStopped()` を足す（`subMu` と `stopMu` は別ロックなので入れ子でも詰まらない）。
    *   `waitDetached`: ループ先頭で `if exec.isStopped() { return streamTerminal{kind: codingagent.EventError, content: stoppedTerminalContent(exec.stoppedReason())}, false }`。登録確認を `cur, ok := s.execRegistry.Get(sessionID); if !ok || cur != exec { return 従来の drain 終端, false }` に変え、置き換え後の session status を読まない。
    *   `attachSSE`: `writeEvent` の `s.applyUsageSideEffects(...)` を `if !exec.suppressesSessionWrites()` で囲む。`writeSyntheticTerminal` の `s.updateSessionStatusOnTerminal(...)` も同様に囲む。
    *   `streamSSERelay`: `if detached && !suspended { return s.waitDetached(...) }` の直後に `if exec.suppressesSessionWrites() { return term, suspended }`（状態更新と reconcile を飛ばす。reconcile は停止ヘルパが済ませている）。
    *   `respondJSONRelay`: 両ループ内の `applyUsageSideEffects` / `updateSessionStatusOnTerminal` / suspended の record 書き込み / `AgentSessionID` 書き込みを、イベントごとに `!exec.suppressesSessionWrites()` で囲む（registry 側の `exec.status` 更新は残す）。`done:` の直後に `if exec.suppressesSessionWrites() { return term, events, suspended }`。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](shared/libs/go/agentservice/handler.go)

*   **Description**: 同一性付きの終了処理、cancel / terminate / respond のゲートと停止ヘルパ化。
*   **Technical Design**:
    ```go
    // finishExecution closes exec's agent session and releases the busy-state registries,
    // but only those that still belong to exec. A stopped execution leaves everything to
    // the API call that stopped it; a superseded one closes only its own agent session.
    func (s *Server) finishExecution(exec *activeExecution, sessionID string, agentSess codingagent.Session, savedFiles []string)

    // writeJSONError writes {"error": msg, "hint": hint} (hint omitted when empty).
    func writeJSONError(w http.ResponseWriter, status int, msg, hint string)
    ```
*   **Logic**:
    *   `finishExecution`（`finishActiveExecution` を置き換え、呼び出し側をすべて移す）:
        1. `exec != nil && exec.isStopped()`: `CleanupMultimodalFiles(savedFiles)` だけ行い return（agent session を閉じない。止めた側が閉じている。Part 2 の Native 移譲で生きている session を閉じないためにも必要）。
        2. `exec != nil` で `cur, ok := s.execRegistry.Get(sessionID); ok && cur != exec`: 置き換え済み。`agentSess != nil` なら `Close()`、`CleanupMultimodalFiles(savedFiles)`、return。
        3. `exec == nil` で `_, ok := s.execRegistry.Get(sessionID); ok`: 登録前に失敗した起動が他ターンの登録を消さないよう、2 と同じ処理で return。
        4. それ以外は従来の本体（`stopReattachTimer`、`ingestActiveTurn`、`Close`、`UnregisterActiveSession`、`UnregisterExecCancel`、登録解除、一時ファイル削除）。登録解除は `exec != nil` なら `UnregisterIf(sessionID, exec)`、nil なら `Unregister(sessionID)`。
    *   `handleCancel`（パス解析、404、`closed` の 409 は従来どおり）:
        ```go
        unlock := s.turnGates.Lock(sessionID)
        defer unlock()
        if exec, ok := s.execRegistry.Get(sessionID); ok {
            s.stopInFlightTurn(r.Context(), exec, stopOptions{reason: stopReasonCancelled})
        } else if s.artifactStore != nil {
            s.reconcileSessionArtifacts(r.Context(), sessionID, "", "") // idle cancel keeps today's reconcile
        }
        if latest, err := s.sessions.Get(sessionID); err == nil {
            record = latest
        }
        record.Status = codingagent.StatusError
        record.Error = turnCancelledError
        s.sessions.Update(record)
        // 200 {"status":"cancelled"}
        ```
    *   `handleTerminate`（404 は従来どおり）:
        ```go
        unlock := s.turnGates.Lock(sessionID)
        defer unlock()
        if exec, ok := s.execRegistry.Get(sessionID); ok {
            s.stopInFlightTurn(r.Context(), exec, stopOptions{reason: stopReasonTerminated})
        } else {
            s.CancelExecution(sessionID)
            if s.artifactStore != nil {
                // idle terminate keeps today's session-end reconcile (tests/reconcile_integration_test.go relies on it)
                s.reconcileSessionArtifacts(r.Context(), sessionID, "", "")
            }
            s.UnregisterActiveSession(sessionID)
            s.UnregisterExecCancel(sessionID)
        }
        // re-read record; Status = closed; Update; artifactStore.CloseSession; 200 {"status":"terminated"}
        ```
    *   `handleRespond`: パス解析の直後に `release := s.turnGates.Lock(sessionID); defer release()`。既存の検査順（no active execution → session not found → not suspended → decode → content → stdin → WriteStdin → status 更新）はそのまま。status を `active` に戻した直後に `release()`（ストリーム中はゲートを持たない）。最後の `s.finishActiveExecution(sessionID, exec.agentSess, nil)` を `s.finishExecution(exec, sessionID, exec.agentSess, nil)` に置き換える。
    *   `handleDeleteSession`: 削除成功後に `s.turnGates.Forget(id)`。
    *   `handleSendMessage`（手順 3 で入れ、手順 5 で `launchTurn` に移す）: 404 → `applySessionModelDefault` の後に `release := s.turnGates.Lock(sessionID); defer release()`、record を読み直してから busy 判定と既存の検証を行い、`s.runTurn(..., savedFiles, runTurnHooks{onRegistered: release})` を呼ぶ。登録の直後にゲートが空くので、実行中ターンへの cancel / PATCH / 2 本目の messages は待たされない。

#### [MODIFY] [shared/libs/go/agentservice/handler_session.go](shared/libs/go/agentservice/handler_session.go)

*   **Description**: PATCH のゲートと、ingest の分割。
*   **Logic**:
    *   `handlePatchSession`: 404 判定の後に `unlock := s.turnGates.Lock(id); defer unlock()`、続けて record を読み直してから busy / suspended の判定に進む。
    *   `ingestActiveTurn(sessionID)` を `ingestTurnEvents(sessionID, events)` と薄い包みに分ける:
        ```go
        func (s *Server) ingestActiveTurn(sessionID string) {
            exec, ok := s.execRegistry.Get(sessionID)
            if !ok || exec == nil || exec.relay == nil {
                return
            }
            s.ingestTurnEvents(sessionID, exec.relay.snapshot())
        }

        // ingestTurnEvents appends a turn's assistant/tool facts to canonical history and
        // refreshes the session's native resume id from the binding.
        func (s *Server) ingestTurnEvents(sessionID string, events []codingagent.StreamEvent) // existing body after the snapshot
        ```

#### [MODIFY] [shared/libs/go/agentservice/service.go](shared/libs/go/agentservice/service.go)

*   **Description**: `Server` に `turnGates turnGates` を足す（ゼロ値で使えるので `New` / `NewWithStore` の初期化は不要）。ルーティングは割り込みコンポーネントで足す。

---

### agentservice 割り込みのプロンプトと正本レコード

#### [NEW] [shared/libs/go/agentservice/interrupt_prompt_test.go](shared/libs/go/agentservice/interrupt_prompt_test.go)

*   **Description**: 進捗要約・再計画プロンプト・正本レコードの単体テスト（package `agentservice`、テーブル駆動）。
*   **Technical Design**:
    ```go
    func TestSummarizeInterruptedTurn(t *testing.T) {
        tests := []struct {
            name        string
            events      []codingagent.StreamEvent
            wantContain []string
            wantAbsent  []string
        }{
            {name: "no events", events: nil, wantContain: []string{interruptNoProgress}},
            {name: "heartbeat only is not progress",
                events: []codingagent.StreamEvent{{Type: codingagent.EventProgress, Content: "tool_still_running"}},
                wantContain: []string{interruptNoProgress}, wantAbsent: []string{"tool_still_running"}},
            {name: "assistant text",
                events: []codingagent.StreamEvent{{Type: codingagent.EventText, Content: "hello "}, {Type: codingagent.EventText, Content: "world"}},
                wantContain: []string{"Assistant output (most recent part):", "hello world"}, wantAbsent: []string{"Tool calls:"}},
            {name: "tool still running",
                events: []codingagent.StreamEvent{{Type: codingagent.EventToolUse, ToolName: "command_execution", ToolInput: map[string]any{"command": "sleep 60"}}},
                wantContain: []string{`- command_execution {"command":"sleep 60"} -> interrupted while running (no result)`}},
            {name: "tool completed",
                events: []codingagent.StreamEvent{
                    {Type: codingagent.EventToolUse, ToolName: "Write", ToolInput: map[string]any{"file_path": "a.txt"}},
                    {Type: codingagent.EventToolResult, Content: "ok"}},
                wantContain: []string{`- Write {"file_path":"a.txt"} -> completed: ok`}},
            {name: "tool_call_id matches the right call",
                events: []codingagent.StreamEvent{
                    {Type: codingagent.EventToolUse, ToolName: "A", ToolCallID: "id1"},
                    {Type: codingagent.EventToolUse, ToolName: "B", ToolCallID: "id2"},
                    {Type: codingagent.EventToolResult, ToolCallID: "id1", Content: "r1"}},
                wantContain: []string{"- A -> completed: r1", "- B -> interrupted while running (no result)"}},
            // "many tools": 25 completed pairs -> "- (5 earlier tool calls omitted)" and exactly 20 tool lines after it
        }
        // ...
    }

    func TestSummarizeInterruptedTurn_TruncatesMultibyteSafely(t *testing.T)
    // text "あ" x 3000: utf8.ValidString; contains truncatedMarker;
    // len <= interruptProgressMaxBytes + len("\n"+truncatedMarker).

    func TestBuildInterruptReplanPrompt_Layout(t *testing.T)
    // golden: header, "interrupted_turn_id: t-old", notice, progress label + progress, footer, instruction last.

    func TestFormatInterruptHistory(t *testing.T)
    // == "[Tern interrupt] interrupted_turn_id=t-old mode=fallback\nchange of plan"
    ```

#### [NEW] [shared/libs/go/agentservice/interrupt_prompt.go](shared/libs/go/agentservice/interrupt_prompt.go)

*   **Description**: Fallback replan の材料（進捗要約、再計画プロンプト、正本レコード本文）。LLM を使わない決定的な処理。
*   **Technical Design**:
    ```go
    const (
        // InterruptPromptHeader opens the prompt of a Fallback replan turn.
        InterruptPromptHeader = "Tern turn interrupt"
        // InterruptHistoryMarker starts the canonical-history user message recorded for an interrupt.
        InterruptHistoryMarker = "[Tern interrupt]"

        interruptPromptNotice = "Your previous turn was interrupted by the user before it completed. " +
            "Tern stopped it and recorded the progress observed so far (best effort; it may be incomplete). " +
            "Reconcile that progress with the user's instruction below: continue work that is still valid, " +
            "re-plan work that the instruction changes, and do not repeat completed steps unless the instruction requires it."
        interruptPromptProgressLabel = "Progress observed before the interruption:"
        interruptPromptFooter        = "End of interrupt context"
        interruptNoProgress          = "(no progress was observed before the interruption)"
        truncatedMarker              = "... [TRUNCATED]"

        interruptProgressMaxBytes     = 4096
        interruptProgressTextMaxBytes = 1536
        interruptProgressMaxTools     = 20
        interruptProgressPreviewBytes = 160
    )

    type interruptPromptInput struct {
        InterruptedTurnID string
        Progress          string
        Instruction       string
    }

    func summarizeInterruptedTurn(events []codingagent.StreamEvent) string
    func buildInterruptReplanPrompt(in interruptPromptInput) string
    func formatInterruptHistory(interruptedTurnID, mode, instruction string) string
    func previewOneLine(s string, max int) string
    func truncateUTF8Head(s string, max int) string
    func truncateUTF8Tail(s string, max int) string
    ```
*   **Logic**:
    *   `summarizeInterruptedTurn`:
        1. `EventText` の `Content` を順に連結して assistant 本文とする。
        2. `EventToolUse` ごとに手順 `{name: ToolName（空なら "tool"）, callID: ToolCallID, input: previewOneLine(json.Marshal(ToolInput), 160)}` を追加する（`ToolInput` が空なら input は ""。`json.Marshal` はキー順が決まるので結果は決定的）。
        3. `EventToolResult` は、`ToolCallID` が空でなければ末尾から同じ id の未完了手順を、無ければ末尾から最初の未完了手順を探して完了にし、`result = previewOneLine(Content, 160)` とする。
        4. その他（`progress` の心拍、`system`、`error`、usage）は無視する。
        5. 本文も手順も無ければ `interruptNoProgress` を返す。
        6. 本文があれば `"Assistant output (most recent part):\n" + truncateUTF8Tail(本文の前後空白を除いたもの, 1536) + "\n"`。
        7. 手順があれば `"Tool calls:\n"`。手順が 20 を超えたら `"- (N earlier tool calls omitted)\n"` を出し、末尾 20 件に絞る。各手順は `"- <name>[ <input>] -> completed[: <result>]"` または `"- <name>[ <input>] -> interrupted while running (no result)"`。
        8. 全体を `truncateUTF8Head(末尾改行を除いたもの, 4096)` で返す。
    *   `buildInterruptReplanPrompt`（`portable.WrapPrompt` と同じく、補足を前に置き利用者の指示を最後に置く）:
        ```text
        Tern turn interrupt

        interrupted_turn_id: <id>

        <notice>

        Progress observed before the interruption:
        <progress>

        End of interrupt context

        <instruction>
        ```
    *   `formatInterruptHistory`: `fmt.Sprintf("%s interrupted_turn_id=%s mode=%s\n%s", InterruptHistoryMarker, interruptedTurnID, mode, instruction)`。
    *   `previewOneLine`: `strings.Join(strings.Fields(s), " ")` で 1 行にし、`max` バイトを超えたら rune 境界まで戻して切り `"..."` を付ける。
    *   `truncateUTF8Head`: `len(s) <= max` ならそのまま。超えたら `cut := max` から `utf8.RuneStart` まで戻り `s[:cut] + "\n" + truncatedMarker`。
    *   `truncateUTF8Tail`: 超えたら `start := len(s) - max` から `utf8.RuneStart` まで進み `truncatedMarker + "\n" + s[start:]`。

---

### agentservice 割り込み API とターン開始の共通化

#### [NEW] [shared/libs/go/agentservice/handler_interrupt_test.go](shared/libs/go/agentservice/handler_interrupt_test.go)

*   **Description**: 割り込みの HTTP テスト（package `agentservice_test`、`turnScriptAgent` と `httptest.NewServer` を使用。待ちは原則 3 秒以内）。
*   **Technical Design**:
    ```go
    func TestInterrupt_FallbackReplacesInFlightTurn(t *testing.T)
    // hang(1)=true. messages SSE "run long tool" in flight; wait until it shows tool_use; oldTurn := waitInFlightTurn.
    // POST /interrupt {"content":"change of plan: create b.txt only","correlation_id":"corr-int"} (SSE) -> 200.
    // interrupt stream: first event system "turn context" with turn_id newTurn != oldTurn;
    //   next system "turn interrupt" with interrupt_mode "fallback", interrupted_turn_id oldTurn;
    //   text "replanned"; result; [DONE]; correlation_id "corr-int" on the turn events.
    // old stream: error "turn interrupted" with turn_id oldTurn, then [DONE].
    // agent.promptAt(1) contains InterruptPromptHeader, "interrupted_turn_id: "+oldTurn,
    //   "working on a.txt", `command_execution {"command":"sleep 60"} -> interrupted while running`,
    //   and ends with the interrupt content.
    // session: same id, status "completed", error "", not followable;
    //   closes == 2 (turn 1 closed by the stop, the replan turn by its own finish before [DONE]).
    // canonical history order: user "run long tool" < assistant "working on a.txt" < tool call
    //   < user "[Tern interrupt] interrupted_turn_id=<oldTurn> mode=fallback\n<content>" < assistant "replanned".

    func TestInterrupt_FallbackResumesNativeConversation(t *testing.T)
    // same flow: agent.resumeAt(1) == "native-thread-1" (turn 1 system session id, ingested before replan).

    func TestInterrupt_ValidationDoesNotStopInFlightTurn(t *testing.T)
    // SetModelProfiles codex MaxPromptBytes 10000. With the turn in flight:
    //   {"content":""} -> 400; {"content":"x","supplement":{"algorithm":"nope"}} -> 400;
    //   2000-byte content -> 413 (2000 + 8192 > 10000).
    // After each: same turn_id still followable, closes == 0, old stream not done. Cleanup: cancel.

    func TestInterrupt_IdleSessionRejected(t *testing.T)
    // new session -> 409 {"error":"no active turn"} with hint; creates == 0.
    // after one completed turn (hang false) -> 409 again; creates == 1.

    func TestInterrupt_SessionClosedRejected(t *testing.T)
    // terminate then interrupt -> 409 {"error":"session closed"}.

    func TestInterrupt_ConcurrentInterruptRejected(t *testing.T)
    // hang(1)=true, blockCreate. interrupt A (SSE goroutine) blocks in CreateSession (wait createEntered).
    // interrupt B -> 409 {"error":"turn transition in progress"}. close(blockCreate): A ends with result.
    // creates == 2; status "completed".

    func TestInterrupt_CancelWaitsForTransitionAndCancelsReplanTurn(t *testing.T)
    // hang(1)=hang(2)=true, blockCreate. interrupt blocks in CreateSession; cancel in a goroutine
    // has not returned after 150ms; close(blockCreate); cancel returns 200 within 3s;
    // interrupt stream ends within 3s and contains the system "turn interrupt" event;
    // status "error", error "turn cancelled"; closes == 2.

    func TestInterrupt_SuspendedTurnCanBeInterrupted(t *testing.T)
    // hang(1)=true, suspend. messages SSE ends at user_input_required; status "suspended".
    // interrupt -> 200 with "replanned" and result; status "completed"; closes == 2; history has the record.

    func TestInterrupt_MessageDuringTransitionGetsBusy(t *testing.T)
    // hang(1)=true, blockCreate. interrupt blocks in CreateSession; POST /messages in a goroutine has not
    // returned after 150ms (it waits on the turn gate); close(blockCreate); the message returns
    // 409 "session busy" within 3s (it sees the registered replan turn); creates == 2 (no third turn).

    func TestInterrupt_JSONModeReturnsEventArray(t *testing.T)
    // interrupt without Accept: text/event-stream -> 200 application/json array that contains
    // {"type":"system","content":"turn interrupt","interrupt_mode":"fallback"} and a result.

    func TestSendMessage_BusyHintMentionsInterrupt(t *testing.T)
    // turn in flight -> POST /messages 409 "session busy"; hint contains "interrupt". Cleanup: cancel.
    ```

#### [NEW] [shared/libs/go/agentservice/interrupt.go](shared/libs/go/agentservice/interrupt.go)

*   **Description**: 割り込みハンドラと定数。
*   **Technical Design**:
    ```go
    // InterruptRequest is the request body for POST /api/v1/sessions/:id/interrupt.
    type InterruptRequest struct {
        Content       string                      `json:"content"`
        CorrelationID string                      `json:"correlation_id,omitempty"`
        Supplement    *session.SupplementStrategy `json:"supplement,omitempty"`
    }

    // Interrupt modes reported on the new turn's stream and in server logs.
    const (
        InterruptModeFallback = "fallback"
        InterruptModeNative   = "native"
    )

    const (
        // InterruptEventContent is the content of the system event that opens a turn started by an interrupt.
        InterruptEventContent = "turn interrupt"
        // interruptPromptReserveBytes is the prompt budget kept for the interrupt notice and progress summary.
        interruptPromptReserveBytes = 8 * 1024

        errNoActiveTurn             = "no active turn"
        errTurnTransitionInProgress = "turn transition in progress"
        errSessionClosed            = "session closed"
        hintInterruptNoActiveTurn   = "send a message to start a new turn"
        hintTurnTransition          = "retry after the current turn transition completes"
        logTurnInterrupted          = "turn interrupted"
    )

    func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request)
    // selectInterruptMode chooses how to interrupt exec. Part 1 always returns InterruptModeFallback.
    func (s *Server) selectInterruptMode(agent codingagent.CodingAgent, exec *activeExecution) string
    ```
*   **Logic**（`handleInterrupt`。検証はすべて旧ターンを止める前に行う）:
    1. メソッドが POST でなければ 405。パスを分解し `parts[1] != "interrupt"` か id が空なら 400。
    2. `record` が無ければ 404。`record.Status == closed` なら `writeJSONError(w, 409, errSessionClosed, "")`。
    3. `InterruptRequest` を decode（失敗 400）。`strings.TrimSpace(req.Content) == ""` なら 400 `content is required`。`req.Supplement` の algorithm が未知なら 400 `unknown supplement algorithm: ...`。
    4. `agent := s.agents[record.AgentName]` が無ければ 500 `agent not available`。
    5. `maxBytes := s.resolveAgentConfig(record.AgentName).MaxPromptBytes`。`len(req.Content)+interruptPromptReserveBytes > maxBytes` なら 413 `interrupt content (N bytes) exceeds the limit (M bytes)`（M は `maxBytes - interruptPromptReserveBytes`）。
    6. `release, ok := s.turnGates.TryLock(sessionID)`。取れなければ `writeJSONError(w, 409, errTurnTransitionInProgress, hintTurnTransition)`。取れたら `defer release()`。
    7. `old, ok := s.execRegistry.Get(sessionID)`。無ければ `writeJSONError(w, 409, errNoActiveTurn, hintInterruptNoActiveTurn)`。
    8. record を読み直し、`closed` なら 409（ゲート下で再確認）。`s.applySessionModelDefault(record)`。
    9. `mode := s.selectInterruptMode(agent, old)`、`newTurnID := s.generateID()`、`stopCtx := context.WithoutCancel(r.Context())`（クライアント切断でも遷移を完了させる）。
    10. `terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}`、`events := s.stopInFlightTurn(stopCtx, old, stopOptions{reason: stopReasonInterrupted, terminal: &terminal, ingest: true})`。
    11. record を読み直し（ingest で `AgentSessionID` が更新されうる）、`Status = error`、`Error = TurnInterruptedContent` を保存（新ターンの `markSessionActive` で `active` / `""` に戻る。新ターンが起動できなかった場合の状態として残る）。
    12. `historyText := formatInterruptHistory(old.turnID, mode, req.Content)` を role `user`、`ContentParts: []session.ContentPart{{Type: "text", Text: historyText}}`、`Origin: record.AgentName` で `AppendSessionMessage`。失敗は Warn ログに留める。
    13. Info ログ `logTurnInterrupted`（`session_id`、`agent`、`interrupted_turn_id`、`turn_id`=newTurnID、`mode`、`correlation_id`）。
    14. `prompt := buildInterruptReplanPrompt(interruptPromptInput{InterruptedTurnID: old.turnID, Progress: summarizeInterruptedTurn(events), Instruction: req.Content})`。
    15. `s.launchTurn(w, r, record, turnLaunch{turnID: newTurnID, agentPrompt: prompt, correlationID: req.CorrelationID, supplement: req.Supplement, preamble: []codingagent.StreamEvent{{Type: codingagent.EventSystem, Content: InterruptEventContent, InterruptMode: mode, InterruptedTurnID: old.turnID}}, onRegistered: release})`。
    *   エージェント名による分岐は書かない（Codex / Claude Code / Wayfinder で同一経路）。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](shared/libs/go/agentservice/handler.go)

*   **Description**: `handleSendMessage` のターン開始部分を `launchTurn` に切り出し、messages と interrupt で共有する。messages の挙動は変えない（既存テスト群で確認）。
*   **Technical Design**:
    ```go
    // turnLaunch carries the inputs of one turn started by POST /messages or POST /interrupt.
    type turnLaunch struct {
        turnID        string                      // optional; generated when empty
        content       []codingagent.ContentPart   // validated request content (POST /messages)
        agentPrompt   string                      // interrupt: sent instead of the prompt built from content; the caller already appended canonical history
        correlationID string
        supplement    *session.SupplementStrategy
        preamble      []codingagent.StreamEvent   // emitted at the head of the new turn's relay
        onRegistered  func()                      // idempotent; called once the execution is registered or the launch fails
    }

    func (s *Server) launchTurn(w http.ResponseWriter, r *http.Request, record *codingagent.SessionRecord, l turnLaunch)
    ```
*   **Logic**:
    *   `handleSendMessage`: パス解析 → 404 → `applySessionModelDefault`（従来どおり）。続けて `release := s.turnGates.Lock(sessionID); defer release()`、record を読み直し、busy なら `writeSessionBusy`。decode / supplement / `ValidateContentParts` の検査（従来どおり）の後、`s.launchTurn(w, r, record, turnLaunch{content: req.Content, correlationID: req.CorrelationID, supplement: req.Supplement, onRegistered: release})`。
    *   `launchTurn`（現行 `handleSendMessage` の `turnID := s.generateID()` 以降をそのまま移し、次の点だけ変える）:
        1. 冒頭で `release := l.onRegistered; if release == nil { release = func() {} }; defer release()`。
        2. `turnID := l.turnID; if turnID == "" { turnID = s.generateID() }`。
        3. プロンプトの決定: `l.agentPrompt != ""` ならそれを使う。そうでなければ従来の multimodal / テキスト分岐（`BuildMultimodalPrompt` の失敗は 400）。
        4. `MaxPromptBytes` 検査は従来どおり（interrupt は手順 5 の予約で必ず通る）。
        5. 正本への user メッセージ追記は `l.agentPrompt == ""` のときだけ（interrupt は呼び出し側が割り込みレコードを追記済み）。
        6. `wrapPromptWithSupplement`、`opts` の組み立て（`WithTurnID(turnID)` を含む）、`execCtx` と `RegisterExecCancel` は従来どおり。
        7. `s.runTurn(..., savedFiles, runTurnHooks{preamble: l.preamble, onRegistered: release})`。

#### [MODIFY] [shared/libs/go/agentservice/service.go](shared/libs/go/agentservice/service.go)

*   **Description**: `routeSessionByID` に `else if strings.HasSuffix(path, "/interrupt") { s.handleInterrupt(w, r) }` を `/cancel` の分岐の後に足す。

---

### client/v1

#### [MODIFY] [client/v1/session_test.go](client/v1/session_test.go)

*   **Technical Design**:
    ```go
    func TestInterrupt_UsesInterruptPathAndBody(t *testing.T)
    // httptest: path "/api/v1/sessions/sess-int/interrupt", POST, Accept text/event-stream,
    // body {"content":"redo","correlation_id":"c1"}; responds with SSE system "turn interrupt" + result + [DONE];
    // InterruptWithOpts(ctx, "redo", InterruptOpts{CorrelationID:"c1"}) streams both events.

    func TestInterrupt_HTTPErrorIncludesStatus(t *testing.T)
    // 409 {"error":"no active turn"} -> error contains "HTTP 409" and "no active turn".
    ```

#### [MODIFY] [client/v1/stream_test.go](client/v1/stream_test.go)

*   **Technical Design**:
    ```go
    func TestStream_Events_DecodesInterruptFields(t *testing.T)
    // data: {"type":"system","content":"turn interrupt","turn_id":"t2","interrupt_mode":"fallback","interrupted_turn_id":"t1"}
    // -> Event{Type: EventSystem, Text: "turn interrupt", InterruptMode: InterruptModeFallback, InterruptedTurnID: "t1"};
    // data: {"type":"error","content":"turn interrupted","turn_id":"t1"} -> Error == TurnInterruptedContent.
    ```

#### [MODIFY] [client/v1/stream.go](client/v1/stream.go)

*   **Technical Design**:
    ```go
    // Interrupt values observed on streams (see POST /api/v1/sessions/:id/interrupt).
    const (
        InterruptModeFallback  = "fallback"
        InterruptModeNative    = "native"
        TurnInterruptedContent = "turn interrupted" // terminal error content on an interrupted turn's stream
    )

    type Event struct {
        // ... existing fields ...
        InterruptMode     string // set on the system event that opens a turn started by Interrupt
        InterruptedTurnID string
    }
    ```
*   **Logic**: `events()` の `raw` に `InterruptMode string json:"interrupt_mode,omitempty"` と `InterruptedTurnID string json:"interrupted_turn_id,omitempty"` を足し、汎用の `Event` 組み立てで写す。

#### [MODIFY] [client/v1/session.go](client/v1/session.go)

*   **Technical Design**:
    ```go
    // InterruptOpts are optional Interrupt fields.
    type InterruptOpts struct {
        CorrelationID string
        Supplement    *SupplementStrategy
    }

    // Interrupt stops the in-flight turn and starts a new turn that continues or
    // re-plans with content. The returned Stream is the new turn. The server returns
    // HTTP 409 when the session has no in-flight turn (use SendText instead).
    func (s *Session) Interrupt(ctx context.Context, content string) (*Stream, error)
    func (s *Session) InterruptWithOpts(ctx context.Context, content string, opts InterruptOpts) (*Stream, error)
    ```
*   **Logic**: `SendMessageWithOpts` と同形。body は `{"content", "correlation_id", "supplement"}`、パス `/api/v1/sessions/<id>/interrupt`、`Accept: text/event-stream`。200 以外は `fmt.Errorf("interrupt failed (HTTP %d): %s", status, body)`。

---

### 統合テストとライブ E2E（tests/）

#### [NEW] [tests/common_turn_interrupt_test.go](tests/common_turn_interrupt_test.go)

*   **Description**: client/v1 と実サーバ（`agentservice.New` + `httptest`）とファイルシステム上の正本 history を通す統合テスト（package `llm_test`）。正本 history の書き込みを伴うため統合テストが必須（planning-rules 2.1）。
*   **Technical Design**:
    ```go
    // turnInterruptAgent: turn 1 emits text "working" and tool_use and blocks until its
    // context is cancelled; later turns wait replanDelay, emit text "replanned" and result.
    type turnInterruptAgent struct { /* name "codex"; replanDelay time.Duration; creates atomic.Int32 */ }

    func newTurnInterruptHTTP(t *testing.T, agent *turnInterruptAgent) *httptest.Server
    // agentservice.New(WithTaskLog(tasklog.New()), WithProcessRetry(MaxAttempts:1), WithToolHeartbeatInterval(0))

    func TestTurnInterrupt_ClientV1RedirectsInFlightTurn(t *testing.T)
    // v1 client: SendText -> read Events until EventToolUse; oldTurn := stream.TurnID().
    // sess.Interrupt(ctx, "Stop. Only create b.txt.") -> EventSystem with InterruptMode == v1.InterruptModeFallback
    //   and InterruptedTurnID == oldTurn; text "replanned"; EventResult; no EventError.
    // remaining old Events -> EventError with Error == v1.TurnInterruptedContent.
    // client.GetSession: Status completed, Error "", Followable false; newStream.TurnID() != oldTurn.

    func TestTurnInterrupt_CanonicalHistoryOrder(t *testing.T)
    // after the same flow: session.OpenCanonical(info.SessionDir).LoadRange(1, 0) has, in order,
    // user original, assistant "working", assistant tool call, user record
    // (agentservice.InterruptHistoryMarker+" interrupted_turn_id="+oldTurn+" mode=fallback"), assistant "replanned".

    func TestTurnInterrupt_IdleSessionClientError(t *testing.T)
    // sess.Interrupt on an idle session -> error containing "409" and "no active turn"; creates == 0.

    func TestTurnInterrupt_FollowSeesReplanTurn(t *testing.T)
    // replanDelay 300ms. Interrupt with raw HTTP and drop its body right after the first event;
    // sess.Follow(ctx) -> TurnID == new turn, replays the system "turn interrupt" event, ends with result.
    ```

#### [NEW] [tests/turn_interrupt_live_test.go](tests/turn_interrupt_live_test.go)

*   **Description**: 実 CLI / 実 LLM での Fallback replan（package `llm_test`、既存の `startHeartbeatCancelLiveServer` / `startCancelableToolSSE` / `sleepToolPrompt` / `createE2ESessionWithModel` / `parseE2ESSEEvents` / `getE2ESession` / `liveWorkDir` / `requireLiveCodexCLI` / `requireLiveClaudeCLI` を再利用）。
*   **Technical Design**:
    ```go
    const liveInterruptInstruction = "Stop waiting immediately. Do not run any more commands or tools. Reply with exactly: INTERRUPT_OK"

    func postInterruptSSE(t *testing.T, baseURL, sessionID, content string, timeout time.Duration) *http.Response
    func assertInterruptHistoryRecorded(t *testing.T, sessionDir, interruptedTurnID string)

    // runLiveInterruptReplanOnce returns ok=false (retryable) only when the tool never started.
    func runLiveInterruptReplanOnce(t *testing.T, baseURL, agent, model, workDir string, sleepSec int) (ok bool, reason string)

    func TestLiveTurnInterrupt_CodexReplan(t *testing.T)      // requireLiveCodexCLI; model liveCodexHeartbeatModel
    func TestLiveTurnInterrupt_ClaudeCodeReplan(t *testing.T) // requireLiveClaudeCLI; model e2eDefaultModel
    func TestLiveTurnInterrupt_WayfinderReplan(t *testing.T)  // no CLI; model e2eDefaultModel
    ```
*   **Logic**（`runLiveInterruptReplanOnce`）:
    1. セッション作成、`startCancelableToolSSE(sleepToolPrompt(sleepSec))`。`toolInFlight` を最大 90 秒待つ。先に終わったら `false`。
    2. `getE2ESession` から `turn_id`（旧ターン）と `session_dir` を得る（`turn_id` が空なら Fatal）。
    3. `postInterruptSSE(..., liveInterruptInstruction, 120s)` が 200、`parseE2ESSEEvents` で読む。
    4. `system` / `InterruptEventContent` のイベントが `InterruptMode == fallback`、`InterruptedTurnID == 旧ターン` で、その `TurnID` が旧ターンと異なる。
    5. `assertTurnTerminal` に加え、`result` があり `error` が無い。
    6. 旧ストリームが 30 秒以内に終わり、`Content == agentservice.TurnInterruptedContent` かつ `TurnID == 旧ターン` の `error` と `[DONE]` がある。
    7. `getE2ESession` が `completed`、error が空、followable でない。
    8. `assertInterruptHistoryRecorded`（`session.OpenCanonical(sessionDir).LoadRange(1, 0)` に `InterruptHistoryMarker+" interrupted_turn_id="+旧ターン` で始まり指示文を含む user メッセージがある）。
    *   各 Test は既存の cancel 試験と同じく、`ok == false` のとき 1 回だけ秒数を延ばして再試行し、それでも駄目なら Fatal。

#### [MODIFY] [tests/tool_heartbeat_cancel_live_test.go](tests/tool_heartbeat_cancel_live_test.go)

*   **Description**: cancel 後の再開ターンが終端状態になることをライブでも確かめる（前提修正 3 の回帰）。
*   **Technical Design**:
    ```go
    // hasEventType reports whether events contain typ (shared with turn_interrupt_live_test.go).
    func hasEventType(events []codingagent.StreamEvent, typ codingagent.EventType) bool
    ```
*   **Logic**: `runCodexCancelResumeOnce` の最後の `assertTurnTerminal` の後で `getE2ESession` を読み、`result` があれば status が `completed`、無ければ status が `active` でないことを確かめる。

## Step-by-Step Implementation Guide

各手順の終わりに、その手順の変更だけを `git add` → `git commit`（英語メッセージ）する。

1.  **SSE フィールドのテスト（レッド）と追加**:
    *   `shared/libs/go/codingagent/event_test.go` に 2 ケースを足し、`./scripts/process/build.sh --skip-frontend` で失敗（未定義フィールドでコンパイルエラー）を確かめる。
    *   `shared/libs/go/codingagent/event.go` に `InterruptMode` / `InterruptedTurnID` を足し、緑を確かめる。
    *   Commit: `feat: add interrupt fields to stream events`
2.  **ライフサイクルのテストを先に書く（レッド）**: 内部テスト（package `agentservice`）と外部テスト（package `agentservice_test`）は同じテストバイナリに入るため、未定義シンボルを参照する内部テストを先に足すと、振る舞いのレッドが観測できない。次の 2 段に分ける。
    *   **2a（振る舞いのレッド）**: 既存のシンボルだけで書けるテストを先に足す。`turn_script_agent_test.go`（新規）、`handler_cancel_test.go` の 2 試験、`handler_session_test.go` の hint 期待値。`./scripts/process/build.sh --skip-frontend` を実行し、`TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes` が「次ターン完了後も status が active」、`TestHandlePatchSession_BusyRejected` が「hint に interrupt が無い」で落ちることを確かめ、レッド記録に書く。`TestHandleTerminate_InFlightTurnStaysClosed` は競合依存のため、落ちたかどうかをそのまま記録する。
    *   続けて `usage_aggregator_test.go` の並行試験を足して流し、`concurrent map writes` の異常終了を確かめる（異常終了はテストバイナリ全体を止めるので、他の振る舞いのレッドを観測した後に足す）。
    *   **2b（未定義のレッド）**: `exec_registry_test.go`、`turn_gate_test.go`（新規）、`turn_lifecycle_internal_test.go`（新規）を足し、未定義シンボルでコンパイルが失敗することを確かめてレッド記録に書く。
3.  **ライフサイクルの実装（グリーン）**:
    *   `exec_registry.go`（停止理由、`UnregisterIf`、`prependEvents`、hint）、`turn_gate.go`（新規）、`usage_aggregator.go`（mutex）、`turn_stop.go`（新規）を実装する。
    *   `handler_retry.go`（`markSessionActive`、`stopExecOnDrainTimeout`、`runTurnHooks` の追加と `runTurn` の変更一式、`emitClassifiedSSE`、`handleRelaySideEffects`、`pumpExecSideEffects`、`armReattachTimer`、`waitDetached`、`attachSSE`、`streamSSERelay`、`respondJSONRelay`）を変更する。`runTurnHooks` はこの手順で足す。ゲートを取る `handleSendMessage` が、登録直後にゲートを手放せるようにするため（フックが無いとターン完了までゲートを握り、cancel などが待たされ続ける）。
    *   `handler.go`（`finishExecution`、`writeJSONError`、`handleCancel`、`handleTerminate`、`handleRespond`、`handleDeleteSession`、`handleSendMessage` のゲート取得と `runTurn(..., runTurnHooks{onRegistered: release})`）、`handler_session.go`（PATCH のゲート、`ingestTurnEvents`）、`service.go`（`turnGates` フィールド）を変更する。
    *   `./scripts/process/build.sh --skip-frontend` で手順 2 のテストと既存テストがすべて緑になることを確かめる。
    *   Commit: `fix: keep stopped executions from touching successor turns and session state`
4.  **割り込みプロンプトのテスト（レッド）と実装**:
    *   `interrupt_prompt_test.go` を書いてレッドを確かめ、`interrupt_prompt.go` を実装して緑にする。
    *   Commit: `feat: add fallback replan prompt and interrupt history record`
5.  **ターン開始の共通化（挙動不変のリファクタ）**:
    *   `handler.go` に `turnLaunch` / `launchTurn` を作り、`handleSendMessage` のターン開始部分を移す（手順 3 で入れたゲートの解放は `turnLaunch.onRegistered` 経由で `runTurnHooks` に渡す。`preamble` もこの手順で `runTurnHooks` へ素通しにする）。
    *   `./scripts/process/build.sh --skip-frontend` で既存テスト（`handler_test.go`、`handler_v2_test.go`、multimodal、follow、cancel ほか）がすべて緑であることを確かめる。
    *   Commit: `refactor: extract launchTurn from handleSendMessage`
6.  **割り込み API のテスト（レッド）と実装**:
    *   `handler_interrupt_test.go` を書き、レッド（`/interrupt` のルートが無く、セッション取得の分岐に落ちて 405 などになる）を確かめる。
    *   `interrupt.go` を実装し、`service.go` にルートを足す（`runTurnHooks` と `launchTurn` は手順 3・5 で用意済み）。
    *   `./scripts/process/build.sh --skip-frontend` で緑を確かめる。
    *   Commit: `feat: add POST /interrupt with fallback replan`
7.  **client/v1 のテスト（レッド）と実装**:
    *   `client/v1/session_test.go`、`client/v1/stream_test.go` を書いてレッドを確かめ、`stream.go` と `session.go` を実装して緑にする。
    *   Commit: `feat: add Session.Interrupt to client v1`
8.  **統合テスト**:
    *   `tests/common_turn_interrupt_test.go` を書く。`./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'` で緑を確かめる。
    *   Commit: `test: add turn interrupt integration tests`
9.  **ライブ E2E**:
    *   `tests/turn_interrupt_live_test.go` を書き、`tests/tool_heartbeat_cancel_live_test.go` に `hasEventType` と状態確認を足す。
    *   Verification Plan のライブ実行コマンドで確かめる。
    *   Commit: `test: add live turn interrupt E2E for codex, claude code and wayfinder`
10. **ドキュメント**:
    *   Documentation 節の各ファイルを更新する。`./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestDocsIndexLayout|TestExamples_'` で文書・例の検査が通ることを確かめる。
    *   Commit: `docs: document turn interrupt API`
11. **最終検証**: Verification Plan をすべて実行し、総合判定を本計画書の「総合判定結果」に記録する。全テスト成功後、far-knowledge の記録（`.cursor/skills/record-far-knowledge/SKILL.md`）と `pre-push-knowledge-check` を行ってから `git push` する。

## Verification Plan

### テスト項目の設計（testing-rules §8）

依存関係は「`handleInterrupt` → ターンゲート / `stopInFlightTurn` / 進捗要約・プロンプト・正本レコード / `launchTurn` → `runTurn`（フック）→ `finishExecution` などの保護」である。末端から順に、実際に動くことを確かめて積み上げる（モックは「台本付きエージェント」だけに限り、サーバ・登録簿・relay・正本 history は本物を使う）。

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 純粋な部品 | 先頭イベント付与、同一性付き登録解除、停止理由、ターンゲート、集計器の並行安全、進捗要約・プロンプト・正本レコード、SSE フィールド | `TestPrependEvents_*`、`TestExecRegistry_UnregisterIf*`、`TestActiveExecution_*`、`TestTurnGates_*`、`TestTurnUsageAggregator_ConcurrentObserveAndMerge`、`TestSummarizeInterruptedTurn*`、`TestBuildInterruptReplanPrompt_Layout`、`TestFormatInterruptHistory`、`TestStreamEventJSONMarshal` |
| L2 ライフサイクル（本物の Server、HTTP なし） | 終了処理の同一性、停止ヘルパ、error クリア、遅れた書き込みの抑止、切断待ち、再接続タイマ、drain の停止記録 | `TestFinishExecution_*`、`TestStopInFlightTurn_*`、`TestMarkSessionActive_ClearsStaleError`、`TestHandleRelaySideEffects_StoppedExecWritesNothing`、`TestWaitDetached_StoppedExecReturnsStopReason`、`TestArmReattachTimer_SkipsStoppedExec`、`TestStopExecOnDrainTimeout_*` |
| L3 HTTP（台本付きエージェント） | cancel / terminate の回帰、busy hint、割り込みの全シナリオ | `TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes`、`TestHandleTerminate_InFlightTurnStaysClosed`、`TestHandlePatchSession_BusyRejected`、`TestSendMessage_BusyHintMentionsInterrupt`、`TestInterrupt_*` |
| L4 client 単体 | パス・本文・ヘッダ・エラー、Event の新フィールド | `TestInterrupt_UsesInterruptPathAndBody`、`TestInterrupt_HTTPErrorIncludesStatus`、`TestStream_Events_DecodesInterruptFields` |
| L5 統合（client + サーバ + ファイル） | client での割り込み、正本 history の順序、idle のエラー、Follow | `TestTurnInterrupt_*` |
| L6 ライブ | 実 CLI / 実 LLM での Fallback replan と cancel 後の状態 | `TestLiveTurnInterrupt_*`、`TestLiveTurnCancel_CodexResume` |

#### 観点チェックリスト（testing-rules §8.3）

| # | 観点 | 主なテスト |
| :--- | :--- | :--- |
| 1 | 正常系 | `TestInterrupt_FallbackReplacesInFlightTurn`、`TestTurnInterrupt_ClientV1RedirectsInFlightTurn`、ライブ 3 種 |
| 2 | 異常系・境界値 | `TestInterrupt_ValidationDoesNotStopInFlightTurn`（空・未知 algorithm・上限超過）、`TestInterrupt_IdleSessionRejected`、`TestInterrupt_SessionClosedRejected`、`TestSummarizeInterruptedTurn`（0 件・20 件超・多バイト切り詰め） |
| 3 | 外部連携の実動作 | 正本 history のファイル（`TestStopInFlightTurn_*`、`TestTurnInterrupt_CanonicalHistoryOrder`）、実 CLI（ライブ） |
| 4 | データの一貫性 | 正本 history の順序と内容、新旧 `turn_id`、`correlation_id`、ネイティブ会話 id の引き継ぎ（`TestInterrupt_FallbackResumesNativeConversation`） |
| 5 | 状態遷移 | busy → interrupt → completed、suspended → interrupt、cancel → error → 次ターン completed、terminate → closed のまま |
| 6 | 設定・構成の反映 | `MaxPromptBytes` による 413、`interrupt_mode` が `fallback`、ゲートによる待ち・拒否 |
| 7 | 副作用 | 旧 agent session が閉じる回数、後継の登録・cancel 関数が残ること（`TestFinishExecution_*`）、二重実行しないこと（`creates` の数） |

#### セルフレビュー（testing-rules §8.4）

1. **網羅性**: 割り込みの「止める・残す・始める・返す」の 4 動作それぞれに L2 と L3 の証拠があり、L5 で client とファイル、L6 で実エージェントを通す。前提修正 4 件（後片付けの取り違え、状態の上書き、cancel 後の completed、順序）にもそれぞれ専用のテストがあるので、全部が通れば機能が動いていると言える。
2. **証拠の十分性**: 「エラーが出ない」ではなく、新旧 `turn_id`、`interrupt_mode`、旧ストリームの終端内容、session の status / error / followable、正本 history の順序、プロンプトの中身、`creates` / `closes` の数を値で確かめる。
3. **迂回の排除**: Fallback を通ったことは、新ストリームの `interrupt_mode=fallback` とプロンプトの `Tern turn interrupt` 見出し、正本の `mode=fallback` の 3 点で確かめる。旧ターンが本当に止まったことは agent session の `Close` と旧ストリームの `turn interrupted` で確かめる。messages の別経路で再計画ターンが走っていないことは、割り込み要求の応答ストリームに新 `turn_id` が乗ることで確かめる。
4. **依存関係の整合**: L3 の割り込みテストは、L1 の要約・プロンプトと L2 の停止ヘルパ・終了処理が緑であることを前提にしている。手順 2〜6 の順序でこの前提が崩れない。

### 先に書く試験（レッド記録）

実行時に「実際のレッド」の列を埋める。想定と違う理由で落ちた場合は testing-rules §3 の修正フローに入る。

| 試験名 | 想定するレッドの理由 | 実際のレッド（実行時に記入） |
| :--- | :--- | :--- |
| `TestStreamEventJSONMarshal`（追加 2 ケース） | `InterruptMode` / `InterruptedTurnID` が未定義 | フィールド追加前に未定義でコンパイル不能になる想定どおり。本 Part では実装と同時に緑化した |
| `TestExecRegistry_UnregisterIfOnlyRemovesSameExecution`、`TestActiveExecution_*`、`TestPrependEvents_YieldsPreambleThenSource` | 対象が未定義 | `UnregisterIf` / `markStopped` / `prependEvents` 未定義でコンパイル不能（`go vet` で確認） |
| `TestTurnGates_*` | `turnGates` が未定義 | `turnGates` 未定義でコンパイル不能 |
| `TestTurnUsageAggregator_ConcurrentObserveAndMerge` | mutex が無く `concurrent map writes` で異常終了 | `fatal error: concurrent map read and map write` |
| `TestFinishExecution_*`、`TestStopInFlightTurn_*`、`TestWaitDetached_*`、`TestArmReattachTimer_*`、`TestStopExecOnDrainTimeout_*`、`TestHandleRelaySideEffects_*` | 対象が未定義、または停止済みでも書き込む | 未定義シンボルでコンパイル不能 |
| `TestMarkSessionActive_ClearsStaleError` | error が残る | 未定義／実装前は error が残る想定。実装後に緑 |
| `TestHandleCancel_DoesNotStartReplanTurnAndNextTurnCompletes` | 次ターン完了後も status が `active` | `status = active, want completed after next turn` |
| `TestHandleTerminate_InFlightTurnStaysClosed` | 旧ターンの遅れた終端で `closed` が `error` に上書きされる（競合のため再現しない回もある。その場合は再現しなかった旨を記録） | `status = error, want closed`（今回再現した） |
| `TestHandlePatchSession_BusyRejected`（期待値更新） | hint に `interrupt` が無い | hint が `follow, respond, cancel or terminate` のまま |
| `TestSummarizeInterruptedTurn*`、`TestBuildInterruptReplanPrompt_Layout`、`TestFormatInterruptHistory` | 対象が未定義 | 実装と同時に緑化（手順 4） |
| `TestInterrupt_*`、`TestSendMessage_BusyHintMentionsInterrupt` | `/interrupt` のルートが無い、hint に `interrupt` が無い | ルート追加前は 405 等。実装後に全件緑 |
| `TestInterrupt_UsesInterruptPathAndBody`、`TestInterrupt_HTTPErrorIncludesStatus`、`TestStream_Events_DecodesInterruptFields` | client の API とフィールドが未定義 | 実装と同時に緑化（手順 7） |

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
    （Linux / Remote-SSH の接続先が Linux の場合は `./scripts/process/build.sh --skip-etc`。開発中の高速確認は `--skip-frontend` 可。最終確認はフラグなし）
2.  **Integration Tests（モック、決定的）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
    ```
    *   **Log Verification**: サーバログに `turn interrupted`（`mode=fallback`、`interrupted_turn_id` と `turn_id` が異なる）が出ること。`failed to ingest turn into canonical history`、`failed to record interrupt in canonical history`、`panic` が出ないこと。
3.  **回帰（ターン・follow・respond・cancel 近傍のモック統合テスト）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestSessionFollow_|TestInteractive_|TestSessionRecover_|TestReconcile_|TestFileChangeCollectors_|TestIntegration_|TestStreamReconnect_'
    ```
    *   **Log Verification**: `SSE drain timed out` が意図しないテストで出ていないこと、`superseded` 系の Debug ログが割り込み以外のテストで出ていないこと。
4.  **E2E（ライブ、実 CLI と LLM 認証が必要）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestLiveTurnInterrupt_|TestLiveTurnCancel_|TestLiveToolHeartbeat_'
    ```
    *   **新規**: [tests/turn_interrupt_live_test.go](tests/turn_interrupt_live_test.go) の `TestLiveTurnInterrupt_CodexReplan` / `_ClaudeCodeReplan` / `_WayfinderReplan`。検証ポイントは「旧ストリームが `turn interrupted` で終わる」「新ストリームが `fallback` の system イベントで始まり `result` で終わる」「session が `completed`」「正本 history に割り込みレコード」。
    *   **変更**: [tests/tool_heartbeat_cancel_live_test.go](tests/tool_heartbeat_cancel_live_test.go) の `TestLiveTurnCancel_CodexResume` が再開ターン後に終端状態であること。
5.  **文書・例の検査**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestDocsIndexLayout|TestExamples_'
    ```

### 総合判定（testing-rules §9）

全テスト成功後、testing-rules §9.2 の 7 項目を一つずつ確かめ、§9.3 の書式で本計画書の末尾「総合判定結果」に記録する。特に次を確かめる。

*   **迂回処理による偽成功**: ライブの再計画ターンが Codex の self-heal（`retrying coding agent process`）を経て成功していないか。経ていた場合は、中断直後の `codex exec resume` が失敗している兆候として ⚠️ にする。
*   **アダプタの誤適用**: ライブ 3 種で、セッションの `agent_name` が意図どおり（codex / claudecode / wayfinder）であること。
*   **事実上のスキップ**: ライブで「ツールが始まらず再試行」が起きた回数を記録する。再試行の後に成功した場合も、その旨を備考に書く。
*   Claude Code の再計画ターンが `--resume` の失敗で error になった場合は ❌ とし、Claude Code の自己回復（self-heal）を別仕様で起票する方針を判定理由に書く。

#### 総合判定結果

**判定**: ✅ 動作確認完了

#### テスト結果サマリ
- 全テスト数: 対象スイート合算（単体 agentservice / client/v1、統合 `TestTurnInterrupt_` 4、ライブ interrupt 3 + cancel 2、文書 `TestDocsIndexLayout`、フル `build.sh`）
- 成功: 上記すべて
- 失敗: 0
- 事実上スキップ: 0（ライブは CLI / LLM 鍵が揃い Fatal 経路は踏まず。ツール未開始による再試行も 0）

#### チェック項目の結果
| # | チェック項目 | 結果 | 備考 |
|---|------------|------|------|
| 1 | スキップされたテスト | ✅ | ライブ 3 種とも Skip なし。再試行なしで初回成功 |
| 2 | 部分的なエラー | ✅ | 実装・検証ログに意図しない panic / ingest 失敗は無し |
| 3 | 迂回処理による偽成功 | ✅ | ライブ再計画が self-heal（`retrying coding agent process`）経由でないことを確認。`interrupt_mode=fallback` と正本の `mode=fallback` で Fallback 経路を値で確認 |
| 4 | アダプタ・コンフィグの誤適用 | ✅ | Codex / Claude Code / Wayfinder それぞれで agent_name と CLI 前提が一致 |
| 5 | テスト間の依存・順序問題 | ✅ | 単体はパッケージ単独、統合・ライブは `--specify` で絞り込み実行して成功 |
| 6 | カバレッジの妥当性 | ✅ | L1〜L6（部品・ライフサイクル・HTTP・client・統合・ライブ）と前提修正 4 件に対応する試験あり |
| 7 | 外部システムの状態 | ✅ | 実 CLI（codex / claude）と anthropic/openai/google 鍵が利用可能な状態でライブ成功 |

#### 判定理由
Verification Plan のビルド・単体・統合（`TestTurnInterrupt_`）・ライブ（interrupt 3 + cancel 回帰）・文書検査がすべて成功した。Fallback 経路は SSE の `interrupt_mode`、再計画プロンプト見出し、正本 history の 3 点で確認し、旧ターン停止は `turn interrupted` と agent session Close で確認した。ライブの偽成功（self-heal 経由）や事実上のスキップは観測されなかった。Part 2（Native seam）は未着手であり、本 Part の完了条件を満たす。

## Documentation

本リポジトリの公開仕様の正本は `docs/` にある（`prompts/specifications` は存在しない）。

#### [MODIFY] [docs/reference-manual-web-apis.md](docs/reference-manual-web-apis.md)
*   **更新内容**:
    *   Endpoint List に `POST /api/v1/sessions/:id/interrupt`（Interrupt the in-flight turn and start a turn that continues or re-plans）を足す。
    *   §8 の 409 の hint を `follow, respond, interrupt, cancel or terminate` に更新する。
    *   §9 Cancel に「interrupt の遷移中に来た cancel は、遷移の完了を待ってから新ターンを止める」「cancel は途中成果を正本に残さない（interrupt は残す）」を足す。
    *   新設 §9.1 Interrupt In-Flight Turn: メソッド・パス・本文（`content` 必須、`correlation_id` / `supplement` 任意）、応答＝新ターンのストリーム（content negotiation は Send Message と同一、新 `turn_id`）、新ターン先頭の system イベント（`content: "turn interrupt"`、`interrupt_mode`、`interrupted_turn_id`）、旧ターン購読者への `turn interrupted` と `[DONE]`、`fallback` の動き（停止・途中成果の ingest・割り込みレコード・`Tern turn interrupt` プロンプト・ネイティブ会話 id の resume）、正本レコード形式、同時実行（`turn transition in progress`、他の API は待つ）、suspended も可、エラー一覧（400 / 404 / 409 `session closed`・`no active turn`・`turn transition in progress` / 413 / 500）、client の対応 API。`native` は Part 2 で追記する旨を書かない（Part 2 で節を拡張する）。
#### [MODIFY] [docs/examples.md](docs/examples.md)
*   **更新内容**: 「3. Interactive Agent Execution」に `session.Interrupt(ctx, "...")` の短い例（409 のときは `SendText` を使う旨）を足し、suspended 中に受け付ける API の文に `interrupt` を加える。`Respond` / `CancelTurn` / `Interrupt` の使い分けを 1〜2 文で書く。
#### [MODIFY] [docs/architecture.md](docs/architecture.md)
*   **更新内容**: CAWA の段落に「実行中ターンへの割り込み（interrupt）で、途中の方針変更を同一セッション上で行える」を 1 文足す。
#### [MODIFY] [docs/roadmap.md](docs/roadmap.md)
*   **更新内容**: Phase 2 に `* [x] Turn interrupt (interrupt API, fallback replan)` を足す。
#### [MODIFY] [README.md](README.md)
*   **更新内容**: Architecture Overview の CAWA 行を `(agent lifecycle, multimodal content, interactive turns, turn interrupt)` にする。

## 継続計画について

本計画は Part 1 / 2 である。Part 2（[001-Turn-Interrupt-Native-Seam-Part2.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/plans/001-Turn-Interrupt-Native-Seam-Part2.md)）は、本 Part の `selectInterruptMode`、`finishExecution` の「停止済みの実行は agent session を閉じない」規則、`stopInFlightTurn`、`launchTurn` を前提に、Native resume の宣言口・経路・観測を足す。Part 1 の完了（総合判定まで）を Part 2 着手の条件とする。
