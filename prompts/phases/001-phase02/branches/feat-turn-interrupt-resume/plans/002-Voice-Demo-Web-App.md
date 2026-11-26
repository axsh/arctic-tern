# 002-Voice-Demo-Web-App

> **Source Specification**: [001-Voice-Demo-Web-App.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/001-Voice-Demo-Web-App.md)

## Goal Description

`features/voice_demo` に、ブラウザから Coding Agent と低遅延双方向音声でやり取りできる **デモ専用 Web アプリ**を追加する。会話レイヤは OpenAI Realtime（仮固定）、実行権威は既存 CAWA（`client/v1` の `messages` / `Follow` / `Interrupt`）に委譲する。busy 中の追発話は必ず `Interrupt` とし、`cancel`→`messages` を既定 barge-in にしない。実装は綺麗な層分割より、コメント多めの平易な Example とする。

## User Review Required

None.

（連続実行承認済み。以下は仕様の仮決定を実装に落とす確定事項である。）

1. **Realtime ベンダー**: OpenAI Realtime API をデモ初版で固定（backlog:3a7b6be4-2d71-41c4-bbc6-346235b5b2d4）。ベンダー抽象層は作らない。
2. **デモ境界**: 本番 Voice Mediator / `tern-voice` には昇格しない（backlog:9e1a6c40-92ce-40bc-980a-f36e8b7d7b26）。
3. **barge-in ポリシー**: idle → `SendText`（messages）、busy → `Interrupt`。cancel は UI 任意、barge-in 既定にはしない（backlog:1281294e-21e2-4637-8e4a-7a8239939b72）。
4. **音声 I/O の置き方**: ブラウザが OpenAI Realtime（WebRTC）でマイク／スピーカーを扱い、確定した書き起こしテキストをデモサーバへ渡す。デモサーバは CAWA だけを操作する（低遅延の会話往復をコーディング完了に依存させない）。
5. **埋め込み Tern（S1）**: 初版は外部 Tern URL 接続のみ。埋め込み起動は作らない。
6. **通話終了**: CAWA session は `Terminate` で単純化する。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 配置・Web・CAWA client | `features/voice_demo/` 新規モジュール、`main.go` 静的配信、`cawa.go` が `client/v1` を使用 |
| M2 双方向音声の感触・画面表示 | `static/` + `POST /api/realtime/session`（ephemeral）+ UI 状態（transcript / busy / summary） |
| M3 会話インタラプト | `policy.go` の分岐、`hub.go` の busy 追跡、`POST /api/utterance` |
| M4 Example スタイル | ファイル少数・コメント多め。DI / ベンダーアダプタ無し。README にデモ専用と明記 |
| M5 秘密情報 | `OPENAI_API_KEY` / `TERN_URL` 等は環境変数。キー欠落時は分かりやすい 4xx/起動エラー |
| M6 自動回帰 | `policy_test.go`、`summarize_test.go`、`utterance_handler_test.go`（mock CAWA httptest）。実マイクは手動 |
| S1 埋め込み Tern | 本計画では実装しない（User Review 5） |
| S2 言語切替 | 初版は英語 UI 固定（Realtime instructions で日本語も受けてよい旨をコメント） |
| S3 長い作業シナリオ | README と UI にサンプル文言を置く |

## Proposed Changes

### features/voice_demo（新規）

TDD: 各 `_test.go` を対応実装より先に書く。`build.sh` が `features/*/go.mod` を列挙して単体テストと `bin/voice_demo` をビルドする。

#### [NEW] [features/voice_demo/policy_test.go](features/voice_demo/policy_test.go)

*   **Description**: idle / busy / 空文字の経路をテーブル駆動で固定する（仕様 M3 / M6）。
*   **Technical Design**:
    ```go
    func TestDecideUtteranceAction(t *testing.T) {
        // cases: idle+"hello" → ActionMessages
        //        busy+"change plan" → ActionInterrupt
        //        idle/busy + "" → ActionNone + err or ActionNone
        //        busy path must NOT be ActionCancel
    }
    ```
*   **Logic**: `DecideUtteranceAction(busy bool, text string) (Action, error)` の戻り値だけを断言する。HTTP や Realtime は触らない。

#### [NEW] [features/voice_demo/policy.go](features/voice_demo/policy.go)

*   **Description**: 発話確定時の CAWA 操作を決める平易な関数（コメントで「なぜ busy だけ interrupt か」を説明する）。
*   **Technical Design**:
    ```go
    type Action string
    const (
        ActionNone      Action = "none"
        ActionMessages  Action = "messages"
        ActionInterrupt Action = "interrupt"
    )

    // DecideUtteranceAction chooses CAWA API for a finalized user utterance.
    // idle  → messages (new turn)
    // busy  → interrupt (do NOT cancel+messages)
    func DecideUtteranceAction(busy bool, text string) (Action, error)
    ```
*   **Logic**:
    1. `strings.TrimSpace(text)` が空なら `ActionNone` とエラー（または None）。
    2. `busy == false` なら `ActionMessages`。
    3. `busy == true` なら `ActionInterrupt`。
    4. `ActionCancel` 定数は定義しない（誤用防止）。

#### [NEW] [features/voice_demo/summarize_test.go](features/voice_demo/summarize_test.go)

*   **Description**: SSE 風イベントから短い進捗文だけを作る（全文ダンプ禁止、仕様 M2 / シナリオ 4）。
*   **Technical Design**:
    ```go
    func TestSummarizeEvent(t *testing.T) {
        // text "hello world" → "Agent: hello world" truncated to MaxSummaryRunes
        // tool_use name=Bash → "Running tool: Bash"
        // result → "Turn finished"
        // interrupt system with interrupt_mode → "Interrupted (fallback|native)"
        // huge text → truncated, no full dump
    }
    ```

#### [NEW] [features/voice_demo/summarize.go](features/voice_demo/summarize.go)

*   **Description**: 進捗アダプタ。Realtime TTS / UI 用の短い一文だけ返す。
*   **Technical Design**:
    ```go
    const MaxSummaryRunes = 120

    // SummarizeEvent maps one CAWA SSE event type+content into a short status line.
    func SummarizeEvent(eventType, content, toolName, interruptMode string) string
    ```
*   **Logic**:
    1. `type=="text"`: `"Agent: "` + rune 制限した content。
    2. `type=="tool_use"`: `"Running tool: "` + toolName（空なら `"tool"`）。
    3. `type=="result"`: `"Turn finished"`。
    4. `type=="system"` かつ content が interrupt 系 / interruptMode 非空: `"Interrupted (" + mode + ")"`。
    5. それ以外は空文字（読み上げない）。

#### [NEW] [features/voice_demo/utterance_handler_test.go](features/voice_demo/utterance_handler_test.go)

*   **Description**: デモ HTTP の発話エンドポイントが、mock CAWA に対して messages / interrupt を正しく呼ぶことを確かめる（M3 / M6）。barge-in で cancel が呼ばれないことも確かめる。
*   **Technical Design**:
    *   `httptest` で偽 CAWA を立てる: `POST /api/v1/sessions` → session_id、`POST .../messages` と `.../interrupt` は SSE（最低限 `data: {"type":"text","content":"ok"}\n\n` + `data: [DONE]\n\n`）、`POST .../cancel` が呼ばれたらテスト失敗用フラグを立てる、`GET .../sessions/:id` は status。
    *   デモの `Server` / mux を同じプロセスで立て、`POST /api/call/start` → `POST /api/utterance` を二回（idle 相当の初回、busy 中の二回目）撃つ。
    *   断言: 1 回目は messages カウント +1、2 回目は interrupt カウント +1、cancel カウント == 0。応答 JSON に `action` が入る。

#### [NEW] [features/voice_demo/cawa.go](features/voice_demo/cawa.go)

*   **Description**: `client/v1` の薄いラッパ。Example として「なぜこの API か」をコメントする。
*   **Technical Design**:
    ```go
    type cawaBridge struct {
        client    *v1.Client
        session   *v1.Session
        agent     string
        workDir   string
        sessionDir string
        mu        sync.Mutex
        busy      bool
        lastMode  string // interrupt_mode from last interrupt SSE, if any
    }

    func (b *cawaBridge) Start(ctx context.Context) error  // CreateSession
    func (b *cawaBridge) End(ctx context.Context) error    // Terminate
    func (b *cawaBridge) HandleUtterance(ctx context.Context, text string) (action string, summary string, err error)
    func (b *cawaBridge) Busy() bool
    ```
*   **Logic** (`HandleUtterance`):
    1. `DecideUtteranceAction(b.Busy(), text)`。
    2. `ActionMessages`: `SendText` → ストリームを読み `busy=true` → イベントごとに `SummarizeEvent` で last summary 更新 → 終了で `busy=false`。ストリーム読みは別 goroutine でも同一 goroutine でもよいが、**次の発話の前に busy が true になるよう**、最初のイベント受信または送信成功直後に busy を立てる。
    3. `ActionInterrupt`: `Interrupt(ctx, text)` → 同様にストリームを読む。SSE の `interrupt_mode` を `lastMode` に保存。**CancelTurn は呼ばない**。
    4. 戻り値に action と最新 summary（または interrupt ラベル）を返す。

#### [NEW] [features/voice_demo/realtime.go](features/voice_demo/realtime.go)

*   **Description**: OpenAI Realtime 用 ephemeral session をサーバ側で発行する（API キーをブラウザに渡さない）。
*   **Technical Design**:
    ```go
    // createRealtimeEphemeralSession POSTs to OpenAI Realtime Sessions API
    // using OPENAI_API_KEY and returns JSON the browser needs for WebRTC
    // (client_secret / expires_at / id). On missing key, return a clear error.
    func createRealtimeEphemeralSession(ctx context.Context, apiKey, model, voice string) ([]byte, error)
    ```
*   **Logic**:
    1. `apiKey == ""` ならエラー `"OPENAI_API_KEY is not set"`。
    2. `POST https://api.openai.com/v1/realtime/sessions`（モデル既定例: `gpt-4o-realtime-preview`、voice 例: `verse`）。Body にデモ用 instructions（「あなたは声の会話役。コーディング実行は裏の Tern が行う。長いコードを読み上げない」）。
    3. 応答 JSON をそのまま（または必要最小フィールドだけ）返す。

#### [NEW] [features/voice_demo/realtime_test.go](features/voice_demo/realtime_test.go)

*   **Description**: キー欠落時のエラーメッセージを単体で固定（仕様シナリオ 5）。実 OpenAI 呼び出しはしない（`httptest` で成功パスを任意で追加可）。

#### [NEW] [features/voice_demo/main.go](features/voice_demo/main.go)

*   **Description**: フラグ・環境変数・HTTP ルーティング・静的ファイル。処理は上から読める順序で並べ、コメント多め。
*   **Technical Design**:
    *   Flags / env:
        *   `-listen` 既定 `:8787`
        *   `-tern-url` / `TERN_URL`（必須）
        *   `-agent` / `TERN_AGENT` 既定 `wayfinder`（または `codex`。README で前提を書く）
        *   `-work-dir` / `TERN_WORK_DIR`（一時でも可）
        *   `OPENAI_API_KEY`（Realtime token 発行に必須。欠落時は token エンドポイントだけ 503）
    *   Routes:
        *   `GET /` → `static/index.html`
        *   `GET /static/` → 埋め込みまたは `embed.FS`
        *   `POST /api/call/start` → CAWA CreateSession、JSON `{ok, session_id}`
        *   `POST /api/call/end` → Terminate
        *   `POST /api/realtime/session` → ephemeral JSON（キー無しは明確な JSON エラー）
        *   `POST /api/utterance` body `{"text":"..."}` → HandleUtterance
        *   `GET /api/status` → `{busy, last_summary, last_action, interrupt_mode}`
    *   単一プロセス・単一通話を想定（マルチユーザー不要）。グローバルな `cawaBridge` でよい（Example）。

#### [NEW] [features/voice_demo/static/index.html](features/voice_demo/static/index.html)

*   **Description**: 開始／終了、状態表示、書き起こし、短い進捗、サンプル長い依頼ボタン。`data-testid` を主要要素に付ける（後続の自動操作や手動確認用）。
*   **要素**:
    *   `data-testid="btn-start"` / `btn-end` / `btn-sample-long` / `status-busy` / `status-summary` / `transcript` / `last-action`

#### [NEW] [features/voice_demo/static/app.js](features/voice_demo/static/app.js)

*   **Description**: コメント多めの平易な JS。
*   **Logic**:
    1. Start: `POST /api/call/start` → `POST /api/realtime/session` → WebRTC で OpenAI Realtime に接続（公式の ephemeral + WebRTC パターン。詳細手順をコメントで書く）。
    2. Realtime の入力文字起こし完了イベント（例: `conversation.item.input_audio_transcription.completed`）で transcript を UI に出し、`POST /api/utterance`。
    3. 定期または応答後に `GET /api/status` で busy / summary / last_action / interrupt_mode を表示。
    4. Realtime 側には短い status を conversation item として送ってよい（任意）。**CAWA SSE 全文は送らない**。
    5. Sample long task ボタンは固定英文／日文を utterance API に直接 POST（マイク無しでもインタラプト経路の一部を試せる）。

#### [NEW] [features/voice_demo/static/style.css](features/voice_demo/static/style.css)

*   **Description**: 最小限の読みやすいスタイル。ダッシュボード化しない。

#### [NEW] [features/voice_demo/go.mod](features/voice_demo/go.mod)

*   **Description**: `module github.com/axsh/arctic-tern/features/voice_demo`、`replace github.com/axsh/arctic-tern => ../../`、`require` に root module（`client/v1` 利用）。`go 1.27.0` を他 feature に合わせる。

#### [NEW] [features/voice_demo/README.md](features/voice_demo/README.md)

*   **Description**: デモ専用であることの明記、環境変数、起動手順、手動シナリオ 1・3、サンプル長い依頼文、本番 Voice Mediator ではないこと。

### tests（統合・E2E 相当）

`features/voice_demo` は Example として `package main` のため、`tests/` から import しない。HTTP+mock CAWA の E2E 相当は feature 内の `TestUtteranceHandler_IdleMessagesBusyInterrupt` とし、`./scripts/process/build.sh` が feature 単体として実行する。

> 実マイク／実 OpenAI Realtime／実 Coding Agent のライブ通話は CI では必須にしない（鍵・マイク依存）。仕様 M6 およびシナリオ 1・3 の手動は README に残し、自動の主証拠はポリシーと HTTP 経路とする。フルブラウザ Playwright E2E は本デモでは計画しない。

## Step-by-Step Implementation Guide

1.  **[x] Scaffold module**:
    *   Create `features/voice_demo/go.mod` with replace to repo root.
    *   Create empty `main.go` that compiles (`package main` + `func main(){}`) so `build.sh` can discover the module after tests exist.
2.  **[x] RED policy tests then implement**:
    *   Add `policy_test.go` → run via `./scripts/process/build.sh`（feature 単体で失敗することを確認）→ implement `policy.go` → green.
3.  **[x] RED summarize tests then implement**:
    *   Add `summarize_test.go` → fail → `summarize.go` → green.
4.  **[x] RED realtime missing-key test then implement**:
    *   Add `realtime_test.go` → `realtime.go`（キー欠落＋任意で httptest 成功）.
5.  **[x] RED utterance HTTP test (feature-local) then implement bridge + handlers**:
    *   Add mock CAWA + `utterance_handler_test.go`.
    *   Implement `cawa.go` and HTTP wiring in `main.go`（または `server.go` にハンドラを分離してもよいが、ファイルは少数に保つ）。
    *   Ensure barge-in never calls cancel.
6.  **[x] Static UI**:
    *   Add `static/index.html`, `app.js`, `style.css` with comments and testids.
    *   Wire `embed.FS` or `http.Dir` for static files.
7.  **[x] README**:
    *   Write startup, env vars, manual scenarios 1 and 3, sample long prompt, demo disclaimer.
8.  **[x] Integration test in tests/**:
    *   `package main` のため `tests/` import は行わず、feature 内 HTTP 試験を E2E 相当とする（計画を更新済み）。
9.  **[x] Docs touch-up**:
    *   Update `docs/examples.md` / `docs/roadmap.md` per Documentation section.
10. **[x] Verification Plan を実行**し、総合判定を本計画末尾に記入する。

## Verification Plan

### テスト項目の設計（testing-rules §8）

依存: UI/Realtime → `POST /api/utterance` → `DecideUtteranceAction` → `cawaBridge` → CAWA。要約は並列の末端部品。

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 末端ロジック | policy / summarize / missing API key | `TestDecideUtteranceAction`、`TestSummarizeEvent`、`TestCreateRealtimeEphemeralSession_MissingKey` |
| L2 HTTP（mock CAWA） | utterance 経路、cancel 非呼び出し | `TestUtteranceHandler_IdleMessagesBusyInterrupt`（feature 内） |
| L3 統合 / E2E 相当 | feature 内 HTTP+mock CAWA（`build.sh` が実行） | `TestUtteranceHandler_IdleMessagesBusyInterrupt` |
| L4 回帰 | 既存 interrupt 契約 | `TestTurnInterrupt_` |
| L5 手動（自動対象外） | 実マイク往復・実 barge-in | README シナリオ 1・3 |

#### 観点チェックリスト（§8.3）

| # | 観点 | 主なテスト |
| :--- | :--- | :--- |
| 1 | 正常系 | idle→messages、busy→interrupt |
| 2 | 異常系 | 空発話、OPENAI_API_KEY 欠落 |
| 3 | 外部連携 | mock CAWA httptest への実 HTTP |
| 4 | データ一貫性 | 応答 JSON の action と mock カウンタ一致 |
| 5 | 状態遷移 | busy フラグがターン中 true、終了後 false |
| 6 | 設定 | TERN_URL / agent フラグ（README＋起動）。鍵は env |
| 7 | 副作用 | cancel が barge-in で呼ばれない。SSE 全文を要約しない |

#### セルフレビュー（§8.4）

1. **網羅性**: 仕様の自動対象（M3 分岐・要約・鍵欠落・cancel 禁止）は L1–L3 で言える。実音声は L5 手動であり、M2 の「感触」は自動では近似のみ（仕様が許容）。
2. **証拠の十分性**: 呼び出し回数と action 文字列を値で断言する。
3. **迂回排除**: interrupt 経路で cancel カウンタ 0 を必須にする。
4. **依存**: L1 緑の上に L2/L3 を置く。CAWA interrupt 自体の正しさは既存 `TestTurnInterrupt_` に依存。

### 先に書く試験（レッド記録）

| 試験名 | 想定するレッドの理由 | 実際のレッド（実行時に記入） |
| :--- | :--- | :--- |
| `TestDecideUtteranceAction` | `policy.go` 未作成 | `undefined: Action` / `DecideUtteranceAction`（`tmp/voice_demo_red1.txt`） |
| `TestSummarizeEvent` | `summarize.go` 未作成 | 同上ビルド失敗で `undefined: SummarizeEvent` |
| `TestCreateRealtimeEphemeralSession_MissingKey` | 関数未作成 | 実装前は未定義。実装後に欠落キーで緑を確認 |
| `TestUtteranceHandler_IdleMessagesBusyInterrupt` | ハンドラ未配線 | 実装中、HTTP リクエスト ctx キャンセルで busy が落ち `action=messages` となり一度レッド。`context.WithoutCancel` で緑化 |

### Automated Verification

1.  **Build & Unit Tests**（features/voice_demo の単体を含む）:
    ```bash
    ./scripts/process/build.sh
    ```
2.  **Integration（既存 interrupt 回帰）**:
    ```bash
    ./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
    ```
    *   **Log Verification**: TurnInterrupt が回帰していないこと。voice_demo の HTTP E2E 相当は step 1 の feature 単体（`TestUtteranceHandler_*`）で確認済みであること。
3.  **E2E**: `TestUtteranceHandler_IdleMessagesBusyInterrupt` を E2E 相当とする（実ブラウザ／実 Realtime は鍵・マイク依存のため自動必須にしない）。手動シナリオは README。

### 総合判定（testing-rules §9）

全自動テスト成功後、§9.2 の 7 項目を確かめ、下記「総合判定結果」を記入する。特に「busy→interrupt が実は cancel+messages でないか」「要約が全文ダンプになっていないか」を見る。

#### 総合判定結果

**判定**: ✅ 動作確認完了（自動経路）。実マイク／実 Realtime の感触は手動シナリオ依存のため ⚠️ を併記。

##### テスト結果サマリ
- Build & Unit（`./scripts/process/build.sh`）: PASS。`Feature: voice_demo` 単体（policy / summarize / realtime / utterance HTTP）と `bin/voice_demo` ビルドを含む。
- 統合（`--specify 'TestTurnInterrupt_'`）: 6 件 PASS。
- 実マイク／OpenAI Realtime ライブ通話: 本実行では未実施（鍵・マイク依存。README 手動シナリオ）。

##### チェック項目の結果
| # | チェック項目 | 結果 | 備考 |
|---|------------|------|------|
| 1 | スキップされたテスト | ✅ | `t.Skip` なし |
| 2 | 部分的なエラー | ✅ | utterance 試験ログは正常な stream done のみ |
| 3 | 迂回処理による偽成功 | ✅ | busy→interrupt で messages 再呼び出し無し、cancel==0 を断言 |
| 4 | アダプタ・コンフィグの誤適用 | ✅ | mock CAWA のパス別カウンタで経路を確認 |
| 5 | テスト間の依存・順序 | ✅ | feature 単体を単独実行しても同結果 |
| 6 | カバレッジの妥当性 | ✅ | M3/M5/M6 の自動対象をカバー。M2 実音声は手動 |
| 7 | 外部システムの状態 | ⚠️ | 実 Realtime / 実 Tern 通話は未実行 |

##### 判定理由
自動で検証すべき会話インタラプト分岐（idle→messages、busy→interrupt、cancel 禁止、要約切り詰め、API キー欠落）は値で緑化した。実双方向音声の「感触」は仕様どおり手動に残るため、製品 Voice Mediator 完了とは言わないが、本デモ Example の自動ゲートは満たす。

## Documentation

#### [NEW] [features/voice_demo/README.md](features/voice_demo/README.md)
*   **更新内容**: デモ専用声明、起動、env、手動シナリオ、サンプル長タスク。

#### [MODIFY] [docs/examples.md](docs/examples.md)
*   **更新内容**: Interrupt の節の近くに「音声デモ Example: `features/voice_demo`（Realtime 会話 + CAWA interrupt。本番 Voice Mediator ではない）」を短く追記。

#### [MODIFY] [docs/roadmap.md](docs/roadmap.md)
*   **更新内容**: Phase 2 の Multimodal Voice 行の近くに、デモ Example への言及を1行足す（チェックボックスはデモ完了でも「本番 Voice Mediator」未着手のまま `[ ] Multimodal Voice support` を維持し、別に `* [x] Voice demo example (features/voice_demo)` を追加してよい）。

## 継続計画について

本計画は単一 Part。本番 Voice Mediator（方式 B）は別 idea（backlog:9e1a6c40-92ce-40bc-980a-f36e8b7d7b26）。Native resume アダプタ本配線は本デモの範囲外。
