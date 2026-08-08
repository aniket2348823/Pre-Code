"""One-shot CI helper: summarize a semgrep --json output file (findings only)."""
import json
import sys
from collections import Counter

path = sys.argv[1] if len(sys.argv) > 1 else "semgrep-local.json"
with open(path, encoding="utf-8", errors="replace") as f:
    d = json.load(f)

rs = d.get("results", [])
print(f"TOTAL: {len(rs)}")

# Focused views
for r in rs:
    sev = r.get("extra", {}).get("severity", "?")
    chk = r.get("check_id", "").split(".")[-1]
    p = r.get("path", "?")
    s = r.get("start", {}).get("line", "?")
    lines = (r.get("extra", {}).get("lines", "") or "").strip().replace("\n", " | ")[:120]
    if sev == "ERROR" or "cookie" in chk or chk in ("tainted-sql-string", "string-formatted-query", "ssrf-injection-requests", "command-injection-os-system", "dangerous-system-call", "use-of-md5", "math-random-used"):
        print(f"[{sev}] {chk} {p}:{s} :: {lines}")
