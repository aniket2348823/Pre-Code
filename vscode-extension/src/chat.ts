import * as vscode from 'vscode';
import { VigilAgentClient, ReviewResult } from './client';

export class VigilAgentChatParticipant {
    private client: VigilAgentClient;

    constructor(client: VigilAgentClient) {
        this.client = client;
    }

    register(context: vscode.ExtensionContext): void {
        const participant = vscode.chat.createChatParticipant(
            'vigilagent',
            this.handleRequest.bind(this)
        );

        participant.iconPath = new vscode.ThemeIcon('shield');

        context.subscriptions.push(participant);
    }

    private async handleRequest(
        request: vscode.ChatRequest,
        context: vscode.ChatContext,
        stream: vscode.ChatResponseStream,
        token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        const prompt = request.prompt;

        // Parse the request to determine what to do
        const action = this.parseAction(prompt);

        switch (action.type) {
            case 'scan':
                return this.handleScan(action, stream, token);
            case 'verify':
                return this.handleVerify(action, stream, token);
            case 'help':
                return this.handleHelp(stream);
            default:
                return this.handleGeneral(prompt, stream, token);
        }
    }

    private parseAction(prompt: string): { type: string; code?: string; language?: string; filename?: string } {
        const lower = prompt.toLowerCase();

        if (lower.startsWith('scan ') || lower.startsWith('scanfile ')) {
            return { type: 'scan' };
        }
        if (lower.startsWith('verify ') || lower.startsWith('review ')) {
            return { type: 'verify' };
        }
        if (lower === 'help' || lower === '?') {
            return { type: 'help' };
        }
        return { type: 'general' };
    }

    private async handleScan(
        action: { code?: string; language?: string; filename?: string },
        stream: vscode.ChatResponseStream,
        token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        // Check if API keys are configured
        if (!(await this.client.isConfigured())) {
            stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette to set up your keys.');
            return {};
        }

        // Try to get code from the active editor
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            stream.markdown('⚠️ No active editor found. Open a file and try again.');
            return {};
        }

        const code = editor.document.getText();
        const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
        const language = editor.document.languageId;

        stream.progress('🔍 Running VigilAgent deterministic scan...');

        try {
            const result = await this.client.scan(code, language, filename);
            this.formatScanResult(result, stream, filename);
        } catch (err: any) {
            if (err.message?.includes('API key not configured')) {
                stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette.');
            } else {
                stream.markdown(`❌ Scan failed: ${err.message}`);
            }
        }

        return {};
    }

    private async handleVerify(
        action: { code?: string; language?: string; filename?: string },
        stream: vscode.ChatResponseStream,
        token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        // Check if API keys are configured
        if (!(await this.client.isConfigured())) {
            stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette to set up your keys.');
            return {};
        }

        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            stream.markdown('⚠️ No active editor found. Open a file and try again.');
            return {};
        }

        const code = editor.document.getText();
        const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
        const language = editor.document.languageId;

        stream.progress('🛡️ Running full Shift-Zero verification pipeline...');

        try {
            const result = await this.client.verify(code, '', language, filename);
            this.formatReviewResult(result, stream, filename);
        } catch (err: any) {
            if (err.message?.includes('API key not configured')) {
                stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette.');
            } else {
                stream.markdown(`❌ Verification failed: ${err.message}`);
            }
        }

        return {};
    }

    private async handleGeneral(
        prompt: string,
        stream: vscode.ChatResponseStream,
        token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        const editor = vscode.window.activeTextEditor;
        
        if (editor) {
            // If there's code in the editor, verify it with the prompt as context
            const code = editor.document.getText();
            const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
            const language = editor.document.languageId;

            stream.progress('🛡️ Running VigilAgent verification...');

            try {
                const result = await this.client.verify(code, prompt, language, filename);
                this.formatReviewResult(result, stream, filename);
            } catch (err: any) {
                stream.markdown(`❌ Verification failed: ${err.message}\n\nTry typing \`help\` for available commands.`);
            }
        } else {
            stream.markdown(this.getHelpText());
        }

        return {};
    }

    private handleHelp(stream: vscode.ChatResponseStream): vscode.ChatResult {
        stream.markdown(this.getHelpText());
        return {};
    }

    private getHelpText(): string {
        return `## 🛡️ VigilAgent Commands

| Command | Description |
|---------|-------------|
| \`scan\` | Run deterministic static analysis on the current file |
| \`verify\` | Run full Shift-Zero pipeline (deterministic + LLM reviewers) |
| \`help\` | Show this help message |

**Usage:**
- Open a file in the editor
- Type \`@vigilagent scan\` or \`@vigilagent verify\` in chat
- Or just type \`@vigilagent\` followed by your question about the code

**Configuration:**
Run \`VigilAgent: Configure API Keys\` from the Command Palette to set up your API keys.
`;
    }

    private formatScanResult(result: Record<string, unknown>, stream: vscode.ChatResponseStream, filename: string): void {
        const scanResult = result.scan_result as Record<string, unknown> | undefined;
        const findings = scanResult?.findings as Record<string, unknown>[] | undefined;
        const pipelineResult = result.pipeline_result as Record<string, unknown> | undefined;

        stream.markdown(`## 🔍 Scan Results — ${filename}\n\n`);

        if (pipelineResult) {
            const passed = pipelineResult.passed as boolean;
            const confidence = pipelineResult.confidence as number;
            const icon = passed ? '✅' : '❌';
            stream.markdown(`${icon} **Pipeline:** ${passed ? 'PASSED' : 'FAILED'} (confidence: ${((confidence || 0) * 100).toFixed(0)}%)\n\n`);
        }

        if (findings && findings.length > 0) {
            stream.markdown(`### Findings (${findings.length})\n\n`);
            for (const f of findings) {
                const severity = (f.severity as string) || 'unknown';
                const message = (f.message as string) || 'No message';
                const fix = f.fix as string | undefined;
                const sevIcon = this.severityIcon(severity);
                stream.markdown(`${sevIcon} **[${severity.toUpperCase()}]** ${message}\n`);
                if (fix) {
                    stream.markdown(`   💡 *Fix:* ${fix}\n`);
                }
                stream.markdown('\n');
            }
        } else {
            stream.markdown('✅ No findings detected.\n');
        }

        const skills = result.skills_extracted as unknown[] | undefined;
        if (skills && skills.length > 0) {
            stream.markdown(`\n### Skills Extracted: ${skills.length}\n`);
        }
    }

    private formatReviewResult(result: Record<string, unknown>, stream: vscode.ChatResponseStream, filename: string): void {
        stream.markdown(`## 🛡️ Verification Results — ${filename}\n\n`);

        // Confidence
        const confidence = result.confidence as Record<string, unknown> | undefined;
        if (confidence) {
            const grade = (confidence.grade as string) || 'N/A';
            const score = confidence.confidence as number;
            const reason = (confidence.reason as string) || '';
            const gradeIcon = this.gradeIcon(grade);
            stream.markdown(`${gradeIcon} **Confidence:** ${grade} (${((score || 0) * 100).toFixed(0)}%)\n`);
            if (reason) {
                stream.markdown(`> ${reason}\n\n`);
            }
        }

        // Reviewer verdicts
        const reviewers = result.reviewers as Record<string, unknown>[] | undefined;
        if (reviewers && reviewers.length > 0) {
            stream.markdown('### Reviewer Verdicts\n\n');
            for (const r of reviewers) {
                const name = (r.name as string) || 'unknown';
                const role = (r.role as string) || '';
                const verdict = (r.verdict as string) || 'unknown';
                const icon = this.verdictIcon(verdict);
                stream.markdown(`${icon} **${name}** (${role}): ${verdict.toUpperCase()}\n`);
                const rFindings = (r.findings as string[]) || [];
                for (const f of rFindings) {
                    stream.markdown(`   • ${f}\n`);
                }
                const rSuggestions = (r.suggestions as string[]) || [];
                if (rSuggestions.length > 0) {
                    stream.markdown(`   *Suggestions:* ${rSuggestions.join('; ')}\n`);
                }
                stream.markdown('\n');
            }
        }

        // Deterministic findings
        const findings = result.deterministic_findings as Record<string, unknown>[] | undefined;
        if (findings && findings.length > 0) {
            stream.markdown(`### Deterministic Findings (${findings.length})\n\n`);
            for (const f of findings) {
                const severity = (f.severity as string) || 'unknown';
                const message = (f.message as string) || '';
                const line = f.line as number | undefined;
                const fix = f.fix as string | undefined;
                const icon = this.severityIcon(severity);
                stream.markdown(`${icon} **[${severity.toUpperCase()}]** ${message}${line ? ` (line ${line})` : ''}\n`);
                if (fix) {
                    stream.markdown(`   💡 *Fix:* ${fix}\n`);
                }
            }
            stream.markdown('\n');
        }

        // Summary
        const summary = result.summary as string | undefined;
        if (summary) {
            stream.markdown(`### Summary\n${summary}\n`);
        }

        // Final output
        const finalOutput = result.final_output as string | undefined;
        const mainResponse = result.main_llm_response as string | undefined;
        if (finalOutput && finalOutput !== mainResponse) {
            stream.markdown(`### 📝 Improved Output\n\n\`\`\`\n${finalOutput}\n\`\`\`\n`);
        }
    }

    private severityIcon(severity: string): string {
        switch (severity?.toLowerCase()) {
            case 'critical': return '🔴';
            case 'high': return '🟠';
            case 'medium': return '🟡';
            case 'low': return '🟢';
            case 'info': return 'ℹ️';
            default: return '⚪';
        }
    }

    private verdictIcon(verdict: string): string {
        switch (verdict?.toLowerCase()) {
            case 'pass': return '✅';
            case 'fail': return '❌';
            case 'warn': return '⚠️';
            default: return '⚪';
        }
    }

    private gradeIcon(grade: string): string {
        switch (grade?.toUpperCase()) {
            case 'A': return '🟢';
            case 'B': return '🟡';
            case 'C': return '🟠';
            case 'D': case 'F': return '🔴';
            default: return '⚪';
        }
    }
}
