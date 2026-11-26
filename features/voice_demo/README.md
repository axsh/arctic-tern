# voice_demo (Example)

Browser demo that pairs **OpenAI Realtime** (conversation audio) with **Tern CAWA** (Coding Agent execution).

This is **not** the production Voice Mediator. It is a commented Example so you can feel low-latency voice and verify that barge-in uses `interrupt` (not `cancel` → `messages`).

## Prerequisites

1. A running Tern with CAWA HTTP reachable (agent-service base URL).
2. Optional for mic/speaker: `OPENAI_API_KEY` (Realtime ephemeral sessions).
3. An available Coding Agent name (default `wayfinder`).

## Run

From the repository root:

```bash
export TERN_URL="http://127.0.0.1:<agentservice-port>"
export OPENAI_API_KEY="sk-..."   # optional; without it, use the long-task button
go run ./features/voice_demo -listen :8787
```

Open `http://127.0.0.1:8787`.

`./scripts/process/build.sh` also builds `bin/voice_demo`.

## Policy (why interrupt)

| CAWA state | User utterance | Demo calls |
| :--- | :--- | :--- |
| idle | finalized text | `Session.SendText` (`messages`) |
| busy | barge-in text | `Session.Interrupt` |
| busy | — | never `CancelTurn` as the default barge-in |

## Manual scenarios

1. **Basic call** — Start call, allow mic, say a short greeting. Realtime should answer without waiting for a coding turn to finish.
2. **Idle → messages** — Ask the agent to create a small file. UI shows busy, then a short summary.
3. **Busy → interrupt (main)** — Click **Send long-task text**, wait until busy=yes, then speak a new instruction (or call `/api/utterance` again). `last_action` should be `interrupt`. Session must not become closed just from barge-in.

## Sample long task

The UI button sends:

> Please run a long shell command that sleeps for about 60 seconds, then create a file named long_task_done.txt with the text done. Narrate briefly only.

## Environment

| Variable / flag | Meaning |
| :--- | :--- |
| `TERN_URL` / `-tern-url` | CAWA base URL (required) |
| `TERN_AGENT` / `-agent` | Agent name (default `wayfinder`) |
| `TERN_WORK_DIR` / `-work-dir` | Work directory (default temp) |
| `OPENAI_API_KEY` | Realtime session minting |
| `-listen` | Demo HTTP listen addr (default `:8787`) |
