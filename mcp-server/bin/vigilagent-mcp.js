#!/usr/bin/env node
'use strict';

/**
 * VigilAgent MCP server launcher.
 *
 * The MCP protocol runs over stdio, so this launcher simply execs the
 * (prebuilt Go) VigilAgent MCP binary with stdio inherited and forwards
 * termination signals. It prints nothing to stdout — stdout belongs to MCP.
 */

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');

function triple() {
  const osMap = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
  const archMap = { x64: 'amd64', arm64: 'arm64' };
  const osName = osMap[process.platform];
  const arch = archMap[process.arch];
  if (!osName || !arch) {
    throw new Error(`unsupported platform/arch: ${process.platform}/${process.arch}`);
  }
  return `${osName}-${arch}`;
}

function resolveBinary() {
  // 1. Explicit override (built locally, in CI, or a custom path).
  if (process.env.VIGILAGENT_MCP_BINARY) {
    const p = path.resolve(process.env.VIGILAGENT_MCP_BINARY);
    if (fs.existsSync(p)) return p;
    console.error(`[vigilagent-mcp] VIGILAGENT_MCP_BINARY points to a missing file: ${p}`);
    process.exit(1);
  }

  // 2. Binary installed by scripts/install.js.
  const exe = process.platform === 'win32' ? '.exe' : '';
  const bundled = path.join(__dirname, '..', 'vendor', triple(), `vigilagent-mcp${exe}`);
  if (fs.existsSync(bundled)) return bundled;

  // 3. Nothing available — explain how to fix it.
  console.error('[vigilagent-mcp] Prebuilt binary not found for ' + triple() + '.');
  console.error('[vigilagent-mcp] Fix: run `npm rebuild vigilagent-mcp` to download it,');
  console.error('[vigilagent-mcp] or build it from source and set VIGILAGENT_MCP_BINARY.');
  process.exit(1);
}

let bin;
try {
  bin = resolveBinary();
} catch (err) {
  console.error('[vigilagent-mcp] ' + err.message);
  process.exit(1);
}

const child = spawn(bin, process.argv.slice(2), { stdio: 'inherit' });

for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(sig, () => {
    try { child.kill(sig); } catch (_) { /* ignore */ }
  });
}

child.on('error', (err) => {
  console.error(`[vigilagent-mcp] Failed to start binary: ${err.message}`);
  process.exit(1);
});

child.on('close', (code, signal) => {
  if (signal) {
    try { process.kill(process.pid, signal); } catch (_) { process.exit(1); }
    return;
  }
  process.exit(code == null ? 0 : code);
});
