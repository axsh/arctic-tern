# 001-Turn-Interrupt-Native-Seam-Part2

> **Source Specification**: [000-Turn-Interrupt-And-Resume.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/000-Turn-Interrupt-And-Resume.md)
> **Part**: 2 / 2（Part 1: [000-Turn-Interrupt-Fallback-Part1.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/plans/000-Turn-Interrupt-Fallback-Part1.md)）

## Goal Description

Coding Agent 自身の中断・再開（Native resume）を、agentservice の第一級の経路として配線する。Agent が能力を宣言し、実行中のセッションが実行口を持つときは Native を優先し、そうでないとき、または Native が失敗したときは Part 1 の Fallback replan に落とす。

1. エージェント単位の宣言 `InterruptResumeSupporter` と、セッション単位の実行口 `TurnInterrupter` を codingagent に定義する。
2. `GET /api/v1/agents` の各エージェントに `supports_interrupt_resume` を出す（仕様 M5）。
3. `selectInterruptMode` を、宣言が真かつ実行中セッションが実行口を持つときだけ `native` を返すように実装する。
4. Native 経路では、エージェントプロセスを止めずに旧ターンを Tern 側で締め、割り込み指示をエージェントに渡し、返された続きのイベントを新ターン（新しい `turn_id`）として流す。プロセスと実行コンテキストの持ち主は旧ターンから新ターンへ移る。
5. Native が拒否・失敗したら、同じ要求の中で Fallback に落とす。
6. 既存アダプタ（Codex / Claude Code / Wayfinder）はどれも宣言しない（`false`）。アダプタごとの Native 本配線は後続仕様とする（backlog:314b541f-199c-496e-a201-c2a5a0e9af38）。

本 Part により、将来アダプタが実行口を実装した時点で、agentservice とクライアントを変えずに Native へ昇格できる（仕様 M3「後から Adapter 差し替えで昇格できるようにする」）。

## User Review Required

1. **capability の公開場所**: `GET /api/v1/agents` のエージェントごとのフィールドとする（GetSession には出さない）。能力はアダプタの性質であり、セッションは PATCH でエージェントを切り替えうるため。セッションの能力は `agent_name` で一覧を引けば分かる。
2. **セッション単位の契約**: `TurnInterrupter.InterruptTurn(ctx, instruction)` は、同じエージェントプロセスの中で現在の作業を止め、指示を注入し、続きのターンを流す**新しいチャネル**を返す。前のイベントチャネルは実装側が必ず閉じる。指示は利用者の文をそのまま渡す（Tern の再計画プロンプトは付けない。エージェントは自分の文脈を持っているため）。
3. **Native 失敗時の扱い**: 同じ要求の中で Fallback に落とす。仕様は「同一リクエスト内で Fallback に落とすか明示エラーにするかを後続で決める」としていたが、利用者の方針「Fallback を安全網として常に備える」に合わせて本計画で決める。失敗は Warn ログに残し、新ストリームの `interrupt_mode` は `fallback` になる。
4. **Native の続きのターン**: 新しい `turn_id` を持つ（CAWA の「1 ターン 1 turn_id」を保つ）。続きのターンでは、プロセス再起動を伴うリトライや self-heal を行わない。リトライ可能なエラーで終わった場合は、分類済みエラー（`[upstream_error]` / `[upstream_overloaded]`）として終端する。
5. **旧ターンの終端は一度だけ**: Native でも、`InterruptTurn` を呼ぶ前に Tern が旧ターンの購読者へ `turn interrupted` を出す。Native が失敗して Fallback に落ちても、旧ターンの終端イベントは重複しない。
6. **既知の制約（後続のアダプタ仕様で扱う）**: Native で続ける場合でも、元のメッセージに付いていた一時画像ファイル（`arctic-tern-multimodal/`）は旧ターンの終了処理で消える。画像を Native の続きで再参照するアダプタを作るときは、一時画像の寿命を別途扱う。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M3 能力を持つ Agent では Native resume を優先 | `interrupt.go`（`selectInterruptMode`、`serveNativeInterrupt`、`handleInterrupt` の分岐） |
| M3 能力が無い・失敗・未配線は Fallback | `selectInterruptMode` の判定、`handleInterrupt` の Native 失敗からの続行。`TestInterrupt_NativeFailureFallsBackInSameRequest`、`TestInterrupt_SessionWithoutHookUsesFallback` |
| M3 MVP は全 Agent が Fallback 既定 | 既存アダプタは変更しない。`TestTurnInterrupt_BuiltinAgentsDeclareNoNativeResume` |
| M3 Native を第一級として固定し、Adapter 差し替えで昇格できる | `codingagent/interface.go` の 2 インタフェース。agentservice 側の経路はスタブで動作確認済みにする |
| M5 Capability 宣言口（機械可読） | `InterruptResumeSupporter`、`handleListAgents` の `supports_interrupt_resume`、client `Agent.SupportsInterruptResume` |
| M5 公開場所を一方に決める | `GET /api/v1/agents`（User Review Required 1） |
| M5 `true` のときだけ Native | `TestSelectInterruptMode`、`TestInterrupt_CapabilityFalseNeverCallsNativeHook` |
| M6 Native 非依存で Fallback が動く | Part 1 の Fallback をそのまま使う（Native の失敗時も同経路） |
| M7 client / docs（capability） | client/v1 `agents.go`、Documentation 節 |
| M8 `native` / `fallback` をログと SSE で区別 | Info ログ `turn interrupted`（`mode=native`）、Warn ログ `native interrupt failed; falling back`、SSE system イベントの `interrupt_mode` |
| 実現方針「Tern は停止と注入を Adapter に委譲し、正本への事実記録は共通で行う」 | `serveNativeInterrupt` が旧ターンの ingest と割り込みレコード（`mode=native`）を Part 1 と同じ関数で記録する |
| 実現方針「失敗時は Fallback か明示エラーかを定義」 | Fallback に落とす（User Review Required 3） |
| 検証シナリオ 6（capability false で Native フックを呼ばない） | `TestInterrupt_CapabilityFalseNeverCallsNativeHook` |
| 検証シナリオ 7 (1)(2) Native 成功と経路の記録 | `TestInterrupt_NativeContinuesInSameAgentSession`、`TestTurnInterrupt_ClientV1NativeStub` |
| 検証シナリオ 7 (3) Native エラー時 | `TestInterrupt_NativeFailureFallsBackInSameRequest` |

## Proposed Changes

### codingagent

#### [MODIFY] [shared/libs/go/codingagent/interface_test.go](shared/libs/go/codingagent/interface_test.go)

*   **Description**: 2 つのインタフェースが実装・型判定できることを確かめる（package `codingagent_test`）。
*   **Technical Design**:
    ```go
    type interruptCapableStub struct{}

    func (interruptCapableStub) SupportsInterruptResume() bool { return true }
    func (interruptCapableStub) InterruptTurn(context.Context, string) (<-chan codingagent.StreamEvent, error) {
        return nil, nil
    }

    func TestInterruptInterfacesAreImplementable(t *testing.T) {
        var v any = interruptCapableStub{}
        sup, ok := v.(codingagent.InterruptResumeSupporter)
        if !ok || !sup.SupportsInterruptResume() {
            t.Fatal("stub must satisfy InterruptResumeSupporter")
        }
        if _, ok := v.(codingagent.TurnInterrupter); !ok {
            t.Fatal("stub must satisfy TurnInterrupter")
        }
    }
    ```

#### [MODIFY] [shared/libs/go/codingagent/interface.go](shared/libs/go/codingagent/interface.go)

*   **Technical Design**:
    ```go
    // InterruptResumeSupporter is an optional CodingAgent capability: sessions of this
    // agent can natively interrupt the in-flight turn and continue it with an injected
    // instruction. Agents that do not implement it are treated as not supporting it.
    type InterruptResumeSupporter interface {
        SupportsInterruptResume() bool
    }

    // TurnInterrupter is implemented by sessions that support native interrupt resume.
    // InterruptTurn stops the in-flight work inside the running agent, injects
    // instruction, and returns a new channel that streams the continued turn until it
    // completes. The implementation must close the previous event channel. An error
    // means the agent did not take over; Tern then falls back to its own replan.
    type TurnInterrupter interface {
        InterruptTurn(ctx context.Context, instruction string) (<-chan StreamEvent, error)
    }
    ```

---

### agentservice

#### [NEW] [shared/libs/go/agentservice/interrupt_native_internal_test.go](shared/libs/go/agentservice/interrupt_native_internal_test.go)

*   **Description**: 経路選択と、Native 用の締め処理の単体テスト（package `agentservice`）。
*   **Technical Design**:
    ```go
    func TestSelectInterruptMode(t *testing.T) {
        tests := []struct {
            name      string
            agent     codingagent.CodingAgent // stubs: plain / declares false / declares true
            sess      codingagent.Session     // with or without InterruptTurn; nil allowed
            wantMode  string
            wantHook  bool
        }{
            {"agent does not declare", plainAgent, hookSession, InterruptModeFallback, false},
            {"agent declares false", falseAgent, hookSession, InterruptModeFallback, false},
            {"session lacks hook", trueAgent, plainSession, InterruptModeFallback, false},
            {"no agent session", trueAgent, nil, InterruptModeFallback, false},
            {"capable", trueAgent, hookSession, InterruptModeNative, true},
        }
        // mode, ti := s.selectInterruptMode(tt.agent, &activeExecution{agentSess: tt.sess})
    }

    func TestFinishTurnRelay_AppendsTerminalOnce(t *testing.T)
    // relay with tool_use; finishTurnRelay(exec, &terminal) twice -> exactly one error "turn interrupted"
    // with TurnID set; relay source done.

    func TestDetachInterruptedTurn_KeepsProcessOwnership(t *testing.T)
    // record with SessionDir; exec registered with RegisterExecCancel(ctx) and RegisterActiveSession(probe);
    // relay has text "partial". detachInterruptedTurn(ctx, exec):
    // exec unregistered; ctx NOT cancelled; probe NOT closed; then CancelExecution("s1") == true
    // (cancel func kept for the continued turn); canonical history has assistant "partial".
    ```

#### [NEW] [shared/libs/go/agentservice/handler_interrupt_native_test.go](shared/libs/go/agentservice/handler_interrupt_native_test.go)

*   **Description**: Native 経路の HTTP テスト（package `agentservice_test`、Part 1 の HTTP ヘルパを再利用）。
*   **Technical Design**:
    ```go
    // nativeStubAgent declares native interrupt resume when capable is true.
    // Turn 1 hangs after tool_use until its context is cancelled or InterruptTurn takes over.
    // InterruptTurn closes the previous channel and returns a continued stream with text
    // "native-continued" and result (or blocks until ctx is cancelled when continueHangs).
    // Turns started by Fallback emit text "replanned" and result.
    type nativeStubAgent struct {
        name          string // "codex"
        capable       bool
        withoutHook   bool  // sessions do not implement TurnInterrupter
        hookErr       error // InterruptTurn returns this error
        continueHangs bool

        mu                sync.Mutex
        instructions      []string
        prompts           []string
        continuedCtx      context.Context // ctx passed to InterruptTurn
        closesAtInterrupt int32           // agent-session closes observed when InterruptTurn ran
        creates           atomic.Int32
        closes            atomic.Int32
        interruptCalls    atomic.Int32
    }

    func (a *nativeStubAgent) SupportsInterruptResume() bool { return a.capable }

    type nativeStubSession struct {
        agent     *nativeStubAgent
        turn      int
        mu        sync.Mutex // guards sends to cur and closing it
        cur       chan codingagent.StreamEvent
        takeover  chan struct{}
        closeOnce sync.Once
        closed    atomic.Bool
    }
    func (s *nativeStubSession) InterruptTurn(ctx context.Context, instruction string) (<-chan codingagent.StreamEvent, error)

    // plainNativeStubSession is the same session without InterruptTurn (withoutHook).
    type plainNativeStubSession struct{ /* wraps the turn behavior, no InterruptTurn */ }

    func TestInterrupt_NativeContinuesInSameAgentSession(t *testing.T)
    // capable. messages in flight (wait tool_use) -> POST /interrupt {"content":"switch to b.txt"} (SSE).
    // new stream: system "turn interrupt" with interrupt_mode "native" and interrupted_turn_id old;
    //   text "native-continued"; result; [DONE].
    // interruptCalls == 1; instructions[0] == "switch to b.txt"; creates == 1 (no new process);
    // closesAtInterrupt == 0 (not closed before the takeover); closes == 1 after the turn.
    // old stream: exactly one error "turn interrupted", then [DONE].
    // status "completed"; canonical record "[Tern interrupt] interrupted_turn_id=<old> mode=native\n..."
    // followed by assistant "native-continued".

    func TestInterrupt_NativeFailureFallsBackInSameRequest(t *testing.T)
    // capable, hookErr. interrupt_mode "fallback"; interruptCalls == 1; creates == 2;
    // prompts[1] starts with agentservice.InterruptPromptHeader; old stream has exactly one
    // "turn interrupted"; canonical record mode=fallback; status "completed".

    func TestInterrupt_CapabilityFalseNeverCallsNativeHook(t *testing.T)
    // capable=false, sessions implement InterruptTurn: interruptCalls == 0; mode fallback; creates == 2.

    func TestInterrupt_SessionWithoutHookUsesFallback(t *testing.T)
    // capable=true, withoutHook: mode fallback; creates == 2.

    func TestInterrupt_NativeTurnCancelStopsTransferredContext(t *testing.T)
    // capable, continueHangs. interrupt SSE (goroutine) until "native-continued"; the new turn is
    // followable with a turn_id different from the old one. POST /cancel -> 200;
    // continuedCtx is Done within 1s; closes == 1; interrupt stream ends; status "error", error "turn cancelled".

    func TestHandleListAgents_ReportsInterruptResume(t *testing.T)
    // register mockCodingAgent{name:"claudecode"} and nativeStubAgent{name:"codex", capable:true}.
    // GET /api/v1/agents: codex -> true; claudecode -> key present and false.
    ```
*   **Logic**（スタブの並行安全）: turn 1 の goroutine は `mu` 下で 3 イベントを送り（バッファ 8 なので塞がらない）、`select { case <-ctx.Done(): closeCur(); case <-takeover: }` で待つ。`InterruptTurn` は `mu` 下で `close(takeover)` と `closeCur()` を行う（`closeCur` は `closeOnce` で 1 回だけ閉じる）。送信と close が同時に起きない。

#### [MODIFY] [shared/libs/go/agentservice/exec_registry.go](shared/libs/go/agentservice/exec_registry.go)

*   **Description**: 実行コンテキストの持ち主を移せるよう、`activeExecution` に実行コンテキストを持たせる。終端イベントを一度だけ足すための印も持たせる。
*   **Technical Design**:
    ```go
    type activeExecution struct {
        // ... existing fields ...
        execCtx          context.Context    // agent process lifetime; transferred on native interrupt
        execCancel       context.CancelFunc
        terminalAppended bool               // guarded by stopMu
    }
    ```

#### [MODIFY] [shared/libs/go/agentservice/handler_retry.go](shared/libs/go/agentservice/handler_retry.go)

*   **Logic**: `runTurn` で最初に `active` を作るとき `execCtx: execCtx, execCancel: execCancel` を入れる。それ以外は変えない。

#### [MODIFY] [shared/libs/go/agentservice/turn_stop.go](shared/libs/go/agentservice/turn_stop.go)

*   **Description**: Part 1 の `stopInFlightTurn` の手順 5〜6 を `finishTurnRelay` に切り出し、Native 用の `detachInterruptedTurn` を足す。
*   **Technical Design**:
    ```go
    // finishTurnRelay ends exec's relay for its subscribers: it appends terminal at most
    // once (with turn/correlation ids and the turn's usage finalized) and marks the source done.
    func (s *Server) finishTurnRelay(exec *activeExecution, terminal *codingagent.StreamEvent)

    // detachInterruptedTurn finalizes a turn whose agent process continues under a new
    // turn (native interrupt): it reconciles and ingests the old turn and unregisters it,
    // but keeps the process, its cancel func and its active-session entry for the successor.
    // The caller must hold the session turn gate and have marked exec stopped.
    func (s *Server) detachInterruptedTurn(ctx context.Context, exec *activeExecution) []codingagent.StreamEvent
    ```
*   **Logic**:
    *   `finishTurnRelay`: `terminal != nil && exec.relay != nil` のとき、`stopMu` 下で `first := !exec.terminalAppended; exec.terminalAppended = true`。`first` なら写しに `TurnID` / `CorrelationID` を入れて `appendEvent` と `applyUsageSideEffects`。最後に `exec.relay.markSourceDone()`（nil ならしない）。
    *   `stopInFlightTurn`: 手順 5〜6 を `s.finishTurnRelay(exec, opts.terminal)` に置き換える（挙動は Part 1 と同じ。`terminal` が nil でも `markSourceDone` は行う）。
    *   `detachInterruptedTurn`: `exec.stopReattachTimer()` → `artifactStore != nil` なら `reconcileSessionArtifacts(ctx, exec.sessionID, exec.turnID, exec.correlationID)` → `events := exec.relay.snapshot()` → `s.ingestTurnEvents(exec.sessionID, events)` → `s.execRegistry.UnregisterIf(exec.sessionID, exec)` → `return events`。`CancelExecution`、`Close`、`UnregisterActiveSession`、`UnregisterExecCancel` は呼ばない。

#### [MODIFY] [shared/libs/go/agentservice/interrupt.go](shared/libs/go/agentservice/interrupt.go)

*   **Description**: 経路選択の本実装と Native 経路。
*   **Technical Design**:
    ```go
    const logNativeInterruptFailed = "native interrupt failed; falling back"

    // selectInterruptMode returns InterruptModeNative and the session hook only when the
    // agent declares native interrupt resume and the running session implements it.
    func (s *Server) selectInterruptMode(agent codingagent.CodingAgent, exec *activeExecution) (string, codingagent.TurnInterrupter)

    // serveNativeInterrupt hands the interrupt to the running agent session and streams the
    // continued turn as a new turn. It returns false, without writing to w, when the agent
    // rejects the interrupt; the caller then falls back.
    func (s *Server) serveNativeInterrupt(w http.ResponseWriter, r *http.Request, stopCtx context.Context,
        record *codingagent.SessionRecord, old *activeExecution, ti codingagent.TurnInterrupter,
        req InterruptRequest, newTurnID string, release func()) bool
    ```
*   **Logic**:
    *   `selectInterruptMode`: `sup, ok := agent.(codingagent.InterruptResumeSupporter)` が無いか `!sup.SupportsInterruptResume()` なら `(fallback, nil)`。`exec.agentSess` が nil か `TurnInterrupter` を満たさなければ `(fallback, nil)`。それ以外は `(native, ti)`。
    *   `handleInterrupt`（Part 1 の手順 9〜10 を次に置き換える。他の手順は Part 1 のまま）:
        ```go
        mode, ti := s.selectInterruptMode(agent, old)
        newTurnID := s.generateID()
        stopCtx := context.WithoutCancel(r.Context())
        terminal := codingagent.StreamEvent{Type: codingagent.EventError, Content: TurnInterruptedContent}
        if mode == InterruptModeNative {
            old.markStopped(stopReasonInterrupted)
            s.finishTurnRelay(old, &terminal) // old subscribers see "turn interrupted" exactly once
            if s.serveNativeInterrupt(w, r, stopCtx, record, old, ti, req, newTurnID, release) {
                return
            }
            mode = InterruptModeFallback
        }
        events := s.stopInFlightTurn(stopCtx, old, stopOptions{reason: stopReasonInterrupted, terminal: &terminal, ingest: true})
        // ... Part 1 steps 11-15 (finishTurnRelay skips the already-appended terminal)
        ```
    *   `serveNativeInterrupt`:
        1. `ctx := old.execCtx`（nil なら `context.Background()`）。`ch, err := ti.InterruptTurn(ctx, req.Content)`。
        2. `err != nil` なら Warn ログ `logNativeInterruptFailed`（`session_id`、`agent`、`interrupted_turn_id`、`error`）を出し、`false` を返す（`w` には何も書かない）。
        3. `s.detachInterruptedTurn(stopCtx, old)`。
        4. record を読み直す。`formatInterruptHistory(old.turnID, InterruptModeNative, req.Content)` を Part 1 と同じ形で `AppendSessionMessage`。Info ログ `logTurnInterrupted`（`mode=native`）。
        5. `s.captureTurnSnapshot(sessionID, newTurnID, record.WorkDir)`。
        6. 新しい実行を作る:
           ```go
           preamble := codingagent.StreamEvent{Type: codingagent.EventSystem, Content: InterruptEventContent,
               InterruptMode: InterruptModeNative, InterruptedTurnID: old.turnID}
           exec := &activeExecution{
               sessionID: sessionID, turnID: newTurnID, sessionModel: record.Model, correlationID: req.CorrelationID,
               agentSess: old.agentSess, stdin: old.stdin,
               relay:  newEventRelayWithHeartbeat(prependEvents(ch, preamble), s.resolvedToolHeartbeatInterval()),
               status: codingagent.StatusActive, usageAgg: newTurnUsageAggregator(newTurnID),
               execCtx: old.execCtx, execCancel: old.execCancel,
           }
           ```
        7. `s.execRegistry.Register(sessionID, exec)` が失敗したら（ゲート下では起きない想定）、`exec.execCancel` を呼び `exec.agentSess.Close()` して `writeSessionBusy` し `true` を返す。
        8. `exec.execCancel != nil` なら `s.RegisterExecCancel(sessionID, exec.execCancel)`、`s.RegisterActiveSession(sessionID, exec.agentSess)`、`s.markSessionActive(sessionID)`、`release()`。
        9. SSE（`Accept: text/event-stream`）なら `s.startSideEffectPump(exec)` → `term, suspended := s.streamSSERelay(r.Context(), w, exec, true)`。`suspended` なら `true` を返す。`!exec.isStopped() && term.retryable` なら `content, overloaded := s.classifiedTerminal(term.content); s.emitClassifiedSSE(w, exec, content, overloaded)`。`s.finishExecution(exec, sessionID, exec.agentSess, nil)`、`r.Context().Err() == nil` なら `s.writeSSEDone(w)`。
        10. JSON なら `term, events, suspended := s.respondJSONRelay(r.Context(), w, exec, true)`。`suspended` なら `true`。`!exec.isStopped() && term.retryable` なら分類済みエラーを `events` に足し、`updateSessionStatusOnTerminal` で状態を書く。`writeJSONEvents(w, events)`、`s.finishExecution(exec, sessionID, exec.agentSess, nil)`。
        11. `true` を返す。
    *   旧ターンの `runTurn`（元の `POST /messages`）は停止済みの短絡で `finishExecution` に進み、Part 1 の規則「停止済みの実行は agent session を閉じない」により、新ターンが使っているプロセスを閉じない。

#### [MODIFY] [shared/libs/go/agentservice/handler.go](shared/libs/go/agentservice/handler.go)

*   **Description**: `handleListAgents` に能力を出す。
*   **Technical Design**:
    ```go
    type agentInfo struct {
        Name                    string `json:"name"`
        SupportsInterruptResume bool   `json:"supports_interrupt_resume"`
    }
    ```
*   **Logic**: 各エージェントについて `codingagent.InterruptResumeSupporter` を満たせば `SupportsInterruptResume()` の値、満たさなければ `false` を入れる（`false` も省略せず出す）。

---

### client/v1

#### [NEW] [client/v1/agents_test.go](client/v1/agents_test.go)

*   **Technical Design**:
    ```go
    func TestListAgents_DecodesInterruptResume(t *testing.T)
    // httptest returns [{"name":"codex","supports_interrupt_resume":true},{"name":"claudecode","supports_interrupt_resume":false}]
    // -> two Agents with the matching SupportsInterruptResume values.
    ```

#### [MODIFY] [client/v1/agents.go](client/v1/agents.go)

*   **Technical Design**:
    ```go
    // Agent describes an available coding agent.
    type Agent struct {
        Name string `json:"name"`
        // SupportsInterruptResume reports native interrupt resume; Interrupt falls back
        // to Tern's replan when false.
        SupportsInterruptResume bool `json:"supports_interrupt_resume"`
    }
    ```

---

### 統合テスト（tests/）

#### [MODIFY] [tests/common_turn_interrupt_test.go](tests/common_turn_interrupt_test.go)

*   **Description**: client を通した Native スタブの流れと、実登録のエージェントがどれも Native を宣言しないことを確かめる。
*   **Technical Design**:
    ```go
    // nativeTurnInterruptAgent is the llm_test counterpart of the agentservice native stub
    // (declares true; InterruptTurn continues with text "native-continued" and result).
    type nativeTurnInterruptAgent struct { /* ... */ }

    func TestTurnInterrupt_ClientV1NativeStub(t *testing.T)
    // client.ListAgents -> codex SupportsInterruptResume true. SendText (wait EventToolUse) ->
    // sess.Interrupt -> EventSystem InterruptMode == v1.InterruptModeNative; EventResult;
    // client.GetSession -> completed.

    func TestTurnInterrupt_BuiltinAgentsDeclareNoNativeResume(t *testing.T)
    // real server.New via startHeartbeatCancelLiveServer (no LLM call). GET /api/v1/agents:
    // "wayfinder" is listed (always registered); every entry has supports_interrupt_resume present and false.
    ```

## Step-by-Step Implementation Guide

各手順の終わりに、その手順の変更だけを commit する（英語メッセージ）。

1.  **インタフェースのテスト（レッド）と定義**:
    *   `shared/libs/go/codingagent/interface_test.go` を書き、`./scripts/process/build.sh --skip-frontend` で未定義のレッドを確かめる。`interface.go` に 2 インタフェースを足して緑にする。
    *   Commit: `feat: declare native interrupt resume interfaces`
2.  **能力の公開（レッド → グリーン）**:
    *   `TestHandleListAgents_ReportsInterruptResume`（`handler_interrupt_native_test.go` にスタブと共に先行して作る）と `client/v1/agents_test.go` を書き、レッド（`supports_interrupt_resume` が無い）を確かめる。
    *   `handler.go` の `handleListAgents` と `client/v1/agents.go` を変更して緑にする。
    *   Commit: `feat: expose supports_interrupt_resume on GET /api/v1/agents`
3.  **Native 経路のテスト（レッド）**:
    *   `interrupt_native_internal_test.go` と、`handler_interrupt_native_test.go` の残りの試験を書く。レッド（未定義シンボル、または Native が呼ばれず `interrupt_mode=fallback`）を確かめる。内部テストの未定義シンボルで外部テストの振る舞いが観測できない場合は、Part 1 の手順 2 と同じく、外部テストのレッドを先に観測してから内部テストを足す。
4.  **Native 経路の実装（グリーン）**:
    *   `exec_registry.go`（`execCtx` / `execCancel` / `terminalAppended`）、`handler_retry.go`（`runTurn` で実行コンテキストを持たせる）、`turn_stop.go`（`finishTurnRelay`、`detachInterruptedTurn`）、`interrupt.go`（`selectInterruptMode`、`serveNativeInterrupt`、`handleInterrupt` の分岐）を実装する。
    *   `./scripts/process/build.sh --skip-frontend` で Part 1 と Part 2 の単体テストがすべて緑になることを確かめる。
    *   Commit: `feat: route interrupts to native resume when the agent supports it`
5.  **統合テスト**:
    *   `tests/common_turn_interrupt_test.go` に 2 試験を足し、`./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'` で緑を確かめる。
    *   Commit: `test: cover native interrupt stub and builtin capability`
6.  **ドキュメント**:
    *   Documentation 節の各ファイルを更新し、文書の検査を通す。
    *   Commit: `docs: document native interrupt resume capability`
7.  **最終検証**: Verification Plan をすべて実行し、総合判定を記録する。全テスト成功後、far-knowledge の記録と `pre-push-knowledge-check` を行ってから `git push` する。

## Verification Plan

### テスト項目の設計（testing-rules §8）

依存関係は「`handleInterrupt` → `selectInterruptMode` → `serveNativeInterrupt` → `finishTurnRelay` / `detachInterruptedTurn` / Part 1 の正本記録・`finishExecution`」である。

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 契約と部品 | インタフェース、経路選択、終端の一度きり、プロセス所有の維持 | `TestInterruptInterfacesAreImplementable`、`TestSelectInterruptMode`、`TestFinishTurnRelay_AppendsTerminalOnce`、`TestDetachInterruptedTurn_KeepsProcessOwnership` |
| L2 HTTP（Native スタブ） | Native 成功、Native 失敗の Fallback、能力 false、実行口なし、移譲後の cancel、能力の一覧 | `TestInterrupt_Native*`、`TestInterrupt_CapabilityFalseNeverCallsNativeHook`、`TestInterrupt_SessionWithoutHookUsesFallback`、`TestHandleListAgents_ReportsInterruptResume` |
| L3 client 単体 | `Agent.SupportsInterruptResume` の復号 | `TestListAgents_DecodesInterruptResume` |
| L4 統合 | client を通した Native、実登録エージェントの宣言 | `TestTurnInterrupt_ClientV1NativeStub`、`TestTurnInterrupt_BuiltinAgentsDeclareNoNativeResume` |
| L5 回帰（ライブ） | 既存アダプタの Fallback が変わっていないこと | `TestLiveTurnInterrupt_*`（Part 1） |

#### 観点チェックリスト（testing-rules §8.3）

| # | 観点 | 主なテスト |
| :--- | :--- | :--- |
| 1 | 正常系 | `TestInterrupt_NativeContinuesInSameAgentSession`、`TestTurnInterrupt_ClientV1NativeStub` |
| 2 | 異常系・境界値 | `TestInterrupt_NativeFailureFallsBackInSameRequest`、`TestSelectInterruptMode`（agent session が nil） |
| 3 | 外部連携の実動作 | 正本 history（Native の割り込みレコード）、実サーバの `GET /api/v1/agents` |
| 4 | データの一貫性 | 新旧 `turn_id`、`interrupt_mode`、旧ターンの終端が一度だけ、正本の順序 |
| 5 | 状態遷移 | Native: busy → 続きのターン → completed。移譲後の cancel: error / `turn cancelled` |
| 6 | 設定・構成の反映 | 能力の宣言の真偽と実行口の有無による経路の切り替え |
| 7 | 副作用 | プロセスを作り直さない（`creates == 1`）、続きのターンが終わるまで閉じない（`closesAtInterrupt == 0`）、移譲したコンテキストが cancel で止まる |

#### セルフレビュー（testing-rules §8.4）

1. **網羅性**: 経路選択の 5 通り（L1）と、Native の成功・失敗・不選択・移譲後の cancel（L2）がそろい、L4 で client と実登録を通すので、宣言・選択・実行・後片付けのすべてに証拠がある。
2. **証拠の十分性**: `interrupt_mode` の値、`InterruptTurn` の呼び出し回数と渡された指示、`CreateSession` の回数、Close の時点、正本の `mode=` を値で確かめる。
3. **迂回の排除**: Native が本当に使われたことは「`creates == 1` かつ `interruptCalls == 1` かつ `interrupt_mode=native`」の組で確かめる。Fallback に落ちたことは「`creates == 2` かつ再計画プロンプトの見出し」で確かめる。
4. **依存関係の整合**: Part 1 の Fallback と終了処理の規則が緑であることを前提にしている（Part 1 完了が着手条件）。

### 先に書く試験（レッド記録）

| 試験名 | 想定するレッドの理由 | 実際のレッド（実行時に記入） |
| :--- | :--- | :--- |
| `TestInterruptInterfacesAreImplementable` | インタフェースが未定義 | |
| `TestHandleListAgents_ReportsInterruptResume`、`TestListAgents_DecodesInterruptResume` | `supports_interrupt_resume` が出ない・復号されない | |
| `TestSelectInterruptMode`、`TestFinishTurnRelay_AppendsTerminalOnce`、`TestDetachInterruptedTurn_KeepsProcessOwnership` | 対象が未定義 | |
| `TestInterrupt_NativeContinuesInSameAgentSession`、`TestInterrupt_NativeTurnCancelStopsTransferredContext` | Native が選ばれず `interrupt_mode=fallback`、`creates == 2` | |
| `TestInterrupt_NativeFailureFallsBackInSameRequest` | `InterruptTurn` が呼ばれない（`interruptCalls == 0`） | |
| `TestInterrupt_CapabilityFalseNeverCallsNativeHook`、`TestInterrupt_SessionWithoutHookUsesFallback` | Part 1 の時点で緑になりうる（Native が無いため）。緑でも記録し、Part 2 実装後も緑のままであることを確かめる | |
| `TestTurnInterrupt_ClientV1NativeStub` | `interrupt_mode=fallback` | |
| `TestTurnInterrupt_BuiltinAgentsDeclareNoNativeResume` | フィールドが無い | |

### Automated Verification

1.  **Build & Unit Tests**:
    ```bash
    ./scripts/process/build.sh
    ```
2.  **Integration Tests**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
    ```
    *   **Log Verification**: Native スタブの試験で `turn interrupted` に `mode=native`、失敗の試験で `native interrupt failed; falling back` の Warn と `mode=fallback` が出ること。
3.  **回帰（Part 1 の範囲）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestSessionFollow_|TestInteractive_|TestSessionRecover_|TestReconcile_|TestFileChangeCollectors_|TestIntegration_|TestStreamReconnect_'
    ```
4.  **E2E（ライブ回帰、実 CLI と LLM 認証が必要）**: 既存アダプタは Native を宣言しないので、Part 1 のライブ試験がそのまま Fallback で通ることを確かめる。E2E の新規追加は不要（Native を実装した実アダプタが無いため。Native の動作は L2・L4 のスタブで確かめる）。
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestLiveTurnInterrupt_'
    ```
5.  **文書・例の検査**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestDocsIndexLayout|TestExamples_'
    ```

### 総合判定（testing-rules §9）

全テスト成功後、§9.2 の 7 項目を確かめ、§9.3 の書式で本計画書の末尾「総合判定結果」に記録する。特に次を確かめる。

*   **迂回処理による偽成功**: Native の試験が、実は Fallback の再計画ターンで `result` を得て通っていないか（`creates` と `interruptCalls` で確認済みであること）。
*   **アダプタ・コンフィグの誤適用**: 実登録のエージェントがどれも `false` で、ライブ試験の `interrupt_mode` がすべて `fallback` であること。

#### 総合判定結果

（実装後に記入）

## Documentation

#### [MODIFY] [docs/reference-manual-web-apis.md](docs/reference-manual-web-apis.md)
*   **更新内容**:
    *   §2 List Agents の応答例を `[{"name":"claudecode","supports_interrupt_resume":false}, ...]` にし、フィールドの意味（Native interrupt resume を宣言したエージェントだけ `true`。現行の組み込みエージェントはすべて `false`）を書く。
    *   §9.1 Interrupt に Modes を足す: `native`（エージェントが宣言し、実行中セッションが対応する場合。同じプロセスで続け、新しい `turn_id` で流す。リトライや self-heal はしない）、`fallback`（それ以外、または Native が失敗した場合。同じ要求の中で切り替わる）。旧ターンの終端 `turn interrupted` はどちらでも一度だけ出る。
#### [MODIFY] [docs/examples.md](docs/examples.md)
*   **更新内容**: 割り込みの例の後に「`client.ListAgents` の `SupportsInterruptResume` で Native 対応を確認できる。`false` でも `Interrupt` は Fallback で動く」を 1 文足す。
#### [MODIFY] [docs/architecture.md](docs/architecture.md)
*   **更新内容**: 割り込みの 1 文に「エージェントが Native interrupt resume を宣言していればそれを優先し、無ければ Tern の再計画（fallback）に落ちる」を足す。
#### [MODIFY] [docs/roadmap.md](docs/roadmap.md)
*   **更新内容**: Phase 2 に `* [ ] Native interrupt resume adapters (Codex / Claude Code / Wayfinder)` を足す。

## 継続計画について

本計画は Part 2 / 2 で、Part 1（[000-Turn-Interrupt-Fallback-Part1.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/plans/000-Turn-Interrupt-Fallback-Part1.md)）の完了を着手条件とする。アダプタごとの Native 本配線（Codex の App Server 経由の割り込み、Claude Code のストリーミング入力など）は、本 Part の `TurnInterrupter` 契約を満たす別仕様として起票する（backlog:314b541f-199c-496e-a201-c2a5a0e9af38）。Voice Mediator（方式 B）は、本機能のマージ後に別仕様として起票する。
