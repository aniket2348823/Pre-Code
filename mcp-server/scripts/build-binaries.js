#!/usr/bin/env node
'use strict';

/**
 * Dev helper: build the VigilAgent MCP binary for the CURRENT platform from
 * the repo checkout (run from mcp-server/ inside the repository).
 *
 * Output: vendor/<os>-<arch>/vigilagent-mcp(.exe)
 *
 * For cross-compiling all platforms (CI/release), use
 * `scripts/build-mcp-binaries.sh` from the repo root instead.
 */

const { execSync } = require('child_process');
const path = require('path');
const fs = require('fs');

const repoRoot = path.resolve(__dirname, '..', '..');

function triple() {
  const osMap = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
  const archMap = { x64: 'amd64', arm64: 'arm64' };
  const osName = osMap[process.platform];
  const arch = archMap[process.arch];
  if (!osName || !arch) {
    console.error(`Unsupported platform/arch: ${process.platform}/${process.arch}`);
    process.exit(1);
  }
  return `${osName}-${arch}`;
}

if (!fs.existsSync(path.join(repoRoot, 'go.mod'))) {
  console.error(`Repo root not found at ${repoRoot} — run this inside the vigilagent checkout.`);
  process.exit(1);
}

const exe = process.platform === 'win32' ? '.exe' : '';
const outDir = path.join(__dirname, '..', 'vendor', triple());
fs.mkdirSync(outDir, { recursive: true });
const out = path.join(outDir, `vigilagent-mcp${exe}`);

execSync(`go build -trimpath -o "${out}" ./cmd/mcp`, { cwd: repoRoot, stdio: 'inherit' });
console.log(`Built ${out}`);
