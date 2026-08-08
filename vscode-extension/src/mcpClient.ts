import * as vscode from 'vscode';
import { spawn, ChildProcessWithoutNullStreams } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import { ScanResult, ReviewResult, DualEngineResult, readStoredLLMKey, isLocalhostUrl, isAuthRejection, LOCAL_DEV_KEY } from './client';

// ═══════════════════════════════════════════════════════════════════════════
// VigilAgentMcpClient — routes the @vigilagent chat participant through the
// vigilagent-mcp server (JSON-RPC 2.0 over stdio) instead of direct REST.
//
//   VS Code chat ──▶ this client ──stdio──▶ vigilagent-mcp ──HTTP──▶ backend
//
// The chat surface therefore speaks the SAME Model Context Protocol as Cursor,
// Cline, Claude Desktop, and every other MCP client — one tool surface, one
// code path. The MCP tools are the source of truth for tool discovery
// (tools/list) and the backend engines stay unchanged.
// ═══════════════════════════════════════════════════════════════════════════

// Newline-delimited JSON-RPC 2.0 frames (mcp-go's stdio transport framing).
interface RpcRequest {
    jsonrpc: '2.0';
    id: number;
    method: string;
    params?: Record<string, unknown>;
}

interface RpcResponse {
    jsonrpc: '2.0';
    id: number;
    result?: unknown;
    error?: { code: number; message: string };
}

export interface McpToolResult {
    content?: Array<{ type: string; text?: string }>;
    isError?: boolean;
}

export class VigilAgentMcpClient {
    private binaryPath: string;
    private backendUrl: string;
    private gatewayUrl: string;
    private extensionContext: vscode.ExtensionContext | undefined;

    private proc: ChildProcessWithoutNullStreams | undefined;
    private buffer = '';
    private nextId = 1;
    private pending = new Map<number, { resolve: (r: RpcResponse) => void; reject: (e: Error) => void }>();
    private stderrLog: string[] = [];
    private ready: Promise<void> | undefined;
    private disposed = false;
    // Set once a 401 auth rejection has been healed with the local-dev key.
    // Guards the fallback so it fires at most once per extension session.
    private localDevFallbackUsed = false;

    constructor(backendUrl: string, gatewayUrl: string, binaryPath: string) {
        this.backendUrl = backendUrl.replace(/\/$/, '');
        this.gatewayUrl = gatewayUrl.replace(/\/$/, '');
        this.binaryPath = binaryPath;
    }

    setContext(ctx: vscode.ExtensionContext): void {
        this.extensionContext = ctx;
    }

    // ── Lifecycle ──────────────────────────────────────────────────────────

    // resolveBinary locates the MCP server binary with the following priority:
    //   1. explicit config (vigilagent.mcpBinaryPath)
    //   2. bundled with the extension (vendor/<platform>-<arch>/vigilagent-mcp)
    //   3. repository layout during development
    static resolveBinary(context: vscode.ExtensionContext | undefined): string {
        const cfg = vscode.workspace.getConfiguration('vigilagent').get<string>('mcpBinaryPath', '');
        if (cfg) {
            return cfg;
        }
        const platform = process.platform === 'win32' ? 'windows' : process.platform;
        const arch = process.arch === 'x64' ? 'amd64' : process.arch;
        const exe = process.platform === 'win32' ? 'vigilagent-mcp.exe' : 'vigilagent-mcp';
        const candidates: string[] = [];
        if (context) {
            // Bundled alongside the extension (vsix packaging adds vendor/).
            candidates.push(path.join(context.extensionPath, 'vendor', `${platform}-${arch}`, exe));
            // Repository layout during development: extension sits next to mcp-server/.
            candidates.push(path.join(context.extensionPath, '..', 'mcp-server', 'vendor', `${platform}-${arch}`, exe));
        }
        // Workspace-relative fallback (dev containers / clone checked out here).
        const ws = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
        if (ws) {
            candidates.push(path.join(ws, 'mcp-server', 'vendor', `${platform}-${arch}`, exe));
        }
        for (const c of candidates) {
            if (fs.existsSync(c)) {
                return c;
            }
        }
        return cfg || candidates[0] || '';
    }

    // ensureStarted lazily spawns the MCP server and completes the MCP
    // initialize handshake. On failure the cached promise is reset so a later
    // call retries — e.g. after the user runs "Configure API Keys" or fixes
    // vigilagent.mcpBinaryPath without reloading the window.
    private ensureStarted(): Promise<void> {
        if (!this.ready) {
            this.disposed = false; // a re-spawn after dispose is a fresh process
            this.ready = this.start().catch((err) => {
                this.ready = undefined;
                throw err;
            });
        }
        return this.ready;
    }

    private async start(): Promise<void> {
        if (!this.binaryPath || !fs.existsSync(this.binaryPath)) {
            throw new Error(
                `VigilAgent MCP binary not found at "${this.binaryPath}". ` +
                'Set "vigilagent.mcpBinaryPath" in settings.json or install the bundled MCP server.'
            );
        }

        // When the 401 fallback has already healed an auth rejection, spawn
        // straight to the local-dev key (see healAuthRejection).
        const apiKey = this.localDevFallbackUsed ? LOCAL_DEV_KEY : await this.readApiKey();
        const env: NodeJS.ProcessEnv = {
            ...process.env,
            VIGILAGENT_API_URL: this.backendUrl,
            VIGILAGENT_API_KEY: apiKey,
        };
        if (this.gatewayUrl) {
            env.VIGILAGENT_GATEWAY_URL = this.gatewayUrl;
        }
        // Forward the user's LLM provider key so LLM-backed tools (verify,
        // dual_engine) work with their chosen provider (BYOK through MCP).
        const llmKey = await this.readLLMKey();
        if (llmKey) {
            env.VIGILAGENT_LLM_KEY = llmKey;
        }

        this.proc = spawn(this.binaryPath, [], {
            env,
            stdio: ['pipe', 'pipe', 'pipe'],
        });
        // Capture the process identity so stale-exit handling below can tell
        // whether THIS proc is still the one the client owns. After the 401
        // heal respawns the server, the old proc's exit event may arrive late
        // and must not reject the new proc's in-flight requests.
        const proc = this.proc;

        this.proc.stdout.on('data', (chunk: Buffer) => this.onData(chunk.toString()));
        this.proc.stderr.on('data', (chunk: Buffer) => {
            const line = chunk.toString().trim();
            if (line) {
                this.stderrLog.push(line);
                if (this.stderrLog.length > 50) {
                    this.stderrLog.shift();
                }
            }
        });
        this.proc.on('error', (err) => {
            if (this.proc === proc) {
                this.rejectAll(new Error(`MCP server failed to start: ${err.message}`));
            }
        });
        this.proc.on('exit', (code, signal) => {
            // Stale process: a heal/dispose respawn now owns the client. Its
            // late exit must not reject the current proc's pending requests.
            if (this.proc !== proc) {
                return;
            }
            if (!this.disposed) {
                const msg = this.withStderr(`MCP server exited unexpectedly (code=${code} signal=${signal})`);
                // Self-healing process: clear the process state so the NEXT
                // tool call spawns a fresh MCP server (picking up any updated
                // binary) instead of erroring on the dead process forever.
                this.proc = undefined;
                this.ready = undefined;
                this.stderrLog = [];
                this.rejectAll(new Error(msg + ' — will restart on next use'));
            }
        });

        // Handshake: initialize + initialized notification.
        await this.request('initialize', {
            protocolVersion: '2024-11-05',
            capabilities: {},
            clientInfo: { name: 'vigilagent-vscode', version: '1.0.0' },
        });
        this.notify('notifications/initialized', {});
    }

    async dispose(): Promise<void> {
        this.disposed = true;
        if (this.proc) {
            this.proc.kill();
            this.proc = undefined;
        }
        this.ready = undefined;
    }

    // ── JSON-RPC transport ─────────────────────────────────────────────────

    private onData(data: string): void {
        this.buffer += data;
        let idx: number;
        while ((idx = this.buffer.indexOf('\n')) >= 0) {
            const line = this.buffer.slice(0, idx).trim();
            this.buffer = this.buffer.slice(idx + 1);
            if (!line) {
                continue;
            }
            try {
                const msg = JSON.parse(line) as RpcResponse;
                if (msg.id !== undefined && this.pending.has(msg.id)) {
                    const p = this.pending.get(msg.id)!;
                    this.pending.delete(msg.id);
                    if (msg.error) {
                        p.reject(new Error(`${msg.error.code}: ${msg.error.message}`));
                    } else {
                        p.resolve(msg);
                    }
                }
            } catch {
                // Non-JSON stderr leakage on stdout — ignore.
            }
        }
    }

    private send(frame: Record<string, unknown>): void {
        if (!this.proc || !this.proc.stdin.writable) {
            throw new Error('MCP server not running');
        }
        this.proc.stdin.write(JSON.stringify(frame) + '\n');
    }

    private sendRequest(req: RpcRequest): void {
        this.send({ ...req });
    }

    private request(method: string, params: Record<string, unknown>): Promise<RpcResponse> {
        const id = this.nextId++;
        return new Promise<RpcResponse>((resolve, reject) => {
            // The timeout is cleared when the request settles so timers don't
            // accumulate across a long session. MCP tools can run the full LLM
            // review pipeline (60-90s), so the window is generous.
            const timer = setTimeout(() => {
                if (this.pending.has(id)) {
                    this.pending.delete(id);
                    reject(new Error(`MCP request timed out: ${method}`));
                }
            }, 120_000);
            timer.unref();

            this.pending.set(id, {
                resolve: (r) => { clearTimeout(timer); resolve(r); },
                reject: (e) => { clearTimeout(timer); reject(e); },
            });
            try {
                this.sendRequest({ jsonrpc: '2.0', id, method, params });
            } catch (e) {
                clearTimeout(timer);
                this.pending.delete(id);
                reject(e instanceof Error ? e : new Error(String(e)));
            }
        });
    }

    private notify(method: string, params: Record<string, unknown>): void {
        try {
            this.send({ jsonrpc: '2.0', method, params });
        } catch {
            // Notifications are fire-and-forget.
        }
    }

    private rejectAll(err: Error): void {
        for (const [, p] of this.pending) {
            p.reject(err);
        }
        this.pending.clear();
    }

    // ── Tool calls ─────────────────────────────────────────────────────────

    private async callTool(name: string, args: Record<string, unknown>): Promise<unknown> {
        await this.ensureStarted();
        const resp = await this.request('tools/call', { name, arguments: args });
        const result = resp.result as McpToolResult | undefined;
        if (result?.isError) {
            const text = result.content?.[0]?.text || 'unknown tool error';
            const err = new Error(this.withStderr(`MCP tool ${name} failed: ${text}`));
            // Self-healing 401: a stale/foreign key stored in the wizard is the
            // #1 local-dev failure mode. When the backend is localhost, restart
            // the MCP server with the local-dev key and retry the SAME call once
            // — the user never has to reconfigure. Remote backends are never
            // retried (their rejection is meaningful, not a dev-mode quirk).
            if (this.canHealAuthRejection(err)) {
                await this.healAuthRejection();
                return this.callTool(name, args); // single retry
            }
            throw err;
        }
        // The MCP server returns raw JSON payloads for machine clients.
        const text = (result?.content || [])
            .filter((c) => c.type === 'text' && c.text)
            .map((c) => c.text as string)
            .join('\n');
        if (!text) {
            throw new Error(`MCP tool ${name} returned no text content`);
        }
        try {
            return JSON.parse(text);
        } catch {
            // Non-JSON (e.g. error prose) — surface as-is.
            throw new Error(this.withStderr(text));
        }
    }

    // withStderr appends the MCP server's most recent stderr lines to an error
    // message so the underlying failure is diagnosable from the chat output.
    private withStderr(message: string): string {
        if (this.stderrLog.length === 0) {
            return message;
        }
        const tail = this.stderrLog.slice(-3).join(' | ');
        return `${message} [mcp stderr: ${tail}]`;
    }

    // canHealAuthRejection reports whether a tool error is an auth rejection
    // that is safe to retry with the local-dev key: it must LOOK like a 401,
    // target a localhost backend, and not have been healed already.
    private canHealAuthRejection(err: Error): boolean {
        if (this.localDevFallbackUsed) {
            return false;
        }
        if (!isLocalhostUrl(this.backendUrl)) {
            return false;
        }
        return isAuthRejection(err.message);
    }

    // healAuthRejection kills the MCP server and marks the session to spawn
    // with the local-dev development key, then notifies the user once.
    private async healAuthRejection(): Promise<void> {
        this.localDevFallbackUsed = true;
        if (this.proc) {
            this.proc.kill();
            this.proc = undefined;
        }
        this.ready = undefined;
        this.stderrLog = [];
        // Fire-and-forget: the retry that follows is what actually verifies the
        // new key; the toast is informational only.
        void vscode.window.showInformationMessage(
            'VigilAgent: stored API key was rejected by the local backend — retrying with the local development key.'
        );
    }

    // ── Backend-agnostic surface (same signatures as VigilAgentClient) ─────

    async isConfigured(): Promise<boolean> {
        try {
            await this.readApiKey();
            return true;
        } catch {
            return false;
        }
    }

    // Deterministic static analysis via the vigil_scan MCP tool.
    async scan(code: string, language: string, filename: string): Promise<ScanResult> {
        const result = await this.callTool('vigil_scan', { code, language, filename });
        return result as ScanResult;
    }

    // Full verification pipeline (deterministic + LLM reviewers) via vigil_verify.
    async verify(
        code: string,
        prompt: string,
        language: string,
        filename: string,
        _suggestionMode = true
    ): Promise<ReviewResult> {
        const result = await this.callTool('vigil_verify', { code, prompt, language, filename });
        return result as ReviewResult;
    }

    // Parallel dual-engine analysis (deterministic + LLM in parallel) — asks
    // the MCP server for the raw structured response (format=json).
    async dualEngine(code: string, language: string): Promise<DualEngineResult> {
        const result = await this.callTool('vigil_dual_engine', { code, language, format: 'json' });
        return result as DualEngineResult;
    }

    // ── Secret access ──────────────────────────────────────────────────────

    private async readApiKey(): Promise<string> {
        if (this.extensionContext) {
            const secret = await this.extensionContext.secrets.get('vigilagent.apiKey');
            if (secret) {
                return secret;
            }
        }
        throw new Error('VigilAgent API key not configured. Run "VigilAgent: Configure API Keys" from the Command Palette.');
    }

    private async readLLMKey(): Promise<string | undefined> {
        return readStoredLLMKey(this.extensionContext);
    }
}
