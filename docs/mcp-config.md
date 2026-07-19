# MCP Bridge Configuration

This repo exposes awareness context to agents through a separate MCP bridge process:

- gRPC server: `go run ./golang/server ...`
- MCP bridge: `go run ./cmd/awareness-mcp -awareness-addr ...`

## Standard local run

Terminal 1:

```bash
sensei serve -no-seed &
sensei build
```

Terminal 2:

```bash
go run ./cmd/awareness-mcp -awareness-addr localhost:10120
```

## Automated (recommended)

`sensei init --mcp` writes or merges the `sensei` MCP server into `.mcp.json` at
your repo root, resolving the `awareness-mcp` bridge path for you. It never
clobbers other servers or an existing `sensei` entry, and it removes stale
legacy `awg` / `awareness-graph` server entries so strict MCP clients do not
keep trying the old client name. It is safe to re-run.

`sensei init --mcp` also installs the built-in Sensei Architect skill unless you
explicitly opt out with `--skills=false`. The skill uses MCP tools when this
bridge is configured and falls back to equivalent `sensei` CLI commands when it
is not.

## Generic MCP config snippet

To configure it by hand, use this as a template for any MCP-capable client that
launches stdio servers:

```json
{
  "mcpServers": {
    "sensei": {
      "command": "go",
      "args": [
        "run",
        "./cmd/awareness-mcp",
        "-awareness-addr",
        "localhost:10120"
      ]
    }
  }
}
```

Notes:
- Run from repository root.
- If your client uses a different working directory, provide an absolute path.
- `-awareness-addr` points at the gRPC server (`localhost:10120` is the
  standalone default server port). The bridge also accepts a comma-separated
  fallback list and automatically retries common local fallback ports when
  given a single localhost address. `-timeout` (default `5s`) sets the
  per-request gRPC timeout.
- Query endpoint (server): `http://localhost:7878/query`
- Load endpoint (loader): `http://localhost:7878/store?default`

## Tools exposed

The bridge speaks JSON-RPC 2.0 over stdio using MCP-compatible
`Content-Length` framing and exposes eight tools. Arguments mirror the request
messages — see
[api-reference.md](./api-reference.md#mcp-bridge-awareness-mcp) for the full
argument tables.

| Tool | Required args | Common optional args |
|---|---|---|
| `awareness_briefing` | `file` **or** `task` | `depth`, `domain` |
| `awareness_impact` | `file` | `domain` |
| `awareness_preflight` | `task` | `files[]`, `mode`, `domain` |
| `awareness_edit_check` | `file`, `proposed_content` | `domain` |
| `awareness_resolve` | `class`, `id` | `domain` |
| `awareness_query` | `mode` | `file`/`id`/`class`, `limit`, `domain` |
| `awareness_metadata` | — | `domain` |
| `awareness_propose` | `kind` | `title`, `contract`, `evidence[]`, `source_files[]`, related ids |

`awareness_query` is typed/whitelisted only — there is no path to send raw
SPARQL. Under Globular, the platform MCP server withholds `awareness_query`
from agents by default and adds a composite `awareness_diagnose` tool (not part
of this standalone bridge).
