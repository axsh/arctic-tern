# Getting Started

This guide covers prerequisites, building Tern from source, vault setup, configuration, and your first run with `ternctl` or the Go client.

## Installation


### Prerequisites

| Requirement | Version | Notes |
| --- | --- | --- |
| Go | 1.26 or later | `go.mod` specifies `go 1.26.3` |
| Git | 2.x or later | Used for submodule checkout |
| Docker / Docker Compose | Docker 20.10+ / Compose v2+ | Only required for container deployment |
| Claude Code CLI | 2.1.x or later | Update with `claude update` |
| Codex CLI | Latest | Optional, for Codex agent support |
| OS | Windows / macOS / Linux | Cross-platform |

> **Note**: On Windows, Git Bash is recommended. Build scripts are written in bash.

> **Important**: Claude Code CLI v2.0.x ignores the `ANTHROPIC_BASE_URL` environment variable,
> which prevents Gateway-proxied requests from working. Make sure to update to v2.1.x or later
> by running `claude update`.

You will also need API keys for at least one LLM provider (OpenAI, Anthropic, or Google).

### Build from Source

```bash
git clone https://github.com/axsh/arctic-tern.git
cd arctic-tern

# Build all features and examples
./scripts/process/build.sh
```

Built binaries are placed in the `bin/` directory:

| Binary | Description |
| --- | --- |
| `bin/tern` | Tern server (full-featured, production) |
| `bin/ternctl` | CLI client for interacting with a running tern server |
| `bin/vault-cli` | Key Vault management CLI |
| `bin/minimal-server` | Minimal server example |
| `bin/minimal-client` | Minimal client example |

---

## Quick Start


### 1. Store API keys in the vault

**Option A: OS Keyring** (recommended for local development)

```bash
# Store an API key using the provider shorthand
# (--provider anthropic expands to vault key: providers/anthropic/default)
$ ./bin/vault-cli set --provider anthropic
Enter value: sk-ant-...

# For non-default key names, use --key with a full path
$ ./bin/vault-cli set --key providers/openai/team-a

# Check which providers have keys registered
$ ./bin/vault-cli status
```

**Option B: Environment Variables** (recommended for Docker / CI)

Set `vault.backend: "env"` in your `config.yaml`, then export API keys as environment variables.
Vault references in `model_profiles.yaml` are mapped to environment variable names automatically:

```bash
# vault://providers/openai/default    -> TERN_VAULT_OPENAI_DEFAULT
# vault://providers/anthropic/primary -> TERN_VAULT_ANTHROPIC_PRIMARY
export TERN_VAULT_OPENAI_DEFAULT="sk-proj-your-openai-api-key"
export TERN_VAULT_ANTHROPIC_PRIMARY="sk-ant-your-anthropic-api-key"

$ ./bin/tern --config settings/demo/config.yaml
```

The mapping rule is: `vault://providers/{provider}/{key}` becomes `TERN_VAULT_{PROVIDER}_{KEY}` (uppercased, hyphens replaced with underscores).

`OPENAI_API_KEY` does not satisfy the env backend for `vault://providers/openai/default`. That reference resolves to `TERN_VAULT_OPENAI_DEFAULT` only. Gateway Debug `openai responses request received` means the handler accepted the HTTP body. Info `openai responses request via bifrost` is emitted only after model routing and vault resolve both succeed. Failures emit ERROR `llm gateway error response` with `code` (`vault_error`, `model_not_found`, and so on). Missing that Info line after `received` is a vault or routing failure, not proof the upstream LLM ran.

### 2. Configure the server

Configuration files are located in the `settings/` directory:

* `settings/example/` -- Minimal configuration for getting started
* `settings/demo/` -- Full-featured configuration for development

Create a `config.yaml`:

```yaml
llm_gateway:
  port: 14000
  model_profiles_path: "model_profiles.yaml"
vault:
  backend: "keyring"
agent_service:
  port: 3100
log:
  level: "info"
  outputs:
    - type: "stdout"
```

Create a `model_profiles.yaml`:

```yaml
default_profile:
  provider: anthropic
  model: claude-sonnet-4-6

providers:
  anthropic:
    api_keys:
      - name: default
        secret: vault://providers/anthropic/default
        models:
          - name: claude-sonnet-4-6
          - name: claude-opus-4-8
  openai:
    api_keys:
      - name: default
        secret: vault://providers/openai/default
        models:
          - name: gpt-4o
          - name: gpt-5.5
  ollama:
    api_keys:
      - name: default
        models:
          - name: qwen2.5-coder:7b
            behavior:
              tool_call_fallback: true
    network_config:
      base_url: "http://localhost:11434"

coding_agents:
  codex:
    max_prompt_bytes: 1048576      # 1 MB stdin limit
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: interactive    # interactive | single_shot
  claudecode:
    max_execution_seconds: 3600
    idle_timeout_seconds: 300
    execution_mode: interactive
```

`execution_mode` controls how the server talks to the CLI:

| Mode | Behavior |
| --- | --- |
| `interactive` | Keeps stdin open so the agent can request user input mid-task (default) |
| `single_shot` | Sends the prompt and closes stdin immediately (legacy behavior) |

### 3. Start the server

In one terminal, start the tern server:

```bash
$ ./bin/tern --config ./settings/demo/config.yaml
tern server started and running...
```

The server exposes:
* CAWA Agent Service on port `3100` (configurable via `agent_service.port`)
* LLM Gateway on port `14000` (configurable via `llm_gateway.port`)

### 4. Run a task with ternctl

Open another terminal and interact with the server:

```bash
# Check server health
$ ./bin/ternctl health

# List available agents and models
$ ./bin/ternctl agents
$ ./bin/ternctl models

# Run a coding task
$ ./bin/ternctl run \
    --agent claudecode \
    --prompt "Analyze the current directory structure and create a summary report in REPORT.md" \
    --work-dir ./tmp
```

When the task completes, ternctl outputs session details as JSON:

```json
{
  "agent_name": "claudecode",
  "id": "a95db64cb646901efb395a18d817a37d",
  "status": "completed",
  "work_dir": "tmp"
}
```

Session `status` values include `active`, `suspended` (waiting for user input), `completed`, `error`, and `closed`. Check status with `ternctl session --id <session-id>`.

### 5. Continue an existing session

Use `--resume` with the session `id` from the previous output to continue the conversation:

```bash
$ ./bin/ternctl run \
    --resume a95db64cb646901efb395a18d817a37d \
    --prompt "Add a table of contents to REPORT.md"
```

The agent resumes the previous session with full context of the prior conversation.

### 6. Or use the Go client library

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100", client.WithNoTimeout())
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "claudecode",
    WorkDir: ".",
})
defer session.Terminate(ctx)

// 1. Text-only message (convenience method)
stream, _ := session.SendText(ctx, "Create hello.txt")
stream.Output(os.Stdout)

// 2. Multimodal message with image (convenience method)
stream, _ = session.SendImageFile(ctx, "screenshot.png", "What is in this screenshot?")
stream.Output(os.Stdout)

// 3. Interactive message with user-input callback
_ = session.SendTextWithHandlers(ctx, "Refactor auth.go", client.StreamHandlers{
    OnText: func(text string) { fmt.Print(text) },
    OnToolUse: func(toolName string, toolInput map[string]any) {
        fmt.Printf("tool=%s input=%v\n", toolName, toolInput)
    },
    OnUserInputRequired: func(ev client.UserInputRequiredEvent) (string, error) {
        return "Proceed with the safer option", nil
    },
})

// 4. Switch coding agent on the same Tern session (do not Terminate)
_, _ = session.UpdateAgent(ctx, "codex")
stream, _ = session.SendText(ctx, "Continue with the previous findings")
stream.Output(os.Stdout)

// Persist a supplement strategy, or override it for one turn.
full := "full"
_, _ = session.Update(ctx, client.UpdateSessionRequest{
    Supplement: &client.SupplementStrategy{Algorithm: full},
})
_, _ = session.UpdateModel(ctx, "gpt-4o")
_, _ = session.SendMessageWithOpts(ctx, []client.ContentPart{{Type: "text", Text: "Summarize so far"}}, client.SendMessageOpts{
    Supplement: &client.SupplementStrategy{Algorithm: "structured"},
})
```

---
