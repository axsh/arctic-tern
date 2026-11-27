# 009-Voice-Demo-Answer-Framing-After-Tern-Turn

> **Source Specification**: [008-Voice-Demo-Answer-Framing-After-Tern-Turn.md](../ideas/008-Voice-Demo-Answer-Framing-After-Tern-Turn.md)

## Goal Description

CAWA Turn 完了時に Voice が (C) へのメタ Ack を即発話するのをやめ、(A) を保持した完了ノートを注入して (B) 再検討ターンを走らせる。(D) は任意の (A) 向け最終発話とし、(C) の必須読上げはしない。進捗注入は無発話のまま。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point (Section/File) |
| :--- | :--- |
| M1 ロール定義 (A)(B)(C)(D) | README / instructions / 注入文コメント |
| M2 完了時は即読上げせず (B) 再検討 | `app.js` reconsider + `BuildTurnCompleteReconsiderNote` |
| M3 (A) 保持・再掲 | `cawaBridge.openQuestion` + status `open_question` / 注入文 |
| M4 進捗は無発話 | `maybeBridgeSummary` で progress は `reconsider:false` |
| M5 Realtime instructions | `realtime.go` の `realtimeSessionInstructions` |
| M6 自動検証 | Verification Plan |
| S1 エラーも (B) 経由 | 同一 reconsider ノート（summary が error でも同じ経路） |
| S2 UI hint | `static/index.html` |
| S3 沈黙時ログ | `bridge> reconsider (turn done)` / 無発話時も create は検討用 |
| Won't (C) 必須読上げ禁止 | 注入文に "Speak the final answer now" を入れない |

## Proposed Changes

### features/voice_demo（テスト先行）

#### [MODIFY] features/voice_demo/context_bridge_test.go

* `TestBuildTurnCompleteReconsiderNote` を追加:
  * empty summary → ok=false
  * question + summary → note に `Open question from the human`、質問文、`Coding agent result`、要約、`Reconsider (B)`、`Do not say that results came back` を含む
  * empty question → プレースホルダ `(no open question recorded)` を含む
  * 旧プレフィックスだけの強制読上げ文（`Speak the final answer to the human now`）を含まないこと

#### [MODIFY] features/voice_demo/context_bridge.go

```go
// BuildTurnCompleteReconsiderNote frames (C) as optional evidence for answering (A).
// It must not instruct the model to narrate agent status or force a spoken readout.
func BuildTurnCompleteReconsiderNote(question, summary string) (note string, ok bool)
```

* summary trim 空 → false
* question trim 空 → `"(no open question recorded)"`
* 本文（仕様の注入文を継承）:

```text
Open question from the human (still unanswered until you decide otherwise):
<A>

Coding agent result (optional evidence; you may ignore details irrelevant to A):
<C>

Reconsider (B): Does this suffice to answer A?
- If yes and you should speak to the human: say D as the answer to A (not an ack to the agent).
- If not enough: request more work via the usual coding-agent path; do not narrate status to the human.
- If you should stay silent for now: say nothing.
Do not say that results came back or that you understood the agent.
```

* `<A>` / `<C>` はそれぞれ適切な rune 上限で truncate（例: question 400、summary は既存 max 近傍）
* `BuildContextBridgeNote`（進捗用 `Tern coding agent status:`）は維持

#### [MODIFY] features/voice_demo/realtime_test.go

* `TestCreateRealtimeEphemeralSession_InstructionsReconsiderFraming`（または既存 Posts* に断言追加）:
  * `session.instructions` に次を含む: `optional evidence` または `Reconsider`、`do not` + `results came back`（大文字小文字は Contains で柔軟に）
  * 旧文「Acknowledge task updates briefly when asked」は含まない

#### [MODIFY] features/voice_demo/realtime.go

* 定数（または関数） `realtimeSessionInstructions` を切り出し、payload の `"instructions"` に使う。

内容（英語・仕様 M5）:

```text
You are the voice conversation layer for a Tern coding demo. Keep replies short. Do not read out long code or logs.
Coding work is executed by a separate Coding Agent behind Tern (two layers: you converse; the agent executes).
When you receive a turn-complete note that includes an open human question and a coding-agent result:
treat the result as optional evidence for answering that question — not as a conversation partner.
First reconsider (B): does the evidence suffice to answer the human? If yes and you should speak, answer the human (D) without acknowledging the agent. If not enough, continue work via the usual coding-agent path and do not narrate status. If you should stay silent, say nothing.
Never say that results came back, or that you understood the agent.
Progress notes prefixed with "Tern coding agent status:" are background only; do not announce them unless the human asks.
```

#### [MODIFY] features/voice_demo/cawa.go

* フィールド `openQuestion string` を追加
* `Start` / セッション破棄時にクリア
* `HandleUtterance` で `send==true` のとき `b.openQuestion = text`（trim 後）
* `Snapshot` の戻りに `openQuestion` を追加するか、別メソッド `OpenQuestion() string` を追加

#### [MODIFY] features/voice_demo/server.go

* status / utterance JSON に:
  * `"open_question": <bridge open question>`
  * busy→idle 完了後のクライアントが使えるよう、常に（summary があるとき）`"reconsider_note"` を `BuildTurnCompleteReconsiderNote(openQuestion, lastSummary)` で付与（ok のときのみ）
* 進捗用 `"bridge_note"` は従来どおり `BuildContextBridgeNote`

#### [MODIFY] features/voice_demo/app_js_assets_test.go

`TestAppJSActiveResponseCancelAndBridge` を更新・拡張:

* `requestRealtimeSpeak` を `requestRealtimeReconsider` に改名したことを要求（旧名が残っていたら Fatal）
* reconsider 関数本体に `response.create` とログ `reconsider`
* `maybeBridgeSummary` が `reconsider` オプションを扱い、progress では強制読上げ専用文言を送らない
* `refreshCawaStatus` が `turnJustFinished` 時に reconsider を要求し、`reconsider_note` または同等を使う
* 注入まわりに `Open question from the human` または `reconsider_note` 参照があること
* 「Speak the final answer to the human now」を含まないこと

#### [MODIFY] features/voice_demo/static/app.js

* `requestRealtimeSpeak` → `requestRealtimeReconsider`:
  * `response.create` 送信
  * ログ: `bridge> reconsider (turn done)`（沈黙可・読上げ強制ではない）
* `maybeBridgeSummary(summary, bridgeNote, opts)`:
  * `opts.reconsider === true` のときだけ `requestRealtimeReconsider()`
  * 完了時は `bridgeNote` にサーバの `reconsider_note` を優先（無ければクライアントで組み立てない／fallback で status note）
* `refreshCawaStatus`:
  * `turnJustFinished` → `maybeBridgeSummary(data.last_summary, data.reconsider_note || data.bridge_note, { reconsider: true })`
  * それ以外で summary 更新 → `{ reconsider: false }`（進捗無発話）
* コメントを (A)(B)(C)(D) の再検討フローに合わせて更新

#### [MODIFY] features/voice_demo/static/index.html

hint を更新:

* Turn 完了後も Voice はすぐ話さず、問い (A) に足りるか考えてから答える（足りなければ Tern に続ける）。結果が返ってきた、というメタ報告はしない。

#### [MODIFY] features/voice_demo/README.md

Context bridge 節を更新:

* 進捗: `Tern coding agent status:` 注入、自動発話なし
* 完了: reconsider ノート（(A)+(C)+再検討指示）注入後に `response.create`（再検討用）。即読上げ強制ではない
* (D) は (A) への答え。エージェントへの Ack 禁止

#### [MODIFY] 既存 cawa / utterance 試験

* Snapshot / status JSON に `open_question` / `reconsider_note` が載ることを、既存または新規の小さな handler 試験で確認（可能なら `TestStatusIncludesReconsiderNote`）

## Step-by-Step Implementation Guide

1. **[ ]** RED: `TestBuildTurnCompleteReconsiderNote` を追加し失敗を確認
2. **[ ]** GREEN: `BuildTurnCompleteReconsiderNote` を実装
3. **[ ]** RED: Realtime instructions 試験を追加し失敗を確認
4. **[ ]** GREEN: `realtimeSessionInstructions` を更新
5. **[ ]** RED: AppJS 契約試験を reconsider 前提に更新し失敗を確認
6. **[ ]** GREEN: `cawa.go` の `openQuestion`、`server.go` の JSON、`app.js` / hint / README を実装
7. **[ ]** utterance/status 試験の調整
8. **[ ]** Verification Plan 実行、総合判定、far-knowledge、push

## Verification Plan

### テスト項目（ボトムアップ）

| 段 | 層 | 試験 |
| :--- | :--- | :--- |
| L1 | Unit | `TestBuildTurnCompleteReconsiderNote` |
| L2 | Unit | Realtime instructions framing |
| L3 | Unit | AppJS reconsider 契約 |
| L4 | Unit | status/open_question/reconsider_note（任意） |
| L5 | Package | `go test -C features/voice_demo -count=1 ./...` |
| L6 | Integration | `TestTurnInterrupt_` リグレッション |

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 記録 |
| :--- | :--- | :--- |
| BuildTurnCompleteReconsiderNote | 関数未実装 | `tmp/red_reconsider_note.txt` |
| Instructions framing | 旧 instructions | `tmp/red_instructions_reconsider.txt` |
| AppJS reconsider | 旧 speak 名 | `tmp/red_appjs_reconsider.txt` |

### Automated Verification

```bash
./scripts/process/build.sh
go test -C features/voice_demo -count=1 ./...
./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

### E2E Tests

Playwright E2E は追加しない。理由: マイク＋OpenAI Realtime＋Tern の聴覚的 (D) 品質は CI で機械判定しにくく、仕様の自動化対象は契約試験（注入文・instructions・AppJS）と TurnInterrupt リグレッションで足りる。耳確認は再現手順レポートへ。

### テスト項目設計セルフレビュー (§11.4)

* 網羅性: M1–M6 / S2 を単体・静的契約でカバー。S1 は同一経路。S3 はログ文字列契約。
* 証拠: note 文字列、instructions JSON、app.js ソース契約。
* 迂回排除: 「Speak the final answer now」禁止を明示断言。
* 依存: L1→L5→L6。

### 総合判定プロセス (§12)

全自動試験成功後、仕様の検証シナリオ 1–10 と実装の対応を短く判定し、計画末尾に結果を追記する。

## Documentation

#### [MODIFY] features/voice_demo/README.md

* Context bridge の完了時 reconsider フローを記載

`prompts/specifications` に voice_demo 専用文書は無し → 追加更新なし。
