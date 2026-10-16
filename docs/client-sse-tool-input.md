# client/v1: tool_input on tool_use SSE events

## Wire

AgentService SSE `data:` lines for `tool_use` may include:

```json
{"type":"tool_use","tool_name":"command_execution","tool_input":{"command":"ls -la"}}
```

`codingagent.StreamEvent` fields `tool_name` / `tool_input` are forwarded by AgentService.

## Official Go client

- `client/v1.Event.ToolInput` (`map[string]any`) is populated from JSON `tool_input`.
- The client does **not** truncate or chunk `tool_input`. Oversized SSE lines remain a separate concern (see Issue #26 / `docs/sse-chunk-protocol.md` for `tool_result` chunking).

## Breaking change

`OnToolUse` / `StreamHandlers.OnToolUse` signature:

```go
// before
func(toolName string)
// after
func(toolName string, toolInput map[string]any)
```

No compatibility wrapper is provided.

## tool_call_id

`tool_use` and `tool_result` SSE events may carry `tool_call_id`, which pairs a `tool_result` with the `tool_use` that produced it (useful when tool calls run in parallel and results arrive out of order):

```json
{"type":"tool_use","tool_name":"Bash","tool_input":{"command":"ls"},"tool_call_id":"toolu_01"}
{"type":"tool_result","content":"...","tool_call_id":"toolu_01"}
```

- Claude Code sets it on both events. Other agents (for example Codex) do not set it, so it is empty.
- `client/v1.Event.ToolCallID` is populated from JSON `tool_call_id` and is read through `Stream.Events()`. The `OnToolUse` / `OnToolResult` callbacks are unchanged and do not receive it.
- For a chunked `tool_result` (see `docs/sse-chunk-protocol.md`), the `tool_result_part` events and the completion marker all carry the same `tool_call_id`.
