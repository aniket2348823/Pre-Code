# CODEOWNERS — Team & Owner Guide

This guide maps every team referenced in [`.github/CODEOWNERS`](../.github/CODEOWNERS) to the real
GitHub users who should sit on it, and explains how to make the rules actually enforceable.

## Current state (verified via GitHub API, August 2026)

| Check | Result |
|---|---|
| Repository | `aniket2348823/Pre-Code` — owned by a **personal user account** (`owner.type: User`) |
| `vigilagent` organization | **Does not exist** (`GET /orgs/vigilagent` → 404) |
| All 8 teams (`@vigilagent/core`, `api-team`, `ai-team`, `infra-team`, `security-team`, `devops-team`, `frontend-team`, `docs-team`) | **Do not exist** (404) |

> **Bottom line:** every `@vigilagent/*` owner in CODEOWNERS is currently unresolvable. GitHub
> shows an "unknown owner" warning on each PR and **enforces no review requirement**. The rules
> become live only after the org, the teams, and their members exist (see
> [Making CODEOWNERS live](#making-codeowners-live)).

## The 8 teams and the paths they own

| Team | Charter | Paths owned (from `.github/CODEOWNERS`) |
|---|---|---|
| `@vigilagent/core` | Project owners; default fallback + shared utilities | `*` (default), `internal/errors/`, `internal/requirements/`, `internal/retry/` |
| `@vigilagent/api-team` | REST API surface, data layer, HTTP plumbing | `internal/router/`, `internal/api/`, `internal/middleware/`, `internal/server/`, `internal/repository/`, `internal/schema/`, `internal/requestid/`, `internal/sse/`, `internal/websocket/`, `internal/util/`, `cmd/api/`, `cmd/migrate/`, `pkg/pagination/`, `pkg/query/`, `pkg/response/`, `pkg/validation/` |
| `@vigilagent/ai-team` | Agent engine, LLM routing, MCP middleware, AI internals | `internal/agent/`, `internal/llm/`, `internal/memory/`, `internal/tools/`, `internal/skills/`, `internal/mcp/`, `internal/skillengine/`, `internal/knowledge/`, `internal/contextbuilder/`, `internal/confidence/`, `internal/critic/`, `internal/extraction/`, `internal/feedback/`, `cmd/mcp/`, `mcp-server/` |
| `@vigilagent/security-team` | Deterministic scanner, dual-engine review pipeline, AI gateway, provenance, auth, rate protection | `internal/security/`, `internal/webhook/`, `internal/scanner/`, `internal/pipeline/`, `internal/proxy/`, `cmd/proxy/`, `internal/signing/`, `internal/auth/`, `internal/audit/`, `internal/compliance/`, `internal/attackgraph/`, `internal/ipfilter/`, `internal/ratelimit/`, `internal/rateguard/` |
| `@vigilagent/infra-team` | Database, cache, queues, telemetry, packaging, deployment | `internal/database/`, `internal/config/`, `internal/cache/`, `internal/queue/`, `internal/telemetry/`, `internal/observability/`, `internal/health/`, `internal/featureflags/`, `internal/idempotency/`, `internal/compression/`, `internal/cors/`, `internal/email/`, `internal/logging/`, `internal/slogger/`, `internal/configdrift/`, `internal/cost/`, `internal/costintel/`, `internal/e2e/`, `migrations/`, `Dockerfile`, `docker-compose*.yml`, `k8s/`, `configs/`, `deploy/`, `cmd/bench/`, `cmd/loadtest/`, `cmd/healthcheck/` |
| `@vigilagent/devops-team` | CI/CD, workflows, scripts, CLI enforcement tooling | `/.github/`, `Makefile`, `scripts/`, `cmd/cli/` |
| `@vigilagent/frontend-team` | Editor / browser extensions | `browser-extension/`, `vscode-extension/` |
| `@vigilagent/docs-team` | Documentation | `docs/`, `*.md` |

## Suggested real-user membership

The project is currently a solo effort; each team's lead is the repo owner. Add future
contributors to the teams matching their role. Teams can have overlapping members.

| Team | Suggested members |
|---|---|
| `@vigilagent/core` | `@aniket2348823` (owner/lead) |
| `@vigilagent/api-team` | `@aniket2348823` (API lead) + API engineers |
| `@vigilagent/ai-team` | `@aniket2348823` (LLM/middleware lead) + ML/LLM engineers |
| `@vigilagent/security-team` | `@aniket2348823` (security lead) + security engineers |
| `@vigilagent/infra-team` | `@aniket2348823` (infra lead) + SREs/backend engineers |
| `@vigilagent/devops-team` | `@aniket2348823` (devops lead) + CI engineers |
| `@vigilagent/frontend-team` | `@aniket2348823` (extension lead) + frontend engineers |
| `@vigilagent/docs-team` | `@aniket2348823` (docs lead) + technical writers |

> Members must have a GitHub account and be added to the organization **before** they can join a team.

## Making CODEOWNERS live (step by step)

1. **Create the `vigilagent` organization** — UI only, no API:
   github.com → `+` (top right) → **New organization** → name `vigilagent`, choose a plan,
   verify your email. An org **cannot** be created via the REST API or `gh` CLI.

2. **Move the repo under the org** (teams resolve cleanly only when the org owns the repo):
   repo **Settings → Danger Zone → Transfer ownership** → `vigilagent` → confirm.
   *(Alternative: keep the repo personal and add the org as an outside collaborator, but team
   membership is only manageable when the org owns the repo.)*

3. **Create the 8 teams** — via UI (org **Settings → Teams → New team**) or API/CLI with a token
   that has `admin:org` scope (the org must also be enabled for the token's SSO, if SAML is on):

   ```bash
   gh api --method POST orgs/vigilagent/teams \
     -f name=core          -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=api-team      -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=ai-team       -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=infra-team    -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=security-team -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=devops-team   -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=frontend-team -f permission=push -f privacy=closed
   gh api --method POST orgs/vigilagent/teams \
     -f name=docs-team     -f permission=push -f privacy=closed
   ```

4. **Add members** to each team (invite them to the org first):

   ```bash
   gh api --method PUT orgs/vigilagent/teams/security-team/memberships/USERNAME -f role=member
   ```

5. **Verify**: open a test PR that touches `internal/scanner/` — the **Reviewers** sidebar should
   show `vigilagent/security-team`, and the CODEOWNERS status check should be "Approved by owner"
   once a team member reviews. Re-run the checks below to confirm resolution.

## Optional: make the rules live *today* (no org yet)

Until the org exists, append the personal account to each owner line so review requirements are
enforced against an individual instead of a phantom team:

```diff
-/internal/scanner/ @vigilagent/security-team
+/internal/scanner/ @vigilagent/security-team @aniket2348823
```

Valid owners in a line are enforced; unknown ones only produce a warning. Swap the individual
out for the team reference once step 3 is done.

## Re-check commands

```bash
# Does the org exist?
curl -s -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/orgs/vigilagent

# Does each team exist?
for t in core api-team ai-team infra-team security-team devops-team frontend-team docs-team; do
  curl -s -o /dev/null -w "$t %{http_code}\n" \
    -H "Authorization: Bearer $GITHUB_TOKEN" "https://api.github.com/orgs/vigilagent/teams/$t"
done
```

`200` = exists · `404` = does not exist (the org is missing too).
