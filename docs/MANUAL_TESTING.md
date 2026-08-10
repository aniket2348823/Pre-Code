# Manual Testing Guide — Suggestion Mode

This guide walks you through testing the new **suggestion-based review flow**
end to end:

- **5 reviewer roles in 1 LLM call** (`security`, `architecture`, `compliance`,
  `cost`, `red_team`) returning a strict JSON contract
- **Line-anchored accept/reject suggestions** — the engine never auto-modifies code
- The **MCP `vigil_suggest` tool** (the crucial surface) + the VS Code quick fixes

---

## 0. Prerequisites

- Go ≥ 1.26.5 (`GOTOOLCHAIN=auto` will fetch it if your local Go is older)
- Docker (Postgres + Redis for the backend) — see `docker-compose.dev.yml`
- An LLM provider API key of your choice (OpenAI `sk-...`, Anthropic `sk-ant-...`,
  Gemini `AIza...`, Groq `gsk_...`, Mistral, Cohere, OpenRouter `sk-or-...`,
  NVIDIA NIM `nvapi-...`)

---

## 1. Start the backend

```bash
docker-compose -f docker-compose.dev.yml up -d     # Postgres + Redis
go run ./cmd/migrate up                            # schema
go run ./cmd/api                                   # API on :8080
```

### Get an API key (once)

```bash
# 1. Register (password min 12 chars)
curl -s -X POST localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"test-password-123","name":"You"}'

# 2. Login → capture the JWT
curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"test-password-123"}'
# → {"token":"<JWT>"}

# 3. Create an API key (replace <JWT>)
#
# NOTE: state-changing protected routes require a CSRF token for JWT bearer
# clients (curl/CLI). API-key clients (va_...) are exempt. Get a token first:
CSRF=$(curl -s http://localhost:8080/api/v1/csrf | python -c 'import sys,json;print(json.load(sys.stdin)["csrf_token"])')
curl -s -X POST localhost:8080/api/v1/api-keys \
  -H "Authorization: Bearer <JWT>" \
  -H "X-CSRF-Token: $CSRF" \
  -H 'Content-Type: application/json' \
  -d '{"name":"manual-test","scopes":["scan:write","analytics:read"]}'
# → {"key":"va_...","prefix":"va_",...}   ← save this, it is shown once
```

---

## 2. Test the review API in suggestion mode

Send AI-generated code with `"suggestion_mode": true`. Your LLM key travels in
the `X-LLM-Key` header (BYOK — the backend never sees your provider key beyond
this request):

```bash
curl -s -X POST localhost:8080/api/v1/review \
  -H "Authorization: Bearer va_..." \
  -H "X-LLM-Key: sk-..." \
  -H 'Content-Type: application/json' \
  -d '{
    "code": "import sqlite3\n\ndef get_user(db, user_id):\n    cur = db.execute(\"SELECT * FROM users WHERE id = \" + user_id)\n    return cur.fetchone()\n",
    "language": "python",
    "prompt": "fetch a user by id",
    "suggestion_mode": true
  }' | python -m json.tool
```

**Expect** (this is the contract the extension + MCP consume):

```json
{
  "reviewers": [ { "name": "security", "role": "Principal Security Architect", "verdict": "fail", ... }, ... ],
  "suggestions": [
    {
      "id": "security:4:4",
      "role": "security",
      "severity": "critical",
      "line_start": 4,
      "line_end": 4,
      "message": "SQL injection: string concatenation with user input",
      "replacement": "cur = db.execute(\"SELECT * FROM users WHERE id = ?\", (user_id,))",
      "confidence": 0.9,
      "corroborated": true
    }
  ],
  "summary": "Suggestions: N line-anchored fixes (accept or reject each)."
}
```

Key assertions:
- `suggestions[].line_start/line_end` point at the exact vulnerable line
- `replacement` contains the fixed line(s) — ready to swap in
- `corroborated: true` when the deterministic engine flagged the same area
- `summary` lists the suggestion count (auto-rewrite is OFF in this mode)
- `reviewers` still carries all 5 role verdicts, now produced by **one** LLM call

---

## 3. Test the MCP server (the crucial surface)

### Build & run the MCP server

```bash
# Terminal 1 — backend must be running (see §1)
export VIGILAGENT_API_KEY=va_...          # backend API key
export VIGILAGENT_API_URL=http://localhost:8080
export VIGILAGENT_LLM_KEY=sk-...          # optional: BYOK default
go run ./cmd/mcp
# logs to stderr; stdio carries the MCP protocol
```

### Option A — Claude Desktop / Cursor / Cline

Add to `claude_desktop_config.json` (or the equivalent for your MCP client):

```json
{
  "mcpServers": {
    "vigilagent": {
      "command": "vigilagent-mcp",
      "env": {
        "VIGILAGENT_API_KEY": "va_...",
        "VIGILAGENT_API_URL": "http://localhost:8080",
        "VIGILAGENT_LLM_KEY": "sk-..."
      }
    }
  }
}
```

Then ask: *"use vigil_suggest to review this AI-generated code:
`<paste code>`"* — you should get a numbered list of suggestions with line
ranges, severity, role, and the exact replacement text in a diff block.

### Option B — quick Node script (no GUI client)

```bash
npm init -y && npm i @modelcontextprotocol/sdk
```

```js
const { Client } = require('@modelcontextprotocol/sdk/client/index.js');
const { StdioClientTransport } = require('@modelcontextprotocol/sdk/client/stdio.js');
const { spawn } = require('child_process');

async function main() {
  const transport = new StdioClientTransport({
    command: 'go', args: ['run', './cmd/mcp'],
    env: { ...process.env, VIGILAGENT_API_KEY: 'va_...', VIGILAGENT_API_URL: 'http://localhost:8080' },
    stderr: 'pipe'
  });
  const client = new Client({ name: 'manual-tester', version: '1.0.0' });
  await client.connect(transport);

  const tools = await client.listTools();
  console.log('tools:', tools.tools.map(t => t.name).join(', ')); // expect vigil_suggest

  const res = await client.callTool({
    name: 'vigil_suggest',
    arguments: {
      code: 'import sqlite3\n\ndef get_user(db, user_id):\n    cur = db.execute("SELECT * FROM users WHERE id = " + user_id)\n    return cur.fetchone()\n',
      language: 'python',
      prompt: 'fetch a user by id'
    }
  });
  console.log(res.content[0].text); // markdown report with line ranges + replacements
  await client.close();
}
main().catch(console.error);
```

Expected output shape (from `formatSuggestionsSummary`):

```
🛡️ VigilAgent Suggestions (accept or reject each)

Confidence: **C** (0.6)

## N line-anchored suggestions

1. 🔴 **[critical]** — security, lines 4–4 (✓ corroborated by deterministic engine)
   SQL injection: string concatenation with user input
   ```diff
   cur = db.execute("SELECT * FROM users WHERE id = ?", (user_id,))
   ```
```

---

## 4. Test the VS Code extension quick fixes

1. `cd vscode-extension && npm install`
2. Open the folder in VS Code and press **F5** (Extension Development Host)
3. Run **VigilAgent: Configure API Keys** → choose **Local development**, pick your
   LLM provider, enter its API key and model
4. Open a Python/JS file, select a vulnerable snippet (e.g. the SQL injection
   above), run **VigilAgent: Verify Selected Code**
5. **Expect:**
   - Squiggles on the flagged lines (from `suggestions` in the review response)
   - Hover/lightbulb on those lines → **⚡ VigilAgent: Apply fix (security)** and
     **🗑 VigilAgent: Dismiss suggestion**
   - `Apply fix` replaces **only those lines** with `replacement`
   - `Dismiss` removes that suggestion until the next verify
   - A results webview with the full review report

> The engine never edits your file — every change is your explicit choice per line.

---

## 4.5 Test the proxy verdict headers (in-flight diversion)

Start the proxy gateway and point any OpenAI-compatible client at it. The proxy
forwards to your LLM provider, runs the dual-engine analysis on code blocks, and
returns an **advisory verdict** the client can read — it never blocks traffic.

```bash
# Terminal 1 — backend must be running (see §1)
export VIGILAGENT_API_KEY=va_...              # proxy auth key (also sent to backend)
export VIGILAGENT_BACKEND_URL=http://localhost:8080
export OPENAI_API_KEY=sk-...                  # or any provider key
GOTOOLCHAIN=auto go run ./cmd/proxy           # proxy on :9090
```

```bash
curl -si -X POST localhost:9090/v1/chat/completions \
  -H "X-API-Key: va_..." \
  -H "X-VigilAgent-Mode: auto" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"write a python function to fetch a user: import sqlite3\ndef get_user(db, user_id):\n    cur = db.execute(f\"SELECT * FROM users WHERE id = {user_id}\")\n    return cur.fetchone()"}]}'
```

**Expect** on the response (non-streaming):

```
X-VigilAgent-Verdict: block
X-VigilAgent-Grade: F
X-VigilAgent-Score: 50
X-VigilAgent-Findings: 2
X-VigilAgent-Corroborated: 1
```

and a `"vigilagent"` object in the JSON body with the same fields. For
streaming requests the verdict is prefixed to the final analysis chunk
(`🛡️ VigilAgent verdict: BLOCK (grade F, 50/100)`).

> Verdict semantics (advisory): `block` = any critical/high finding,
> `warn` = any medium finding or score < 70, `pass` = otherwise. Clients decide
> whether to enforce it — the proxy itself never drops or alters traffic.

## 4.6 Test with the vulnerability fixture corpus

The corpus lives in `testdata/fixtures/` (`manifest.json` = golden
expectations). Every vulnerable snippet is shaped like real AI-generated code:

| Fixture | Expected finding |
|---|---|
| `vulnerable/python_sqli_fstring.py` | `python_sql_injection` (critical) |
| `vulnerable/python_cmd_injection.py` | `python_command_injection` (critical) |
| `vulnerable/python_ssrf.py` | `python_ssrf` (high) |
| `vulnerable/python_path_traversal.py` | `python_path_traversal` (high) |
| `vulnerable/go_sqli_concat.go` | `sql_injection` (critical) |
| `vulnerable/go_hardcoded_secret.go` | `hardcoded_password` (critical) |
| `vulnerable/js_xss_innerhtml.js` | `xss_unsafe_js` (medium) |

Golden check (deterministic engine must flag each vulnerable fixture and leave
the `clean/` fixtures alone — via the CLI, no LLM required):

```bash
GOTOOLCHAIN=auto go run ./cmd/cli scan --path testdata/fixtures --fail-on high,critical
# → exits non-zero listing findings for every vulnerable/ fixture; clean/ stays quiet
```

**Manual UX test:** paste any vulnerable fixture into a VS Code file (with
`vigilagent.autoVerify` on) → expect squiggles on the exact vulnerable line and
the **⚡ Apply fix / 🗑 Dismiss** quick fixes within ~2 seconds of the paste.

## 4.7 Test the gateway (spec Phase 1: policy modes, provenance, /v1/responses, design gate)

The proxy at `:9090` is the **Secure AI Gateway**. It speaks the OpenAI API
(`/v1/chat/completions`, `/v1/messages`, `/v1/responses`) and adds a
scan-and-release layer on every generation.

### Policy modes (observe → balanced → strict)

Set `X-VigilAgent-Mode` on the request:

```bash
# 1) OBSERVE (default, advisory — the verdict is reported, never enforced)
curl -s http://localhost:9090/v1/chat/completions \
  -H "X-API-Key: proxy-key" -H "X-VigilAgent-Mode: observe" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Write a SQL query with SELECT * FROM users WHERE id = " + user_input"}]}'
# → 200; body has "vigilagent": {"verdict":"block","policy":"hold_for_review","scan_id":"..."}

# 2) STRICT — a block verdict means NO output is released (HTTP 451)
curl -s -o /dev/null -w '%{http_code}' http://localhost:9090/v1/chat/completions \
  -H "X-API-Key: proxy-key" -H "X-VigilAgent-Mode: strict" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Generate code with a hardcoded password: \"hunter2secret\""}]}'
# → 451 + JSON body with decision=block, findings, scan_id, signed provenance

# 3) BALANCED — prose flows, fenced code blocks are withheld on a held review
curl -s http://localhost:9090/v1/chat/completions \
  -H "X-API-Key: proxy-key" -H "X-VigilAgent-Mode: balanced" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"..."}]}'
```

### Provenance & audit (verified → signed → verifiable)

Every analyzed response carries `X-VigilAgent-Scan-ID`,
`X-VigilAgent-Provenance: verified`, `X-VigilAgent-Provenance-Signature`, and
a `provenance: {record, signature}` object in the body. Verify it later:

```bash
SCAN_ID=<from X-VigilAgent-Scan-ID>
SIG=<from X-VigilAgent-Provenance-Signature>
curl -s http://localhost:9090/v1/provenance?scan_id=$SCAN_ID -H "X-API-Key: proxy-key"        # fetch record
curl -s http://localhost:9090/v1/provenance/verify -H "X-API-Key: proxy-key" \
  -H 'Content-Type: application/json' -d "{\"scan_id\":\"$SCAN_ID\",\"signature\":\"$SIG\"}"  # → {"valid":true}
curl -s http://localhost:9090/v1/provenance/verify -H "X-API-Key: proxy-key" \
  -H 'Content-Type: application/json' -d "{\"scan_id\":\"$SCAN_ID\",\"signature\":\"tampered\"}"  # → {"valid":false}
# Create a signed attestation for externally-scanned content:
curl -s http://localhost:9090/v1/provenance/attest -H "X-API-Key: proxy-key" \
  -H 'Content-Type: application/json' -d '{"provider":"openai","model":"gpt-4o","decision":"allow","response_hash":"<sha256-hex>"}'
```

### OpenAI Responses API

```bash
curl -s http://localhost:9090/v1/responses -H "X-API-Key: proxy-key" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","input":"Write a hello world in Go"}'
# → Responses-shaped body: {"object":"response","output":[{..."type":"message"...}],"vigilagent":{...}}
```

### Design-stage gate (scan the prompt BEFORE generation)

Send a prompt that embeds a secret or a command; the gateway appends
policy-mandated secure constraints to the provider request and reports it:

```bash
curl -s http://localhost:9090/v1/chat/completions -H "X-API-Key: proxy-key" \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Design a config system with api_key: \"sk-1234567890abc\" embedded"}]}'
# → X-VigilAgent-Design-Gate: constrained + body.design_gate {status:constrained, findings:N}
```

## 5. Unit tests covering this flow

```bash
GOTOOLCHAIN=auto go test ./internal/pipeline ./internal/mcp ./internal/router ./internal/proxy ./internal/signing
```

Highlights:
- `TestParseSingleCallReviewContent` / `TestRunSingleCallReviewers_SingleCall` —
  proves all 5 roles come from **one** LLM call (call count == 1)
- `TestRunSingleCallReviewers_FallbackOnMalformed` — unparseable model output
  degrades to the parallel path instead of failing
- `TestBuildSuggestions_Corroboration` — deterministic + LLM agreement boosts
  confidence and marks `corroborated`
- `TestRunSuggestionMode_ReturnsSuggestions` — the invariant: in suggestion mode
  the auto-rewrite loop never runs (`Retries == 0`, `FinalOutput` untouched)
- `TestHandleSuggest` / `TestFormatSuggestionsSummary` — MCP tool contract
- `go run ./cmd/cli scan --path testdata/fixtures --fail-on high,critical` — deterministic-engine golden corpus check (flags every `vulnerable/` fixture, leaves `clean/` alone)
- `TestComputeVerdict_*` / `TestApplyAnalysisHeaders` — proxy verdict matrix + headers
- `TestComputePolicy_Matrix` / `TestEnforcePolicy` — observe/balanced/strict decision table
- `TestRedactCodeBlocks` — balanced-mode withholding
- `TestProvenanceAttestGetVerify` / `TestProvenanceVerifyFullRecord` — signed record lifecycle + tamper rejection
- `TestApplyDesignGate_*` — design-stage constraints appended for risky prompts, skipped for clean ones
- `TestResponsesEndpoint*` / `TestParseResponsesInput` — Responses API contract
- `TestSignProvenanceAndVerify` / `TestVerifyProvenanceTampered` — signing round-trip + tamper detection
- `TestHandleValidateGeneratedDiff` / `TestHandleVerifyProvenance` / `TestHandleCreateScanAttestation` — spec MCP tools

## 4.8 End-to-end: gateway chat → insert into file → inline suggestions

This is the full controlled-client loop (spec section E): code generated through
the VigilAgent chat model gets scanned at the gateway, and when you insert it
into an editor the AutoVerifier recognizes it and scans the inserted region
again for line-anchored suggestions — even when the insert is a small block
below the generic 2-line threshold.

### Prerequisites

1. Backend + gateway running (see §4.7).
2. Extension built: `cd vscode-extension && npm run compile`.
3. In VS Code, run **VigilAgent: Configure API Keys** (local mode or a backend key),
   and set `vigilagent.gatewayUrl` to `http://localhost:9090` in settings.

### The flow

1. Open the chat view (`Ctrl+Alt+I`), pick the **VigilAgent** vendor from the
   model dropdown — the list comes from the gateway's `/v1/models` catalog.
2. Ask something like: *"Write a Python function that looks up a user by name
   and returns their profile"* — the kind of prompt that tempts SQL string
   concatenation.
3. Watch the response stream in. It ends with the provenance footer:
   `🛡️ [VigilAgent] provenance: verified · scan resp_… · design gate: passed`.
4. Open a `.py` file and use **Insert into File** on the code block (or copy it
   and paste). The editor change fires the AutoVerifier, which matches the
   inserted text against the gateway-output registry and scans that exact
   region — squiggles land on the vulnerable lines with **Apply fix** /
   **Dismiss** quick fixes.
5. Open the **Security Findings** view in the Explorer sidebar: the scan record
   shows `grade … · verified` and the gateway scan id, and clicking a finding
   jumps to its line.

### What proves the loop works

- The chat model list is empty without the gateway running (models come from
  `/v1/models`).
- Inserting code that was NOT gateway-generated still scans (generic
  classifier), but its provenance label is `unverified` — the registry match is
  what upgrades it to `verified`.
- Small inserts (1–3 lines) that the generic classifier would ignore still get
  scanned when they match a registered gateway block.

### Troubleshooting

- **No model in the picker** → gateway down or `vigilagent.gatewayUrl` wrong.
- **No squiggles after insert** → check `vigilagent.autoVerify` is on, and look
  at the Output panel (`VigilAgent` channel) for AutoVerifier errors.
- **Blocked response** → the gateway returned a policy block; the chat shows
  `VigilAgent blocked the response: …` and nothing was inserted.
