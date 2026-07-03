# Deterministic Engine (Code Static Analysis) — Design

**Date:** 2026-07-03
**Status:** Approved design
**Slice:** 2 of the Pre-Code build order (see `2026-07-03-shiftzero-scope-decision.md`).
**Governs:** refactor of `internal/scanner` into a pluggable multi-analyzer engine.

## Problem

`internal/scanner` is a single struct running ~20 hardcoded Go regex rules over raw
lines. It has **no callers and no HTTP route** yet. The decision doc's guardrails require:
consume real tools (Semgrep/Bandit) rather than reinvent detection; every finding must ship
evidence + a calibrated confidence (false positives are existential); and findings must be
skill-able. Semgrep has **no native Windows support** (Docker/WSL/Linux-CI only); Bandit is
pure-Python and installs via pip but analyzes Python only. Neither is currently installed;
Python 3.13 + pip are present.

## Scope

**In:** code static analysis — a pluggable engine that runs whatever analyzers are available,
normalizes their output into one evidence-backed, confidence-scored finding schema, dedupes
across analyzers, and degrades gracefully when a tool is absent or broken.

**Out (deferred to a later slice, noted here so scope is explicit):**
- **Schema-validation layer** and **security-rule layer** from the decision doc — these operate
  on the LLM's *architecture output* (does a payment service declare audit logs?), not on
  code, and depend on a requirement-injection format that does not exist yet.
- Skill extraction (Slice 3), HTTP route wiring, VS Code surface.

## Architecture

A finding pipeline with swappable front-ends:

```
Input{Language, Code}
      │
      ▼
Engine.Run  ──►  for each analyzer where Available():
                   builtin (regex, always on)   ─┐
                   bandit  (if on PATH)          ─┼─► []Finding (analyzer-native → normalized)
                   semgrep (if on PATH/Docker)   ─┘
      │
      ▼
merge → dedupe by Fingerprint → confidence (corroboration) → sort by severity
      │
      ▼
Report{Findings, AnalyzersRun, AnalyzersSkipped, AnalyzerErrors}
```

The engine depends only on the `Analyzer` interface. Adapters depend only on an injectable
command `Runner`, so they unit-test without the real tool installed.

## Components

Each file has one responsibility.

| File | Responsibility |
|------|----------------|
| `internal/scanner/analyzer.go` | `Analyzer` interface + `Input` type. |
| `internal/scanner/finding.go` | Unified `Finding` + `Severity` + `Report` types + `Fingerprint()`. |
| `internal/scanner/runner.go` | `Runner` interface + `ExecRunner` (wraps `exec.CommandContext`) + `toolExists`. |
| `internal/scanner/builtin.go` | The existing regex rules, wrapped as an always-available `Analyzer`. |
| `internal/scanner/bandit.go` | Bandit adapter: temp `.py` → `bandit -f json` → normalize. |
| `internal/scanner/semgrep.go` | Semgrep adapter: temp file → `semgrep --json` → normalize. |
| `internal/scanner/confidence.go` | Severity→base score + corroboration bump. |
| `internal/scanner/engine.go` | `Engine` orchestration: run/merge/dedupe/score/report. |

### Interfaces

```go
type Input struct {
    Language string // "go", "python", "javascript", … ("" = analyzer decides/skips)
    Code     string
    Filename string // optional hint; engine synthesizes a temp name if empty
}

type Analyzer interface {
    Name() string
    Available() bool
    Analyze(ctx context.Context, in Input) ([]Finding, error)
}

type Runner interface {
    Run(ctx context.Context, name string, args []string, stdin string) (stdout, stderr string, err error)
}
```

### Finding schema

```go
type Finding struct {
    RuleID      string   // stable id, e.g. "sql_injection" / "B608" / "python.lang.security..."
    Analyzers   []string // which analyzers reported it (>1 after corroboration merge)
    Severity    Severity // critical|high|medium|low|info
    Category    string   // "injection", "secrets", "crypto", …
    Title       string
    Message     string
    Filename    string
    Line        int
    Snippet     string   // evidence: the offending source line(s)
    Fix         string
    Confidence  float64  // 0..1, calibrated (see confidence.go)
    Fingerprint string   // dedupe key
}
```

## Confidence model (v1 = corroboration)

The launch-gate signal against false positives (decision-doc F3). v1 is deliberately simple
and explainable:

- **Base** from severity: critical 0.6, high 0.5, medium 0.4, low 0.3, info 0.2.
- **Analyzer weight:** real tools (bandit/semgrep) start +0.1 over the builtin regex (regex is
  the noisiest source).
- **Corroboration bump:** a finding reported by N≥2 distinct analyzers gets +0.25 (capped 0.99).
  Agreement across independent tools is the strongest cheap signal that a finding is real.
- Confidence is clamped to [0.05, 0.99] — never 0 (it was reported) and never 1.0 (nothing is
  certain; the dump's own "never say secure" rule).

Evidence for every finding = the `Snippet` plus the `Analyzers` list, so a reviewer sees both
*what* and *who flagged it*. Richer calibration (historical accept/reject rates) is a Slice-3+
concern once the skill registry records outcomes.

## Dedup

`Fingerprint = sha256(category + "|" + filename + "|" + line + "|" + normalize(snippet))[:16]`,
where `normalize` trims and collapses whitespace. Merge rule: findings with equal fingerprints
collapse into one; `Analyzers` union; keep the highest severity and the most actionable `Fix`
(first non-empty); recompute confidence with the corroboration bump.

## Error handling & graceful degradation

- `Available()` false → analyzer is listed in `Report.AnalyzersSkipped` with a reason
  ("bandit not on PATH"). No silent gaps.
- An available analyzer that returns an error (tool crash, non-zero exit that isn't "findings
  found", unparseable JSON) → recorded in `Report.AnalyzerErrors[name]`; other analyzers still
  run. One broken tool never fails the whole scan.
- Bandit/Semgrep signal "findings present" via a non-zero exit code by design; the adapter
  treats documented finding-exit-codes as success and only real failures as errors.
- Temp files are written to `os.MkdirTemp` and always cleaned up (`defer os.RemoveAll`).

## Testing

All pass on this Windows box with no external tools installed:

- **builtin_test.go** — known-vuln Go snippets (SQLi, hardcoded secret, MD5) → expected RuleIDs.
- **bandit_test.go / semgrep_test.go** — inject a fake `Runner` returning canned tool JSON;
  assert native→`Finding` normalization (severity map, rule id, line, snippet). Assert
  `Available()` reflects a stubbed `toolExists`.
- **engine_test.go** — two fake analyzers flagging the same line → one merged finding, both
  analyzers listed, confidence bumped; one analyzer returning an error → recorded in
  `AnalyzerErrors`, the other's findings still present; an unavailable analyzer → in
  `AnalyzersSkipped`.
- **confidence_test.go** — base-by-severity, real-tool weight, corroboration bump, clamps.
- **integration (build-tagged / t.Skip):** if real `bandit` is on PATH, analyze a real Python
  snippet end-to-end. Skipped otherwise.

## Backward compatibility

`Scanner`, `Scan`, `ScanResult`, `Vulnerability` have no callers → removed and replaced by the
new API. The package stays `scanner`. The 20 regex patterns are preserved verbatim inside
`builtin.go`.
