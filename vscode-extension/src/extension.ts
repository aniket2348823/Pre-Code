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
            await configureAPIKeys(context);
        }),
        vscode.commands.registerCommand('vigilagent.scanFile', async () => {
            await scanCurrentFile(client);
        }),
        vscode.commands.registerCommand('vigilagent.verifySelection', async () => {
            await verifySelection(client);
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

async function configureAPIKeys(context: vscode.ExtensionContext): Promise<void> {
    // Ask if running locally (no API key needed) or remote
    const mode = await vscode.window.showQuickPick(
        [
            'Local development (no API key needed)',
            'Remote / Production (enter API key)'
        ],
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
        // Store a placeholder key so the client doesn't throw
        await context.secrets.store('vigilagent.apiKey', 'local-dev');
        vscode.window.showInformationMessage('Configured for local development (no auth).');
    }

    // Store LLM provider API key
    const provider = await vscode.window.showQuickPick(
        ['NVIDIA NIM', 'OpenAI', 'Anthropic', 'Google Gemini', 'Mistral', 'Groq', 'Cohere'],
        { placeHolder: 'Select your LLM provider' }
    );
    if (provider) {
        const llmKey = await vscode.window.showInputBox({
            prompt: `Enter your ${provider} API key`,
            password: true,
            placeHolder: provider === 'NVIDIA NIM' ? 'nvapi-...' : 'sk-...'
        });
        if (llmKey) {
            await context.secrets.store(`vigilagent.llmKey.${provider}`, llmKey);
            await context.secrets.store('vigilagent.selectedProvider', provider);
            vscode.window.showInformationMessage(`${provider} API key saved securely.`);
        }

        // Ask for model name
        let defaultModel = 'gpt-4o';
        if (provider === 'NVIDIA NIM') { defaultModel = 'kimi-k2.6'; }
        else if (provider === 'Anthropic') { defaultModel = 'claude-sonnet-4-20250514'; }
        else if (provider === 'Google Gemini') { defaultModel = 'gemini-2.5-pro'; }
        else if (provider === 'Mistral') { defaultModel = 'mistral-large-latest'; }

        const model = await vscode.window.showInputBox({
            prompt: `Enter the model name to use with ${provider}`,
            value: defaultModel,
            placeHolder: defaultModel
        });
        if (model) {
            await context.secrets.store('vigilagent.selectedModel', model);
            vscode.window.showInformationMessage(`Model set to ${model}.`);
        }
    }
}

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

function escapeHtml(text: string): string {
    if (!text) { return ''; }
    return text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function formatResultsWebview(result: Record<string, unknown>, filename: string): string {
    const confidence = (result.confidence as Record<string, unknown>) || {};
    const grade = (confidence.grade as string) || 'N/A';
    const confScore = confidence.confidence as number;
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
            const line = f.line ? `<br><small>Line ${f.line}</small>` : '';
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
    <div class="grade grade-${grade.toLowerCase()}">${escapeHtml(grade)} — ${escapeHtml(score)}</div>
    ${reviewerHtml}
    ${findingsHtml}
    ${outputHtml}
</body>
</html>`;
}

export function deactivate() {}
