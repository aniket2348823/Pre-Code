import * as vscode from 'vscode';
import { VigilAgentChatParticipant } from './chat';
import { VigilAgentClient, Finding, GatewayModel } from './client';
import { VigilAgentStatusBar } from './statusbar';
import { DiagnosticManager } from './diagnostics';
import { AutoVerifier } from './autoVerify';
import { SuggestionStore, suggestionsToFindings, offsetSuggestions } from './suggestionStore';
import { SuggestionCodeActionProvider } from './codeActions';
import { GatewayOutputRegistry, extractCodeBlocks } from './detect';

// ═══════════════════════════════════════════════════════════════════════════
// SECURE AI GATEWAY — NATIVE LANGUAGE MODEL CHAT PROVIDER (spec section E)
// The gateway is registered as a vendor in VS Code's standard chat model
// picker. Every chat request therefore flows through the gateway by
// construction — this is how VS Code traffic is guaranteed to hit the
// dual-engine scan (Plane 1, authoritative).
// ═══════════════════════════════════════════════════════════════════════════

class VigilAgentChatModelProvider implements vscode.LanguageModelChatProvider {
    private cachedModels: GatewayModel[] = [];
    private changeEmitter = new vscode.EventEmitter<void>();

    constructor(
        private readonly client: VigilAgentClient,
        private readonly gatewayUrl: string,
        private readonly gatewayOutputs: GatewayOutputRegistry
    ) {}

    // Fired when the gateway model catalog changes (e.g. after re-fetch).
    get onDidChangeLanguageModelChatInformation(): vscode.Event<void> {
        return this.changeEmitter.event;
    }

    // Model discovery: the models shown in the picker come from the gateway's
    // /v1/models catalog, so only gateway-approved models are ever usable.
    // Results are cached so the editor isn't hammered with re-fetches; the
    // change event fires only when the catalog actually differs.
    async provideLanguageModelChatInformation(
        _options: vscode.PrepareLanguageModelChatModelOptions,
        _token: vscode.CancellationToken
    ): Promise<vscode.LanguageModelChatInformation[]> {
        if (this.cachedModels.length > 0) {
            return this.cachedModels.map((m) => ({
                id: m.id,
                name: m.name,
                family: m.provider,
                version: '1.0.0',
                tooltip: `Secured by VigilAgent — ${m.provider} model, scanned before release`,
                detail: `${m.provider} · ${m.context_window} context`,
                maxInputTokens: m.context_window,
                maxOutputTokens: m.max_output,
                capabilities: {
                    toolCalling: m.capabilities.includes('tool_call') || m.capabilities.includes('function_call'),
                    imageInput: m.capabilities.includes('vision'),
                },
            }));
        }
        const models = await this.client.listGatewayModels(this.gatewayUrl);
        if (models.length > 0) {
            const changed = this.cachedModels.length !== models.length ||
                this.cachedModels.some((m, i) => m.id !== models[i]?.id);
            this.cachedModels = models;
            if (changed) {
                this.changeEmitter.fire();
            }
        }
        return models.map((m) => ({
            id: m.id,
            name: m.name,
            family: m.provider,
            version: '1.0.0',
            tooltip: `Secured by VigilAgent — ${m.provider} model, scanned before release`,
            detail: `${m.provider} · ${m.context_window} context`,
            maxInputTokens: m.context_window,
            maxOutputTokens: m.max_output,
            capabilities: {
                toolCalling: m.capabilities.includes('tool_call') || m.capabilities.includes('function_call'),
                imageInput: m.capabilities.includes('vision'),
            },
        }));
    }

    // Gateway chat: every request is forwarded to /v1/responses, where the
    // design gate runs, both engines scan the output, policy decides, and a
    // provenance record is signed — before any of it reaches the user. Text
    // chunks stream through as they arrive.
    async provideLanguageModelChatResponse(
        model: vscode.LanguageModelChatInformation,
        messages: readonly vscode.LanguageModelChatRequestMessage[],
        _options: vscode.ProvideLanguageModelChatResponseOptions,
        progress: vscode.Progress<vscode.LanguageModelResponsePart>,
        token: vscode.CancellationToken
    ): Promise<void> {
        const history = messages.map((m) => ({
            role: roleToString(m.role),
            content: partsToString(m.content),
        }));

        try {
            const result = await this.client.chatViaGateway(
                this.gatewayUrl,
                history,
                model.id,
                (chunk) => {
                    if (!token.isCancellationRequested) {
                        progress.report(new vscode.LanguageModelTextPart(chunk));
                    }
                }
            );

            // Provenance footer — makes verified / unverified visible right in
            // the chat response, alongside the scan id (spec section 4).
            const provLabel = result.provenance || 'unverified';
            const scanRef = result.scanId ? ` · scan ${result.scanId}` : '';
            const gate = result.designGate ? ` · design gate: ${result.designGate}` : '';
            progress.report(new vscode.LanguageModelTextPart(`\n\n🛡️ [VigilAgent] provenance: ${provLabel}${scanRef}${gate}`));

            // Register every code block this response generated so that when
            // the user inserts it into an editor, AutoVerifier recognizes it
            // and scans it regardless of size (not just big pastes).
            const scanId = result.scanId || `gateway-${Date.now().toString(36)}`;
            for (const block of extractCodeBlocks(result.text)) {
                this.gatewayOutputs.register(block, {
                    scanId,
                    provenance: provLabel,
                });
            }

            // Record the scan in the Security Findings view so the audit trail
            // (scan id + provenance) is visible after the fact.
            recordScan({
                scanId,
                filename: model.id,
                grade: 'N/A',
                score: 0,
                provenance: provLabel,
                findings: [],
                timestamp: Date.now(),
            });
        } catch (err) {
            // User cancelled mid-stream — surface as a cancellation, not an error.
            if (token.isCancellationRequested) {
                throw new vscode.CancellationError();
            }
            const message = err instanceof Error ? err.message : String(err);
            // Only genuine policy blocks (HTTP 451 / held review / quota) are
            // "Blocked"; network failures and timeouts rethrow as-is so the UI
            // distinguishes a gateway outage from a policy decision.
            if (/blocked by VigilAgent policy|policy_block|quota exceeded|model not allowed|451/i.test(message)) {
                throw vscode.LanguageModelError.Blocked(message);
            }
            throw err instanceof Error ? err : new Error(message);
        }
    }

    // Rough token estimate — used by the chat UI for context accounting.
    async provideTokenCount(
        _model: vscode.LanguageModelChatInformation,
        text: string | vscode.LanguageModelChatRequestMessage,
        _token: vscode.CancellationToken
    ): Promise<number> {
        const content = typeof text === 'string' ? text : partsToString(text.content);
        return Math.ceil(content.length / 4);
    }
}

function roleToString(role: vscode.LanguageModelChatMessageRole): string {
    // The chat-view role enum only distinguishes User from Assistant; any
    // future/unknown role is forwarded as user to keep the gateway happy.
    switch (role) {
        case vscode.LanguageModelChatMessageRole.Assistant:
            return 'assistant';
        case vscode.LanguageModelChatMessageRole.User:
        default:
            return 'user';
    }
}

function partsToString(parts: ReadonlyArray<unknown>): string {
    return parts
        .map((part) => {
            if (part instanceof vscode.LanguageModelTextPart) {
                return part.value;
            }
            if (part && typeof part === 'object' && 'value' in (part as { value?: unknown })) {
                const v = (part as { value?: unknown }).value;
                return typeof v === 'string' ? v : '';
            }
            return '';
        })
        .filter((s) => s.length > 0)
        .join('\n');
}

// ═══════════════════════════════════════════════════════════════════════════
// SECURITY FINDINGS VIEW (spec section E)
// Sidebar tree grouped by severity, then scan id, showing findings with
// provenance labels (verified / unverified / bypassed) and grade.
// ═══════════════════════════════════════════════════════════════════════════

interface ScanRecord {
    scanId: string;
    filename: string;
    grade: string;
    score: number;
    provenance: string; // verified | unverified | bypassed
    findings: Finding[];
    timestamp: number;
    uri?: string; // document URI so findings can be opened even in nested paths
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low', 'info'];
const SEVERITY_LABEL: Record<string, string> = {
    critical: 'Critical',
    high: 'High',
    medium: 'Medium',
    low: 'Low',
    info: 'Info',
    clean: 'Clean scans',
};

class SeverityNode extends vscode.TreeItem {
    constructor(
        readonly severity: string,
        count: number,
        readonly scans: ScanRecord[]
    ) {
        super(
            `${SEVERITY_LABEL[severity] || severity} (${count})`,
            vscode.TreeItemCollapsibleState.Expanded
        );
        this.contextValue = 'severity';
    }
}

class ScanNode extends vscode.TreeItem {
    constructor(
        readonly record: ScanRecord,
        readonly severity: string,
        findingCount: number
    ) {
        const shortId = record.scanId.length > 12 ? `${record.scanId.slice(0, 12)}…` : record.scanId;
        super(
            `${record.filename} — ${shortId}`,
            findingCount > 0 ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.None
        );
        this.description = `grade ${record.grade} · ${record.provenance}`;
        this.tooltip = `scan ${record.scanId}\nprovenance: ${record.provenance}\n${new Date(record.timestamp).toLocaleString()}`;
        this.contextValue = 'scan';
    }
}

class FindingNode extends vscode.TreeItem {
    constructor(readonly finding: Finding, readonly record: ScanRecord) {
        const line = finding.line ? ` (line ${finding.line})` : '';
        super(`${finding.message}${line}`, vscode.TreeItemCollapsibleState.None);
        this.description = `${finding.analyzers?.join(', ') || ''}`;
        this.tooltip = finding.fix || finding.message;
        this.contextValue = 'finding';
        this.command = {
            command: 'vigilagent.openFinding',
            title: 'Open',
            arguments: [record, finding],
        };
    }
}

class SecurityFindingsProvider implements vscode.TreeDataProvider<SeverityNode | ScanNode | FindingNode> {
    private records: ScanRecord[] = [];
    private readonly changeEmitter = new vscode.EventEmitter<SeverityNode | ScanNode | FindingNode | undefined>();

    readonly onDidChangeTreeData = this.changeEmitter.event;

    add(record: ScanRecord): void {
        this.records.unshift(record);
        this.changeEmitter.fire(undefined);
    }

    clear(): void {
        this.records = [];
        this.changeEmitter.fire(undefined);
    }

    // Repaints the tree without clearing history.
    refresh(): void {
        this.changeEmitter.fire(undefined);
    }

    getTreeItem(element: SeverityNode | ScanNode | FindingNode): vscode.TreeItem {
        return element;
    }

    getChildren(element?: SeverityNode | ScanNode | FindingNode): vscode.ProviderResult<Array<SeverityNode | ScanNode | FindingNode>> {
        if (!element) {
            // Root: group records by severity, newest first, plus a "Clean"
            // group so zero-finding scans (the common case) still surface with
            // their scan id + provenance label — the audit trail stays visible.
            const bySeverity = new Map<string, ScanRecord[]>();
            const clean: ScanRecord[] = [];
            for (const r of this.records) {
                if (r.findings.length === 0) {
                    clean.push(r);
                    continue;
                }
                for (const f of r.findings) {
                    const sev = f.severity.toLowerCase();
                    if (!bySeverity.has(sev)) {
                        bySeverity.set(sev, []);
                    }
                    const list = bySeverity.get(sev)!;
                    if (list.indexOf(r) === -1) {
                        list.push(r);
                    }
                }
            }
            const nodes: Array<SeverityNode | ScanNode | FindingNode> = [];
            for (const sev of SEVERITY_ORDER) {
                const scans = bySeverity.get(sev);
                if (scans && scans.length > 0) {
                    nodes.push(new SeverityNode(sev, countFindings(scans, sev), scans));
                }
            }
            if (clean.length > 0) {
                nodes.push(new SeverityNode('clean', clean.length, clean));
            }
            return nodes;
        }
        if (element instanceof SeverityNode) {
            return element.scans.map((r) => new ScanNode(r, element.severity, countFindings([r], element.severity)));
        }
        if (element instanceof ScanNode) {
            return element.record.findings
                .filter((f) => f.severity.toLowerCase() === element.severity)
                .map((f) => new FindingNode(f, element.record));
        }
        return [];
    }
}

function countFindings(scans: ScanRecord[], severity: string): number {
    return scans.reduce((n, r) => n + r.findings.filter((f) => f.severity.toLowerCase() === severity).length, 0);
}

// Scan-history store shared by every scan command + the chat provider, so the
// Security Findings view shows the complete audit trail for the session.
let findingsProvider: SecurityFindingsProvider | undefined;

function recordScan(record: ScanRecord): void {
    if (findingsProvider) {
        findingsProvider.add(record);
    }
}

export function activate(context: vscode.ExtensionContext) {
    const config = vscode.workspace.getConfiguration('vigilagent');
    const backendUrl = config.get<string>('backendUrl', 'http://localhost:8080');
    const gatewayUrl = config.get<string>('gatewayUrl', 'http://localhost:9090');

    // Initialize the backend client
    const client = new VigilAgentClient(backendUrl);
    client.setContext(context);

    // Suggestion store: line-anchored accept/reject fixes + dismiss state.
    const suggestionStore = new SuggestionStore();
    context.subscriptions.push(suggestionStore);

    // Initialize Diagnostic Manager (created before commands so verifySelection
    // can push line-anchored suggestion squiggles) & AutoVerifier
    const diagnosticManager = new DiagnosticManager();
    context.subscriptions.push(diagnosticManager);

    // Register the chat participant
    const participant = new VigilAgentChatParticipant(client);
    participant.register(context);

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('vigilagent.configure', async () => {
            await configureProviderWizard(context);
        }),
        vscode.commands.registerCommand('vigilagent.scanFile', async () => {
            await scanCurrentFile(client, suggestionStore);
        }),
        vscode.commands.registerCommand('vigilagent.scanChangedFiles', async () => {
            await scanChangedFiles(client);
        }),
        vscode.commands.registerCommand('vigilagent.scanDesignDocument', async () => {
            await scanDesignDocument(client);
        }),
        vscode.commands.registerCommand('vigilagent.verifySelection', async () => {
            await verifySelection(client, suggestionStore, diagnosticManager);
        }),
        vscode.commands.registerCommand('vigilagent.dualEngine', async () => {
            await dualEngineAnalysis(client, suggestionStore);
        }),
        vscode.commands.registerCommand('vigilagent.dismissSuggestion', (uri: vscode.Uri, id: string) => {
            suggestionStore.dismiss(uri, id);
            // Rebuild diagnostics from the remaining suggestions so the
            // dismissed squiggle disappears immediately.
            diagnosticManager.updateDiagnostics(uri, suggestionsToFindings(suggestionStore.get(uri)));
        })
    );

    // Quick fixes: every suggestion gets an Apply fix / Dismiss action. The
    // user decides per line — the engine never auto-modifies the file.
    context.subscriptions.push(
        vscode.languages.registerCodeActionsProvider(
            { scheme: 'file' },
            new SuggestionCodeActionProvider(suggestionStore),
            { providedCodeActionKinds: SuggestionCodeActionProvider.providedCodeActionKinds }
        )
    );

    // Status bar for confidence scores
    const statusBar = new VigilAgentStatusBar();
    context.subscriptions.push(statusBar);

    // ── Secure AI Gateway: native language-model chat provider ──
    // Registers as vendor 'vigilagent' so gateway models appear in the standard
    // chat model picker; every chat request flows through the gateway. The
    // gateway-output registry lets AutoVerifier attribute editor inserts back
    // to the gateway scan that produced them.
    const gatewayOutputs = new GatewayOutputRegistry();
    const chatModelProvider = new VigilAgentChatModelProvider(client, gatewayUrl, gatewayOutputs);
    context.subscriptions.push(
        vscode.lm.registerLanguageModelChatProvider('vigilagent', chatModelProvider)
    );

    const autoVerifier = new AutoVerifier(client, diagnosticManager, suggestionStore, gatewayOutputs);
    // Every AutoVerifier scan emits an audit record (verified provenance when
    // the inserted code was gateway-generated) into the Security Findings view.
    autoVerifier.onRecord = (record) => recordScan(record);
    autoVerifier.register(context);
    context.subscriptions.push(autoVerifier);

    // ── Security Findings view (sidebar tree) ──
    findingsProvider = new SecurityFindingsProvider();
    context.subscriptions.push(
        vscode.window.registerTreeDataProvider('vigilagentFindings', findingsProvider),
        vscode.commands.registerCommand('vigilagent.refreshFindings', () => {
            if (findingsProvider) {
                findingsProvider.refresh();
            }
        }),
        vscode.commands.registerCommand('vigilagent.clearFindings', () => {
            if (findingsProvider) {
                findingsProvider.clear();
            }
        }),
        // Opens the file at the finding's line from the Security Findings view.
        vscode.commands.registerCommand('vigilagent.openFinding', async (record: ScanRecord, finding: Finding) => {
            const filename = record.filename;
            // Prefer the exact document URI captured at scan time (handles
            // nested paths), then the active editor, then a workspace search.
            let uri: vscode.Uri | undefined;
            if (record.uri) {
                uri = vscode.Uri.parse(record.uri);
            } else {
                const editor = vscode.window.activeTextEditor;
                if (editor && editor.document.uri.path.split('/').pop() === filename) {
                    uri = editor.document.uri;
                } else if (vscode.workspace.workspaceFolders?.[0]) {
                    const matches = await vscode.workspace.findFiles(`**/${filename}`);
                    uri = matches[0];
                }
            }
            if (!uri) {
                vscode.window.showWarningMessage(`VigilAgent: cannot locate ${filename} in the workspace.`);
                return;
            }
            const doc = await vscode.workspace.openTextDocument(uri);
            const view = await vscode.window.showTextDocument(doc);
            if (finding.line && finding.line > 0) {
                const line = Math.min(finding.line - 1, doc.lineCount - 1);
                const range = doc.lineAt(line).range;
                view.selection = new vscode.Selection(range.start, range.end);
                view.revealRange(range, vscode.TextEditorRevealType.InCenter);
            }
        })
    );

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

async function scanCurrentFile(client: VigilAgentClient, suggestionStore: SuggestionStore): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const code = editor.document.getText();
    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
    const language = editor.document.languageId;

    // A fresh scan replaces any previous suggestions on this file.
    suggestionStore.clear(editor.document.uri);

    vscode.window.showInformationMessage('VigilAgent: Scanning file...');

    try {
        const result = await client.scan(code, language, filename);
        recordScan({
            scanId: (result.scan_id as string) || `scan-${Date.now().toString(36)}`,
            filename,
            grade: (result.grade as string) || 'N/A',
            score: Number(result.score) || 0,
            provenance: 'unverified',
            findings: extractFindings(result),
            timestamp: Date.now(),
            uri: editor.document.uri.toString(),
        });
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

// ── Git integration for "Scan Changed Files" (spec: Plane 3 fallback) ──
// Uses the built-in vscode.git extension API — no extra dependencies.
interface GitRepository {
    state: { workingTreeChanges: Array<{ uri: vscode.Uri }> };
}
interface GitAPI {
    repositories: GitRepository[];
}
interface GitExtension {
    getAPI(version: number): GitAPI;
}

interface ChangedFileResult {
    filename: string;
    grade: string;
    score: number;
    findings: number;
}

// scanChangedFiles scans every file with uncommitted working-tree changes. This
// is the post-generation safety net: even when code never flowed through the
// gateway, anything that landed in the workspace still gets scanned before merge.
async function scanChangedFiles(client: VigilAgentClient): Promise<void> {
    const gitExt = vscode.extensions.getExtension<GitExtension>('vscode.git');
    const git = gitExt && gitExt.exports;
    if (!git) {
        vscode.window.showWarningMessage('VigilAgent: the Git extension is unavailable — cannot list changed files.');
        return;
    }

    let api: GitAPI;
    try {
        api = git.getAPI(1);
    } catch {
        vscode.window.showWarningMessage('VigilAgent: this VS Code version does not expose the Git API.');
        return;
    }

    const changed = new Map<string, vscode.Uri>();
    for (const repo of api.repositories) {
        for (const change of repo.state.workingTreeChanges) {
            changed.set(change.uri.toString(), change.uri);
        }
    }
    if (changed.size === 0) {
        vscode.window.showInformationMessage('VigilAgent: no changed files in the working tree.');
        return;
    }

    vscode.window.showInformationMessage(`VigilAgent: scanning ${changed.size} changed file(s)...`);
    const results: ChangedFileResult[] = [];
    let failed = 0;
    for (const uri of changed.values()) {
        try {
            const doc = await vscode.workspace.openTextDocument(uri);
            const code = doc.getText();
            const filename = uri.path.split('/').pop() || 'unknown';
            const result = await client.scan(code, doc.languageId, filename);
            const findings = extractFindings(result);
            results.push({
                filename,
                grade: (result.grade as string) || 'N/A',
                score: Number(result.score) || 0,
                findings: findings.length,
            });
            recordScan({
                scanId: (result.scan_id as string) || `scan-${Date.now().toString(36)}`,
                filename,
                grade: (result.grade as string) || 'N/A',
                score: Number(result.score) || 0,
                provenance: 'unverified',
                findings,
                timestamp: Date.now(),
                uri: uri.toString(),
            });
        } catch {
            failed++;
        }
    }

    const panel = vscode.window.createWebviewPanel(
        'vigilagent-changed',
        'VigilAgent — Changed Files Scan',
        vscode.ViewColumn.Beside,
        {}
    );
    panel.webview.html = formatChangedFilesWebview(results, failed);
}

function formatChangedFilesWebview(results: ChangedFileResult[], failed: number): string {
    const rows = results.map(r => `
        <tr>
            <td class="file">${escapeHtml(r.filename)}</td>
            <td class="grade grade-${escapeHtml(r.grade.toLowerCase())}">${escapeHtml(r.grade)}</td>
            <td>${r.score}</td>
            <td>${r.findings}</td>
        </tr>`).join('');
    const failNote = failed > 0 ? `<p class="warn-note">⚠️ ${failed} file(s) could not be scanned (unreadable or backend error).</p>` : '';
    return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
    <style>
        body { font-family: var(--vscode-font-family); padding: 20px; color: var(--vscode-foreground); }
        table { border-collapse: collapse; width: 100%; }
        th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid var(--vscode-panel-border); }
        th { color: var(--vscode-descriptionForeground); }
        .file { font-family: var(--vscode-editor-font-family); }
        .grade { font-weight: bold; }
        .grade-a { color: #4ec9b0; } .grade-b { color: #dcdcaa; } .grade-c { color: #ce9178; }
        .grade-d, .grade-f { color: #f44747; }
        .warn-note { color: #ce9178; }
    </style>
</head>
<body>
    <h1>🛡️ Changed Files Scan</h1>
    <p><em>Post-generation safety net: ${results.length} file(s) with uncommitted changes scanned.</em></p>
    <table>
        <tr><th>File</th><th>Grade</th><th>Score</th><th>Findings</th></tr>
        ${rows}
    </table>
    ${failNote}
</body>
</html>`;
}

// scanDesignDocument runs the deterministic engine over a design document,
// prompt, or architecture plan (spec section 7 — design-stage gate).
async function scanDesignDocument(client: VigilAgentClient): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const text = editor.document.getText();
    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'design.md';

    vscode.window.showInformationMessage('VigilAgent: scanning design document...');

    try {
        const result = await client.scan(text, 'markdown', filename);
        recordScan({
            scanId: (result.scan_id as string) || `design-${Date.now().toString(36)}`,
            filename,
            grade: (result.grade as string) || 'N/A',
            score: Number(result.score) || 0,
            provenance: 'unverified',
            findings: extractFindings(result),
            timestamp: Date.now(),
            uri: editor.document.uri.toString(),
        });
        const panel = vscode.window.createWebviewPanel(
            'vigilagent-design',
            'VigilAgent — Design Document Scan',
            vscode.ViewColumn.Beside,
            {}
        );
        panel.webview.html = formatDesignWebview(result, filename);
    } catch (err: any) {
        vscode.window.showErrorMessage(`Design scan failed: ${err.message}`);
    }
}

function formatDesignWebview(result: Record<string, unknown>, filename: string): string {
    const grade = (result.grade as string) || 'N/A';
    const score = Number(result.score) || 0;
    const findings = (result.findings as Record<string, unknown>[]) || [];

    let findingsHtml = '';
    if (findings.length > 0) {
        findingsHtml = `<h2>Design Findings (${findings.length})</h2>`;
        for (const f of findings) {
            const fix = f.fix ? `<br><em>Fix: ${escapeHtml(f.fix as string)}</em>` : '';
            findingsHtml += `
        <div class="finding ${escapeHtml(f.severity as string)}">
            <strong>[${escapeHtml((f.severity as string).toUpperCase())}]</strong> ${escapeHtml(f.message as string)}
            ${fix}
        </div>`;
        }
    } else {
        findingsHtml = '<h2>✅ No design-stage issues found</h2><p>The design document scans clean.</p>';
    }

    return `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
    <style>
        body { font-family: var(--vscode-font-family); padding: 20px; color: var(--vscode-foreground); }
        h1 { color: var(--vscode-textLink-foreground); }
        .grade { font-size: 2em; font-weight: bold; }
        .grade-a { color: #4ec9b0; } .grade-b { color: #dcdcaa; } .grade-c { color: #ce9178; }
        .grade-d, .grade-f { color: #f44747; }
        .finding { margin: 8px 0; padding: 10px; background: var(--vscode-editor-background); border-radius: 4px; }
        .critical { border-left: 3px solid #f44747; } .high { border-left: 3px solid #ce9178; }
        .medium { border-left: 3px solid #dcdcaa; } .low { border-left: 3px solid #4ec9b0; }
    </style>
</head>
<body>
    <h1>🛡️ Design Document Scan — ${escapeHtml(filename)}</h1>
    <div class="grade grade-${escapeHtml(grade.toLowerCase())}">Grade ${escapeHtml(grade)} — ${score}%</div>
    ${findingsHtml}
    <hr>
    <p><em>Design-stage gate: findings here become policy-mandated constraints when code is generated through the VigilAgent gateway.</em></p>
</body>
</html>`;
}

async function verifySelection(client: VigilAgentClient, suggestionStore: SuggestionStore, diagnosticManager: DiagnosticManager): Promise<void> {
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
    const uri = editor.document.uri;

    vscode.window.showInformationMessage('VigilAgent: Verifying selection...');

    try {
        const result = await client.verify(selection, '', language, filename);

        // Surface line-anchored suggestions as squiggles + quick fixes.
        // The backend numbers lines 1-indexed WITHIN the selected snippet, so
        // remap them to document coordinates before attaching them.
        const suggestions = offsetSuggestions(result.suggestions || [], editor.selection.start.line);
        suggestionStore.set(uri, suggestions);
        diagnosticManager.updateDiagnostics(uri, suggestionsToFindings(suggestions));

        recordScan({
            scanId: (result.scan_id as string) || `verify-${Date.now().toString(36)}`,
            filename,
            grade: (result.confidence?.grade as string) || 'N/A',
            score: Number(result.confidence?.confidence) * 100 || 0,
            provenance: 'unverified',
            findings: (result.deterministic_findings as Finding[]) || [],
            timestamp: Date.now(),
            uri: editor.document.uri.toString(),
        });

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



async function dualEngineAnalysis(client: VigilAgentClient, suggestionStore: SuggestionStore): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
        vscode.window.showWarningMessage('No active editor found.');
        return;
    }

    const code = editor.document.getText();
    const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
    const language = editor.document.languageId;

    // A fresh analysis replaces any previous suggestions on this file.
    suggestionStore.clear(editor.document.uri);

    vscode.window.showInformationMessage('VigilAgent: Running dual-engine analysis...');

    try {
        const result = await client.dualEngine(code, language);
        recordScan({
            scanId: (result.scan_id as string) || `dual-${Date.now().toString(36)}`,
            filename,
            grade: (result.grade as string) || 'N/A',
            score: Number(result.score) || 0,
            provenance: 'unverified',
            findings: (result.findings || []).map((f) => ({
                severity: String(f.severity || 'info'),
                message: String(f.message || ''),
                filename,
                line: Number(f.line) || 0,
                snippet: f.snippet ? String(f.snippet) : '',
                fix: f.fix ? String(f.fix) : '',
                confidence: Number(f.confidence) || 0,
                analyzers: f.engine ? [String(f.engine)] : [],
            })),
            timestamp: Date.now(),
            uri: editor.document.uri.toString(),
        });
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

// extractFindings tolerantly pulls a Finding[] out of any backend result
// shape: top-level `findings`, `scan_result.findings`, or the review-shaped
// `deterministic_findings` — whichever the endpoint happened to return.
function extractFindings(result: Record<string, unknown>): Finding[] {
    const top = result.findings as Record<string, unknown>[] | undefined;
    const scanResult = result.scan_result as Record<string, unknown> | undefined;
    const nested = scanResult?.findings as Record<string, unknown>[] | undefined;
    const det = result.deterministic_findings as Record<string, unknown>[] | undefined;
    const raw = top || nested || det || [];
    const filename = String(result.filename || 'unknown');
    return raw.map((f) => ({
        severity: String(f.severity || 'info'),
        message: String(f.message || ''),
        filename: String(f.filename || filename),
        line: Number(f.line) || 0,
        snippet: f.snippet ? String(f.snippet) : '',
        fix: f.fix ? String(f.fix) : '',
        confidence: Number(f.confidence) || 0,
        analyzers: Array.isArray(f.analyzers) ? (f.analyzers as string[]).map(String) : [],
    }));
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
