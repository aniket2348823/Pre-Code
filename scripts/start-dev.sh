#!/usr/bin/env bash
# start-dev.sh — Start the VigilAgent API server with a CLEAN environment.
#
# WHY: internal/config's loadEnvFile only applies .env values to variables
# that are NOT already set in the OS environment. Stale VIGILAGENT_* exports
# from a previous testing session (e.g. an old VIGILAGENT_REDIS_PORT=6380 or
# VIGILAGENT_SERVER_WRITE_TIMEOUT=10s) silently override the corrected .env
# on every restart. This script unsets all inherited VIGILAGENT_* variables
# so the .env file and configs/ are the single source of truth.
#
# Usage: bash scripts/start-dev.sh   (or: make run)
# Note: requires bash (git-bash / MSYS2 on Windows, bash on Linux/macOS) —
# the Makefile `run` target already invokes this via `bash`.
set -euo pipefail

cd "$(dirname "$0")/.."

# Unset every stale VIGILAGENT_* variable inherited from the parent shell.
for v in $(env | grep '^VIGILAGENT_' | cut -d= -f1); do
  unset "$v"
  echo "  unset $v (stale)"
done

echo "Starting VigilAgent API with a clean environment (config from .env + configs/config.yaml)..."
exec go run ./cmd/api
