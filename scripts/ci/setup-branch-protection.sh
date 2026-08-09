#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# setup-branch-protection.sh — enforce security CI on protected branches
#
# Blocks merges to main/master unless EVERY required check passes, including:
#   - VigilAgent Policy Scan   (deterministic engine, SARIF fail gate)
#   - Static Analysis (SAST)  (vet + staticcheck + govulncheck + semgrep)
#   - Container Security Scan (trivy CRITICAL/HIGH gate)
#   - Dynamic Analysis (DAST) (ZAP baseline + nuclei against a live server)
#   - Dependency Security Audit
#   - Run Tests & Verification
#   - Typecheck, Lint & Package VSIX
#
# Uses the GitHub REST API directly (curl) — no gh CLI required.
# Idempotent: safe to re-run; always converges to the declared configuration.
#
# Usage:
#   export GITHUB_TOKEN=<PAT with repo admin scope>
#   ./scripts/ci/setup-branch-protection.sh [--repo owner/repo] [--branch main]
#   # or via env vars (works in CI without args):
#   REPO=owner/repo BRANCH=main ./scripts/ci/setup-branch-protection.sh
#
# Requirements:
#   - GITHUB_TOKEN (or GH_TOKEN) with admin:repo / repo scope
#   - jq (optional, only for nicer output)
# ─────────────────────────────────────────────────────────────────────────────
set -euo pipefail

# ── Parse CLI args (also overridable via REPO/BRANCH env vars) ───────────────
REPO="${REPO:-${GITHUB_REPOSITORY:-}}"
BRANCH="${BRANCH:-main}"
while [ $# -gt 0 ]; do
  case "$1" in
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --branch)
      BRANCH="${2:-}"
      shift 2
      ;;
    *)
      echo "❌ Unknown argument: $1" >&2
      echo "   Usage: $0 [--repo owner/repo] [--branch main]" >&2
      exit 1
      ;;
  esac
done

if [ -z "$REPO" ]; then
  # Fall back to the git remote when GITHUB_REPOSITORY is not set.
  REPO=$(git remote get-url origin 2>/dev/null | sed -E 's#(https?://[^/]+/|git@[^:]+:)##; s/\.git$//') || true
fi
if [ -z "$REPO" ]; then
  echo "❌ Cannot determine repo. Set REPO=owner/repo or run inside a clone." >&2
  exit 1
fi

TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
  echo "❌ GITHUB_TOKEN / GH_TOKEN is not set. Create a PAT with repo admin scope." >&2
  exit 1
fi

API="https://api.github.com/repos/${REPO}/branches/${BRANCH}/protection"
AUTH="Authorization: Bearer ${TOKEN}"
ACCEPT="Accept: application/vnd.github+json"
API_VERSION="X-GitHub-Api-Version: 2022-11-28"

# ── Checks that must be green before merge ───────────────────────────────────
# IMPORTANT: only jobs that run on EVERY PR belong here. Jobs that are skipped
# on PRs (deploy-only jobs, blue-green rollout, scheduled load tests) do NOT
# report "success", so requiring them would block every merge. DAST runs on
# every PR (decoupled from container-scan in sast.yml), so it is requirable.
REQUIRED_CHECKS=(
  "VigilAgent Policy Scan"
  "Static Analysis (SAST)"
  "Container Security Scan"
  "Dynamic Analysis (DAST)"
  "Dependency Security Audit"
  "Run Tests & Verification"
  "Typecheck, Lint & Package VSIX"
)

# Require code-owner review? The repo's CODEOWNERS references @vigilagent/*
# teams — on a personal repo those teams do not exist, so keep this OFF unless
# you have actually created the teams (set CODE_OWNER_REVIEW=true to enable).
CODE_OWNER_REVIEW="${CODE_OWNER_REVIEW:-false}"

echo "🔒 Applying branch protection for ${REPO} @ ${BRANCH}"
echo "   Required checks:"
for c in "${REQUIRED_CHECKS[@]}"; do
  echo "     - ${c}"
done
echo "   Code-owner review required: ${CODE_OWNER_REVIEW}"
echo

# Build the JSON payload with jq (available on ubuntu runners + most dev boxes).
# If jq is missing, fall back to a heredoc JSON (checks must still be injected).
if command -v jq >/dev/null 2>&1; then
  CONTEXTS_JSON=$(printf '%s\n' "${REQUIRED_CHECKS[@]}" | jq -R -s 'split("\n") | map(select(length > 0))')
  PAYLOAD=$(jq -n \
    --argjson contexts "$CONTEXTS_JSON" \
    --argjson reviews 1 \
    --argjson codeowner "${CODE_OWNER_REVIEW}" \
    --argjson strict true \
    --argjson enforceAdmins true \
    '{
      required_status_checks: {
        strict: $strict,
        contexts: $contexts
      },
      enforce_admins: $enforceAdmins,
      required_pull_request_reviews: {
        required_approving_review_count: $reviews,
        dismiss_stale_reviews: true,
        require_code_owner_reviews: $codeowner
      },
      restrictions: null,
      allow_force_pushes: false,
      allow_deletions: false,
      required_linear_history: false
    }')
else
  echo "⚠️  jq not found — installing fallback JSON builder" >&2
  PAYLOAD='{"required_status_checks":{"strict":true,"contexts":['
  first=1
  for c in "${REQUIRED_CHECKS[@]}"; do
    if [ "$first" -eq 1 ]; then first=0; else PAYLOAD+=','; fi
    PAYLOAD+="\"$c\""
  done
  # NOTE: the `]}` closes the contexts array AND the required_status_checks
  # object — omitting that brace nests the other top-level keys inside
  # required_status_checks, which GitHub rejects with 422.
  PAYLOAD+="]},\"enforce_admins\":true,\"required_pull_request_reviews\":{\"required_approving_review_count\":1,\"dismiss_stale_reviews\":true,\"require_code_owner_reviews\":${CODE_OWNER_REVIEW}},\"restrictions\":null,\"allow_force_pushes\":false,\"allow_deletions\":false,\"required_linear_history\":false}"
fi

echo "📤 PUT ${API}"
if [ -n "${VERBOSE:-}" ]; then
  echo "   Payload: $(echo "$PAYLOAD" | tr '\n' ' ')"
fi

HTTP_CODE=$(curl -sS -o /tmp/bp-response.json -w '%{http_code}' -X PUT \
  -H "$AUTH" \
  -H "$ACCEPT" \
  -H "$API_VERSION" \
  -H "Content-Type: application/json" \
  -d "$PAYLOAD" \
  "$API")

echo "   HTTP ${HTTP_CODE}"
case "${HTTP_CODE}" in
  200|201)
    echo "✅ Branch protection configured successfully on ${BRANCH}."
    echo "   Required status checks enforced:"
    if command -v jq >/dev/null 2>&1; then
      jq -r '.required_status_checks.contexts[] | "     - " + .' /tmp/bp-response.json
    else
      echo "     (install jq to list the applied contexts)"
    fi
    ;;
  403)
    echo "❌ Forbidden — the token needs admin:repo scope for ${REPO}." >&2
    cat /tmp/bp-response.json >&2
    exit 1
    ;;
  404)
    echo "❌ Not found — check REPO=${REPO}, BRANCH=${BRANCH}, and that you have admin access." >&2
    cat /tmp/bp-response.json >&2
    exit 1
    ;;
  *)
    echo "❌ Unexpected HTTP ${HTTP_CODE}" >&2
    cat /tmp/bp-response.json >&2
    exit 1
    ;;
esac

echo
echo "ℹ️  Also consider enabling in the repo UI:"
echo "   - 'Do not allow bypassing the above settings' (enforce_admins=true already set)"
echo "   - 'Dismiss stale pull request approvals when new commits are pushed' (enabled)"
echo "   - Set default branch to main (branch protection applies to it)"
