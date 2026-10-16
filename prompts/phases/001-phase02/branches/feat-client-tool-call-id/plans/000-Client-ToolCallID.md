# 実装計画: client/v1 ToolCallID

仕様: `../ideas/000-Client-ToolCallID.md`
テスト実行: `bash scripts/process/build.sh`（既知の既存失敗: `codingagent` の `TestWSLCommandBuilder`）。個別確認は `go test`。

## Phase 1: サーバー（チャンク分割 tool_result の ID 保持）

### Proposed Changes
- [MODIFY] `shared/libs/go/codingagent/sse_chunk_test.go`（先に書く）
  - `ToolCallID: "toolu_1"` 付きの大きな `tool_result` を `buildChunkedToolResult` 相当の経路に通し、`tool_result_part` と最終 `tool_result` の全てに `tool_call_id == "toolu_1"` が載ることを検証する。
  - ID が空のとき JSON に `tool_call_id` が出ない（omitempty）ことを検証する。
  - 既存テストの置き換え・削除はしない。
- [MODIFY] `shared/libs/go/codingagent/sse_chunk.go`
  - `buildChunkedToolResult` の `tool_result_part` と最終 `tool_result` に `ToolCallID: ev.ToolCallID` を追加する。

### 検証
- `go test ./shared/libs/go/codingagent/ -run 'Chunk|ToolResult'`（追加前に失敗することを確認してから実装）

## Phase 2: クライアント

### Proposed Changes
- [MODIFY] `client/v1/stream_test.go`（先に書く）
  - SSE 入力に `tool_use`（`tool_call_id: "a"`）、`tool_use`（`"b"`）、`tool_result`（`"b"`）、`tool_result`（`"a"`）を順不同で流し、`Events()` の `ToolCallID` が各イベントで正しく取れることを検証する。
  - チャンク分割された `tool_result`（parts + 最終）で、最終イベントの `ToolCallID` が取れることを検証する。
  - `tool_call_id` なしのイベントは `ToolCallID == ""`。
  - `OnToolUse` / `OnToolResult` を使う既存テストが無変更で通る。
- [MODIFY] `client/v1/stream.go`
  - `Event` に `ToolCallID string` を末尾に追加する。
  - SSE の `raw` 構造体に `ToolCallID string \`json:"tool_call_id,omitempty"\`` を追加する。
  - `tool_result` 経路（`Event{Type: EventToolResult, ...}`）と汎用経路（`tool_use` 含む）の両方で `ToolCallID: raw.ToolCallID` を設定する。
  - `Event` を値で比較・リテラル生成している箇所（テスト含む）に影響がないことを確認する。
- [MODIFY] `docs/reference-manual-web-apis.md`（`tool_use` / `tool_result` イベントの項に `tool_call_id` を追記。Claude Code のみで設定される旨も書く）
- 派生物の確認: legacy `client` パッケージは現在のツリーに存在しない。`docs/` 内のクライアント解説（`docs/client-sse-tool-input.md` 等）に Event フィールド一覧があれば同時に更新する。

### 検証
- `go test ./client/... ./shared/libs/go/codingagent/...`
- `bash scripts/process/build.sh`（既知の失敗以外が増えないこと）

## 要確認事項

1. Codex の `tool_use` / `tool_result` にも ToolCallID を付けるか（本計画では対象外。vv4 は Claude Code のみ使用）。
2. #36（`tool_input` の client 公開）は現行コードで実装済みに見える。issue のクローズ、または関連コメントを付けるか。
3. コールバック（`OnToolUse` 等）に ID を渡す拡張は入れない方針で良いか（余分なノブを増やさないため）。
