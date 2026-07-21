# Shift-Zero / Pre-Code — Scope Lock & Build Order (Review-Board Decision)

**Date:** 2026-07-03
**Status:** Decision doc — governs build order for the next phases.
**Method:** Engineering-review-board critique (Security Architect, Staff Eng, Cloud Architect, DevSecOps, SRE, Product, CTO, YC Partner, Enterprise CISO), synthesized to consensus. Brutal-honesty mode, per request.

---

## 0. Naming reconciliation (do this first, it's causing confusion)

Three names for one codebase:
- `README.md` → **Pre-Code**
- `CLAUDE.md` → **VigilAgent** ("AI agent management platform")
- This session's brief → **Shift-Zero** ("AI-native pre-code security platform")

**Decision:** One product name, one positioning. The strongest positioning (from the roast itself) is:

> **Pre-Code — the trust layer that makes AI-generated software safe, regardless of which model wrote it.**

Action: pick `Pre-Code` as the product name, update `CLAUDE.md` (it still describes a generic agent platform — stale). `VigilAgent` survives only as the Go module path (`github.com/vigilagent/vigilagent`); renaming the module is churn with no user value → **deferred, not now.**

---

## 1. The core problem (consensus, unanimous)

The vision is 8 products stacked in one V1:

| # | Product in the dump | Reality check |
|---|---------------------|---------------|
| 1 | AI Gateway (provider-agnostic LLM) | **BUILT** (`internal/llm`,`router`,`cost`) — the most mature part |
| 2 | Deterministic Engine (schema/rules/static analysis) | **STUB** (`internal/scanner` = regex rules, in-memory) |
| 3 | Skill Registry / Marketplace | **SKELETON** (`internal/skills` = in-memory, no persistence/ranking) |
| 4 | Knowledge Graph (GraphRAG) | **NOT BUILT** |
| 5 | Reviewer-LLM panel (security/arch/cost/compliance) | **NOT BUILT** |
| 6 | Attack Graph Engine | **NOT BUILT** |
| 7 | Threat Modeling / Digital Twin | **NOT BUILT** |
| 8 | Confidence Engine + Audit Layer | **PARTIAL** (audit/telemetry scaffolding exists) |

**Verdict:** Building all 8 now = the 95%-overengineering outcome the dump predicts. The winning move is a thin vertical slice that proves the loop end-to-end, then deepen the moat.

---

## 2. Brutal findings (ranked by impact)

**F1 — The moat is NOT the reviewer LLMs. It's the Skill Registry with evidence.** (Security Architect, CTO, YC)
Reviewer-LLM panels are copyable in a weekend by anyone with API keys. 5,000 *verified* skills (trigger→fix→verification→evidence→ranking), accumulated from real accepted fixes, are not. **Everything should serve skill accumulation.** If a feature doesn't feed the registry, it's Phase 2+.

**F2 — "Deterministic engine as truth" is half-wrong and dangerous.** (Staff Eng)
It can validate *consistency, security-control presence, compliance, static-analysis findings*. It **cannot** validate architecture *quality* (RBAC vs ABAC vs ReBAC has no deterministic winner). Scope the engine to what's actually decidable. Marketing it as "truth" invites the first embarrassing false-positive to sink credibility.

**F3 — False positives are an existential risk, not a QA detail.** (DevSecOps, Enterprise CISO)
A security tool that cries wolf gets uninstalled in week one. The **Confidence Engine is not a nice-to-have — it's a launch gate.** Every finding ships with evidence + a calibrated confidence, or it doesn't ship. This must exist in the MVP, not Phase 2.

**F4 — Real static analysis beats a custom regex scanner. Don't reinvent Semgrep.** (Staff Eng, SRE)
`internal/scanner`'s hand-rolled regex rules will lose to Semgrep/Bandit on both coverage and maintenance. Wrap the real tools; keep the custom layer only for org-specific rules Semgrep can't express. Reinventing this is the "unnecessary complexity" the dump warns against.

**F5 — Knowledge Graph is high-cost, deferrable.** (Cloud Architect)
GraphRAG is powerful but heavy (graph store, ingestion, relationship modeling). The MVP loop (detect → fix → verify → skill) works **without** it using Postgres + pgvector, which is already in the stack. Graph is Phase 2 when skill relationships actually need traversal. Building it now is premature.

**F6 — Differentiation vs incumbents is weak where you're loudest.** (YC, Product)
- vs **Semgrep/Snyk**: they own detection. You will not out-detect them → don't try; *consume* them (F4).
- vs **Pixee**: they own auto-fix PRs. Your edge is *pre-code / design-time* + the *evidence-ranked skill library*, not "we also fix."
- vs **Qodo**: they own test/code-quality gen.
- **Where you're actually differentiated:** design-time security requirement injection (before code exists) + a compounding, evidence-backed skill registry. **Market that. Drop "we improve LLMs" — it's your weakest claim** and pits you against frontier labs you can't beat.

**F7 — No RL. (Everyone agrees, including the dump.)** Preference/feedback learning only (accept/reject/modify → skill score). RL has no reward signal here (breaches surface months later). Settled.

**F8 — Agent loops / LLM-judging-LLM-judging-LLM: cut.** Expensive, slow, unstable, doubles hallucinations (dump's own point). Deterministic checks + single specialized security reviewer + human-in-loop. Max two retries. Settled.

---

## 3. Locked MVP (the only thing that matters next)

The **thinnest vertical slice that proves the whole value loop and feeds the moat:**

```
Developer prompt (VS Code)
      ↓
Requirement injection (deterministic: entity → required controls)
      ↓
LLM (provider-agnostic — ALREADY BUILT)
      ↓
Deterministic validation:  schema → security-rule → static analysis (Semgrep/Bandit)
      ↓
Confidence Engine (finding + evidence + calibrated score)   ← launch gate (F3)
      ↓ (fail → 1 specialized security-reviewer LLM → revalidate, max 2 retries)
Skill extraction: validated finding → skill (trigger/fix/verify/evidence/rank)  ← the moat (F1)
      ↓
Developer sees ranked, evidence-backed result
```

**In MVP:** provider-agnostic gateway (done), requirement injection, Semgrep/Bandit-backed deterministic engine, confidence+evidence, skill registry with persistence+ranking, one security-reviewer LLM, VS Code surface (thin).

**NOT in MVP (Phase 2+):** Knowledge Graph, Attack Graph, Threat Modeling, Digital Twin, multi-reviewer panel, multi-model consensus, marketplace *publishing/community* (registry persistence yes; public marketplace no), RL.

---

## 4. Build order (the sequence "all" actually means)

Cheap + proves value first → moat next → graph/expansion later. Each slice is `brainstorm → plan → build`, one at a time, checkpointed.

| Order | Slice | Why here | Cost |
|-------|-------|----------|------|
| **1** | **Bench harness** (already planned, T0-T5) | Closes the open thread; proves the gateway's cost story; unbreaks the build (cost import). Deterministic, ~$0. | S |
| **2** | **Deterministic Engine** — real Semgrep+Bandit + schema/security-rule layers, replacing the regex stub | The trust layer; no LLM spend; everything downstream depends on real findings (F4). | M |
| **3** | **Confidence + Evidence** on findings | Launch gate (F3); small once the engine emits structured findings. Fold into slice 2's tail or its own short slice. | S |
| **4** | **Skill Registry + persistence + ranking** — the moat | Depends on real findings existing (slice 2/3). Postgres-backed, lifecycle, ranking. No public marketplace yet. | M |
| **5+** | Knowledge Graph, Attack Graph, reviewer panel, public marketplace | Phase 2. Only after the loop above is real and false positives are under control. | L |

Requirement injection is small and can ride alongside slice 2. VS Code surface is a thin client over the API — build after slice 4 makes the loop worth surfacing, or stub earlier for demos.

---

## 5. Guardrails for every slice (non-negotiable)

- No feature ships without tests (TDD, per the plans skill).
- No finding ships without evidence + confidence.
- Consume Semgrep/Bandit; don't reinvent detection.
- Every validated finding must be capable of becoming a skill — if it can't, question the feature.
- No RL, no agent-judging loops, max two reviewer retries.
- One slice at a time; checkpoint with the user between slices.

---

## 6. Immediate next action

Proceed to **Slice 1 (bench harness)** — already specced and planned this session, ready to execute T0-T5. On completion, checkpoint, then brainstorm Slice 2 (Deterministic Engine).
