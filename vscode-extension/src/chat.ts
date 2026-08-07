import * as vscode from 'vscode';
import { VigilAgentClient } from './client';

// Sanitize server-derived text before it becomes chat markdown. Finding
// messages can embed snippets of scanned (untrusted) code; neutralizing
// markdown image syntax and javascript: URLs stops an attacker-controlled
// snippet from turning into an exfiltrating image load or a clickable URL
// inside the chat view.
function sanitizeMarkdown(text: string | undefined | null): string {
    if (!text) {
        return '';
    }
    const out = String(text)
        .replace(/!\[/g, '[')
        // \b prevents mangling words that merely end in "data:" (e.g. "metadata:").
        .replace(/\b(?:javascript|vbscript|data)\s*:/gi, '');
    // Drop C0/C1 control characters (except tab) — avoids embedding raw
    // control bytes and keeps the regex lint-clean.
    let cleaned = '';
    for (const ch of out) {
        const code = ch.charCodeAt(0);
        if ((code < 0x20 && code !== 0x09) || code === 0x7f) {
            continue;
        }
        cleaned += ch;
    }
    return cleaned;
}

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
            case 'dualengine':
                return this.handleDualEngine(action, stream, token);
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
        if (lower.startsWith('dual') || lower.startsWith('deep') || lower.startsWith('parallel')) {
            return { type: 'dualengine' };
        }
        if (lower === 'help' || lower === '?') {
            return { type: 'help' };
        }
        return { type: 'general' };
    }

    private async handleScan(
        action: { code?: string; language?: string; filename?: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
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
                stream.markdown(`❌ Scan failed: ${sanitizeMarkdown(err.message)}`);
            }
        }

        return {};
    }

    private async handleVerify(
        action: { code?: string; language?: string; filename?: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
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
            // Classic chat flow: auto-fixed "Improved Output" (not suggestion
            // mode) — suggestion mode powers the inline quick-fix surfaces.
            const result = await this.client.verify(code, '', language, filename, false);
            this.formatReviewResult(result, stream, filename);
        } catch (err: any) {
            if (err.message?.includes('API key not configured')) {
                stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette.');
            } else {
                stream.markdown(`❌ Verification failed: ${sanitizeMarkdown(err.message)}`);
            }
        }

        return {};
    }

    private async handleDualEngine(
        action: { code?: string; language?: string; filename?: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
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

        stream.progress('🛡️ Running dual-engine analysis (deterministic + LLM in parallel)...');

        try {
            const result = await this.client.dualEngine(code, language);
            this.formatDualEngineResult(result, stream, filename);
        } catch (err: any) {
            if (err.message?.includes('API key not configured')) {
                stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette.');
            } else {
                stream.markdown(`❌ Dual-engine analysis failed: ${sanitizeMarkdown(err.message)}`);
            }
        }

        return {};
    }

    private async handleGeneral(
        prompt: string,
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        const editor = vscode.window.activeTextEditor;
        
        if (editor) {
            // If there's code in the editor, verify it with the prompt as context
            const code = editor.document.getText();
            const filename = editor.document.fileName.split(/[/\\]/).pop() || 'unknown';
            const language = editor.document.languageId;

            stream.progress('🛡️ Running VigilAgent verification...');

            try {
                // The chat participant keeps the classic flow (auto-fixed
                // "Improved Output") — suggestion mode is for the inline
                // quick-fix surfaces (auto-verify, verify-selection).
                const result = await this.client.verify(code, prompt, language, filename, false);
                this.formatReviewResult(result, stream, filename);
            } catch (err: any) {
                stream.markdown(`❌ Verification failed: ${sanitizeMarkdown(err.message)}\n\nTry typing \`help\` for available commands.`);
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
| \`scan\` | Run deterministic static analysis (fast, free) |
| \`verify\` | Run full Shift-Zero pipeline (deterministic + LLM reviewers) |
| \`dual\` | Run parallel dual-engine analysis (deterministic + LLM in parallel) |
| \`help\` | Show this help message |

**Usage:**
- Open a file in the editor
- Type \`@vigilagent scan\`, \`@vigilagent verify\`, or \`@vigilagent dual\` in chat
- Or just type \`@vigilagent\` followed by your question about the code

**Dual-Engine Analysis:**
The \`dual\` command runs BOTH engines simultaneously:
- 🔍 Deterministic Engine: Semgrep, builtin rules, regex (fast, free)
- 🤖 LLM Engine: GPT-4o-mini analyzing for semantic issues (cheap)
- Findings are merged with corroboration scoring

**Configuration:**
Run \`VigilAgent: Configure API Keys\` from the Command Palette to set up your API keys.
`;
    }

    private formatDualEngineResult(result: Record<string, unknown>, stream: vscode.ChatResponseStream, filename: string): void {
        stream.markdown(`## 🛡️ Dual-Engine Analysis — ${filename}\n\n`);

        // Grade and score
        const grade = sanitizeMarkdown((result.grade as string) || 'N/A');
        const score = Number(result.score) || 0;
        const gradeIcon = this.gradeIcon(grade);
        stream.markdown(`${gradeIcon} **Grade:** ${grade} (${score}%)\n\n`);

        // Engine stats
        const stats = result.engine_stats as Record<string, unknown> | undefined;
        if (stats) {
            const det = stats.deterministic as Record<string, unknown> | undefined;
            const llm = stats.llm as Record<string, unknown> | undefined;
            const total = Number(stats.total_latency_ms) || 0;

            stream.markdown('### Engine Statistics\n\n');
            if (det) {
                stream.markdown(`- **Deterministic:** ${sanitizeMarkdown(String(det.findings_count ?? 0))} findings in ${sanitizeMarkdown(String(det.latency_ms ?? 0))}ms\n`);
            }
            if (llm) {
                stream.markdown(`- **LLM Engine:** ${sanitizeMarkdown(String(llm.findings_count ?? 0))} findings in ${sanitizeMarkdown(String(llm.latency_ms ?? 0))}ms (${sanitizeMarkdown(String(llm.model ?? 'unknown'))})\n`);
            }
            if (total > 0) {
                stream.markdown(`- **Total Latency:** ${total.toFixed(0)}ms\n`);
            }
            stream.markdown('\n');
        }

        // Findings
        const findings = result.findings as Record<string, unknown>[] | undefined;
        if (findings && findings.length > 0) {
            const corroborated = findings.filter(f => (f.rule_id as string || '').endsWith('+llm')).length;
            stream.markdown(`### Findings (${findings.length}`);
            if (corroborated > 0) {
                stream.markdown(` | ${corroborated} corroborated`);
            }
            stream.markdown(')\n\n');

            for (const f of findings) {
                const severity = (f.severity as string) || 'unknown';
                const engine = (f.engine as string) || 'unknown';
                const message = sanitizeMarkdown((f.message as string) || '');
                const fix = f.fix ? sanitizeMarkdown(f.fix as string) : undefined;
                const icon = this.severityIcon(severity);
                const corroboratedMark = (f.rule_id as string || '').endsWith('+llm') ? ' ✓' : '';
                stream.markdown(`${icon} **[${sanitizeMarkdown(severity.toUpperCase())}]** (${sanitizeMarkdown(engine)})${corroboratedMark}\n   ${message}\n`);
                if (fix) {
                    stream.markdown(`   💡 *Fix:* ${fix}\n`);
                }
                stream.markdown('\n');
            }
        } else {
            stream.markdown('✅ No issues found. Code looks clean!\n');
        }

        stream.markdown('\n---\n*Powered by VigilAgent Dual-Engine Analysis*');
    }

    private formatScanResult(result: Record<string, unknown>, stream: vscode.ChatResponseStream, filename: string): void {
        const scanResult = result.scan_result as Record<string, unknown> | undefined;
        const findings = scanResult?.findings as Record<string, unknown>[] | undefined;
        const pipelineResult = result.pipeline_result as Record<string, unknown> | undefined;

        stream.markdown(`## 🔍 Scan Results — ${filename}\n\n`);

        if (pipelineResult) {
            const passed = pipelineResult.passed as boolean;
            const confidence = Number(pipelineResult.confidence) || 0;
            const icon = passed ? '✅' : '❌';
            stream.markdown(`${icon} **Pipeline:** ${passed ? 'PASSED' : 'FAILED'} (confidence: ${(confidence * 100).toFixed(0)}%)\n\n`);
        }

        if (findings && findings.length > 0) {
            stream.markdown(`### Findings (${findings.length})\n\n`);
            for (const f of findings) {
                const severity = (f.severity as string) || 'unknown';
                const message = sanitizeMarkdown((f.message as string) || 'No message');
                const fix = f.fix ? sanitizeMarkdown(f.fix as string) : undefined;
                const sevIcon = this.severityIcon(severity);
                stream.markdown(`${sevIcon} **[${sanitizeMarkdown(severity.toUpperCase())}]** ${message}\n`);
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
            const grade = sanitizeMarkdown((confidence.grade as string) || 'N/A');
            const score = Number(confidence.confidence) || 0;
            const reason = sanitizeMarkdown((confidence.reason as string) || '');
            const gradeIcon = this.gradeIcon(grade);
            stream.markdown(`${gradeIcon} **Confidence:** ${grade} (${(score * 100).toFixed(0)}%)\n`);
            if (reason) {
                stream.markdown(`> ${reason}\n\n`);
            }
        }

        // Reviewer verdicts
        const reviewers = result.reviewers as Record<string, unknown>[] | undefined;
        if (reviewers && reviewers.length > 0) {
            stream.markdown('### Reviewer Verdicts\n\n');
            for (const r of reviewers) {
                const name = sanitizeMarkdown((r.name as string) || 'unknown');
                const role = sanitizeMarkdown((r.role as string) || '');
                const verdict = (r.verdict as string) || 'unknown';
                const icon = this.verdictIcon(verdict);
                stream.markdown(`${icon} **${name}** (${role}): ${sanitizeMarkdown(verdict.toUpperCase())}\n`);
                const rFindings = (r.findings as string[]) || [];
                for (const f of rFindings) {
                    stream.markdown(`   • ${sanitizeMarkdown(f)}\n`);
                }
                const rSuggestions = (r.suggestions as string[]) || [];
                if (rSuggestions.length > 0) {
                    stream.markdown(`   *Suggestions:* ${rSuggestions.map(s => sanitizeMarkdown(s)).join('; ')}\n`);
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
                const message = sanitizeMarkdown((f.message as string) || '');
                const line = f.line as number | undefined;
                const fix = f.fix ? sanitizeMarkdown(f.fix as string) : undefined;
                const icon = this.severityIcon(severity);
                stream.markdown(`${icon} **[${sanitizeMarkdown(severity.toUpperCase())}]** ${message}${line ? ` (line ${Number(line)})` : ''}\n`);
                if (fix) {
                    stream.markdown(`   💡 *Fix:* ${fix}\n`);
                }
            }
            stream.markdown('\n');
        }

        // Summary
        const summary = result.summary as string | undefined;
        if (summary) {
            stream.markdown(`### Summary\n${sanitizeMarkdown(summary)}\n`);
        }

        // Final output
        const finalOutput = result.final_output as string | undefined;
        const mainResponse = result.main_llm_response as string | undefined;
        if (finalOutput && finalOutput !== mainResponse) {
            // Collapse triple-backtick runs so untrusted output cannot close
            // the code fence and render the remainder as markdown.
            stream.markdown(`### 📝 Improved Output\n\n\`\`\`\n${finalOutput.replace(/`{3,}/g, '``')}\n\`\`\`\n`);
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
