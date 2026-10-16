# client/v1: expose ToolCallID on tool_use / tool_result events

## 背景

axsh/vv4 は Claude Code セッションの `tool_use` と `tool_result` を到着順でペアリングしている。並列ツール呼び出しでは順序が入れ替わり得るため、対応付けが壊れる。

## 現状（main = 4322f7b で確認）

- サーバー側ワイヤ: `codingagent.StreamEvent.ToolCallID`（`json:"tool_call_id,omitempty"`、`shared/libs/go/codingagent/event.go`）。
  - Claude Code は `tool_use`（`block.ID`）と `tool_result`（`block.ToolUseID`）の両方で設定する（`codingagent/claudecode/protocol.go`）。
  - Codex はどの経路でも設定しない（本スコープ外）。
- チャンク分割された `tool_result`（`buildChunkedToolResult`、`codingagent/sse_chunk.go`）は `tool_result_part` と最終 `tool_result` に `ToolCallID` を載せない。大きい結果ほど ID が欠落する。
- `client/v1` の SSE デコーダ（`client/v1/stream.go`）は `tool_call_id` を読まず、`Event` にもフィールドがない。

## 要件

1. `client/v1.Event` に `ToolCallID string` を追加する（既存フィールドは変更しない）。`tool_use` / `tool_result` の両方で、ワイヤの `tool_call_id` を設定する。
2. チャンク分割された `tool_result` でも、最終 `tool_result` に `tool_call_id` が載る（サーバー側修正）。
3. 後方互換: `OnToolUse` / `OnToolResult` のシグネチャは変えない。ID が必要な利用者は `Stream.Events()` を使う。
4. ワイヤに `tool_call_id` がない場合（Codex 等）は空文字のまま。

## 非ゴール

- Codex への ToolCallID 付与。
- #36 の `tool_input`（`Event.ToolInput` は現行コードで既に存在する。#36 の扱いは別途確認）。
- コールバックへの ID 追加。
