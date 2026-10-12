# Use Cases

Tern is a lightweight, embeddable toolkit for Coding Agents. It is designed to be pulled into an application for a momentary task, not to force a heavy always-on multi-tenant control plane.

## Designed for embed and on-demand use

You can embed Tern in-process (or run the small server binary) when you need an agent, then tear it down when you are done. Containers are optional; Tern does not require them. Short-lived use is a natural fit, but short-lived operation is not mandatory—you may keep sessions or run Tern as a service when that is convenient. Even in service mode, Tern stays much more lightweight than a dedicated Coding Agent common-processing platform centered on always-on service deployment.

Because Coding Agents tend to consume significant memory, starting them only when needed keeps resource use practical. Tern does not need to stay resident between tasks.

## Continuity when you need it

Sessions are continuous: you can resume work, follow streams after disconnects, and switch Coding Agents without throwing away the surrounding workflow. Cross-agent continuity is a first-class capability, not an afterthought.

## Casual agent tooling

The intended tone is a casual agent tool: easy to embed, easy to start, easy to stop, with enough session and interoperability surface to grow into longer workflows when required.

## Additional scenarios

These scenarios remain useful and are supported as capabilities (not the primary product pitch):

### Maintain existing workflows

Organizations often standardize on a Coding Agent while allowing flexibility in model selection.

Examples include:

* Claude Code with hosted Anthropic models
* Claude Code with private Anthropic-compatible deployments
* Claude Code with local inference infrastructure

### Evaluate new models

Teams should be able to experiment with new models without rebuilding integrations.

Examples include:

* GPT-5 → GPT-OSS
* Gemini → Gemma
* Hosted → Local
* Cloud → Private

### Reduce migration costs

The cost of changing tools often exceeds the cost of adopting them. Tern aims to reduce that cost by providing common interfaces and portable context.

### Future: context-preserving agent migration

A long-term goal is movement between Coding Agents while preserving context and workflow continuity:

```text
Claude Code
      ↓
    Codex
      ↓
 Gemini CLI
```

without requiring developers to start over.

## Vision

We envision a future where:

* Agents can be embedded and run on demand without a heavy always-on platform
* Sessions remain continuous across reconnects and agent switches
* Context is portable
* Coding Agents and Language Models are interchangeable when you need them
* Local and cloud deployments coexist
* Vendor lock-in becomes optional

Tern is being built to support that future.
