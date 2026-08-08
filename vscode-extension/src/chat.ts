import * as vscode from 'vscode';
import { VigilAgentMcpClient } from './mcpClient';

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
    // The @vigilagent chat participant routes through the MCP server (stdio
    // JSON-RPC) so the chat surface speaks the same Model Context Protocol as
    // Cursor / Cline / Claude Desktop — one tool surface for every client.
    private client: VigilAgentMcpClient;

    constructor(client: VigilAgentMcpClient) {
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

    private parseAction(prompt: string): { type: string; args: string } {
        const lower = prompt.toLowerCase().trim();
        // Strip the leading command keyword; the remainder is the generation
        // prompt used when no editor code is available.
        const restAfter = (keyword: string): string => prompt.slice(keyword.length).trim();

        if (lower.startsWith('scanfile ')) {
            return { type: 'scan', args: restAfter('scanfile') };
        }
        if (lower.startsWith('scan ')) {
            return { type: 'scan', args: restAfter('scan') };
        }
        if (lower === 'scan' || lower === 'scanfile') {
            return { type: 'scan', args: '' };
        }
        if (lower.startsWith('verify ') || lower.startsWith('review ')) {
            return { type: 'verify', args: restAfter(lower.startsWith('verify ') ? 'verify' : 'review') };
        }
        if (lower === 'verify' || lower === 'review') {
            return { type: 'verify', args: '' };
        }
        if (lower.startsWith('dual') || lower.startsWith('deep') || lower.startsWith('parallel')) {
            const space = prompt.indexOf(' ');
            const args = space >= 0 ? prompt.slice(space + 1).trim() : '';
            return { type: 'dualengine', args };
        }
        if (lower === 'help' || lower === '?') {
            return { type: 'help', args: '' };
        }
        return { type: 'general', args: prompt };
    }

    // getEditorCode returns the active editor's code + context when a file
    // with non-empty content is open; undefined otherwise (chat focused, no
    // file, or empty file). The command handlers use this to decide between
    // scanning editor code and generating from the prompt.
    private getEditorCode(): { code: string; filename: string; language: string } | undefined {
        const editor = vscode.window.activeTextEditor;
        if (!editor) {
            return undefined;
        }
        const code = editor.document.getText();
        if (code.trim() === '') {
            return undefined;
        }
        return {
            code,
            filename: editor.document.fileName.split(/[/\\]/).pop() || 'unknown',
            language: editor.document.languageId,
        };
    }

    private async handleScan(
        action: { args: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        // Check if API keys are configured
        if (!(await this.client.isConfigured())) {
            stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette to set up your keys.');
            return {};
        }

        const editor = this.getEditorCode();

        // No code in the editor: the LLM generates the code from the prompt,
        // then it flows through the VigilAgent backend layer (deterministic
        // engine + LLM reviewers) before it reaches the user. This is the
        // middleware path — the generated code is never shown un-scanned.
        if (!editor) {
            if (!action.args) {
                stream.markdown('⚠️ No active editor with code found, and no prompt to generate from.\n\nOpen a file and type `@vigilagent scan`, or type `@vigilagent scan <prompt>` to generate + scan.');
                return {};
            }
            stream.progress('⚙️ Generating code from prompt → passing through VigilAgent analysis layer...');
            try {
                // suggestion_mode=false: classic flow, auto-fixed improved output.
                const result = await this.client.verify('', action.args, '', '', false);
                this.formatReviewResult(result, stream, 'generated code', true);
            } catch (err: any) {
                stream.markdown(`❌ Generate + scan failed: ${sanitizeMarkdown(err.message)}`);
            }
            return {};
        }

        stream.progress('🔍 Running VigilAgent deterministic scan...');

        try {
            const result = await this.client.scan(editor.code, editor.language, editor.filename);
            this.formatScanResult(result, stream, editor.filename);
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
        action: { args: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        // Check if API keys are configured
        if (!(await this.client.isConfigured())) {
            stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette to set up your keys.');
            return {};
        }

        const editor = this.getEditorCode();

        // No editor code: the prompt drives the generation, and the generated
        // code is verified by the full pipeline before it is shown.
        if (!editor) {
            if (!action.args) {
                stream.markdown('⚠️ No active editor with code found, and no prompt to verify from.\n\nOpen a file and type `@vigilagent verify`, or type `@vigilagent verify <prompt>` to generate + verify.');
                return {};
            }
            stream.progress('⚙️ Generating code from prompt → running full verification pipeline...');
            try {
                const result = await this.client.verify('', action.args, '', '', false);
                this.formatReviewResult(result, stream, 'generated code', true);
            } catch (err: any) {
                stream.markdown(`❌ Generate + verify failed: ${sanitizeMarkdown(err.message)}`);
            }
            return {};
        }

        stream.progress('🛡️ Running full Shift-Zero verification pipeline...');

        try {
            // Classic chat flow: auto-fixed "Improved Output" (not suggestion
            // mode) — suggestion mode powers the inline quick-fix surfaces.
            const result = await this.client.verify(editor.code, '', editor.language, editor.filename, false);
            this.formatReviewResult(result, stream, editor.filename);
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
        action: { args: string },
        stream: vscode.ChatResponseStream,
        _token: vscode.CancellationToken
    ): Promise<vscode.ChatResult> {
        // Check if API keys are configured
        if (!(await this.client.isConfigured())) {
            stream.markdown('⚠️ VigilAgent API key not configured.\n\nRun **VigilAgent: Configure API Keys** from the Command Palette to set up your keys.');
            return {};
        }

        const editor = this.getEditorCode();

        // No editor code: the dual-engine endpoint requires code, so route the
        // prompt through the review pipeline instead — it runs BOTH engines
        // (deterministic + LLM reviewers) on the generated code.
        if (!editor) {
            if (!action.args) {
                stream.markdown('⚠️ No active editor with code found, and no prompt to analyze.\n\nOpen a file and type `@vigilagent dual`, or type `@vigilagent dual <prompt>` to generate + analyze.');
                return {};
            }
            stream.progress('⚙️ Generating code from prompt → running dual-engine analysis...');
            try {
                const result = await this.client.verify('', action.args, '', '', false);
                this.formatReviewResult(result, stream, 'generated code', true);
            } catch (err: any) {
                stream.markdown(`❌ Generate + analyze failed: ${sanitizeMarkdown(err.message)}`);
            }
            return {};
        }

        stream.progress('🛡️ Running dual-engine analysis (deterministic + LLM in parallel)...');

        try {
            const result = await this.client.dualEngine(editor.code, editor.language);
            this.formatDualEngineResult(result, stream, editor.filename);
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
        const editor = this.getEditorCode();
        
        if (editor) {
            // If there's code in the editor, verify it with the prompt as context
            stream.progress('🛡️ Running VigilAgent verification...');

            try {
                // The chat participant keeps the classic flow (auto-fixed
                // "Improved Output") — suggestion mode is for the inline
                // quick-fix surfaces (auto-verify, verify-selection).
                const result = await this.client.verify(editor.code, prompt, editor.language, editor.filename, false);
                this.formatReviewResult(result, stream, editor.filename);
            } catch (err: any) {
                stream.markdown(`❌ Verification failed: ${sanitizeMarkdown(err.message)}\n\nTry typing \`help\` for available commands.`);
            }
        } else if (prompt.trim() !== '') {
            // No editor code — this is the middleware use case: the prompt is
            // handed to the LLM THROUGH the VigilAgent backend layer, and the
            // generated code is scanned by the deterministic engine + LLM
            // reviewers before it reaches the user. Nothing generated by an
            // LLM is ever shown without passing through the analysis layer.
            stream.progress('⚙️ Sending prompt through VigilAgent middleware (generate → analyze → deliver)...');

            try {
                const result = await this.client.verify('', prompt, '', '', false);
                this.formatReviewResult(result, stream, 'generated code', true);
            } catch (err: any) {
                stream.markdown(`❌ VigilAgent middleware failed: ${sanitizeMarkdown(err.message)}\n\nTry typing \`help\` for available commands.`);
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
- Open a file in the editor → type \`@vigilagent scan\`, \`@vigilagent verify\`, or \`@vigilagent dual\`
- **No file open?** Type \`@vigilagent scan <prompt>\` (e.g. \`@vigilagent scan generate a function to add two numbers\`) — the LLM generates the code, and it passes through the VigilAgent backend layer (deterministic engine + LLM reviewers) before you see it
- Or just type \`@vigilagent <prompt>\` — the generated output is scanned the same way

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

    private formatReviewResult(result: Record<string, unknown>, stream: vscode.ChatResponseStream, filename: string, generated = false): void {
        stream.markdown(`## 🛡️ Verification Results — ${filename}\n\n`);

        // When the LLM generated the code from a prompt (middleware flow), show
        // what it produced first — the code that just passed through the
        // VigilAgent analysis layer. Collapse triple-backtick runs so untrusted
        // output cannot close the fence and render the remainder as markdown.
        const mainResponse = result.main_llm_response as string | undefined;
        if (generated && mainResponse && mainResponse.trim() !== '') {
            stream.markdown(`### 💻 Generated Code\n\n\`\`\`\n${mainResponse.replace(/`{3,}/g, '``')}\n\`\`\`\n\n`);
        }

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

        // Final output (dedupe when it equals the generated code above)
        const finalOutput = result.final_output as string | undefined;
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
