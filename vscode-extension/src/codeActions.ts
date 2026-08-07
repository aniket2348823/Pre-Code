import * as vscode from 'vscode';
import { Suggestion } from './client';
import { SuggestionStore, suggestionRange } from './suggestionStore';

/**
 * SuggestionCodeActionProvider turns every VigilAgent suggestion into two
 * quick-fix actions the user chooses from:
 *
 *   ⚡ "VigilAgent: Apply fix"      → replaces the suggested lines via WorkspaceEdit
 *   🗑  "VigilAgent: Dismiss"        → hides the suggestion for this session
 *
 * Nothing is ever applied without an explicit user action.
 */
export class SuggestionCodeActionProvider implements vscode.CodeActionProvider {
    public static readonly providedCodeActionKinds = [vscode.CodeActionKind.QuickFix];

    constructor(private store: SuggestionStore) {}

    provideCodeActions(
        document: vscode.TextDocument,
        range: vscode.Range,
        _context: vscode.CodeActionContext,
        _token: vscode.CancellationToken
    ): vscode.CodeAction[] {
        const actions: vscode.CodeAction[] = [];
        const suggestions = this.store.get(document.uri);

        for (const suggestion of suggestions) {
            const sRange = suggestionRange(suggestion);
            // Only surface actions when the cursor/hover overlaps the suggestion's lines.
            if (!range.intersection(sRange)) {
                continue;
            }

            if (suggestion.replacement) {
                const apply = new vscode.CodeAction(
                    `⚡ VigilAgent: Apply fix (${suggestion.role})`,
                    vscode.CodeActionKind.QuickFix
                );
                apply.edit = new vscode.WorkspaceEdit();
                apply.edit.replace(document.uri, sRange, suggestion.replacement);
                apply.isPreferred = suggestion.corroborated === true;
                apply.diagnostics = this.diagnosticsFor(document.uri, suggestion);
                actions.push(apply);
            }

            const dismiss = new vscode.CodeAction(
                '🗑 VigilAgent: Dismiss suggestion',
                vscode.CodeActionKind.QuickFix
            );
            dismiss.command = {
                command: 'vigilagent.dismissSuggestion',
                title: 'Dismiss suggestion',
                arguments: [document.uri, suggestion.id]
            };
            actions.push(dismiss);
        }

        return actions;
    }

    private diagnosticsFor(uri: vscode.Uri, suggestion: Suggestion): vscode.Diagnostic[] {
        const severity = severityFor(suggestion.severity);
        const message = suggestion.message +
            (suggestion.corroborated ? ' (corroborated by deterministic engine)' : '');
        return [new vscode.Diagnostic(suggestionRange(suggestion), message, severity)];
    }
}

export function severityFor(severity: string): vscode.DiagnosticSeverity {
    switch ((severity || '').toLowerCase()) {
        case 'critical':
        case 'high':
            return vscode.DiagnosticSeverity.Error;
        case 'medium':
            return vscode.DiagnosticSeverity.Warning;
        case 'low':
        case 'info':
        default:
            return vscode.DiagnosticSeverity.Information;
    }
}
