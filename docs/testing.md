# Testing

How to run builds, unit tests, and integration tests for Tern.

```bash
# Full build + unit tests
./scripts/process/build.sh

# Integration tests (full suite; LIVE tests require Codex/Claude CLI + vault)
./scripts/process/integration_test.sh

# Required merge gate (fake Codex reconnect + real Codex LIVE)
./scripts/process/build.sh && ./scripts/process/integration_test.sh --specify "TestStreamReconnectRegression" && ./scripts/process/integration_test.sh --specify "TestLiveCodex_"
```

`TestLiveCodex_` talks to a real `codex` CLI and vault. Missing CLI or vault is a hard failure (`t.Fatal`), not a skip.

```bash
# Unit tests for shared libraries only
cd shared/libs/go
go test ./... -v
```

---
