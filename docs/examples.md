# Examples

Working samples under `examples/` and the corresponding client/server snippets.

The `examples/` directory contains working samples that demonstrate Tern's core concepts.

### Server ([examples/minimal-server](../examples/minimal-server/main.go))

All built-in coding agents and LLM providers are auto-registered by the `server` package.
Starting a server requires only a config path:

```go
srv, err := server.New(server.WithConfigPath("config.yaml"))
// Optional: enable specific API versions only:
// srv, err := server.New(server.WithConfigPath("config.yaml"), server.WithEnableVersion(1))
srv.Launch(ctx)
defer srv.Shutdown(ctx)
```

`Launch` also fetches LLMGP `GET /v1/models` into the Agent Service cache (`default_model` and the model list). Callers do not need to call `FetchModelsFromGateway` themselves. CreateSession may omit `model` to use that gateway default (see [docs/reference-manual-web-apis.md](reference-manual-web-apis.md)).

### Client Examples

Tern client libraries ([examples/minimal-client](../examples/minimal-client/main.go), [examples/multimodal-client](../examples/multimodal-client/main.go)) simplify session interaction.

You can switch `config_dir` on an existing session without changing `session_id` or terminating. Conversation continues on the next `SendText` / `SendMessage`; the overlay applies then. Do not call `Terminate` merely to switch config. Full walkthrough: [examples/config-dir-switch/README.md](../examples/config-dir-switch/README.md) and [examples/config-dir-switch/main.go](../examples/config-dir-switch/main.go). HTTP details: [docs/reference-manual-web-apis.md](reference-manual-web-apis.md) (PATCH `/api/v1/sessions/:id`).

If an in-flight turn SSE drops, do not send another user message. Call `GetSession`, and when `followable` (or status `active` / `suspended`) reattach with `Follow` / `FollowFrom`. Walkthrough: [examples/session-follow/README.md](../examples/session-follow/README.md) and [examples/session-follow/main.go](../examples/session-follow/main.go). HTTP: [docs/reference-manual-web-apis.md](reference-manual-web-apis.md) (`GET /api/v1/sessions/:id/events`).

```go
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent: "claudecode", WorkDir: workDir, SessionDir: sessionDir, ConfigDir: alphaDir,
})
stream1, _ := session.SendText(ctx, prompt1)
// ... drain stream1 ...
info, _ := session.UpdateConfigDir(ctx, betaDir) // PATCH; do not Terminate
_ = info
stream2, _ := session.SendText(ctx, prompt2)
```

For long-running SSE streams, disable the HTTP client timeout:

```go
c := client.New("http://localhost:3100", client.WithNoTimeout())
```

### Vault API Examples

Tern also provides a reusable Vault API package at [`vault/`](../vault), designed for tool authors who want Vault functionality without embedding CLI parsing logic.

#### 1. Low-level Vault Operations

```go
import (
    sharedvault "github.com/axsh/arctic-tern/shared/libs/go/vault"
    apivault "github.com/axsh/arctic-tern/vault"
)

svc := apivault.NewService(sharedvault.NewKeyringVaultBackend())
_, _ = svc.Set("anthropic", "", "sk-ant-...")
res, _ := svc.Get("anthropic", "", false)
states, _ := svc.Status([]string{"anthropic", "openai", "google"})
_ = res
_ = states
```

#### 2. High-level CLI Facade (main-ready)

```go
runner := apivault.NewCLIRunner(apivault.CLIConfig{
    Store:      sharedvault.NewKeyringVaultBackend(),
    Stdin:      os.Stdin,
    Stdout:     os.Stdout,
    Stderr:     os.Stderr,
    AppName:    "vault-cli",
    AppVersion: "0.1.0",
})
os.Exit(runner.Run(os.Args[1:]))
```

The `features/vault-cli` binary uses this facade and now serves as a minimal sample entrypoint for the public Vault API.

#### 1. Setup and Session Initialization

First, import the library, connect to the Tern server, and create an active session:

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100", client.WithNoTimeout())

session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "claudecode",          // Use Wayfinder, Claude Code, or Codex
    WorkDir: ".",
})
defer session.Terminate(ctx)
```

#### 2. Send Message Options

**Option A: Send Text-only Messages (convenience method)**

```go
stream, _ := session.SendText(ctx, "Create hello.txt with 'Hello, World!'")
stream.Output(os.Stdout)
```

**Option B: Send Image alongside Text (SendImageFile shortcut)**

For typical image + text queries, pass the local file path directly. Media type is automatically detected:

```go
stream, _ := session.SendImageFile(ctx, "screenshot.png", "Describe this image:")
stream.Output(os.Stdout)
```

**Option C: Complex Multimodal Layouts (Message Builder)**

For multiple text blocks or multiple images in a single message:

```go
parts, _ := client.NewMessage().
    Text("Compare these two images:").
    ImageFile("image1.png").
    ImageFile("image2.png").
    Build()

stream, _ := session.SendMessage(ctx, parts)
stream.Output(os.Stdout)
```

#### 3. Interactive Agent Execution

Coding Agents (Codex, Claude Code) may ask the user for input during a task. Tern detects this and emits a `user_input_required` SSE event. The session enters `suspended` status until the client responds.

**Respond to a user-input request (low-level API):**

```go
// After receiving user_input_required on the SSE stream:
stream, _ := session.Respond(ctx, "Yes, proceed with Option A")
stream.Output(os.Stdout)
```

**Handle user input with callbacks (recommended):**

```go
err := session.SendTextWithHandlers(ctx, "Refactor the auth module", client.StreamHandlers{
    OnText: func(text string) { fmt.Print(text) },
    OnToolUse: func(toolName string, toolInput map[string]any) {
        fmt.Printf("tool=%s input=%v\n", toolName, toolInput)
    },
    OnUserInputRequired: func(ev client.UserInputRequiredEvent) (string, error) {
        // ev.Content holds the question; ev.Choices holds structured options (Wayfinder only)
        return "Use the default approach and continue", nil
    },
})
```

If `OnUserInputRequired` is not set, the SDK stops with an error when input is required. This prevents unintended auto-responses.

`OnToolUse` receives tool arguments from the SSE `tool_input` field (`Event.ToolInput`). This is a breaking change from the previous `func(toolName string)` signature; see [docs/client-sse-tool-input.md](client-sse-tool-input.md).

While a session is `suspended`, only `respond`, `cancel`, and `terminate` are accepted. Sending a new message to the same session returns HTTP 409 Conflict.

If a turn appears stuck but you want to keep the session for resume, call `session.CancelTurn(ctx)` (status stays non-closed). Use `session.Terminate(ctx)` only when you intend to close the session.

While a tool runs with no agent stdout, Tern emits SSE `progress` events with `content: "tool_still_running"` (default every 30s) so clients can treat the stream as alive. See [docs/reference-manual-web-apis.md](reference-manual-web-apis.md).

### Multimodal Messages and Context

Tern acts as a stateless proxy for multimodal content: each `SendMessage` request carries the content for that turn. The server does not automatically re-inject images from prior turns. Re-send images when the agent needs visual context from earlier messages.

Codex enforces a 1 MB stdin limit. Configure per-agent limits in `model_profiles.yaml` under `coding_agents` (see [Getting Started](getting-started.md)). Requests exceeding the limit are rejected before the agent starts.

### Agent and Model Interoperability

Switching agents or models is a matter of changing session parameters. The surrounding application remains unchanged:

```go
client.SessionRequest{Agent: "claudecode", Model: "sonnet"}   // Claude Code
client.SessionRequest{Agent: "codex",      Model: "gpt-5.5"}  // Codex
```

---
