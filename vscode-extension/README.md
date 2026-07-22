# VigilAgent VS Code Extension

Verify AI-generated code with VigilAgent's Shift-Zero pipeline directly from VS Code.

## Features

- **@vigilagent chat participant** — Type `@vigilagent` in the chat to verify code
- **Deterministic scanning** — Run static analysis without LLM costs
- **Full verification pipeline** — Security, architecture, compliance, cost, and red team analysis
- **Confidence scoring** — Calibrated grade (A-F) for code quality
- **Inline diagnostics** — Findings shown as warnings/errors in the editor
- **Status bar** — Real-time confidence score display

## Setup

1. Install the extension
2. Run `VigilAgent: Configure API Keys` from the Command Palette
3. Enter your VigilAgent backend API key (`va_...`)
4. Select your LLM provider and enter the API key

## Usage

### Chat Commands

| Command | Description |
|---------|-------------|
| `@vigilagent scan` | Run deterministic scan on current file |
| `@vigilagent verify` | Run full Shift-Zero verification |
| `@vigilagent help` | Show available commands |

### Commands

| Command | Description |
|---------|-------------|
| `VigilAgent: Configure API Keys` | Set up API keys |
| `VigilAgent: Scan Current File` | Scan the active file |
| `VigilAgent: Verify Selected Code` | Verify selected code |

## Architecture

```
VS Code Extension (TypeScript)
    │
    ├── Chat Participant (@vigilagent)
    │   └── User types prompt → Extension calls VigilAgent backend
    │
    ├── Commands (scan, verify)
    │   └── Active editor code → VigilAgent backend API
    │
    └── Status Bar
        └── Shows confidence score from last verification

VigilAgent Backend (Go)
    │
    ├── POST /api/v1/review (full pipeline)
    ├── POST /api/v1/middleware/process (scan only)
    └── SSE streaming for real-time results
```

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `vigilagent.backendUrl` | `http://localhost:8080` | Backend URL |
| `vigilagent.defaultLanguage` | `auto` | Default language for scans |
| `vigilagent.autoVerify` | `true` | Auto-verify LLM responses |
| `vigilagent.showConfidenceBadge` | `true` | Show confidence in status bar |

## Requirements

- VS Code 1.100+
- VigilAgent backend running (or use hosted version)
- API key from VigilAgent dashboard
