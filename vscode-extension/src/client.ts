import * as vscode from 'vscode';

export interface ReviewResult {
    [key: string]: unknown;
    original_prompt?: string;
    main_llm_response?: string;
    deterministic_findings?: Finding[];
    reviewers?: ReviewerOutput[];
    confidence?: ConfidenceScore;
    // Line-anchored accept/reject suggestions (5 roles, 1 LLM call).
    suggestions?: Suggestion[];
    final_output?: string;
    duration?: string;
    summary?: string;
}

export interface Finding {
    severity: string;
    message: string;
    filename: string;
    line: number;
    snippet: string;
    fix: string;
    confidence: number;
    analyzers: string[];
}

export interface ReviewerOutput {
    name: string;
    role: string;
    verdict: string;
    findings: string[];
    suggestions: string[];
    raw_output: string;
}

export interface ConfidenceScore {
    grade: string;
    confidence: number;
    passed: number;
    failed: number;
    warned: number;
    reason: string;
}

export interface Suggestion {
    id: string;
    role: string;           // security | architecture | compliance | cost | red_team | deterministic
    severity: string;       // critical | high | medium | low | info
    line_start: number;     // 1-indexed, inclusive
    line_end: number;       // 1-indexed, inclusive
    message: string;
    replacement?: string;   // exact text to swap in (empty = description only)
    confidence: number;
    corroborated?: boolean; // deterministic engine agreed on a nearby line
}

export interface ScanResult {
    [key: string]: unknown;
    description: string;
    task_type: string;
    scan_result?: {
        findings: Finding[];
        analyzers_run: string[];
        analyzers_skipped: Record<string, string>;
    };
    pipeline_result?: {
        passed: boolean;
        confidence: number;
        layers: { name: string; passed: boolean }[];
    };
    skills_extracted?: unknown[];
    metrics?: Record<string, unknown>;
}

export interface DualEngineResult {
    [key: string]: unknown;
    findings: DualEngineFinding[];
    score: number;
    grade: string;
    engine_stats: {
        deterministic: {
            findings_count: number;
            latency_ms: number;
            engine_errors?: string[];
        };
        llm: {
            findings_count: number;
            latency_ms: number;
            model: string;
            cost: number;
            error?: string;
        };
        total_latency_ms: number;
    };
    summary: string;
    metadata: {
        code_length: number;
        language: string;
        analyzed_at: string;
        corroborated_findings: number;
    };
}

export interface DualEngineFinding {
    rule_id: string;
    engine: string;
    severity: string;
    category: string;
    message: string;
    fix?: string;
    line?: number;
    confidence: number;
    snippet?: string;
}

export class VigilAgentClient {
    private backendUrl: string;
    private extensionContext: vscode.ExtensionContext | undefined;

    constructor(backendUrl: string) {
        this.backendUrl = backendUrl.replace(/\/$/, '');
    }

    setContext(ctx: vscode.ExtensionContext): void {
        this.extensionContext = ctx;
    }

    private async getApiKey(): Promise<string> {
        if (this.extensionContext) {
            const secret = await this.extensionContext.secrets.get('vigilagent.apiKey');
            if (secret) {
                return secret;
            }
        }
        // Keys must live in SecretStorage — never in settings.json, which can
        // be committed to source control or synced to other machines.
        throw new Error('VigilAgent API key not configured. Run "VigilAgent: Configure API Keys" from the Command Palette.');
    }

    private async getLLMKey(provider?: string): Promise<string | undefined> {
        if (this.extensionContext) {
            // Use the stored provider preference, or try each provider in order
            if (provider) {
                const key = await this.extensionContext.secrets.get(`vigilagent.llmKey.${provider}`);
                if (key) { return key; }
            }
            // Try the stored provider preference
            const storedProvider = await this.extensionContext.secrets.get('vigilagent.selectedProvider');
            if (storedProvider) {
                const key = await this.extensionContext.secrets.get(`vigilagent.llmKey.${storedProvider}`);
                if (key) { return key; }
            }
            // Fallback: try each provider ID in order (must match the IDs the
            // configure wizard stores under, e.g. 'nvidia_nim', not 'NVIDIA NIM').
            const providerIds = ['nvidia_nim', 'openai', 'anthropic', 'gemini', 'mistral', 'groq', 'cohere', 'openrouter'];
            for (const p of providerIds) {
                const key = await this.extensionContext.secrets.get(`vigilagent.llmKey.${p}`);
                if (key) { return key; }
            }
        }
        return undefined;
    }

    private async getSelectedProvider(): Promise<string | undefined> {
        if (this.extensionContext) {
            const stored = await this.extensionContext.secrets.get('vigilagent.selectedProvider');
            if (stored) { return stored; }
        }
        // Fallback to settings.json
        const config = vscode.workspace.getConfiguration('vigilagent');
        return config.get<string>('llmProvider', 'NVIDIA NIM');
    }

    private async getSelectedModel(): Promise<string | undefined> {
        if (this.extensionContext) {
            const stored = await this.extensionContext.secrets.get('vigilagent.selectedModel');
            if (stored) { return stored; }
        }
        // Fallback to settings.json
        const config = vscode.workspace.getConfiguration('vigilagent');
        return config.get<string>('llmModel', 'kimi-k2.6');
    }

    private async request<T>(path: string, body: Record<string, unknown>): Promise<T> {
        const apiKey = await this.getApiKey();
        const llmKey = await this.getLLMKey();
        const provider = await this.getSelectedProvider();
        const model = await this.getSelectedModel();
        // Never transmit provider API keys over plaintext HTTP outside localhost.
        assertSecureBackendUrl(this.backendUrl);

        const url = `${this.backendUrl}${path}`;

        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${apiKey}`,
        };
        // Pass user's LLM key to backend so it can use it for the review pipeline
        if (llmKey) {
            headers['X-LLM-Key'] = llmKey;
        }
        // Pass provider and model so backend routes to the correct LLM
        if (provider) {
            headers['X-LLM-Provider'] = provider;
        }
        if (model) {
            headers['X-LLM-Model'] = model;
        }

        const response = await fetch(url, {
            method: 'POST',
            headers,
            body: JSON.stringify(body),
            // A hung backend must not leave the command/chat hanging forever.
            signal: AbortSignal.timeout(15000),
        });

        if (!response.ok) {
            const text = await response.text();
            throw new Error(`VigilAgent API error (${response.status}): ${text}`);
        }

        return response.json() as Promise<T>;
    }

    async verify(
        code: string,
        prompt: string,
        language: string,
        filename: string,
        suggestionMode: boolean = true
    ): Promise<ReviewResult> {
        return this.request<ReviewResult>('/api/v1/review', {
            code,
            prompt,
            language,
            filename,
            // Suggestion mode: line-anchored accept/reject suggestions, no
            // auto-rewriting of the code.
            suggestion_mode: suggestionMode,
        });
    }

    async scan(code: string, language: string, filename: string): Promise<ScanResult> {
        return this.request<ScanResult>('/api/v1/middleware/process', {
            description: `static analysis scan of ${filename}`,
            code,
            language,
            filename,
        });
    }

    async dualEngine(code: string, language: string): Promise<DualEngineResult> {
        // Deep-analyze is a protected route (auth required) and the router mounts
        // all v1 routes under /api/v1 — calling /v1/deep-analyze would 404.
        return this.request<DualEngineResult>('/api/v1/deep-analyze', {
            code,
            language,
        });
    }

    async process(
        description: string,
        code: string,
        language: string,
        taskType: string
    ): Promise<ScanResult> {
        return this.request<ScanResult>('/api/v1/middleware/process', {
            description,
            code,
            language,
            task_type: taskType,
        });
    }

    async healthCheck(): Promise<boolean> {
        try {
            const apiKey = await this.getApiKey();
            assertSecureBackendUrl(this.backendUrl);
            const response = await fetch(`${this.backendUrl}/api/v1/health`, {
                headers: { 'Authorization': `Bearer ${apiKey}` },
                signal: AbortSignal.timeout(10000),
            });
            return response.ok;
        } catch {
            return false;
        }
    }

    async isConfigured(): Promise<boolean> {
        try {
            await this.getApiKey();
            return true;
        } catch {
            return false;
        }
    }

    // ── Secure AI Gateway methods (Plan-1 controlled generation) ────────────

    // listGatewayModels fetches the gateway's model catalog (GET /v1/models).
    async listGatewayModels(gatewayUrl: string): Promise<GatewayModel[]> {
        assertSecureBackendUrl(gatewayUrl);
        try {
            const response = await fetch(`${gatewayUrl.replace(/\/$/, '')}/v1/models`, {
                signal: AbortSignal.timeout(10000),
            });
            if (!response.ok) {
                return [];
            }
            const data = await response.json() as { models?: GatewayModel[] };
            return (data.models || []).filter(m => !m.deprecated);
        } catch {
            return [];
        }
    }

    // chatViaGateway streams a chat request through the Secure AI Gateway's
    // Responses API (/v1/responses, stream:true). The gateway runs the
    // design-stage gate, scans every code block, applies the policy decision
    // (balanced mode: code withheld on a held review), and signs a provenance
    // record — all before the text is returned here.
    //
    // onDelta, when provided, is invoked with each incremental text chunk as
    // it streams off the wire so callers (e.g. the language-model chat
    // provider) can surface partial output while the scan is in flight.
    async chatViaGateway(
        gatewayUrl: string,
        messages: Array<{ role: string; content: string }>,
        model: string,
        onDelta?: (text: string) => void
    ): Promise<GatewayChatResult> {
        const apiKey = await this.getApiKey();
        const llmKey = await this.getLLMKey();
        const provider = await this.getSelectedProvider();
        assertSecureBackendUrl(gatewayUrl);

        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
            'X-API-Key': apiKey,
            'X-VigilAgent-Mode': 'balanced',
        };
        if (llmKey) {
            headers['X-LLM-Key'] = llmKey;
        }
        if (provider) {
            headers['X-LLM-Provider'] = provider;
        }

        const response = await fetch(`${gatewayUrl.replace(/\/$/, '')}/v1/responses`, {
            method: 'POST',
            headers,
            body: JSON.stringify({ model, stream: true, input: messages }),
            signal: AbortSignal.timeout(120000),
        });

        // Headers are available before the SSE body streams.
        const scanId = response.headers.get('X-VigilAgent-Scan-ID') || undefined;
        const provenance = response.headers.get('X-VigilAgent-Provenance') || undefined;
        const designGate = response.headers.get('X-VigilAgent-Design-Gate') || undefined;

        if (!response.ok || !response.body) {
            const text = await response.text();
            throw new Error(`Gateway error (${response.status}): ${text}`);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let text = '';
        let failure: string | undefined;

        const handleLine = (line: string): void => {
            if (!line.startsWith('data: ')) {
                return;
            }
            const payload = line.slice(6).trim();
            if (payload === '[DONE]') {
                return;
            }
            try {
                const evt = JSON.parse(payload) as Record<string, unknown>;
                if (evt.type === 'response.output_text.delta') {
                    const chunk = String(evt.delta || '');
                    text += chunk;
                    if (onDelta && chunk) {
                        onDelta(chunk);
                    }
                } else if (evt.type === 'response.completed') {
                    const resp = evt.response as { output?: Array<{ content?: Array<{ text?: string }> }> };
                    const out = resp.output?.[0]?.content?.[0]?.text;
                    if (typeof out === 'string' && out.length > 0) {
                        text = out; // authoritative full text
                    }
                } else if (evt.type === 'response.failed') {
                    const resp = evt.response as { error?: { message?: string; decision?: string } };
                    failure = resp.error?.message || 'blocked by VigilAgent policy';
                }
            } catch {
                // partial/irrelevant SSE line — ignore
            }
        };

        // eslint-disable-next-line no-constant-condition
        while (true) {
            const { done, value } = await reader.read();
            if (done) {
                break;
            }
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop() || '';
            for (const line of lines) {
                handleLine(line.trim());
            }
        }
        if (buffer.trim()) {
            handleLine(buffer.trim());
        }

        if (failure) {
            throw new Error(`VigilAgent blocked the response: ${failure}`);
        }

        return { text, model, scanId, provenance, designGate };
    }
}

export interface GatewayModel {
    id: string;
    name: string;
    provider: string;
    context_window: number;
    max_output: number;
    capabilities: string[];
    deprecated?: boolean;
}

export interface GatewayChatResult {
    text: string;
    model: string;
    scanId?: string;
    provenance?: string; // verified | unverified | bypassed
    designGate?: string; // passed | constrained
}

// Rejects plain-HTTP backend URLs unless they point at the local machine.
// The extension forwards the user's LLM provider API key to the backend, so
// sending it unencrypted to a remote host would expose it on the wire.
function assertSecureBackendUrl(backendUrl: string): void {
    let parsed: URL;
    try {
        parsed = new URL(backendUrl);
    } catch {
        throw new Error(`Invalid VigilAgent backend URL: ${backendUrl}`);
    }

    // Node's URL.hostname returns IPv6 addresses WITH brackets ('[::1]'), so
    // strip them before comparing.
    const host = parsed.hostname.replace(/^\[|\]$/g, '');
    const isLocalhost =
        host === 'localhost' ||
        host === '127.0.0.1' ||
        host === '::1';

    if (parsed.protocol === 'http:' && !isLocalhost) {
        throw new Error('VigilAgent refuses to send API keys over plain HTTP. Use an https:// backend URL for remote servers (http://localhost is allowed for local development).');
    }
}
