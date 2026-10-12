<p align="center">
  <img src="docs/resources/images/tern_logo_circle_transparent.png" alt="Tern Logo" width="200">
</p>

# Tern

> Fly with the best agent. Anywhere. Anytime.

Tern is an open-source, **lightweight** toolkit for running Coding Agents and Language Models.

It is designed to be **embedded** into your application for a momentary task—spin an agent up when you need it, tear it down when you do not. Containers are optional. You can also run Tern as a small service when that fits, without adopting a heavy always-on common-processing platform.

Sessions stay continuous when you need them: resume work, reattach streams, and switch Coding Agents without discarding the surrounding workflow.

**Start here:** [Getting Started](docs/getting-started.md)

---

## Why Tern?

The Arctic Tern migrates toward better conditions rather than staying fixed in one place. Coding tooling should be able to move that lightly too.

Coding Agents are powerful and resource-hungry. Tern treats them as something you can embed and run **on-demand**, instead of committing up front to a large resident control plane. Short-lived use is a natural starting point, but it is not mandatory—keep sessions alive or run the server longer whenever you want.

When continuity matters, Tern keeps session state recoverable and lets you cross agents without rebuilding the application around a single vendor CLI.

---

## What Tern Provides

### Embeddable, on-demand runtime

Pull Tern into a process (or start the small server binary) for the task at hand. No container requirement.

### Lightweight operation

Service mode is available, but Tern stays far lighter than a dedicated always-on agent platform. Non-resident use keeps memory use practical between tasks.

### Session continuity and cross-agent workflows

Resume sessions, follow streams after disconnects, and switch Coding Agents while preserving the surrounding workflow.

### Interoperability (capability)

Context portability, agent interchangeability, and model interchangeability remain supported capabilities—useful when tools change—not the sole product pitch.

See [Use Cases](docs/use-cases.md) for more.

---

## Architecture Overview

Tern consists of three major components:

* **CAWA** — Coding Agent Web API (agent lifecycle, multimodal content, interactive turns)
* **LLMGP** — LLM Gateway Protocol (providers, vault-backed credentials, routing)
* **Integration Layer** — connects agents and models through a unified abstraction

Details: [Architecture](docs/architecture.md)

---

## Status

Tern is in early design and implementation.

Current phase: **Phase 2** — see [Roadmap](docs/roadmap.md).

Contributors, reviewers, and early adopters are welcome.

---

## Documentation

* [Documentation Hub](docs/README.md)
* [Getting Started](docs/getting-started.md) — install, vault, config, first run
* [Examples](docs/examples.md) — server, client, vault, multimodal snippets
* [Artifacts](docs/artifacts.md) — system/user artifacts and collectors
* [Use Cases](docs/use-cases.md)
* [Architecture](docs/architecture.md)
* [Web API reference](docs/reference-manual-web-apis.md)
* [Testing](docs/testing.md)
* [Project structure](docs/project-structure.md)

Related protocol notes: [SSE chunk protocol](docs/sse-chunk-protocol.md), [Client SSE tool_input](docs/client-sse-tool-input.md)

Working samples also live under [`examples/`](examples/).

---

## Contributing

Ideas, experiments, implementation feedback, and specification discussions are welcome.

Please open an issue to start a conversation.

---

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
