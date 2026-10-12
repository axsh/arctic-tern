# Architecture

Tern is organized around CAWA, LLMGP, and an integration layer that connects Coding Agents to Language Models.

Tern consists of three major components.

### CAWA

Coding Agent Web API.

CAWA defines a common interface for Coding Agents. The v1 API uses structured content blocks supporting both text-only and multimodal inputs (text + images). It also supports interactive execution: agents can request user input mid-task via `user_input_required` events, and clients reply through the `respond` endpoint.

### LLMGP

LLM Gateway Protocol.

LLMGP defines a common interface for Language Models and inference backends.

### Integration Layer

The Integration Layer connects Coding Agents and Language Models through a unified abstraction.

Additional architectural details will be documented separately.

---

## Design Documents

Tern is built around two core protocols:

* **CAWA (Coding Agent Web API)** -- A REST/WebSocket API that abstracts Coding Agent lifecycle and communication. Agents register themselves via Go `init()` imports, making it simple to add support for new agents.

* **LLMGP (LLM Gateway Protocol)** -- A reverse-proxy layer that routes LLM requests to configured providers (OpenAI, Anthropic, Google, Ollama). API keys are managed through a secure vault with support for keyring, environment variables, and encrypted storage.

Full protocol specifications are being developed and will be published as the project matures.

---

## Related documents

* [Web API reference](reference-manual-web-apis.md)
* [SSE chunk protocol](sse-chunk-protocol.md)
* [Client SSE tool_input](client-sse-tool-input.md)
