# 007-Voice-Demo-Voice-Speed-And-Context-Bridge

> **Source Specification**: [006-Voice-Demo-Voice-Speed-And-Context-Bridge.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/006-Voice-Demo-Voice-Speed-And-Context-Bridge.md)

## Goal Description

`features/voice_demo` に (1) voice/speed UI と一覧 API、(2) CAWA→Realtime 要約橋渡し、(3) 発話ルーティング auto/always/never、(4) Realtime active response の cancel 整合と resume ログ間引きを入れる。

## User Review Required

None.

## Requirement Traceability

| Requirement | Implementation Point |
| :--- | :--- |
| M1 voices API | `voices.go` + `GET /api/realtime/voices` |
| M2/M3/M4 voice/speed | `output_audio.go`, `realtime.go`, UI |
| M5 bridge | `context_bridge.go` + `app.js` inject |
| M6 routing | `route_policy.go` + utterance API + UI |
| M7 concurrent response | `app.js` state + cancel + resume once |
| M8 tests | 下記 Verification |
| S1–S4 | speed presets, bridge log, README work_dir, Send to Tern ボタン |

## Proposed Changes

### features/voice_demo

#### [NEW] voices.go / voices_test.go
* Curated list of 10 built-ins; `handleVoices` JSON as in spec.

#### [NEW] output_audio.go / output_audio_test.go
```go
func ResolveOutputAudio(voice string, speed float64) (string, float64)
// default marin; unknown voice → marin; speed clamp [0.25, 1.5], default 1.0 if 0
```

#### [MODIFY] realtime.go
* Signature adds `speed float64` (or OutputAudio params).
* `audio.output` includes `voice` and `speed`.

#### [NEW] context_bridge.go / context_bridge_test.go
```go
func BuildContextBridgeNote(summary string) (note string, ok bool)
// empty → false; prefix "Tern coding agent status: "; truncate ~280 runes
```

#### [NEW] route_policy.go / route_policy_test.go
```go
type RouteMode string // always|never|auto
func DecideUtteranceRoute(busy bool, text, mode string) (send bool, action Action, reason string)
```
* **auto idle**: send messages if `looksLikeWork(text)` else skip (`chat`/`short`).
* **auto busy**: send interrupt if `looksLikeWorkChange(text)` else skip.
* **looksLikeWork**: len>=40 OR keywords (please, create, file, run, sleep, 作って, 実行, shell, …).
* **looksLikeWorkChange**: looksLikeWork OR stop/cancel/やめて/変更/interrupt.
* **always**: DecideUtteranceAction; **never**: send=false.

#### [MODIFY] server.go
* `GET /api/realtime/voices`
* session POST: `voice`, `speed`; echo `output: {voice,speed}`
* utterance POST: `route_mode`; if DecideUtteranceRoute send=false → 200 `{ok, sent:false, reason}` without CAWA call.
* Optional `POST /api/utterance` with force for Send to Tern (route_mode=always).

#### [MODIFY] static/index.html, style.css, app.js
* voice select, speed, presets, output-applied, context-bridge checkbox, cawa-route-mode, btn-send-tern.
* connectRealtime posts voice/speed; loads voices on start.
* transcription → Decide path via server; bridge on summary change; response.cancel on speech_started if active; resume log only on response.created.

#### [MODIFY] tests + README
* static testids; AppJS contracts for cancel/resume; README work_dir + new controls.

## Step-by-Step Implementation Guide

1. **[ ]** RED/GREEN: voices, output_audio, context_bridge, route_policy (+tests)
2. **[ ]** Wire realtime.go + server handlers + utterance route
3. **[ ]** Update HTML/CSS/app.js + static/app.js contracts
4. **[ ]** README; Verification; 総合判定; far-knowledge; push

## Verification Plan

### テスト項目

| 段 | テスト |
| :--- | :--- |
| L1 | TestResolveOutputAudio, TestBuildContextBridgeNote, TestDecideUtteranceRoute, TestVoicesHandler |
| L2 | TestCreateRealtimeEphemeralSession_PostsVoiceAndSpeed |
| L3 | Utterance always path regression; AppJS contracts |
| L4 | TestTurnInterrupt_ |

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 実際 |
| :--- | :--- | :--- |
| TestDecideUtteranceRoute | 未定義 | （実装時） |
| TestVoicesHandler | 未定義 | （実装時） |

### Automated Verification

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

E2E Playwright 不要: Example 埋め込み UI。契約は feature 内 Go 試験。

### 総合判定結果

（実装後記入）

## Documentation

#### [MODIFY] features/voice_demo/README.md
* voice/speed, routing, bridge, work_dir note, build -C.
