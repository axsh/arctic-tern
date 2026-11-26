# 003-Voice-Demo-Natural-Barge-In

> **Source Specification**: [002-Voice-Demo-Natural-Barge-In.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/002-Voice-Demo-Natural-Barge-In.md)

## Goal Description

`features/voice_demo` に (1) abrupt / natural の barge-in 比較モード、(2) OpenAI Realtime と Tern CAWA の並列表記、(3) 既に手元にある Realtime `client_secrets` 修正の取り込みを行い、Example として「自然さを考慮できること」と「どちらが動いているか」が分かるようにする。CAWA の busy→`interrupt` は即時のまま維持する。

## User Review Required

None.

（連続実行承認済み。仕様の仮決定を次のとおり実装に落とす。）

1. **自然さの主手段**: natural は Realtime session の `turn_detection.interrupt_response=false`（soft_vad）＋ブラウザ側の短い再生フェード（fade, 既定 180ms）。ラベルは `soft_vad+fade`。Ack 発話は初版では入れない（S2 見送り）。
2. **abrupt**: `interrupt_response=true`（または省略＝即切断寄り）＋フェード無し。ラベル `abrupt_cut`。
3. **CAWA**: モードに関係なく busy 確定発話は即 `interrupt`。
4. **UI**: 会話層パネルと実行層パネルを横並び（狭幅では縦積み）。`data-testid="status-realtime"` / `status-cawa"`。
5. **Realtime API**: `POST /v1/realtime/client_secrets` とブラウザ `POST /v1/realtime/calls`（既存未コミット修正を本計画で確定）。

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| M1 二層明示 | README + コードコメント + UI 二パネル |
| M1b 層の可視化 | `index.html` / `app.js` / `style.css` |
| M2 natural | `bargein_policy.go` + `realtime.go` session 注入 + `app.js` fade |
| M3 abrupt | 同上、モード切替 UI |
| M4 CAWA 観測性 | 既存 utterance 経路維持 + CAWA パネル |
| M5 Example スタイル | コメント多め、ファイル少数 |
| M6 自動化 | `bargein_policy_test.go`、HTML testid 試験、既存 utterance 回帰 |

## Proposed Changes

### features/voice_demo

#### [NEW] [features/voice_demo/bargein_policy_test.go](features/voice_demo/bargein_policy_test.go)

*   **Description**: モード → パラメータの表を固定する。
*   **Cases**:
    *   `natural` → `InterruptResponse=false`, `FadeMs=180`, `StopLabel=soft_vad+fade`
    *   `abrupt` → `InterruptResponse=true`, `FadeMs=0`, `StopLabel=abrupt_cut`
    *   未知モード → natural にフォールバックまたは error（実装はフォールバック＋ログ）

#### [NEW] [features/voice_demo/bargein_policy.go](features/voice_demo/bargein_policy.go)

```go
type BargeInMode string
const (
    BargeInNatural BargeInMode = "natural"
    BargeInAbrupt  BargeInMode = "abrupt"
)

type BargeInParams struct {
    Mode              BargeInMode
    InterruptResponse bool
    FadeMs            int
    StopLabel         string
}

func ResolveBargeIn(mode string) BargeInParams
```

*   **Logic**: trim+lower。空または natural → natural パラメータ。abrupt → abrupt。その他 → natural。

#### [NEW] [features/voice_demo/static_assets_test.go](features/voice_demo/static_assets_test.go)

*   **Description**: `static/index.html` に `data-testid="status-realtime"`、`status-cawa`、`bargein-mode`、初期テキスト（Realtime `disconnected`、CAWA `no session`）が含まれることを読む。

#### [MODIFY] [features/voice_demo/realtime.go](features/voice_demo/realtime.go)

*   **Description**: `createRealtimeEphemeralSession` に `BargeInParams` を渡し、session.audio.input.turn_detection に `type=server_vad`, `interrupt_response=<params>` を載せる。URL は `client_secrets`（未コミット修正を確定）。
*   **Signature change**:
    ```go
    func createRealtimeEphemeralSession(ctx context.Context, apiKey, model, voice string, barge BargeInParams) ([]byte, error)
    ```

#### [MODIFY] [features/voice_demo/realtime_test.go](features/voice_demo/realtime_test.go)

*   **Description**: client_secrets パスと、リクエスト JSON に `interrupt_response` がモードどおり入ることを断言。

#### [MODIFY] [features/voice_demo/server.go](features/voice_demo/server.go)

*   **Description**: `POST /api/realtime/session` が任意 JSON `{ "barge_in_mode": "natural"|"abrupt" }` を受け、`ResolveBargeIn` して ephemeral 作成。応答に `barge_in` オブジェクト（mode, stop_label, fade_ms）をマージして返す（OpenAI 生 JSON を包むか、フィールド追加。実装は `{ "openai": <raw>, "barge_in": {...} }` とし、ブラウザは両対応）。

#### [MODIFY] [features/voice_demo/static/index.html](features/voice_demo/static/index.html)

*   **Description**:
    *   barge-in モード: `<select data-testid="bargein-mode">` natural/abrupt、表示 `data-testid="bargein-stop-label"`
    *   二パネル:
        *   OpenAI Realtime: `data-testid="status-realtime"`（初期 `disconnected`）、説明「Voice conversation」
        *   Tern CAWA: `data-testid="status-cawa"`（初期 `no session`）、`last_action` / `interrupt_mode` / `summary` をこのパネル内へ移す

#### [MODIFY] [features/voice_demo/static/app.js](features/voice_demo/static/app.js)

*   **Description**:
    *   `setRealtimeStatus` / `setCawaStatus` でパネル更新
    *   Start: CAWA start → realtime connect（失敗なら realtime=`unavailable` でも CAWA 続行）
    *   connectRealtime: mode を POST、`/v1/realtime/calls` で SDP、connected に更新
    *   natural: remote `<audio>` に fadeOut(FadeMs) を user speech start（`input_audio_buffer.speech_started`）で適用。abrupt は即 `audio.pause()`+volume=0
    *   `response.output_audio.delta` または track 再生で speaking 表示（取れる範囲で）
    *   utterance 後に CAWA パネル更新

#### [MODIFY] [features/voice_demo/static/style.css](features/voice_demo/static/style.css)

*   **Description**: `.layers` 二列グリッド、狭幅で1列。

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)

*   **Description**: パネルの見方、abrupt vs natural 聞き比べ、client_secrets 注意、再ビルド手順。

#### [MODIFY] [features/voice_demo/utterance_handler_test.go](features/voice_demo/utterance_handler_test.go)

*   **Description**: 既存 PASS を維持。Realtime session エンドポイントが mode 付きで 503（鍵無し）を返すことだけ追加してもよい。

## Step-by-Step Implementation Guide

1. **[x]** RED: `bargein_policy_test.go` → implement `bargein_policy.go`
2. **[x]** RED: `static_assets_test.go`（失敗）→ update `index.html` で green
3. **[x]** Update `realtime.go` / tests for client_secrets + turn_detection from params
4. **[x]** Update `server.go` to accept barge_in_mode and wrap response
5. **[x]** Update `app.js` / `style.css` for panels, mode, fade, status
6. **[x]** Update README
7. **[x]** Run Verification Plan; write 総合判定; commit; far-knowledge; push

## Verification Plan

### テスト項目の設計（§8）

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 | ResolveBargeIn | `TestResolveBargeIn` |
| L1 | HTML fixtures | `TestStaticIndexHasLayerTestIDs` |
| L2 | realtime payload | `TestCreateRealtimeEphemeralSession_*` |
| L3 | utterance 回帰 | `TestUtteranceHandler_IdleMessagesBusyInterrupt` |
| L4 | interrupt 回帰 | `TestTurnInterrupt_` |

#### セルフレビュー（§8.4）

網羅: モードパラメータ・testid・CAWA 経路。聞こえは手動。迂回: abrupt/natural で InterruptResponse の真偽を値で区別。

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 実際のレッド（実行時記入） |
| :--- | :--- | :--- |
| `TestResolveBargeIn` | 未定義 | 実装前にポリシー未定義のため失敗想定。実装後 PASS（`go test ./features/voice_demo`） |
| `TestStaticIndexHasLayerTestIDs` | testid 無し | HTML 更新前は testid 欠落で失敗想定。更新後 PASS |

### Automated Verification

```bash
./scripts/process/build.sh
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

### 総合判定結果

**合格。** `./scripts/process/build.sh` PASS。`features/voice_demo` 単体（ResolveBargeIn / static testids / realtime interrupt_response / utterance 回帰）PASS。`integration_test.sh --specify 'TestTurnInterrupt_'` PASS（Windows で FollowSeesReplanTurn の TempDir 掃除失敗があったため、httptest Close を TempDir 削除より先に走らせるよう修正して再 PASS）。聞こえ（natural vs abrupt）は手動聞き比べ前提のまま。

## Documentation

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)
*   パネル見方、モード聞き比べ、Realtime API パス。

#### [MODIFY] [docs/examples.md](docs/examples.md)
*   voice_demo 一文に「layer status panels + abrupt/natural barge-in」を足す。
