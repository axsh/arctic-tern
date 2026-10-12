# Artifacts

System Artifacts track files created or modified by Coding Agents. User Artifacts hold user-uploaded data.

Tern tracks every file created or modified by Coding Agents as a **System Artifact**, and lets users upload their own data as **User Artifacts**.

System Artifacts record **path + operation** metadata (`create` / `update` / `delete`), not unified diffs. File contents are read from disk via the download API when needed.

### File change collectors (per session)

CreateSession accepts optional `file_change_collectors` to choose which collection algorithms run:

| Algorithm | Default | Role |
| :--- | :--- | :--- |
| `structured_tool` | ON | Write / Edit / Codex `file_change`, etc. |
| `shell_parser` | ON | Bash / `command_execution` command parsing (create/update only when the resolved path exists after the command; delete kept without existence check) |
| `workdir_reconcile` | **OFF** | End-of-turn git diff / snapshot supplement |

`workdir_reconcile` is off by default because it can include background-process or out-of-session edits. Set it to `true` when you want maximum coverage of workdir changes.

```go
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "codex",
    WorkDir: dir,
    FileChangeCollectors: &client.FileChangeCollectors{
        WorkdirReconcile: client.BoolPtr(true), // enable Tier 3
    },
})
```

Runnable demo: [`examples/file-change-collectors/`](../examples/file-change-collectors/).

### Explicitly registering system artifacts

In addition to automatic collectors, you can append System Artifact events explicitly.
This is useful when you want to register files that were changed outside Tier 1/2/3 detection.

```go
session, _ := c.CreateSession(ctx, client.SessionRequest{
    Agent:   "codex",
    WorkDir: dir,
})

// create/update decision is made by the server (upsert semantics).
created, _ := c.SystemArtifacts().Put(ctx, "reports/output.txt", client.SystemArtifactWriteRequest{
    SessionID: session.ID,
    // Optional: if omitted, server resolves work_dir + key.
    ActualPath: filepath.Join(dir, "reports", "output.txt"),
})
fmt.Println(created.Status, created.Operation) // created create

updated, _ := c.SystemArtifacts().Put(ctx, "reports/output.txt", client.SystemArtifactWriteRequest{
    SessionID: session.ID,
})
fmt.Println(updated.Status, updated.Operation) // updated update

deleted, _ := c.SystemArtifacts().Delete(ctx, "reports/output.txt", client.SystemArtifactWriteRequest{
    SessionID: session.ID,
})
fmt.Println(deleted.Status, deleted.Operation) // deleted delete

// Low-level append API with explicit operation:
_, _ = c.SystemArtifacts().AppendEvent(ctx, client.SystemArtifactAppendRequest{
    Key:       "reports/manual.txt",
    Operation: "create",
    SessionID: session.ID,
    ActualPath: filepath.Join(dir, "reports", "manual.txt"),
})
```

### Listing system artifacts (files written by agents)

```go
import client "github.com/axsh/arctic-tern/client/v1"

c := client.New("http://localhost:3100")

// List all Go files written in a specific session.
// When PerPage is omitted, the server defaults to 100 (safety limit).
// Explicit PerPage values are honored without a hard maximum.
page, _ := c.SystemArtifacts().List(ctx, client.SystemArtifactFilter{
    Q:          "**/*.go",
    SessionIDs: []string{"sess-abc123"},
    PerPage:    200,
})
for _, item := range page.Items {
    fmt.Printf("%s  op=%s  session=%s\n", item.Key, item.Operation, item.SessionID)
}

// Or collect every matching page in one call:
items, _ := c.SystemArtifacts().ListAll(ctx, client.SystemArtifactFilter{
    SessionIDs: []string{"sess-abc123"},
})
_ = items

// Turn-scoped list: get only files from one SendMessage turn.
turnOnly, _ := c.SystemArtifacts().List(ctx, client.SystemArtifactFilter{
    SessionIDs: []string{"sess-abc123"},
    TurnIDs:    []string{"turn-123"},
})
_ = turnOnly
```

### Downloading a single file

```go
rc, _ := c.SystemArtifacts().Download(ctx, "internal/handler.go")
defer rc.Close()
io.Copy(os.Stdout, rc)
```

### Downloading multiple files as a ZIP archive

```go
rc, _ := c.SystemArtifacts().Archive(ctx, client.ArchiveRequest{
    Q: "**/*.go", // all Go files across all sessions
})
defer rc.Close()
os.WriteFile("artifacts.zip", readAll(rc), 0o644)
```

### Uploading a user artifact

```go
// Upload from an io.Reader.
resp, _ := c.UserArtifacts().Put(ctx, "datasets/train.csv", file)
fmt.Println(resp.SHA)

// Or upload from a local file path.
resp, _ = c.UserArtifacts().PutFile(ctx, "configs/prompt.yaml", "/path/to/prompt.yaml")
```

### Retrieving a user artifact (from a Coding Agent via MCP)

When an MCP server is configured, Coding Agents can access user artifacts directly:

```
# MCP tool: get_user_artifact
# key: datasets/train.csv
```

The agent receives the raw file content as a tool result, without needing filesystem access.

### Runnable Example: artifact-pipeline

The `examples/artifact-pipeline/` directory contains a complete runnable example
demonstrating the full artifact lifecycle end-to-end:

```bash
# 1. Start the tern server (in a separate terminal)
cd examples/minimal-server && go run . -config ../../settings/example/config.yaml

# 2. Prepare a sample input file
echo "Name: Alice\nRole: Engineer\nSkills: Go, Docker" > input.txt

# 3. Run the pipeline: upload -> agent generate -> download
cd examples/artifact-pipeline
go run . --input ../input.txt --key inputs/profile.txt --output-key output.txt
# [Step 1] Uploaded: inputs/profile.txt (42 bytes, sha256: ..., status: created)
# [Step 2] Artifact content fetched: 42 bytes
# [Step 2] Session created: sess-XXXXX
# [Step 2] Agent response: ...
# [Step 3] Generated files in session sess-XXXXX (1 total):
#   output.txt  (create)
# [Step 3] Downloaded: output.txt -> ./output.txt (128 bytes)
```

Additional flags: `--agent`, `--model`, `--server`, `--work-dir`, `--save`.

---
