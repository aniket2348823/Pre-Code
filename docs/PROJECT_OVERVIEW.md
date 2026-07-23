# VigilAgent: Comprehensive Full-System Architecture & Project Overview

This document provides a massive, end-to-end breakdown of everything that currently exists within the VigilAgent repository. It covers the core foundational engines, the various clients and servers built to interact with it, the features merged from the `sgupta-100/VigilAgent---Precode` branch, and the latest auto-verification ecosystem.

---

## 1. Executive Summary

VigilAgent is an AI-assisted code verification platform built on a deterministic static-analysis engine combined with parallel LLM-based reviewers. It introduces a **"Shift-Zero"** pipeline that intercepts, verifies, and corrects AI-generated code before a developer even reads it. 

The project has evolved from a simple API into a fully robust ecosystem comprising multiple server binaries, integrations, CLI tools, and background processing systems. The entire repository currently achieves a **100% clean build** and **100% passing test suite** across all 60+ packages.

---

## 2. The Core Architecture: Shift-Zero & Deterministic Engines

At the heart of the backend (`cmd/api`) is the verification engine, orchestrated in `internal/pipeline/review.go`.

### The 9-Stage Shift-Zero Pipeline
1. **Main LLM Generation**: Initial code is generated or accepted via payload.
2. **Deterministic Engine**: Runs a strict 5-layer static analysis (Syntax, Semantic, Security, Quality, Complexity).
3. **Parallel LLM Reviewers**: Spawns multiple specialized AI subagents (Security, Architecture, Performance, Compliance, Cost) to challenge the code concurrently.
4. **Evidence Engine**: Aggregates findings from both the static engine and the LLMs.
5. **Knowledge Graph Validation**: Validates the code against established project context and historical patterns.
6. **Skill Extraction**: Identifies and logs reusable coding patterns.
7. **Attack Graph Generation**: Identifies potential exploitation paths based on security findings.
8. **Confidence Scoring**: Computes a final calibrated letter grade (A-F) based on accumulated evidence.
9. **Re-Validation Loop**: If critical issues are found, the pipeline automatically forces the AI to fix them (up to 2 retries) before final delivery.

### Background Systems
* **Webhook Engine (`internal/webhook/`)**: Handles external event routing, allowing VigilAgent to receive and process events asynchronously.
* **Batch Processing & Queueing (`internal/queue/`, `internal/batch/`)**: Handles high-volume workloads using NATS JetStream, processing requests concurrently in the background.
* **Server-Sent Events (SSE) (`internal/sse/`)**: Allows real-time streaming of review progress and findings to clients.

---

## 3. The Client Ecosystem: How VigilAgent is Accessed

VigilAgent provides multiple ways for developers and AI agents to access the verification engine.

### A. The Model Context Protocol (MCP) Server (`cmd/mcp/`)
VigilAgent acts as an MCP server, allowing modern AI assistants (like Claude Desktop, Cursor, and Cline) to directly invoke VigilAgent's capabilities as "tools".
* **`vigil_verify`**: Triggers the full Shift-Zero pipeline (Deterministic + LLM reviewers).
* **`vigil_scan`**: Triggers a fast, deterministic-only scan without incurring additional LLM costs.
* **`vigil_review`**: Triggers a contextual review.
* **`vigil_confidence`**: Requests a confidence score calculation.
* **Integration**: AI assistants discover these tools and automatically call them when generating or auditing code.

### B. The VSCode Extension (`vscode-extension/`)
A fully-featured TypeScript extension that embeds VigilAgent directly into the IDE.
* **Chat Participant (`@vigilagent`)**: Developers can open the VSCode chat and type `@vigilagent scan` or `@vigilagent verify` to explicitly analyze code in their editor.
* **Status Bar UI**: Displays the current connection status and verification grades.
* **API Client (`src/client.ts`)**: Strongly typed TypeScript integration with the Go backend.

### C. The Command Line Interface (CLI) (`cmd/cli/`)
A dedicated terminal tool for running VigilAgent pipelines directly from the command line or CI/CD systems.
* Allows developers to run scans on local files.
* Can be easily hooked into Git pre-commit hooks to prevent unverified AI code from being committed.

---

## 4. The Merge: Integration of the `sgupta` Branch

The integration of the `sgupta` branch brought in massive enterprise-grade architectural enhancements, though it required extensive conflict resolution.

### What Was Merged & Fixed
* **Extended API Handlers**: Massive additions to `internal/router/extended_handlers.go`, exposing endpoints for skills extraction, knowledge graphs, and complex confidence scoring.
* **Advanced Middleware Stack**: We integrated and fixed routing for security logging, audit trails, idempotency keys, JWT scopes, and CSRF protection.
* **Universal LLM Router**: Replaced basic routing with support for a vast array of providers: Anthropic, Gemini, OpenRouter, Mistral, Groq, NVIDIA NIM, and Cohere.
* **Conflict Resolution**: We successfully repaired all routing sequence bugs (`protected.Post` panics), fixed the MCP server's type assertion bugs (`map[string]interface{}` parsing), and rewrote broken config validation schemas to ensure a 100% clean compilation. Dozens of redeclared test functions and missing imports were systematically resolved.

---

## 5. Post-Merge Enhancements: The Auto-Verification Ecosystem

While the MCP server and CLI are powerful, they require the developer or the AI to *explicitly* call VigilAgent. To make the Shift-Zero pipeline truly invisible, we built three new client-side integrations that **automatically intercept AI output**.

### A. The LLM Proxy Gateway (`cmd/proxy/`)
* **What it is**: An OpenAI-compatible HTTP proxy server written in Go.
* **How it works**: Developers point their AI tools (e.g., Cursor, custom scripts) to `http://localhost:9090`. The proxy forwards the request to the real LLM (OpenAI, Anthropic, etc.), captures the streamed response, extracts any generated code, sends it to VigilAgent for analysis, and appends the VigilAgent Confidence Badge directly into the AI's chat response.

### B. VSCode Auto-Verify (Enhancement)
* **What it is**: A massive upgrade to the existing VSCode extension.
* **How it works**: It silently listens to `onDidChangeTextDocument` events in the editor. When it detects a large bulk insertion of code (characteristic of AI generation or Copilot acceptance), it automatically triggers a background VigilAgent scan. The findings are surfaced directly in the editor as native VSCode squiggly lines (inline diagnostics).

### C. Browser Extension (`browser-extension/`)
* **What it is**: A Chrome extension (Manifest V3) for users interacting with AI via web browsers.
* **How it works**: It uses a `MutationObserver` to watch the DOM on `chatgpt.com`, `claude.ai`, and `gemini.google.com`. When a code block renders on the page, the extension extracts the code, queries the local VigilAgent backend, and overlays a sleek, color-coded confidence badge directly onto the code block UI.

---

## 6. Project Architecture Summary

The repository structure now reflects a highly mature Go project:

```text
├── cmd/
│   ├── api/      # Main REST server (Shift-Zero pipeline)
│   ├── cli/      # Terminal CLI tool
│   ├── mcp/      # Model Context Protocol server (for Cursor/Claude)
│   ├── proxy/    # Auto-intercept LLM proxy gateway
│   ├── migrate/  # Database migration runner
│   └── bench/    # Benchmarking suite
├── internal/
│   ├── pipeline/ # The 9-stage verification engine
│   ├── router/   # HTTP Handlers and API layer
│   ├── webhook/  # Webhook ingestion engine
│   ├── queue/    # NATS JetStream batch processing
│   ├── proxy/    # Proxy gateway logic
│   └── mcp/      # MCP tool definitions
├── vscode-extension/ # The IDE integration (Chat + Auto-Verify)
└── browser-extension/ # The Web UI integration
```

---

## 7. Getting Started

**1. Start the VigilAgent Backend**
```bash
go run ./cmd/api
```

**2. Start the MCP Server (for Claude Desktop / Cursor)**
```bash
go run ./cmd/mcp
```

**3. Start the LLM Proxy Gateway**
```bash
export VIGILAGENT_API_KEY="your-api-key"
export OPENAI_API_KEY="sk-..."
go run ./cmd/proxy
```

All systems are fully operational and ready for use.
