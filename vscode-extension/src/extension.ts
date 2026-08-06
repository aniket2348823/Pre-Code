import * as vscode from 'vscode';
import { VigilAgentChatParticipant } from './chat';
import { VigilAgentClient } from './client';
import { VigilAgentStatusBar } from './statusbar';
import { DiagnosticManager } from './diagnostics';
import { AutoVerifier } from './autoVerify';

export function activate(context: vscode.ExtensionContext) {
    const config = vscode.workspace.getConfiguration('vigilagent');
    const backendUrl = config.get<string>('backendUrl', 'http://localhost:8080');

    // Initialize the backend client
    const client = new VigilAgentClient(backendUrl);
    client.setContext(context);

    // Register the chat participant
    const participant = new VigilAgentChatParticipant(client);
    participant.register(context);

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('vigilagent.configure', async () => {
            await configureProviderWizard(context);
        }),
        vscode.commands.registerCommand('vigilagent.scanFile', async () => {
            await scanCurrentFile(client);
        }),
        vscode.commands.registerCommand('vigilagent.verifySelection', async () => {
            await verifySelection(client);
        }),
        vscode.commands.registerCommand('vigilagent.dualEngine', async () => {
            await dualEngineAnalysis(client);
        })
    );

    // Status bar for confidence scores
    const statusBar = new VigilAgentStatusBar();
    context.subscriptions.push(statusBar);

    // Initialize Diagnostic Manager & AutoVerifier
    const diagnosticManager = new DiagnosticManager();
    context.subscriptions.push(diagnosticManager);
    
    const autoVerifier = new AutoVerifier(client, diagnosticManager);
    autoVerifier.register(context);
    context.subscriptions.push(autoVerifier);

    // Show info message
    vscode.window.showInformationMessage(
        'VigilAgent activated! Use @vigilagent in chat or run commands from the Command Palette.'
    );
}

// ═══════════════════════════════════════════════════════════════════════════
// PROVIDER/MODEL SELECTION WIZARD
// ═══════════════════════════════════════════════════════════════════════════

interface ProviderInfo {
    id: string;
    name: string;
    base_url: string;
    key_prefix: string;
    description: string;
    key_hint: string;
}

interface ModelEntry {
    id: string;
    name: string;
    provider: string;
    context_window: number;
    max_output: number;
    input_cost_per_1m: number;
    output_cost_per_1m: number;
    capabilities: string[];
    description: string;
    deprecated?: boolean;
}

interface ProviderModelsResponse {
    provider: ProviderInfo;
    models: ModelEntry[];
    count: number;
}

// All known providers (fallback if API is unreachable)
const KNOWN_PROVIDERS: ProviderInfo[] = [
    { id: 'openai', name: 'OpenAI', base_url: 'https://api.openai.com/v1', key_prefix: 'sk-', key_hint: 'sk-...', description: 'GPT models including o-series reasoning' },
    { id: 'anthropic', name: 'Anthropic', base_url: 'https://api.anthropic.com', key_prefix: 'sk-ant-', key_hint: 'sk-ant-...', description: 'Claude models with strong safety and reasoning' },
    { id: 'gemini', name: 'Google Gemini', base_url: 'https://generativelanguage.googleapis.com', key_prefix: 'AIza', key_hint: 'AIza...', description: "Google's multimodal models with huge context windows" },
    { id: 'groq', name: 'Groq', base_url: 'https://api.groq.com/openai/v1', key_prefix: 'gsk_', key_hint: 'gsk_...', description: 'Ultra-fast inference on custom LPU hardware' },
    { id: 'mistral', name: 'Mistral AI', base_url: 'https://api.mistral.ai/v1', key_prefix: 'ms-', key_hint: 'ms-...', description: 'European AI lab with strong open and closed models' },
    { id: 'cohere', name: 'Cohere', base_url: 'https://api.cohere.com', key_prefix: 'co-', key_hint: 'co-...', description: 'RAG-optimized models with strong enterprise features' },
    { id: 'nvidia_nim', name: 'NVIDIA NIM', base_url: 'https://build.nvidia.com/v1', key_prefix: 'nvapi-', key_hint: 'nvapi-...', description: "NVIDIA's inference microservices with optimized open models" },
    { id: 'openrouter', name: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1', key_prefix: 'sk-or-', key_hint: 'sk-or-...', description: 'Unified gateway to 200+ models from all providers' },
];

async function configureProviderWizard(context: vscode.ExtensionContext): Promise<void> {
    // ── Step 1: VigilAgent Backend Auth ──
    const mode = await vscode.window.showQuickPick(
        ['Local development (no API key needed)', 'Remote / Production (enter API key)'],
        { placeHolder: 'How are you connecting to the VigilAgent backend?' }
    );

    if (mode === 'Remote / Production (enter API key)') {
        const vigilApiKey = await vscode.window.showInputBox({
            prompt: 'Enter your VigilAgent API key (va_...)',
            password: true,
            placeHolder: 'va_xxxxxxxxxxxxxxxxxxxx'
        });
        if (vigilApiKey) {
            await context.secrets.store('vigilagent.apiKey', vigilApiKey);
            vscode.window.showInformationMessage('VigilAgent API key saved securely.');
        }
    } else if (mode === 'Local development (no API key needed)') {
        await context.secrets.store('vigilagent.apiKey', 'local-dev');
        vscode.window.showInformationMessage('Configured for local development (no auth).');
    }

    // ── Step 2: Select LLM Provider ──
    const backendUrl = vscode.workspace.getConfiguration('vigilagent').get<string>('backendUrl', 'http://localhost:8080');
    
    // Try to fetch providers from the API, fall back to static list
    let providers: ProviderInfo[] = KNOWN_PROVIDERS;
    try {
        const resp = await fetch(`${backendUrl}/api/v1/providers`, {
            signal: AbortSignal.timeout(10000),
        });
        if (resp.ok) {
            const data = await resp.json() as { providers: ProviderInfo[] };
            if (data.providers && data.providers.length > 0) {
                providers = data.providers;
            }
        }
    } catch {
        // API unreachable, use static list
    }

    const providerItems = providers.map(p => ({
        label: p.name,
        description: p.description,
        detail: `Key prefix: ${p.key_hint}`,
        provider: p,
    }));

    const selectedProviderItem = await vscode.window.showQuickPick(providerItems, {
        placeHolder: 'Select your LLM provider',
        matchOnDescription: true,
        matchOnDetail: true,
    });

    if (!selectedProviderItem) { return; }
    const selectedProvider = selectedProviderItem.provider;

    // ── Step 3: Enter API Key ──
    const llmKey = await vscode.window.showInputBox({
        prompt: `Enter your ${selectedProvider.name} API key`,
        password: true,
        placeHolder: selectedProvider.key_hint,
        validateInput: (value) => {
            if (!value || value.trim().length === 0) {
                return 'API key is required';
            }
            if (selectedProvider.key_prefix && !value.startsWith(selectedProvider.key_prefix)) {
                return `Key should start with "${selectedProvider.key_prefix}" — you entered a key starting with "${value.substring(0, Math.min(6, value.length))}..."`;
            }
            return undefined;
        }
    });

    if (!llmKey) { return; }

    await context.secrets.store(`vigilagent.llmKey.${selectedProvider.id}`, llmKey);
    await context.secrets.store('vigilagent.selectedProvider', selectedProvider.id);
    vscode.window.showInformationMessage(`${selectedProvider.name} API key saved securely.`);

    // ── Step 4: Select Model ──
    let models: ModelEntry[] = [];

    // Try to fetch models from the API
    try {
        const resp = await fetch(`${backendUrl}/api/v1/providers/${selectedProvider.id}/models`, {
            signal: AbortSignal.timeout(10000),
        });
        if (resp.ok) {
            const data = await resp.json() as ProviderModelsResponse;
            if (data.models) {
                models = data.models;
            }
        }
    } catch {
        // API unreachable
    }

    if (models.length === 0) {
        // Fallback: ask user to type the model name manually
        const defaultModel = getDefaultModel(selectedProvider.id);
        const model = await vscode.window.showInputBox({
            prompt: `Enter the model name to use with ${selectedProvider.name}`,
            value: defaultModel,
            placeHolder: defaultModel,
        });
        if (model) {
            await context.secrets.store('vigilagent.selectedModel', model);
            vscode.window.showInformationMessage(`Model set to ${model}.`);
        }
        return;
    }

    // Show model picker with rich metadata
    const modelItems = models
        .filter(m => !m.deprecated)
        .map(m => {
            const caps = m.capabilities.length > 0 ? m.capabilities.join(', ') : 'basic';
            const cost = `$${m.input_cost_per_1m.toFixed(2)} / $${m.output_cost_per_1m.toFixed(2)} per 1M tokens`;
            const ctx = formatContextWindow(m.context_window);
            return {
                label: m.name,
                description: `${ctx} context`,
                detail: `${cost} — Capabilities: ${caps} — ${m.description}`,
                model: m,
            };
        });

    // Group models by capability tier
    const selectedModelItem = await vscode.window.showQuickPick(modelItems, {
        placeHolder: `Select a model from ${selectedProvider.name}`,
        matchOnDescription: true,
        matchOnDetail: true,
    });

    if (!selectedModelItem) { return; }

    await context.secrets.store('vigilagent.selectedModel', selectedModelItem.model.id);
    
    const modelInfo = `${selectedModelItem.model.name} (${formatContextWindow(selectedModelItem.model.context_window)} ctx, ${selectedModelItem.model.capabilities.join(', ')})`;
    vscode.window.showInformationMessage(`Model set to ${modelInfo}`);
}

function getDefaultModel(providerId: string): string {
    switch (providerId) {
        case 'openai': return 'gpt-4o';
        case 'anthropic': return 'claude-sonnet-4-20250514';
        case 'gemini': return 'gemini-2.5-pro';
        case 'groq': return 'llama-3.3-70b-versatile';
        case 'mistral': return 'mistral-large-latest';
        case 'cohere': return 'command-r-plus';
        case 'nvidia_nim': return 'nvidia/llama-3.1-70b-instruct';
        case 'openrouter': return 'openai/gpt-4o';
        default: return 'gpt-4o';
    }
}

function formatContextWindow(tokens: number): string {
    if (tokens >= 1000000) {
        return `${(tokens / 1000000).toFixed(0)}M`;
    }
    return `${(tokens / 1000).toFixed(0)}K`;
}

// ═══════════════════════════════════════════════════════════════════════════
// FILE OPERATIONS
// ═══════════════════════════════════════════════════════════════════════════

async function scanCurrentFile(client: VigilAgentClient): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const code = editor.document.getText();
    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
    const language = editor.document.languageId;

    vscode.window.showInformationMessage('VigilAgent: Scanning file...');

    try {
        const result = await client.scan(code, language, filename);
        const panel = vscode.window.createWebviewPanel(
            'vigilagent-results',
            'VigilAgent Scan Results',
            vscode.ViewColumn.Beside,
            {}
        );
        panel.webview.html = formatResultsWebview(result, filename);
    } catch (err: any) {
        vscode.window.showErrorMessage(`Scan failed: ${err.message}`);
    }
}

async function verifySelection(client: VigilAgentClient): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const selection = editor.document.getText(editor.selection);
    if (!selection) {
        vscode.window.showWarningMessage('No code selected.');
        return;
    }

    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
    const language = editor.document.languageId;

    vscode.window.showInformationMessage('VigilAgent: Verifying selection...');

    try {
        const result = await client.verify(selection, '', language, filename);
        const panel = vscode.window.createWebviewPanel(
            'vigilagent-results',
            'VigilAgent Verification Results',
            vscode.ViewColumn.Beside,
            {}
        );
        panel.webview.html = formatResultsWebview(result, filename);
    } catch (err: any) {
        vscode.window.showErrorMessage(`Verification failed: ${err.message}`);
    }
}

async function dualEngineAnalysis(client: VigilAgentClient): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const code = editor.document.getText();
    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
    const language = editor.document.languageId;

    vscode.window.showInformationMessage('VigilAgent: Running dual-engine analysis...');

    try {
        const result = await client.dualEngine(code, language);
        const panel = vscode.window.createWebviewPanel(
            'vigilagent-results',
            'VigilAgent Dual-Engine Results',
            vscode.ViewColumn.Beside,
            {}
        );
        panel.webview.html = formatDualEngineWebview(result, filename);
    } catch (err: any) {
        vscode.window.showErrorMessage(`Dual-engine analysis failed: ${err.message}`);
    }
}

function escapeHtml(text: string): string {
    if (!text) { return ''; }
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function formatDualEngineWebview(result: Record<string, unknown>, filename: string): string {
    const grade = (result.grade as string) || 'N/A';
    const score = (result.score as number) || 0;
    const stats = (result.engine_stats as Record<string, unknown>) || {};
    const det = (stats.deterministic as Record<string, unknown>) || {};
    const llm = (stats.llm as Record<string, unknown>) || {};
    const findings = (result.findings as Record<string, unknown>[]) || [];
    const corroborated = findings.filter((f: Record<string, unknown>) => (f.rule_id as string || '').endsWith('+llm')).length;

    let findingsHtml = '';
    if (findings.length > 0) {
        findingsHtml = `<h2>Findings (${findings.length}`;
        if (corroborated > 0) {
            findingsHtml += ` | ${corroborated} corroborated`;
        }
        findingsHtml += ')</h2>';
        for (const f of findings) {
            const severity = (f.severity as string) || 'unknown';
            const engine = (f.engine as string) || 'unknown';
            const message = (f.message as string) || '';
            const fix = f.fix ? `<br><em>Fix: ${escapeHtml(f.fix as string)}</em>` : '';
            const corrobMark = (f.rule_id as string || '').endsWith('+llm') ? ' ✓' : '';
            findingsHtml += `
        <div class="finding ${escapeHtml(severity)}">
            <strong>[${escapeHtml(severity.toUpperCase())}]</strong> (${escapeHtml(engine)})${corrobMark}
            ${escapeHtml(message)}
            ${fix}
        </div>`;
        }
    } else {
        findingsHtml = '<h2>✅ No issues found</h2><p>Code looks clean!</p>';
    }

    return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
    <style>
        body { font-family: var(--vscode-font-family); padding: 20px; color: var(--vscode-foreground); }
        h1 { color: var(--vscode-textLink-foreground); }
        h2 { margin-top: 20px; }
        .grade { font-size: 2em; font-weight: bold; }
        .grade-a { color: #4ec9b0; }
        .grade-b { color: #dcdcaa; }
        .grade-c { color: #ce9178; }
        .grade-d, .grade-f { color: #f44747; }
        .stats { display: flex; gap: 20px; margin: 15px 0; }
        .stat-box { padding: 10px; background: var(--vscode-editor-background); border-radius: 4px; flex: 1; }
        .stat-label { font-size: 0.9em; color: var(--vscode-descriptionForeground); }
        .stat-value { font-size: 1.2em; font-weight: bold; }
        .finding { margin: 8px 0; padding: 10px; background: var(--vscode-editor-background); border-radius: 4px; }
        .critical { border-left: 3px solid #f44747; }
        .high { border-left: 3px solid #ce9178; }
        .medium { border-left: 3px solid #dcdcaa; }
        .low { border-left: 3px solid #4ec9b0; }
        .info { border-left: 3px solid #808080; }
    </style>
</head>
<body>
    <h1>🛡️ Dual-Engine Analysis — ${escapeHtml(filename)}</h1>
    <div class="grade grade-${escapeHtml(grade.toLowerCase())}">Grade ${escapeHtml(grade)} — ${escapeHtml(String(score))}%</div>
    
    <div class="stats">
        <div class="stat-box">
            <div class="stat-label">Deterministic Engine</div>
            <div class="stat-value">${escapeHtml(String(det.findings_count ?? 0))} findings</div>
            <div class="stat-label">${escapeHtml(String(det.latency_ms ?? 0))}ms</div>
        </div>
        <div class="stat-box">
            <div class="stat-label">LLM Engine</div>
            <div class="stat-value">${escapeHtml(String(llm.findings_count ?? 0))} findings</div>
            <div class="stat-label">${escapeHtml(String(llm.latency_ms ?? 0))}ms (${escapeHtml(String(llm.model ?? 'unknown'))})</div>
        </div>
        <div class="stat-box">
            <div class="stat-label">Total Latency</div>
            <div class="stat-value">${escapeHtml(Number(stats.total_latency_ms || 0).toFixed(0))}ms</div>
        </div>
    </div>
    
    ${findingsHtml}
    
    <hr>
    <p><em>Powered by VigilAgent Dual-Engine Analysis</em></p>
</body>
</html>`;
}

function formatResultsWebview(result: Record<string, unknown>, filename: string): string {
    const confidence = (result.confidence as Record<string, unknown>) || {};
    const grade = (confidence.grade as string) || 'N/A';
    // Coerce to a number so a malicious/non-numeric backend value cannot flow
    // through to the HTML template (string * 100 → NaN, not injection, but
    // Number() + toFixed keeps rendering deterministic).
    const confScore = Number(confidence.confidence) || 0;
    const score = confScore ? `${(confScore * 100).toFixed(0)}%` : 'N/A';
    const reviewers = (result.reviewers as Record<string, unknown>[]) || [];
    const findings = (result.deterministic_findings as Record<string, unknown>[]) || [];
    const finalOutput = result.final_output as string | undefined;

    let reviewerHtml = '';
    if (reviewers.length > 0) {
        reviewerHtml = '<h2>Reviewer Verdicts</h2>';
        for (const r of reviewers) {
            const rFindings = (r.findings as string[]) || [];
            const rSuggestions = (r.suggestions as string[]) || [];
            const fHtml = rFindings.map((f: string) => `<div class="finding">• ${escapeHtml(f)}</div>`).join('');
            const sHtml = rSuggestions.length > 0 ? '<br><em>Suggestions:</em>' + rSuggestions.map((s: string) => `<div class="finding">→ ${escapeHtml(s)}</div>`).join('') : '';
            const verdict = (r.verdict as string) || 'unknown';
            const name = (r.name as string) || 'unknown';
            const role = (r.role as string) || '';
            reviewerHtml += `
        <div class="reviewer ${escapeHtml(verdict)}">
            <strong>${escapeHtml(name)}</strong> (${escapeHtml(role)}): ${escapeHtml(verdict.toUpperCase())}
            ${fHtml}${sHtml}
        </div>`;
        }
    }

    let findingsHtml = '';
    if (findings.length > 0) {
        findingsHtml = `<h2>Deterministic Findings (${findings.length})</h2>`;
        for (const f of findings) {
            const fix = f.fix ? `<br><em>Fix: ${escapeHtml(f.fix as string)}</em>` : '';
            const line = f.line ? `<br><small>Line ${escapeHtml(String(f.line))}</small>` : '';
            findingsHtml += `
        <div class="finding ${escapeHtml(f.severity as string)}">
            <strong>[${escapeHtml((f.severity as string).toUpperCase())}]</strong> ${escapeHtml(f.message as string)}
            ${fix}${line}
        </div>`;
        }
    }

    const outputHtml = finalOutput ? `<h2>Final Output</h2><pre>${escapeHtml(finalOutput)}</pre>` : '';

    return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
    <style>
        body { font-family: var(--vscode-font-family); padding: 20px; color: var(--vscode-foreground); }
        h1 { color: var(--vscode-textLink-foreground); }
        .grade { font-size: 2em; font-weight: bold; }
        .grade-a { color: #4ec9b0; }
        .grade-b { color: #dcdcaa; }
        .grade-c { color: #ce9178; }
        .grade-d { color: #f44747; }
        .reviewer { margin: 10px 0; padding: 10px; border-left: 3px solid; }
        .pass { border-color: #4ec9b0; }
        .fail { border-color: #f44747; }
        .warn { border-color: #dcdcaa; }
        .finding { margin: 5px 0; padding: 8px; background: var(--vscode-editor-background); border-radius: 4px; }
        .critical { border-left: 3px solid #f44747; }
        .high { border-left: 3px solid #ce9178; }
        .medium { border-left: 3px solid #dcdcaa; }
        .low { border-left: 3px solid #4ec9b0; }
        pre { background: var(--vscode-editor-background); padding: 10px; border-radius: 4px; overflow-x: auto; }
    </style>
</head>
<body>
    <h1>🛡️ VigilAgent Results — ${escapeHtml(filename)}</h1>
    <div class="grade grade-${escapeHtml(grade.toLowerCase())}">${escapeHtml(grade)} — ${escapeHtml(score)}</div>
    ${reviewerHtml}
    ${findingsHtml}
    ${outputHtml}
</body>
</html>`;
}

export function deactivate() {}
