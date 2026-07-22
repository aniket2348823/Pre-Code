import * as vscode from 'vscode';

export interface ReviewResult {
    [key: string]: unknown;
    original_prompt?: string;
    main_llm_response?: string;
    deterministic_findings?: Finding[];
    reviewers?: ReviewerOutput[];
    confidence?: ConfidenceScore;
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
        // Fallback to workspace configuration
        const config = vscode.workspace.getConfiguration('vigilagent');
        const apiKey = config.get<string>('apiKey', '');
        if (apiKey) {
            return apiKey;
        }
        throw new Error('VigilAgent API key not configured. Run "VigilAgent: Configure API Keys" from the Command Palette.');
    }

    private async getLLMKey(provider?: string): Promise<string | undefined> {
        if (!this.extensionContext) { return undefined; }
        // Use the stored provider preference, or try each provider in order
        if (provider) {
            return this.extensionContext.secrets.get(`vigilagent.llmKey.${provider}`);
        }
        // Try the stored provider preference
        const storedProvider = await this.extensionContext.secrets.get('vigilagent.selectedProvider');
        if (storedProvider) {
            return this.extensionContext.secrets.get(`vigilagent.llmKey.${storedProvider}`);
        }
        // Fallback: try each provider in order
        const providers = ['OpenAI', 'Anthropic', 'Google Gemini', 'Mistral', 'Groq', 'Cohere'];
        for (const p of providers) {
            const key = await this.extensionContext.secrets.get(`vigilagent.llmKey.${p}`);
            if (key) { return key; }
        }
        return undefined;
    }

    private async request<T>(path: string, body: Record<string, unknown>): Promise<T> {
        const apiKey = await this.getApiKey();
        const llmKey = await this.getLLMKey();
        const url = `${this.backendUrl}${path}`;

        const headers: Record<string, string> = {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${apiKey}`,
        };
        // Pass user's LLM key to backend so it can use it for the review pipeline
        if (llmKey) {
            headers['X-LLM-Key'] = llmKey;
        }

        const response = await fetch(url, {
            method: 'POST',
            headers,
            body: JSON.stringify(body),
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
        filename: string
    ): Promise<ReviewResult> {
        return this.request<ReviewResult>('/api/v1/review', {
            code,
            prompt,
            language,
            filename,
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
            const response = await fetch(`${this.backendUrl}/api/v1/health`, {
                headers: { 'Authorization': `Bearer ${apiKey}` },
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
}
