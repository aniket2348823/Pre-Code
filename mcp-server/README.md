# VigilAgent MCP Server (`vigilagent-mcp`)

The VigilAgent **MCP (Model Context Protocol)** server exposes VigilAgent's
verification pipeline as tools that any MCP client can call:

| Tool | What it does |
|---|---|
| `vigil_verify` | Full Shift-Zero verification pipeline (findings + confidence + fixed code) |
| `vigil_scan` | Deterministic static analysis only (fast, no LLM cost) |
| `vigil_review` | Parallel specialized LLM reviewers (Security Architect, Staff Engineer, …) |
| `vigil_confidence` | Calibrated confidence grade (A–F) for code |
| `vigil_process` | Middleware pipeline: scan, requirements, compliance, patterns |
| `vigil_dual_engine` | Parallel deterministic + LLM analysis with corroboration scoring |
| `vigil_improve` | Improve AI-generated code via the dual-engine pipeline |

This npm package is a thin launcher around the prebuilt Go binary (the real
server lives in `cmd/mcp`). It downloads the correct binary for your platform
on install, so **`npx vigilagent-mcp` just works** — no Go toolchain needed.

## Quick start

```bash
# Run once, on demand (nothing to install globally):
npx -y vigilagent-mcp

# Or install as a project dependency:
npm install vigilagent-mcp
```

Requirements: Node ≥ 16 (for the launcher) and an environment variable:

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `VIGILAGENT_API_KEY` | **yes** | — | Backend API key (`va_...`) |
| `VIGILAGENT_API_URL` | no | `http://localhost:8080` | VigilAgent backend URL |
| `VIGILAGENT_LLM_KEY` | no | — | Your LLM provider key for BYOK review |
| `VIGILAGENT_MCP_BINARY` | no | — | Point the launcher at a specific binary path |
| `VIGILAGENT_MCP_VERSION` | no | `latest` | Pin the binary release tag (e.g. `v0.0.1`) |

> The MCP server **requires a running VigilAgent backend** (default
> `http://localhost:8080`). Start it with `make run` (or your hosted instance)
> and set `VIGILAGENT_API_KEY` to a valid backend key.

## Claude Desktop

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "vigilagent": {
      "command": "npx",
      "args": ["-y", "vigilagent-mcp"],
      "env": {
        "VIGILAGENT_API_KEY": "va_your_backend_key",
        "VIGILAGENT_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

## Cursor

Cursor Settings → MCP → Add new server:

```json
{
  "mcpServers": {
    "vigilagent": {
      "command": "npx",
      "args": ["-y", "vigilagent-mcp"],
      "env": {
        "VIGILAGENT_API_KEY": "va_your_backend_key",
        "VIGILAGENT_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

Or via the CLI:

```bash
cursor --mcp-server vigilagent --command npx --args "-y vigilagent-mcp"
```

## Other MCP clients (Cline, Roo Code, …)

Any client that supports stdio MCP servers accepts:

```
npx -y vigilagent-mcp
```

## Building from source (no prebuilt binary yet)

Until the first release is published, the postinstall can't download a binary.
Build it yourself and point the launcher at it:

```bash
# From the vigilagent repo root:
./scripts/build-mcp-binaries.sh            # all platforms → dist/
# or just the current platform:
cd mcp-server && npm run build             # → mcp-server/vendor/<os>-<arch>/

# Point the launcher at a custom build:
export VIGILAGENT_MCP_BINARY=/path/to/vigilagent-mcp
npx -y vigilagent-mcp
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `Prebuilt binary not found` | Run `npm rebuild vigilagent-mcp`, or build from source and set `VIGILAGENT_MCP_BINARY` |
| `VIGILAGENT_API_KEY ... required` | Set the `VIGILAGENT_API_KEY` env var (stderr only — stdout is MCP) |
| Tools error with backend connection refused | Start the backend (`make run`) or fix `VIGILAGENT_API_URL` |

All logs go to **stderr**; stdout carries the MCP protocol and must stay clean.
