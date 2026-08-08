import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── Test doubles ────────────────────────────────────────────────────────────
// The REST client uses global fetch; each test installs a stub that returns a
// configurable sequence of responses so we can simulate a 401 followed by a
// successful retry.

interface FetchCall {
    url: string;
    init: { method: string; headers: Record<string, string>; body?: string };
}

function jsonResponse(status: number, body: unknown): Response {
    return {
        ok: status >= 200 && status < 300,
        status,
        text: async () => JSON.stringify(body),
        json: async () => body,
    } as unknown as Response;
}

// Fake extension context whose secret storage returns a stale key — the
// real-world case that triggers the heal.
function fakeContext(storedKey = 'va_stale-key-from-old-setup'): { secrets: { get: (k: string) => Promise<string | undefined> } } {
    return {
        secrets: {
            get: async (k: string) => (k === 'vigilagent.apiKey' ? storedKey : undefined),
        },
    };
}

vi.mock('vscode', () => ({
    workspace: {
        getConfiguration: () => ({ get: () => '' }),
        workspaceFolders: undefined,
    },
    window: {
        showInformationMessage: () => Promise.resolve(undefined),
    },
}));

let fetchMock: ReturnType<typeof vi.fn>;
let calls: FetchCall[];

beforeEach(() => {
    calls = [];
    fetchMock = vi.fn(async (url: string, init: RequestInit) => {
        calls.push({
            url,
            init: {
                method: String(init.method || 'GET'),
                headers: (init.headers || {}) as Record<string, string>,
                body: typeof init.body === 'string' ? init.body : undefined,
            },
        });
        throw new Error('fetchMock: no response configured — call setResponses()');
    });
    vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

function setResponses(responses: Response[]): void {
    fetchMock.mockImplementation(async (url: string, init: RequestInit) => {
        calls.push({
            url,
            init: {
                method: String(init.method || 'GET'),
                headers: (init.headers || {}) as Record<string, string>,
                body: typeof init.body === 'string' ? init.body : undefined,
            },
        });
        return responses.shift() ?? jsonResponse(500, { error: 'no more responses' });
    });
}

describe('VigilAgentClient 401 self-heal', () => {
    it('heals a 401 against a localhost backend by retrying with local-dev', async () => {
        const { VigilAgentClient } = await import('./client.js');
        const client = new VigilAgentClient('http://localhost:8080');
        client.setContext(fakeContext() as any);

        setResponses([
            jsonResponse(401, { code: 'AUTH_011', message: 'invalid API key' }),
            jsonResponse(200, { description: 'static analysis scan', scan_result: { findings: [] } }),
        ]);

        const result = await client.scan('code', 'python', 'f.py');

        expect(result.scan_result).toBeDefined();
        expect(calls).toHaveLength(2);
        expect(calls[0].init.headers['Authorization']).toBe('Bearer va_stale-key-from-old-setup');
        expect(calls[1].init.headers['Authorization']).toBe('Bearer local-dev');
    });

    it('never falls back against a remote backend', async () => {
        const { VigilAgentClient } = await import('./client.js');
        const client = new VigilAgentClient('https://api.example.com');
        client.setContext(fakeContext() as any);

        setResponses([jsonResponse(401, { code: 'AUTH_011', message: 'invalid API key' })]);

        await expect(client.scan('code', 'python', 'f.py')).rejects.toThrow(/401/);
        expect(calls).toHaveLength(1);
        expect(calls[0].init.headers['Authorization']).toBe('Bearer va_stale-key-from-old-setup');
    });

    it('heals at most once per session — a second 401 surfaces', async () => {
        const { VigilAgentClient } = await import('./client.js');
        const client = new VigilAgentClient('http://localhost:8080');
        client.setContext(fakeContext() as any);

        setResponses([
            jsonResponse(401, { code: 'AUTH_011', message: 'invalid API key' }),
            jsonResponse(401, { code: 'AUTH_011', message: 'invalid API key' }),
        ]);

        await expect(client.scan('code', 'python', 'f.py')).rejects.toThrow(/401/);
        expect(calls).toHaveLength(2);
        expect(calls[0].init.headers['Authorization']).toBe('Bearer va_stale-key-from-old-setup');
        expect(calls[1].init.headers['Authorization']).toBe('Bearer local-dev');
    });

    it('passes a non-401 failure through untouched', async () => {
        const { VigilAgentClient } = await import('./client.js');
        const client = new VigilAgentClient('http://localhost:8080');
        client.setContext(fakeContext() as any);

        setResponses([jsonResponse(500, { error: 'boom' })]);

        await expect(client.scan('code', 'python', 'f.py')).rejects.toThrow(/500/);
        expect(calls).toHaveLength(1);
    });

    it('uses local-dev directly on every call after a heal', async () => {
        const { VigilAgentClient } = await import('./client.js');
        const client = new VigilAgentClient('http://localhost:8080');
        client.setContext(fakeContext() as any);

        // First call: stale key → 401 → heal → local-dev → 200.
        // Second independent call: must ALSO use local-dev (no re-heal, no 401).
        setResponses([
            jsonResponse(401, { code: 'AUTH_011', message: 'invalid API key' }),
            jsonResponse(200, { description: 'scan one', scan_result: { findings: [] } }),
            jsonResponse(200, { description: 'scan two', scan_result: { findings: [] } }),
        ]);

        await client.scan('a', 'python', 'a.py');
        await client.scan('b', 'python', 'b.py');

        expect(calls).toHaveLength(3);
        expect(calls[0].init.headers['Authorization']).toBe('Bearer va_stale-key-from-old-setup');
        expect(calls[1].init.headers['Authorization']).toBe('Bearer local-dev');
        // The third call skips the stored key entirely — session is healed.
        expect(calls[2].init.headers['Authorization']).toBe('Bearer local-dev');
    });
});
