import * as vscode from 'vscode';
import { VigilAgentClient } from './client';
import { DiagnosticManager } from './diagnostics';

export class AutoVerifier {
    private client: VigilAgentClient;
    private diagnosticManager: DiagnosticManager;
    private debounceTimer: NodeJS.Timeout | undefined;
    private enabled: boolean;
    private lastDocVersions: Map<string, number>;
    private disposables: vscode.Disposable[] = [];
    
    constructor(client: VigilAgentClient, diagnosticManager: DiagnosticManager) {
        this.client = client;
        this.diagnosticManager = diagnosticManager;
        this.lastDocVersions = new Map<string, number>();
        this.enabled = vscode.workspace.getConfiguration('vigilagent').get<boolean>('autoVerify', true);
    }
    
    // Register listeners
    register(context: vscode.ExtensionContext): void {
        this.disposables.push(
            vscode.workspace.onDidChangeTextDocument(this.onDidChangeTextDocument, this),
            vscode.workspace.onDidChangeConfiguration(this.onDidChangeConfiguration, this),
            vscode.window.onDidChangeActiveTextEditor(this.onDidChangeActiveTextEditor, this)
        );
        
        context.subscriptions.push(...this.disposables);
    }
    
    private onDidChangeConfiguration(e: vscode.ConfigurationChangeEvent): void {
        if (e.affectsConfiguration('vigilagent.autoVerify')) {
            this.enabled = vscode.workspace.getConfiguration('vigilagent').get<boolean>('autoVerify', true);
            if (!this.enabled) {
                // Clear all diagnostics when disabled
                for (const editor of vscode.window.visibleTextEditors) {
                    this.diagnosticManager.clear(editor.document.uri);
                }
            }
        }
    }
    
    private onDidChangeActiveTextEditor(editor: vscode.TextEditor | undefined): void {
        if (editor && this.enabled) {
            // Optionally analyze on active editor change if it hasn't been analyzed recently
        }
    }
    
    private onDidChangeTextDocument(event: vscode.TextDocumentChangeEvent): void {
        if (!this.enabled) {
            return;
        }
        
        if (this.isLikelyAIGenerated(event)) {
            this.scheduleAnalysis(event.document);
        }
    }
    
    // Detect AI-generated code insertion:
    // - Large insertions (>5 lines added at once)
    // - Multiple lines replaced at once
    // - Copilot ghost text acceptance patterns
    private isLikelyAIGenerated(event: vscode.TextDocumentChangeEvent): boolean {
        for (const change of event.contentChanges) {
            const addedLines = change.text.split('\n').length;
            
            // If they inserted more than 5 lines at once
            if (addedLines > 5) {
                return true;
            }
            
            // If they inserted a decent amount of text (e.g. > 100 chars) in one go
            if (change.text.length > 100) {
                return true;
            }
        }
        
        return false;
    }
    
    // Debounced analysis - wait 1.5s after last change
    private scheduleAnalysis(document: vscode.TextDocument): void {
        if (this.debounceTimer) {
            clearTimeout(this.debounceTimer);
        }
        
        this.debounceTimer = setTimeout(() => {
            this.analyze(document).catch(err => {
                console.error('AutoVerifier analysis failed', err);
            });
        }, 1500);
    }
    
    // Run the analysis
    private async analyze(document: vscode.TextDocument): Promise<void> {
        const uri = document.uri;
        const code = document.getText();
        const filename = uri.path.split(/[/\\]/).pop() || 'unknown';
        const language = document.languageId;
        
        try {
            const result = await this.client.scan(code, language, filename);
            
            let findings: any[] = [];
            if (result.scan_result && result.scan_result.findings) {
                findings = result.scan_result.findings;
            } else if (result.deterministic_findings && Array.isArray(result.deterministic_findings)) {
                findings = result.deterministic_findings;
            }
            
            this.diagnosticManager.updateDiagnostics(uri, findings);
        } catch (error) {
            console.error('Failed to analyze document', error);
        }
    }
    
    dispose(): void {
        if (this.debounceTimer) {
            clearTimeout(this.debounceTimer);
        }
        this.disposables.forEach(d => d.dispose());
    }
}
