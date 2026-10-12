# Project Structure

High-level layout of the Tern repository.

## Layout

```
tern/
  client/             # Go client library (Root package)
  server/             # Server framework (Root package)
  features/           # Deployable applications
    tern/             # Main server (CAWA + LLMGP)
    ternctl/          # CLI client
    vault-cli/        # Key Vault management CLI
  shared/libs/go/     # Shared Go libraries
    codingagent/      # Coding Agent adapters
    llmgateway/       # LLM Gateway providers
    config/           # Configuration loading
    vault/            # Secret management
  settings/           # Configuration files
    example/          # Minimal config for getting started
    demo/             # Full-featured config for development
  examples/           # Working examples
    minimal-server/   # Minimal server setup
    minimal-client/   # Minimal client usage
  scripts/            # Build and test scripts
  docs/               # Documentation resources
```

Detailed API documentation and protocol specifications are planned for a future release.

---
