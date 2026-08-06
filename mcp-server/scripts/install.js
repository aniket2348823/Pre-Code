#!/usr/bin/env node
'use strict';

/**
 * Postinstall: fetch the prebuilt VigilAgent MCP binary for this platform
 * into vendor/<os>-<arch>/vigilagent-mcp(.exe).
 *
 * Download sources, in order:
 *   1. $VIGILAGENT_MCP_BINARY   — explicit path (already built)
 *   2. GitHub Releases           — vigilagent/vigilagent releases
 *   3. (fallback)                — leave a helpful message; never fail install
 *
 * The download URL resolves to the latest release by default; pin a specific
 * version with VIGILAGENT_MCP_VERSION (e.g. "v0.0.1").
 */

const fs = require('fs');
const path = require('path');
const https = require('https');

const REPO = 'vigilagent/vigilagent';
const VERSION = process.env.VIGILAGENT_MCP_VERSION || 'latest';

function log(msg) {
  console.error(`[vigilagent-mcp] ${msg}`);
}

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

function fetch(url, redirectsLeft) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: { 'User-Agent': 'vigilagent-mcp-installer' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location && redirectsLeft > 0) {
        res.resume(); // drain
        resolve(fetch(res.headers.location, redirectsLeft - 1));
        return;
      }
      if (res.statusCode !== 200) {
        res.resume();
        reject(new Error(`HTTP ${res.statusCode}`));
        return;
      }
      resolve(res);
    });
    req.on('error', reject);
  });
}

async function main() {
  if (process.env.VIGILAGENT_MCP_BINARY) {
    log('VIGILAGENT_MCP_BINARY set; skipping prebuilt download.');
    return;
  }

  const t = triple();
  const exe = process.platform === 'win32' ? '.exe' : '';
  const dir = path.join(__dirname, '..', 'vendor', t);
  const dest = path.join(dir, `vigilagent-mcp${exe}`);

  if (fs.existsSync(dest)) {
    log(`Binary already present: ${dest}`);
    return;
  }

  fs.mkdirSync(dir, { recursive: true });

  const url = `https://github.com/${REPO}/releases/${VERSION}/download/vigilagent-mcp-${t}${exe}`;
  try {
    log(`Downloading ${url}`);
    const res = await fetch(url, 3);
    const tmp = dest + '.tmp';
    await new Promise((resolve, reject) => {
      const out = fs.createWriteStream(tmp);
      res.pipe(out);
      out.on('finish', resolve);
      out.on('error', reject);
    });
    fs.renameSync(tmp, dest);
    if (process.platform !== 'win32') {
      try { fs.chmodSync(dest, 0o755); } catch (_) { /* ignore */ }
    }
    log(`Installed binary: ${dest}`);
  } catch (err) {
    try { fs.unlinkSync(dest + '.tmp'); } catch (_) { /* ignore */ }
    log(`Could not download prebuilt binary (${err.message}).`);
    log('This usually means no release binaries have been published yet.');
    log('Fix: build it yourself (see README) and set VIGILAGENT_MCP_BINARY to the binary path.');
  }
}

main().catch(() => process.exit(0)); // never fail `npm install`
