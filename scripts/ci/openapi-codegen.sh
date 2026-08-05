#!/bin/bash
# OpenAPI SDK Generation Script
# Generates client SDKs from the OpenAPI specification
set -euo pipefail

SPEC_FILE="internal/router/openapi.yaml"
OUTPUT_DIR="./sdk"

echo "=== VigilAgent SDK Generator ==="

# Check for openapi-generator-cli
if ! command -v openapi-generator-cli &> /dev/null; then
    echo "Installing openapi-generator-cli..."
    npm install -g @openapitools/openapi-generator-cli
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Generate Go client
echo "Generating Go client SDK..."
openapi-generator-cli generate \
    -i "$SPEC_FILE" \
    -g go \
    -o "$OUTPUT_DIR/go" \
    --additional-properties=packageName=vigilagent,generateInterfaces=true,isGoSubmodule=true

# Generate TypeScript client
echo "Generating TypeScript client SDK..."
openapi-generator-cli generate \
    -i "$SPEC_FILE" \
    -g typescript-axios \
    -o "$OUTPUT_DIR/typescript" \
    --additional-properties=npmName=@vigilagent/client,supportsES6=true

# Generate Python client
echo "Generating Python client SDK..."
openapi-generator-cli generate \
    -i "$SPEC_FILE" \
    -g python \
    -o "$OUTPUT_DIR/python" \
    --additional-properties=packageName=vigilagent,projectName=vigilagent

echo "=== SDK Generation Complete ==="
echo "Generated SDKs in $OUTPUT_DIR/"
ls -la "$OUTPUT_DIR/"
