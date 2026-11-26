# 006-Voice-Demo-VAD-Turn-End-UI

> **Source Specification**: [005-Voice-Demo-VAD-Turn-End-UI.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/005-Voice-Demo-VAD-Turn-End-UI.md)

## Goal Description

`features/voice_demo` の Web UI で OpenAI Realtime `server_vad` の発話終了待ち（`silence_duration_ms` / `threshold` / `prefix_padding_ms`）を調整し、Start 時の `client_secrets` に載せて耳で合わせられるようにする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| M1 UI コントロール | `index.html` / `style.css` |
| M2 session 反映 | `server.go` + `realtime.go` + `app.js` |
| M3 クランプ | `vad_policy.go` `ResolveVAD` |
| M4 反映タイミング明示 | `index.html` 文言 |
| M5 適用値表示 | `vad-applied` + transcript |
| M6 自動検証 | `vad_policy_test.go` / static / realtime / session 試験 |
| S1 プリセット | Responsive / Patient ボタン（実装する） |
| S2 session.update | 見送り |

## Proposed Changes

### features/voice_demo

#### [NEW] [features/voice_demo/vad_policy_test.go](features/voice_demo/vad_policy_test.go)

*   **Cases**:
    *   ゼロ値入力 → 既定 silence=900, threshold=0.5, prefix=300
    *   範囲内 → そのまま
    *   下限未満 / 上限超過 → クランプ
    *   threshold は float、整数 ms は int

#### [NEW] [features/voice_demo/vad_policy.go](features/voice_demo/vad_policy.go)

```go
type VADParams struct {
    SilenceDurationMs int
    Threshold         float64
    PrefixPaddingMs   int
}

type VADInput struct {
    SilenceDurationMs *int
    Threshold         *float64
    PrefixPaddingMs   *int
}

func ResolveVAD(in VADInput) VADParams
```

*   **Logic**: nil は既定。値は [200,2000] / [0,1] / [0,1000] にクランプ。

#### [MODIFY] [features/voice_demo/realtime.go](features/voice_demo/realtime.go)

*   **Signature**: `createRealtimeEphemeralSession(ctx, apiKey, model, voice string, barge BargeInParams, vad VADParams)`
*   **Logic**: `turn_detection` に `silence_duration_ms`, `threshold`, `prefix_padding_ms` を追加。

#### [MODIFY] [features/voice_demo/realtime_test.go](features/voice_demo/realtime_test.go)

*   既存試験に `ResolveVAD(VADInput{})` を渡す。
*   新規: turn_detection に VAD 値が載ることを断言。

#### [MODIFY] [features/voice_demo/server.go](features/voice_demo/server.go)

*   `handleRealtimeSession` が JSON から VAD を読み `ResolveVAD`、応答に `"vad": {...}` をエコー。

#### [NEW] [features/voice_demo/realtime_session_handler_test.go](features/voice_demo/realtime_session_handler_test.go)

*   httptest + 差し替え URL で POST、応答 `vad` がクランプ後である／OpenAI body に値が入る（ハンドラ経由または create 試験で十分なら create 側を主とし、ハンドラはエコーのみ軽く）。

#### [MODIFY] [features/voice_demo/static_assets_test.go](features/voice_demo/static_assets_test.go)

*   `vad-silence-ms`, `vad-threshold`, `vad-prefix-ms`, `vad-applied`, “Applies on next Realtime connect” 相当文言。

#### [MODIFY] [features/voice_demo/static/index.html](features/voice_demo/static/index.html)

*   VAD 行: number/range 入力、説明、プリセットボタン、`vad-applied`、適用タイミング注記。

#### [MODIFY] [features/voice_demo/static/app.js](features/voice_demo/static/app.js)

*   `connectRealtime` の POST に VAD 値。応答で `vad-applied` と transcript 更新。プリセット。

#### [MODIFY] [features/voice_demo/static/style.css](features/voice_demo/static/style.css)

*   `.vad-row` レイアウト。

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)

*   VAD UI の説明、`go build -C features/voice_demo -o ../../bin/voice_demo .`

## Step-by-Step Implementation Guide

1. **[ ]** RED: `vad_policy_test.go` → `vad_policy.go`
2. **[ ]** Update `realtime.go` / tests for VAD in turn_detection
3. **[ ]** Update `server.go` + session echo test
4. **[ ]** Update HTML / CSS / app.js / static tests
5. **[ ]** README; Verification; 総合判定; commit; far-knowledge; push

## Verification Plan

### テスト項目の設計（§8）

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 | ResolveVAD | `TestResolveVAD` |
| L1 | HTML | static VAD testids |
| L2 | realtime payload | `TestCreateRealtimeEphemeralSession_PostsVADParams` |
| L3 | utterance / app.js | 既存回帰 |
| L4 | interrupt | `TestTurnInterrupt_` |

#### セルフレビュー（§8.4）

網羅: クランプ・testid・turn_detection 値。主観の「間」は手動。迂回: JSON の数値で固定。

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 実際のレッド（実行時記入） |
| :--- | :--- | :--- |
| `TestResolveVAD` | 未定義 | （実装時） |
| static VAD testids | 無し | （実装時） |

### Automated Verification

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

E2E Playwright 不要理由: Example 埋め込み UI。契約は feature 内 Go 試験。

### 総合判定結果

（実装後記入）

## Documentation

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)
