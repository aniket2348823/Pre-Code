# Organization Migration Checklist

> Migrating the VigilAgent repo from a **personal GitHub account** to a
> **GitHub organization** unlocks team access, real `CODEOWNERS` teams,
> org-level security policies (rulesets, code scanning defaults, audit log),
> and SAML/SSO — the standard shape for a product that ships CI-enforced
> security scanning.

Current state: repo `aniket2348823/Pre-Code` on a personal account, with
branch protection **already live on `main`** (7 required checks, strict mode,
enforce-admins — see `docs/DEPLOYMENT.md` → *Branch Protection*).

---

## 1. Why migrate to an org

| Capability | Personal repo | Organization |
|---|---|---|
| Team-based access (read/write/admin per team) | ❌ | ✅ |
| `CODEOWNERS` with team handles (`@org/team`) | ❌ (teams need an org) | ✅ |
| Org-level repository rulesets (branch protection at scale) | ❌ | ✅ |
| Org secrets shared across repos | ❌ | ✅ |
| SAML/SSO + SCIM | ❌ | ✅ |
| Audit log of org actions | ❌ | ✅ |
| Code scanning / secret scanning defaults applied to all repos | ❌ | ✅ |

---

## 2. Pre-migration checklist (do BEFORE transferring)

- [ ] **Rotate secrets that may have leaked.** The `.env` (with the NVIDIA
      NIM key and Supabase pooler password) is **not** tracked — only
      `.env.example` is committed. Still, rotate the NVIDIA key and Supabase
      password before going public: paste the new values into `.env` and the
      GitHub Actions secrets.
- [ ] **Create the org first** — `https://github.com/organizations/new`
      (suggested handle: `vigilagent-org` or `vigilagent`). Choose the plan;
      free orgs support teams, rulesets and required checks.
- [ ] **Decide visibility.** If the repo will become public, scrub history
      with `git filter-repo` / BFG for any keys ever committed (check with
      `git log -p --all -S 'nvapi-'` first).
- [ ] **Inventory the required checks** (they transfer with the repo, but
      verify after — step 4):
      `VigilAgent Policy Scan`, `Static Analysis (SAST)`,
      `Container Security Scan`, `Dynamic Analysis (DAST)`,
      `Dependency Security Audit`, `Run Tests & Verification`,
      `Typecheck, Lint & Package VSIX`.
- [ ] **Fix CODEOWNERS before enabling code-owner review.** The repo's
      `.github/CODEOWNERS` references `@vigilagent/*` teams that do not exist
      yet. Decide the team names now (step 4 creates them), e.g.
      `@vigilagent/security`, `@vigilagent/maintainers`, `@vigilagent/core`.
- [ ] **Note hardcoded repo references** to update after transfer (grep for
      the old `owner/repo`): README badge links, docs URLs. Today only
      `README.md:2` matches.

---

## 3. Transfer the repository

### Via UI (recommended)

1. Repo → **Settings** → **General** → scroll to **Danger Zone**.
2. **Transfer ownership** → select the new org → type the repo name to
   confirm → **I understand, transfer this repository**.

### Via API (scriptable)

```bash
curl -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  https://api.github.com/repos/aniket2348823/Pre-Code/transfer \
  -d '{"new_owner":"vigilagent-org"}'
```

### Update local clones afterwards

```bash
git remote set-url origin git@github.com:vigilagent-org/Pre-Code.git
git remote -v   # verify
```

> Branch protection, required checks, and settings **transfer with the repo**.
> GitHub redirects the old `aniket2348823/Pre-Code` URL to the new location.

---

## 4. Post-transfer checklist (first week)

### 4.1 Re-verify branch protection

```bash
export GITHUB_TOKEN=ghp_...            # PAT with repo admin scope
REPO=vigilagent-org/Pre-Code ./scripts/ci/setup-branch-protection.sh
```

The script is idempotent — it converges to the declared configuration on the
new owner path.

### 4.2 Create teams + enable code-owner review

1. Org → **People** → **Teams** → create:
   - `@vigilagent/security` — admin of `internal/scanner`, `internal/proxy`
   - `@vigilagent/maintainers` — repo admin, owns release process
   - `@vigilagent/core` — write access, owns `internal/`, `cmd/`
2. Update `.github/CODEOWNERS` so each team matches the handles you created.
3. Enable code-owner review (teams now exist):

```bash
CODE_OWNER_REVIEW=true REPO=vigilagent-org/Pre-Code ./scripts/ci/setup-branch-protection.sh
```

### 4.3 Secrets: repo → org

Move every repo secret (Settings → Secrets and variables → Actions) to
**org-level secrets** so all repos under the org inherit them:

| Secret | Used by |
|---|---|
| `NVIDIA_API_KEY` / `VIGILAGENT_LLM_NVIDIA_NIM_KEY` | review pipeline / CI |
| `GITHUB_TOKEN` (fine-grained, `security-events:write`) | SARIF uploads |
| `LOADTEST_TARGET_URL` | scheduled k6 load tests |

### 4.4 Org-level security defaults

- [ ] Org → **Settings → Code security and analysis**: enable *code scanning
      defaults* (CodeQL), *secret scanning*, *push protection*, and
      *Dependabot alerts* for all repositories.
- [ ] Replace the repo branch-protection rule with an **org repository
      ruleset** (`Settings → Rulesets`) so every future repo inherits the same
      gate — same 7 required checks + enforce-admins + no force-push.
- [ ] Add `security-events: write` to the workflows' `permissions` (already
      set in `sast.yml`).

### 4.5 SAML/SSO (enterprise only)

- [ ] If the org enforces SSO, authorize the classic PAT for SSO in
      **Settings → Personal access tokens → Configure SSO**.

### 4.6 Update references

- [ ] `README.md` badge URLs: `aniket2348823/Pre-Code` → `vigilagent-org/Pre-Code`.
- [ ] Any `GITHUB_REPOSITORY`-derived config in CI (none hardcoded today).
- [ ] Local clone remotes (step 3).

### 4.7 Smoke-test a PR end to end

Open a tiny PR (e.g. a docs edit) against `main` and confirm **all 7
required checks run and pass**, then merge it. This validates:
- checks are not "Expected — waiting for status" (name mismatch would block)
- DAST runs on every PR (it is decoupled from `container-scan`)
- SARIF uploads still work (code scanning permission)

---

## 5. Verification checklist

| Item | Command / where | Expected |
|---|---|---|
| Repo owned by org | repo page | `vigilagent-org/Pre-Code` |
| Branch protection intact | `scripts/ci/setup-branch-protection.sh --repo vigilagent-org/Pre-Code` (re-run) | HTTP 200 |
| Required checks present | GET `/repos/.../branches/main/protection` | 7 contexts incl. `Dynamic Analysis (DAST)` |
| Teams exist | org People → Teams | `security`, `maintainers`, `core` |
| CODEOWNERS valid | open a PR touching `internal/scanner/` | owner team requested as reviewer |
| Code-owner review on | GET protection | `require_code_owner_reviews: true` |
| Org secrets | org Settings → Secrets | NVIDIA + load-test secrets present |
| Code scanning | Security tab | semgrep + trivy + vigilagent SARIF results |
| New PR passes | open a docs-only PR | all 7 checks green → merge allowed |

---

## 6. Rollback / notes

- Transferring back to a personal account is allowed, but org-only features
  (rulesets, org secrets, SAML) go with the org — re-verify after any
  reverse transfer.
- Classic PATs with `repo` scope work for branch protection on repos the
  account administers; after transfer, use a PAT owned by an org admin or a
  **fine-grained token** scoped to the org (Administration read/write) and
  keep it as an org secret.
- The `vigilagent-scan` job and `.zap/rules.tsv` are repo-local; org rulesets
  reference check names, so keep the check names stable across repos.
