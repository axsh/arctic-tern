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

Rebuild after pulling changes (old binaries still call deprecated `/v1/realtime/sessions`).
`voice_demo` is its **own Go module** (`features/voice_demo/go.mod`), so build from that directory (or use `./scripts/process/build.sh`):

```bash
# from repo root
go build -C features/voice_demo -o ../../bin/voice_demo .

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

## Turn-end wait (VAD)

UI knobs map to Realtime `server_vad` and apply on **next Start / Realtime connect** (not live mid-call):

| Control | Default | Effect |
| :--- | ---: | :--- |
| Silence end wait (ms) | 900 | Longer = wait through mid-utterance pauses before the model replies |
| Sensitivity threshold | 0.5 | Higher = less sensitive to quiet speech |
| Prefix padding (ms) | 300 | Audio kept before detected speech start |

Presets: **Responsive (500ms)** vs **Patient (1200ms)**. Applied values show under the controls and in the transcript (`realtime> vad ...`).

## Manual scenarios

1. Start call — Realtime panel becomes connected (or unavailable); CAWA becomes idle; `vad-applied` shows clamped values.
2. Speak a short greeting — transcript shows `you>`; **assistant voice must be audible** after barge-in (if silent, check that barge-in softens volume but does not leave `<audio>` paused).
3. Compare silence 300 vs 1200 (End call, change slider, Start again) — short pauses should trigger replies less often at 1200.
4. Long-task button — CAWA busy; Realtime may stay unavailable.
5. Barge-in while busy — CAWA last_action=`interrupt`; compare abrupt vs natural audio; assistant voice must resume after the next Realtime response.

## Environment

| Variable / flag | Meaning |
| :--- | :--- |
| `TERN_URL` / `-tern-url` | CAWA base URL (required), demo default `http://127.0.0.1:3100` |
| `TERN_AGENT` / `-agent` | Agent name (default `wayfinder`) |
| `TERN_WORK_DIR` / `-work-dir` | Work directory (default temp) |
| `OPENAI_API_KEY` | Realtime ephemeral minting (optional) |
| `-listen` | Demo HTTP listen addr (default `:8787`) |
