import * as vscode from 'vscode';
import { Suggestion, Finding } from './client';

/**
 * SuggestionStore keeps the line-anchored suggestions produced by the review
 * pipeline per document, plus the set of suggestions the user dismissed.
 *
 * The engine NEVER applies a suggestion by itself — every edit is triggered by
 * an explicit user action (the "Apply fix" CodeAction). Dismissed suggestions
 * stay dismissed until the document is re-scanned with a fresh report.
 */
export class SuggestionStore implements vscode.Disposable {
    private byUri = new Map<string, Suggestion[]>();
    private dismissed = new Set<string>();

    set(uri: vscode.Uri, suggestions: Suggestion[]): void {
        const live = (suggestions || []).filter(s => !this.dismissed.has(s.id));
        this.byUri.set(uri.toString(), live);
    }

    get(uri: vscode.Uri): Suggestion[] {
        return this.byUri.get(uri.toString()) || [];
    }

    all(): Suggestion[] {
        const out: Suggestion[] = [];
        for (const list of this.byUri.values()) {
            out.push(...list);
        }
        return out;
    }

    dismiss(uri: vscode.Uri, id: string): void {
        this.dismissed.add(id);
        const key = uri.toString();
        const list = this.byUri.get(key) || [];
        this.byUri.set(key, list.filter(s => s.id !== id));
    }

    clear(uri: vscode.Uri): void {
        this.byUri.delete(uri.toString());
    }

    dispose(): void {
        this.byUri.clear();
        this.dismissed.clear();
    }
}

/**
 * Converts a suggestion's 1-indexed line range into a vscode.Range covering
 * the full lines so a WorkspaceEdit can replace exactly the suggested lines.
 */
export function suggestionRange(suggestion: Suggestion): vscode.Range {
    const startLine = Math.max(0, (suggestion.line_start || 1) - 1);
    const endLine = Math.max(startLine, (suggestion.line_end || suggestion.line_start || 1) - 1);
    return new vscode.Range(
        new vscode.Position(startLine, 0),
        new vscode.Position(endLine, 1000) // clamps to the line length on replace
    );
}

/**
 * Maps line-anchored suggestions onto the Finding shape DiagnosticManager
 * renders as squiggles. The CodeActions provider reads the same suggestions
 * from the store for Apply/Dismiss.
 */
export function suggestionsToFindings(suggestions: Suggestion[]): Finding[] {
    return (suggestions || []).map(s => ({
        severity: s.severity || 'info',
        message: s.message || 'Unknown issue',
        filename: '',
        line: s.line_start || 0,
        snippet: '',
        fix: s.replacement || '',
        confidence: s.confidence || 0,
        analyzers: [s.role || 'reviewer'],
    }));
}

/**
 * Remaps suggestions whose lines are numbered 1-indexed WITHIN a scanned
 * region to document coordinates. `startLine` is the region's 0-indexed start
 * line in the document.
 */
export function offsetSuggestions(suggestions: Suggestion[], startLine: number): Suggestion[] {
    return (suggestions || []).map(s => {
        const lineStart = s.line_start > 0 ? s.line_start + startLine : s.line_start;
        const lineEnd = s.line_end > 0 ? Math.max(s.line_start, s.line_end) + startLine : s.line_end;
        return { ...s, line_start: lineStart, line_end: lineEnd };
    });
}
