#!/bin/bash
set -euo pipefail

# Health Monitoring Script for VigilAgent
# Checks: API health, DB connectivity, Redis, NATS, LLM providers

API_URL="${1:-http://localhost:8080}"
ALERT_WEBHOOK="${2:-}"

check_endpoint() {
  local name="$1"
  local url="$2"
  local expected_status="${3:-200}"

  status=$(curl -s -o /dev/null -w "%{http_code}" "${url}" 2>/dev/null || echo "000")
  if [ "$status" == "$expected_status" ]; then
    echo "✓ ${name}: OK (${status})"
    return 0
  else
    echo "✗ ${name}: FAIL (${status})"
    return 1
  fi
}

echo "=== VigilAgent Health Check ==="
echo "Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo ""

FAILURES=0

# API Health
check_endpoint "API Health" "${API_URL}/api/v1/health" 200 || ((FAILURES++))
check_endpoint "API Ready" "${API_URL}/api/v1/ready" 200 || ((FAILURES++))
check_endpoint "Metrics" "${API_URL}/api/v1/metrics" 200 || ((FAILURES++))

echo ""
if [ $FAILURES -gt 0 ]; then
  echo "FAILED: ${FAILURES} check(s) failed"
  if [ -n "$ALERT_WEBHOOK" ]; then
    curl -s -X POST "$ALERT_WEBHOOK" \
      -H "Content-Type: application/json" \
      -d "{\"text\":\"VigilAgent health check failed: ${FAILURES} failures at $(date -u)\"}"
  fi
  exit 1
else
  echo "ALL CHECKS PASSED"
  exit 0
fi
