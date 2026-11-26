# 005-Voice-Demo-Realtime-Audio-Resume

> **Source Specification**: [004-Voice-Demo-Realtime-Audio-Resume.md](prompts/phases/001-phase02/branches/feat-turn-interrupt-resume/ideas/004-Voice-Demo-Realtime-Audio-Resume.md)

## Goal Description

`features/voice_demo` のブラウザ再生で、barge-in 時にリモート `<audio>` を `pause()` したままにせず、アシスタント発話開始で volume 復帰と `play()` を行い、文字起こし後も OpenAI の声が聞こえるようにする。

## User Review Required

None.

## Requirement Traceability

| Requirement (from Spec) | Implementation Point |
| :--- | :--- |
| M1 聞こえ復帰 | `app.js` soften + ensure playing |
| M2 pause 恒久禁止 | `softenRemoteAudioOnBargeIn` が `pause()` を呼ばない |
| M3 play 復帰 | `ensureRemoteAudioPlaying` を `response.created` / `output_audio_buffer.started` で呼ぶ |
| M4 CAWA 維持 | utterance 経路は変更しない |
| M5 自動検証 | `app_js_assets_test.go` |
| S1 ログ一行 | `ensureRemoteAudioPlaying` 内で任意の `realtime> resume playback`（実装する） |

## Proposed Changes

### features/voice_demo

#### [NEW] [features/voice_demo/app_js_assets_test.go](features/voice_demo/app_js_assets_test.go)

*   **Description**: `static/app.js` の契約試験（TDD レッド先）。
*   **Cases** (`TestAppJSResumesRemoteAudioAfterBargeIn`):
    *   `softenRemoteAudioOnBargeIn` という関数名が存在する（または同等名を仕様どおり採用したらその名）。
    *   当該関数の本体に `.pause(` が含まれないこと（簡易: `function softenRemoteAudioOnBargeIn` から次の `function ` までの区間に `.pause(` が無い）。
    *   `ensureRemoteAudioPlaying` が存在し、本体に `.play(` と `volume` 代入があること。
    *   `input_audio_buffer.speech_started` 付近で `softenRemoteAudioOnBargeIn` を呼ぶこと。
    *   `response.created` または `output_audio_buffer.started` の分岐で `ensureRemoteAudioPlaying` を呼ぶこと。
    *   旧名 `fadeAndPauseRemoteAudio` が残っていないこと。

#### [MODIFY] [features/voice_demo/static/app.js](features/voice_demo/static/app.js)

*   **Description**: barge-in 再生制御の修正。
*   **Logic**:
    1. `fadeAndPauseRemoteAudio` を削除し、次を置く。

```javascript
function softenRemoteAudioOnBargeIn() {
  if (!remoteAudio) return;
  if (!fadeMs || fadeMs <= 0) {
    remoteAudio.volume = 0;
    return;
  }
  const start = remoteAudio.volume || 1;
  const steps = 8;
  const stepMs = Math.max(16, Math.floor(fadeMs / steps));
  let i = 0;
  const timer = setInterval(() => {
    i += 1;
    remoteAudio.volume = Math.max(0, start * (1 - i / steps));
    if (i >= steps) clearInterval(timer);
  }, stepMs);
}

function ensureRemoteAudioPlaying() {
  if (!remoteAudio) return;
  remoteAudio.volume = 1;
  const p = remoteAudio.play();
  if (p && typeof p.catch === "function") {
    p.catch(() => {});
  }
  logLine("realtime> resume playback");
}
```

    2. `speech_started` で `softenRemoteAudioOnBargeIn()` を呼ぶ（`fadeAndPauseRemoteAudio` は呼ばない）。
    3. `response.created` / `output_audio_buffer.started` で `ensureRemoteAudioPlaying()` を呼び、その後 `setRealtimeStatus("speaking")`。
    4. `stopRealtime` の終了時 `pause()` は呼んでよい（コール終了時の破棄）。

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)

*   **Description**: Manual scenarios に「barge-in 後もアシスタント音声が聞こえる。無音なら再生復帰（pause 禁止）を確認」を1行追加。

## Step-by-Step Implementation Guide

1. **[x]** RED: `app_js_assets_test.go` を追加し、現行 `app.js` で失敗することを確認する。
2. **[x]** GREEN: `app.js` を上記 Logic どおり修正する。
3. **[x]** README 一文を更新する。
4. **[x]** Verification Plan を実行し、総合判定を書いてコミット、far-knowledge、push する。

## Verification Plan

### テスト項目の設計（§8）

| 段 | 対象 | テスト |
| :--- | :--- | :--- |
| L1 | app.js 契約 | `TestAppJSResumesRemoteAudioAfterBargeIn` |
| L3 | utterance 回帰 | `TestUtteranceHandler_IdleMessagesBusyInterrupt` |
| L4 | interrupt 回帰 | `TestTurnInterrupt_` |

#### セルフレビュー（§8.4）

網羅: pause 禁止・play 復帰・speech_started / response フック。聞こえは手動。迂回: 関数名と `.pause(` / `.play(` のソース契約で固定。

### 先に書く試験（レッド記録）

| 試験 | 想定レッド | 実際のレッド（実行時記入） |
| :--- | :--- | :--- |
| `TestAppJSResumesRemoteAudioAfterBargeIn` | 旧 `fadeAndPauseRemoteAudio` + pause | 実際: `fadeAndPauseRemoteAudio` 残存で FAIL（`tmp/app_js_red.txt`）。修正後 PASS |

### Automated Verification

```bash
./scripts/process/build.sh
./scripts/process/integration_test.sh --specify 'TestTurnInterrupt_'
```

E2E（Playwright）不要理由: 本変更は Example の埋め込み `app.js` の再生制御であり、プロジェクト標準の Web E2E 画面対象外。契約は feature 内 Go 試験で固定し、聞こえは仕様の手動シナリオとする。

### 総合判定結果

**合格。** `TestAppJSResumesRemoteAudioAfterBargeIn` RED→GREEN。`build.sh` PASS。`TestTurnInterrupt_` PASS。聞こえは手動シナリオ（仕様 §検証シナリオ）に委ねる。

## Documentation

#### [MODIFY] [features/voice_demo/README.md](features/voice_demo/README.md)
*   barge-in 後の聞こえ確認一文。
