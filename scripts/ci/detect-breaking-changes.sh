#!/bin/bash
# Breaking Change Detection for OpenAPI Specifications
# Detects breaking changes between current and previous API spec
set -euo pipefail

SPEC_FILE="internal/router/openapi.yaml"
PREV_SPEC="${1:-}"
EXIT_CODE=0

echo "=== VigilAgent Breaking Change Detection ==="

# Check if openapi-diff is available
if ! command -v openapi-diff &> /dev/null; then
    echo "Installing openapi-diff..."
    # Download openapi-diff jar if not present
    if [ ! -f "tools/openapi-diff.jar" ]; then
        mkdir -p tools
        curl -sL "https://github.com/OpenAPITools/openapi-diff/releases/latest/download/openapi-diff.jar" \
            -o tools/openapi-diff.jar
    fi
    DIFF_CMD="java -jar tools/openapi-diff.jar"
else
    DIFF_CMD="openapi-diff"
fi

# If no previous spec provided, check git history
if [ -z "$PREV_SPEC" ]; then
    echo "No previous spec provided, checking git history..."
    PREV_COMMIT=$(git log --oneline --follow --diff-filter=M -- "$SPEC_FILE" | head -2 | tail -1 | awk '{print $1}')
    
    if [ -z "$PREV_COMMIT" ]; then
        echo "No previous version found in git history. Skipping breaking change detection."
        exit 0
    fi
    
    echo "Comparing with commit: $PREV_COMMIT"
    git show "$PREV_COMMIT:$SPEC_FILE" > /tmp/prev-spec.yaml 2>/dev/null || {
        echo "Could not extract previous spec. Skipping."
        exit 0
    }
    PREV_SPEC="/tmp/prev-spec.yaml"
fi

# Run diff
echo "Comparing API specifications..."
if $DIFF_CMD "$PREV_SPEC" "$SPEC_FILE" --fail-on-incompatible; then
    echo "✅ No breaking changes detected"
else
    EXIT_CODE=$?
    echo "⚠️  Breaking changes detected!"
    echo ""
    echo "Breaking changes include:"
    echo "  - Removed endpoints"
    echo "  - Removed required parameters"
    echo "  - Changed parameter types"
    echo "  - Removed response fields"
    echo "  - Changed response status codes"
    echo ""
    echo "Please update the API version or provide backwards compatibility."
fi

# Additional checks: verify spec is valid
echo ""
echo "=== Validating OpenAPI Specification ==="

if command -v npx &> /dev/null; then
    npx @redocly/cli lint "$SPEC_FILE" --format=stylish || true
fi

exit $EXIT_CODE
