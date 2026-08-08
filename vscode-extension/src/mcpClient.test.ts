import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { EventEmitter } from 'events';

// The fake binary path must pass the existence check in start().
vi.mock('fs', () => ({
    existsSync: (p: string) => p === 'C:/fake/vigilagent-mcp.exe',
}));

// ── Test doubles ────────────────────────────────────────────────────────────
// The client spawns the MCP binary; for unit tests we inject a fake child
// process that auto-responds to JSON-RPC messages like the real server.

// Fake extension context whose secret storage returns a key — the client
// needs it to populate VIGILAGENT_API_KEY before spawning the MCP server.
function fakeContext(storedKey = 'local-dev'): { secrets: { get: (k: string) => Promise<string | undefined> } } {
    return {
        secrets: {
            get: async (k: string) => (k === 'vigilagent.apiKey' ? storedKey : undefined),
        },
    };
}

interface IncomingMsg {
    jsonrpc: '2.0';
    id?: number;
    method?: string;
    params?: Record<string, unknown>;
}

class FakeProc extends EventEmitter {
    // env captured from the spawn() call so the fake can decide whether the
    // client is using the local-dev fallback key.
    env: NodeJS.ProcessEnv = {};
    // When true, fire the 'exit' event asynchronously on kill() — simulating
    // the real OS delivering SIGTERM-exit after the caller has moved on.
    delayedExitOnKill = false;
    stdin = {
        writable: true,
        write: (data: string) => {
            const msg = JSON.parse(data) as IncomingMsg;
            this.respond(msg);
            return true;
        },
    };
    stdout = new EventEmitter();
    stderr = new EventEmitter();
    killed = false;
    kill = vi.fn(() => {
        this.killed = true;
        if (this.delayedExitOnKill) {
            // Deliver the exit event AFTER any respawn happens — reproduces
            // the stale-proc-exit race from the 401 heal path.
            setTimeout(() => this.emit('exit', 0, null), 25);
        }
    });

    private respond(msg: IncomingMsg): void {
        if (msg.method === 'initialize') {
            this.emitResponse(msg.id!, {
                protocolVersion: '2024-11-05',
                serverInfo: { name: 'vigilagent', version: '1.0.0' },
                capabilities: { tools: {} },
            });
        } else if (msg.method === 'tools/call') {
            const args = (msg.params as { arguments?: Record<string, unknown> })?.arguments || {};
            if (args.fail || args.code === 'FAIL') {
                this.emitResponse(msg.id!, {
                    isError: true,
                    content: [{ type: 'text', text: 'backend 500' }],
                });
            } else if (this.env.VIGILAGENT_API_KEY !== 'local-dev') {
                // Simulate the real backend rejecting a stale/unknown key.
                this.emitResponse(msg.id!, {
                    isError: true,
                    content: [{ type: 'text', text: 'VigilAgent scan failed: backend returned 401: {"code":"AUTH_003","message":"invalid or expired token"}' }],
                });
            } else {
                this.emitResponse(msg.id!, {
                    content: [{
                        type: 'text',
                        text: JSON.stringify({ scan_result: { findings: [{ severity: 'high', message: 'x' }] } }),
                    }],
                });
            }
        }
    }

    private emitResponse(id: number, result: unknown): void {
        setTimeout(() => {
            this.stdout.emit('data', Buffer.from(JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n'));
        }, 0);
    }
}

// vi.mock is hoisted above the class definition, so the shared fake instance
// must live in a hoisted container that the mock factory can reach.
const hoisted = vi.hoisted(() => ({ fakeProc: undefined as unknown as FakeProc }));

vi.mock('child_process', () => ({
    spawn: vi.fn((_bin: string, _args: string[], opts: { env?: NodeJS.ProcessEnv }) => {
        hoisted.fakeProc.env = opts?.env || {};
        return hoisted.fakeProc;
    }),
}));

// The real vscode module only exists inside the extension host — tests run
// outside it, so provide a minimal stub for the surface mcpClient touches
// (workspace config lookup in resolveBinary).
vi.mock('vscode', () => ({
    workspace: {
        getConfiguration: () => ({ get: () => '' }),
        workspaceFolders: undefined,
    },
    window: {
        showInformationMessage: () => Promise.resolve(undefined),
    },
}));

import { spawn } from 'child_process';

beforeEach(() => {
    hoisted.fakeProc = new FakeProc();
    vi.mocked(spawn).mockClear();
});

afterEach(() => {
    vi.restoreAllMocks();
});

describe('VigilAgentMcpClient', () => {
    it('spawns the MCP binary with backend env vars', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080/', 'http://localhost:9090/', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext() as any);
        await client.scan('code', 'python', 'f.py');

        expect(spawn).toHaveBeenCalledWith(
            'C:/fake/vigilagent-mcp.exe',
            [],
            expect.objectContaining({
                env: expect.objectContaining({
                    VIGILAGENT_API_URL: 'http://localhost:8080',
                    VIGILAGENT_API_KEY: expect.any(String),
                    VIGILAGENT_GATEWAY_URL: 'http://localhost:9090',
                }),
            })
        );
        await client.dispose();
    });

    it('calls tools/call and parses JSON content into the result', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext() as any);
        const result = await client.scan('code', 'python', 'f.py');

        expect(result.scan_result).toBeDefined();
        expect(result.scan_result!.findings[0].severity).toBe('high');
        await client.dispose();
    });

    it('rejects when the server reports a tool error', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext() as any);
        await expect(client.scan('FAIL', 'python', 'f.py')).rejects.toThrow(/MCP tool vigil_scan failed: backend 500/);
        await client.dispose();
    });

    it('reuses one spawned process across calls', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext() as any);
        await client.scan('a', 'python', 'a.py');
        await client.scan('b', 'python', 'b.py');
        expect(spawn).toHaveBeenCalledTimes(1);
        await client.dispose();
    });

    it('dispose kills the child process', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext() as any);
        await client.scan('code', 'python', 'f.py');
        await client.dispose();
        expect(hoisted.fakeProc.killed).toBe(true);
    });

    it('heals a 401 on a localhost backend by retrying with local-dev', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        // Stale/foreign key stored in the wizard — the real-world 401 case.
        client.setContext(fakeContext('va_stale-key-from-old-setup') as any);

        const result = await client.scan('code', 'python', 'f.py');

        expect(result.scan_result).toBeDefined();
        // First spawn got the stale key; the healed retry respawned with local-dev.
        expect(spawn).toHaveBeenCalledTimes(2);
        const calls = vi.mocked(spawn).mock.calls;
        expect((calls[0][2] as { env: NodeJS.ProcessEnv }).env.VIGILAGENT_API_KEY).toBe('va_stale-key-from-old-setup');
        expect((calls[1][2] as { env: NodeJS.ProcessEnv }).env.VIGILAGENT_API_KEY).toBe('local-dev');
        await client.dispose();
    });

    it('never falls back to local-dev against a remote backend', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('https://api.example.com', 'https://gw.example.com', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext('va_stale-key-from-old-setup') as any);

        await expect(client.scan('code', 'python', 'f.py')).rejects.toThrow(/401/);
        // Exactly one spawn — no fallback respawn for remote backends.
        expect(spawn).toHaveBeenCalledTimes(1);
        const env = (vi.mocked(spawn).mock.calls[0][2] as { env: NodeJS.ProcessEnv }).env;
        expect(env.VIGILAGENT_API_KEY).toBe('va_stale-key-from-old-setup');
        await client.dispose();
    });

    it('heals at most once per session', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext('va_stale-key-from-old-setup') as any);

        await client.scan('a', 'python', 'a.py');  // heals to local-dev
        await client.scan('b', 'python', 'b.py');  // already healed — no new spawn

        expect(spawn).toHaveBeenCalledTimes(2); // initial + one heal respawn
        await client.dispose();
    });

    it('a stale process exit after the heal respawn must not kill the retry', async () => {
        const { VigilAgentMcpClient } = await import('./mcpClient.js');
        const client = new VigilAgentMcpClient('http://localhost:8080', 'http://localhost:9090', 'C:/fake/vigilagent-mcp.exe');
        client.setContext(fakeContext('va_stale-key-from-old-setup') as any);
        // Old proc delivers its exit event ~25ms after kill() — by then the
        // healed respawn's initialize may already be pending.
        hoisted.fakeProc.delayedExitOnKill = true;

        const result = await client.scan('code', 'python', 'f.py');

        // The retry must still succeed despite the stale exit event.
        expect(result.scan_result).toBeDefined();
        expect(spawn).toHaveBeenCalledTimes(2);
        await client.dispose();
    });
});
