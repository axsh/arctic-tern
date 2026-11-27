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

Demo `settings/demo/model_profiles.yaml` sets `coding_agents.*.execution_mode: single_shot` so Codex closes stdin after the prompt (interactive without EOF hangs `codex exec -`). **Restart Tern after pulling this config change.**

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
export TERN_AGENT="wayfinder"   # or codex
export OPENAI_API_KEY="sk-..."   # optional; without it use long-task button

# Prefer a non-Temp work dir (Codex refuses helper binaries under AppData\Local\Temp)
mkdir -p tmp/voice-demo-work
export TERN_WORK_DIR="$(pwd)/tmp/voice-demo-work"

./bin/voice_demo -listen :8787
```

Open `http://127.0.0.1:8787`.

Realtime uses `POST /v1/realtime/client_secrets` then browser `POST /v1/realtime/calls`.

### work_dir and sandbox

* Default `-work-dir` is a temp folder under the OS temp tree. On Windows, Codex may refuse `CODEX_HOME` under `AppData\Local\Temp`. Set `TERN_WORK_DIR` to a writable path outside Temp (example above).
* voice_demo CreateSession uses `sandbox_mode=danger-full-access` so Codex can list/run without interactive approval gates (Example only).

## Where to look: OpenAI vs Tern

| Panel | Meaning |
| :--- | :--- |
| **OpenAI Realtime** | Voice conversation. `disconnected` / `connecting` / `connected` / `speaking` / `unavailable` |
| **Tern CAWA** | Coding Agent. `no session` / `idle` / `busy`, plus last action (`messages` / `interrupt`) |

If Realtime is `unavailable` but you click **Send long-task text**, only the Tern panel should move to `busy`. That proves Tern works without OpenAI voice.

## CAWA route mode

| Mode | Behavior |
| :--- | :--- |
| **auto** (default) | Send only work-like utterances (and work-change while busy). Skips small talk so the agent is not interrupt-starved |
| **always** | Every finalized transcript goes to CAWA (original barge-in demo) |
| **never** | Realtime only; CAWA is not called |

**Send last line to Tern** forces `route_mode=always` for the last user line.

## Context bridge

When **Context bridge** is on (default):

* **Progress** (while CAWA is busy): short notes `Tern coding agent status: …` are injected. No automatic `response.create`.
* **Turn complete** (busy → idle): a **reconsider** note is injected. It re-states the open human question (A) and the coding-agent result (C) as optional evidence, then asks Realtime to reconsider (B): answer the human (D), request more Tern work, or stay silent. A `response.create` starts that reconsider turn; it does **not** force reading out agent status. Spoken replies must answer the human, not acknowledge Tern.

## Voice / speed

UI knobs map to Realtime `audio.output.voice` and `audio.output.speed`. They apply on the **next** Realtime connect (voice cannot change mid-session after first audio).

| Control | Default | Notes |
| :--- | ---: | :--- |
| Voice | marin | Loaded from `GET /api/realtime/voices` (curated list) |
| Speed | 1.0 | Clamped 0.25–1.5; presets Slow 0.8 / Normal 1.0 / Fast 1.25 |

Applied values show under the controls (`realtime-output-applied`).

## Barge-in modes

| Mode | Assistant audio on user barge-in | CAWA when busy |
| :--- | :--- | :--- |
| **natural** (default) | soft VAD (`interrupt_response=false`) + short fade + `response.cancel` if active | immediate `interrupt` (when routed) |
| **abrupt** | hard cut (`interrupt_response=true`, no fade) | immediate `interrupt` (when routed) |

Compare by ear: abrupt first, then natural. CAWA `last_action` should still show `interrupt` when the agent was busy and the utterance was routed.

## Turn-end wait (VAD)

UI knobs map to Realtime `server_vad` and apply on **next Start / Realtime connect** (not live mid-call):

| Control | Default | Effect |
| :--- | ---: | :--- |
| Silence end wait (ms) | 900 | Longer = wait through mid-utterance pauses before the model replies |
| Sensitivity threshold | 0.5 | Higher = less sensitive to quiet speech |
| Prefix padding (ms) | 300 | Audio kept before detected speech start |

Presets: **Responsive (500ms)** vs **Patient (1200ms)**. Applied values show under the controls and in the transcript (`realtime> vad ...`).

## Manual scenarios

1. Start call — Realtime panel becomes connected (or unavailable); CAWA becomes idle; voice/speed and vad-applied show clamped values.
2. Speak a short greeting with **auto** — transcript may show `route> skip cawa`; Tern stays idle.
3. Speak a clear work request or use **Send long-task text** — CAWA busy; optional `bridge>` lines when summaries update.
4. Switch route to **always** and barge-in while busy — CAWA last_action=`interrupt`.
5. Compare abrupt vs natural audio; assistant voice must resume after the next Realtime response (resume log once per response).

## Environment

| Variable / flag | Meaning |
| :--- | :--- |
| `TERN_URL` / `-tern-url` | CAWA base URL (required), demo default `http://127.0.0.1:3100` |
| `TERN_AGENT` / `-agent` | Agent name (default `wayfinder`) |
| `TERN_WORK_DIR` / `-work-dir` | Work directory (default temp; prefer a writable path) |
| `OPENAI_API_KEY` | Realtime ephemeral minting (optional) |
| `-listen` | Demo HTTP listen addr (default `:8787`) |
