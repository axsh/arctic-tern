# voice_demo (Example)

Browser demo that pairs **OpenAI Realtime** (conversation audio) with **Tern CAWA** (Coding Agent execution).

This is **not** the production Voice Mediator. It is a commented Example so you can feel low-latency voice, see which layer is active, and compare **natural** vs **abrupt** barge-in audio.

## What you need (two processes)

`voice_demo` does **not** start Tern by itself.

```text
Terminal A:  ./bin/tern          → CAWA at http://127.0.0.1:3100
Terminal B:  voice_demo          → UI at     http://127.0.0.1:8787
```

## 1. Start Tern (CAWA)

```bash
./scripts/process/build.sh
export TERN_VAULT_OPENAI_DEFAULT="sk-..."        # if needed for agents
export TERN_VAULT_ANTHROPIC_PRIMARY="sk-ant-..." # if needed
./bin/tern --config ./settings/demo/config.yaml
```

Default CAWA URL: `http://127.0.0.1:3100` (`agent_service.port`).

```bash
curl -s http://127.0.0.1:3100/health
./bin/ternctl agents
```

## 2. Start voice_demo

Rebuild after pulling changes (old binaries still call deprecated `/v1/realtime/sessions`):

```bash
go build -o bin/voice_demo ./features/voice_demo

export TERN_URL="http://127.0.0.1:3100"
export TERN_AGENT="wayfinder"
export OPENAI_API_KEY="sk-..."   # optional; without it use long-task button

./bin/voice_demo -listen :8787
```

Open `http://127.0.0.1:8787`.

Realtime uses `POST /v1/realtime/client_secrets` then browser `POST /v1/realtime/calls`.

## Where to look: OpenAI vs Tern

| Panel | Meaning |
| :--- | :--- |
| **OpenAI Realtime** | Voice conversation. `disconnected` / `connecting` / `connected` / `speaking` / `unavailable` |
| **Tern CAWA** | Coding Agent. `no session` / `idle` / `busy`, plus last action (`messages` / `interrupt`) |

If Realtime is `unavailable` but you click **Send long-task text**, only the Tern panel should move to `busy`. That proves Tern works without OpenAI voice.

## Barge-in modes

| Mode | Assistant audio on user barge-in | CAWA when busy |
| :--- | :--- | :--- |
| **natural** (default) | soft VAD (`interrupt_response=false`) + short fade | immediate `interrupt` |
| **abrupt** | hard cut (`interrupt_response=true`, no fade) | immediate `interrupt` |

Compare by ear: abrupt first, then natural. CAWA `last_action` should still show `interrupt` when the agent was busy.

## Manual scenarios

1. Start call — Realtime panel becomes connected (or unavailable); CAWA becomes idle.
2. Long-task button — CAWA busy; Realtime may stay unavailable.
3. Barge-in while busy — CAWA last_action=`interrupt`; compare abrupt vs natural audio.

## Environment

| Variable / flag | Meaning |
| :--- | :--- |
| `TERN_URL` / `-tern-url` | CAWA base URL (required), demo default `http://127.0.0.1:3100` |
| `TERN_AGENT` / `-agent` | Agent name (default `wayfinder`) |
| `TERN_WORK_DIR` / `-work-dir` | Work directory (default temp) |
| `OPENAI_API_KEY` | Realtime ephemeral minting (optional) |
| `-listen` | Demo HTTP listen addr (default `:8787`) |
