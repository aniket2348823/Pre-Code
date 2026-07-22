import * as vscode from 'vscode';
import { Finding } from './client';

export class DiagnosticManager {
    private collection: vscode.DiagnosticCollection;
    
    constructor() {
        this.collection = vscode.languages.createDiagnosticCollection('vigilagent');
    }
    
    // Convert scan findings to VSCode diagnostics
    updateDiagnostics(uri: vscode.Uri, findings: Finding[]): void {
        const diagnostics: vscode.Diagnostic[] = [];
        
        for (const finding of findings) {
            let severity = vscode.DiagnosticSeverity.Information;
            const findingSev = finding.severity?.toLowerCase() || '';
            
            if (findingSev === 'critical' || findingSev === 'high') {
                severity = vscode.DiagnosticSeverity.Error;
            } else if (findingSev === 'medium') {
                severity = vscode.DiagnosticSeverity.Warning;
            }
            
            // Map line numbers to ranges (finding.line is usually 1-indexed)
            const lineIndex = Math.max(0, (finding.line || 1) - 1);
            const range = new vscode.Range(
                new vscode.Position(lineIndex, 0),
                new vscode.Position(lineIndex, 1000) // Highlight the whole line roughly
            );
            
            let message = finding.message || 'Unknown finding';
            if (finding.fix) {
                message += `\nFix: ${finding.fix}`;
            }
            
            const diagnostic = new vscode.Diagnostic(range, message, severity);
            diagnostic.source = 'VigilAgent';
            diagnostic.code = finding.analyzers ? finding.analyzers.join(', ') : undefined;
            
            diagnostics.push(diagnostic);
        }
        
        this.collection.set(uri, diagnostics);
    }
    
    // Clear diagnostics for a document
    clear(uri: vscode.Uri): void {
        this.collection.delete(uri);
    }
    
    dispose(): void {
        this.collection.dispose();
    }
}
