import * as vscode from 'vscode';
import { VigilAgentClient, Finding, Suggestion } from './client';
import { DiagnosticManager } from './diagnostics';
import { SuggestionStore, suggestionsToFindings, offsetSuggestions } from './suggestionStore';
import {
    EditSignal,
    classifyEdit,
    hashText,
    isLikelyUndoRedo,
    Snapshot,
    GatewayOutputRegistry,
    GatewayOutputMeta,
    HISTORY_CAP,
    SNAPSHOT_CAP,
} from './detect';

/** Documents above this size skip snapshotting (undo detection off — too slow to hash). */
const MAX_SNAPSHOT_DOC_CHARS = 2_000_000;

/**
 * AutoVerifier watches for bulk code insertion (paste from ChatGPT/Claude,
 * Copilot/Cursor tab-accept) entering the editor and runs VigilAgent's review
 * pipeline on ONLY the inserted region. Detection is a scored classifier
 * (see detect.ts): multi-line inserts, large edits, selection overwrites and
 * burst timing raise the score; undo/redo, deletion-only edits and
 * format-on-save rewrites veto it. The result is a set of line-anchored
 * accept/reject suggestions rendered as squiggles + quick fixes.
 *
 * The engine never modifies the file — every edit is the user's explicit
 * choice via the "Apply fix" code action.
 */
/** Audit record emitted when a scan completes — consumed by the findings view. */
export interface AutoVerifyRecord {
    scanId: string;
    filename: string;
    grade: string;
    score: number;
    provenance: string;
    findings: Finding[];
    timestamp: number;
    uri?: string;
}

export class AutoVerifier {
    private client: VigilAgentClient;
    private diagnosticManager: DiagnosticManager;
    private suggestionStore: SuggestionStore;
    private enabled: boolean;
    private scheduledVersions: Map<string, number>;
    private pendingRegions: Map<string, vscode.Range>;
    private pendingGateway: Map<string, GatewayOutputMeta>;
    private debounceTimers: Map<string, NodeJS.Timeout>;
    private editHistory: Map<string, EditSignal[]>;
    private snapshots: Map<string, Snapshot[]>;
    private disposables: vscode.Disposable[] = [];

    /**
     * Optional callback invoked when a scan completes, so the findings view
     * gets an audit record. Gateway-matched scans carry the gateway's scan id
     * and provenance (verified) instead of unverified.
     */
    onRecord: ((record: AutoVerifyRecord) => void) | undefined;

    constructor(
        client: VigilAgentClient,
        diagnosticManager: DiagnosticManager,
        suggestionStore: SuggestionStore,
        private readonly gatewayOutputs?: GatewayOutputRegistry
    ) {
        this.client = client;
        this.diagnosticManager = diagnosticManager;
        this.suggestionStore = suggestionStore;
        this.scheduledVersions = new Map<string, number>();
        this.pendingRegions = new Map<string, vscode.Range>();
        this.pendingGateway = new Map<string, GatewayOutputMeta>();
        this.debounceTimers = new Map<string, NodeJS.Timeout>();
        this.editHistory = new Map<string, EditSignal[]>();
        this.snapshots = new Map<string, Snapshot[]>();
        this.enabled = vscode.workspace.getConfiguration('vigilagent').get<boolean>('autoVerify', true);
    }

    // Register listeners
    register(context: vscode.ExtensionContext): void {
        this.disposables.push(
            vscode.workspace.onDidChangeTextDocument(this.onDidChangeTextDocument, this),
            vscode.workspace.onDidChangeConfiguration(this.onDidChangeConfiguration, this),
            vscode.window.onDidChangeActiveTextEditor(this.onDidChangeActiveTextEditor, this),
            vscode.workspace.onDidCloseTextDocument(this.onDidCloseTextDocument, this)
        );

        context.subscriptions.push(...this.disposables);
    }

    private onDidCloseTextDocument(document: vscode.TextDocument): void {
        // Drop per-document state so the maps don't grow unboundedly.
        const uriKey = document.uri.toString();
        this.cancelPending(uriKey);
        this.editHistory.delete(uriKey);
        this.snapshots.delete(uriKey);
        this.pendingGateway.delete(uriKey);
        this.diagnosticManager.clear(document.uri);
        this.suggestionStore.clear(document.uri);
    }

    private onDidChangeConfiguration(e: vscode.ConfigurationChangeEvent): void {
        if (e.affectsConfiguration('vigilagent.autoVerify')) {
            this.enabled = vscode.workspace.getConfiguration('vigilagent').get<boolean>('autoVerify', true);
            if (!this.enabled) {
                // Clear all diagnostics when disabled
                for (const editor of vscode.window.visibleTextEditors) {
                    this.diagnosticManager.clear(editor.document.uri);
                    this.suggestionStore.clear(editor.document.uri);
                }
            }
        }
    }

    private onDidChangeActiveTextEditor(_editor: vscode.TextEditor | undefined): void {
        // Optionally analyze on active editor change — intentionally empty.
    }

    private onDidChangeTextDocument(event: vscode.TextDocumentChangeEvent): void {
        if (!this.enabled) {
            return;
        }

        const doc = event.document;
        const uriKey = doc.uri.toString();
        // Only watch real files; skipping closed/untitled avoids stray scans.
        if (doc.isClosed || doc.uri.scheme !== 'file') {
            return;
        }

        const now = Date.now();
        const history = this.editHistory.get(uriKey) ?? [];
        const snapshots = this.snapshots.get(uriKey) ?? [];

        // Undo/redo detection: the document is already updated when this event
        // fires, so its full text is the *post-edit* state. If that state
        // matches a version at least 2 back, the edit was a revert.
        const text = doc.getText();
        const newHash = text.length <= MAX_SNAPSHOT_DOC_CHARS ? hashText(text) : undefined;
        const isUndoRedo = newHash !== undefined && isLikelyUndoRedo(newHash, snapshots, doc.version);

        // Classify each content change against the prior edit history. Any
        // single change that scores high enough routes the union region
        // through the review layer. A change that matches a registered gateway
        // output is scanned even below the generic threshold (guaranteed
        // coverage for gateway-generated code, whatever its size).
        let anyScan = false;
        let anyVeto = false;
        const signals: EditSignal[] = event.contentChanges.map(change => ({
            ts: now,
            newlineCount: change.text.split('\n').length - 1,
            addedChars: change.text.length,
            replacedChars: change.rangeLength,
            isUndoRedo,
            coversWholeDocument: isWholeDocumentChange(change, doc),
        }));

        let gatewayMatch: GatewayOutputMeta | undefined;
        for (let i = 0; i < signals.length; i++) {
            const signal = signals[i];
            const result = classifyEdit(signal, history, now);
            if (result.veto !== undefined) {
                anyVeto = true;
                continue;
            }
            if (result.scan) {
                anyScan = true;
            }
            // Registry match: gateway-generated code inserted into the editor
            // (chat "Insert into File", copy-paste of a generated block).
            if (this.gatewayOutputs) {
                const change = event.contentChanges[i];
                const gw = this.gatewayOutputs.match(change.text);
                if (gw) {
                    gatewayMatch = gw;
                    anyScan = true;
                }
            }
        }
        if (gatewayMatch) {
            this.pendingGateway.set(uriKey, gatewayMatch);
        }

        // Maintain per-document edit history (cadence) and version snapshots.
        history.push(...signals);
        while (history.length > HISTORY_CAP) {
            history.shift();
        }
        if (newHash !== undefined) {
            snapshots.push({ version: doc.version, hash: newHash });
            while (snapshots.length > SNAPSHOT_CAP) {
                snapshots.shift();
            }
        }
        this.editHistory.set(uriKey, history);
        this.snapshots.set(uriKey, snapshots);

        if (!anyScan) {
            // Undo/redo, deletion-only or format-on-save: cancel any pending
            // scan for this document — the region may have moved or vanished.
            if (anyVeto) {
                this.cancelPending(uriKey);
            }
            return;
        }

        const region = this.regionFromChanges(event);
        if (region) {
            this.scheduleAnalysis(doc, region);
        }
    }

    /**
     * Compute the union of all changed ranges — this is the bulk-inserted
     * region. Scanning only this region (not the whole file) keeps scans fast
     * and private, and suggestion line numbers map back to document
     * coordinates via the region start line.
     */
    private regionFromChanges(event: vscode.TextDocumentChangeEvent): vscode.Range | undefined {
        if (!event.contentChanges.length) {
            return undefined;
        }
        let startLine = Number.MAX_SAFE_INTEGER;
        let endLine = 0;
        for (const change of event.contentChanges) {
            startLine = Math.min(startLine, change.range.start.line);
            endLine = Math.max(endLine, change.range.end.line);
        }
        return new vscode.Range(startLine, 0, endLine, 0);
    }

    // Debounced analysis — wait 1.5s after the last change, per document.
    private scheduleAnalysis(document: vscode.TextDocument, region: vscode.Range): void {
        const key = document.uri.toString();
        this.scheduledVersions.set(key, document.version);
        this.pendingRegions.set(key, region);

        const existing = this.debounceTimers.get(key);
        if (existing) {
            clearTimeout(existing);
        }

        const timer = setTimeout(() => {
            this.debounceTimers.delete(key);
            this.analyze(document).catch(err => {
                console.error('AutoVerifier analysis failed', err);
            });
        }, 1500);
        this.debounceTimers.set(key, timer);
    }

    /** Cancel a pending (not yet fired) scan for a document. */
    private cancelPending(uriKey: string): void {
        const timer = this.debounceTimers.get(uriKey);
        if (timer) {
            clearTimeout(timer);
            this.debounceTimers.delete(uriKey);
        }
        this.scheduledVersions.delete(uriKey);
        this.pendingRegions.delete(uriKey);
    }

    /**
     * Run the review pipeline (5 roles, 1 LLM call, suggestion mode) on the
     * inserted region. Falls back to the deterministic dual-engine scan when
     * no LLM key is configured, so basic squiggles still appear.
     */
    private async analyze(document: vscode.TextDocument): Promise<void> {
        const uri = document.uri;
        const key = uri.toString();
        const scheduledVersion = this.scheduledVersions.get(key);
        const region = this.pendingRegions.get(key);
        if (!region || scheduledVersion === undefined) {
            return;
        }
        const gwMeta = this.pendingGateway.get(key);
        this.pendingGateway.delete(key);

        // Stale guard (pre-flight): if the document changed again while the
        // scan was pending, the old findings would point at lines that no
        // longer exist.
        if (document.version !== scheduledVersion) {
            return;
        }

        // Ownership guard for cleanup: a newer bulk insertion may re-schedule
        // this document while this scan is in flight. The finally block must
        // only delete the entry it owns, otherwise it would wipe the newer
        // pending scan and that region would never be analyzed.
        const ownsEntry = () => this.scheduledVersions.get(key) === scheduledVersion;

        const code = document.getText(region);
        const language = document.languageId;
        const filename = document.fileName.split(/[/\\\\]/).pop() || 'unknown';

        // isStale must be re-checked AFTER any await: if the user typed during
        // the network call, the document's lines have moved and the result
        // would land on the wrong places.
        const isStale = () => document.version !== scheduledVersion;

        try {
            const result = await this.client.verify(code, '', language, filename);
            if (isStale()) {
                return;
            }

            // Remap region-relative lines to document coordinates.
            const suggestions = offsetSuggestions(result.suggestions || [], region.start.line);
            this.suggestionStore.set(uri, suggestions);
            this.diagnosticManager.updateDiagnostics(uri, suggestionsToFindings(suggestions));
            this.emitRecord(document, suggestions, gwMeta);
        } catch {
            // LLM pipeline unavailable (e.g. no key) — fall back to the
            // deterministic dual-engine scan.
            try {
                const result = await this.client.dualEngine(code, language);
                if (isStale()) {
                    return;
                }

                const findings: any[] = (result.findings || []).map((f: any) => ({
                    severity: f.severity || 'info',
                    message: f.message || '',
                    line: (f.line || 0) + region.start.line,
                    fix: f.fix || '',
                    confidence: f.confidence || 0,
                }));

                this.diagnosticManager.updateDiagnostics(uri, findings);
            } catch (fallbackErr) {
                console.error('AutoVerifier analysis failed (both paths)', fallbackErr);
            }
        } finally {
            if (ownsEntry()) {
                this.pendingRegions.delete(key);
                this.scheduledVersions.delete(key);
            }
        }
    }

    /**
     * Emit an audit record for the findings view. Gateway-matched scans carry
     * the gateway's scan id + provenance (verified); everything else is
     * labelled unverified (unknown source — scanned, never model-guessed).
     */
    private emitRecord(
        document: vscode.TextDocument,
        suggestions: Suggestion[],
        gwMeta: GatewayOutputMeta | undefined
    ): void {
        if (!this.onRecord) {
            return;
        }
        const findings: Finding[] = suggestions.map((s) => ({
            severity: s.severity || 'info',
            message: s.message,
            filename: document.fileName.split(/[/\\\\]/).pop() || 'unknown',
            line: s.line_start,
            snippet: '',
            fix: s.replacement || '',
            confidence: s.confidence || 0,
            analyzers: s.role ? [s.role] : [],
        }));
        this.onRecord({
            scanId: gwMeta?.scanId || `auto-${Date.now().toString(36)}`,
            filename: document.fileName.split(/[/\\\\]/).pop() || 'unknown',
            grade: 'N/A',
            score: 0,
            provenance: gwMeta?.provenance || 'unverified',
            findings,
            timestamp: Date.now(),
            uri: document.uri.toString(),
        });
    }

    dispose(): void {
        for (const timer of this.debounceTimers.values()) {
            clearTimeout(timer);
        }
        this.debounceTimers.clear();
        this.disposables.forEach(d => d.dispose());
    }
}

/** True when a content change's range spans the entire document. */
function isWholeDocumentChange(
    change: vscode.TextDocumentContentChangeEvent,
    doc: vscode.TextDocument
): boolean {
    const r = change.range;
    if (r.start.line !== 0 || r.start.character !== 0) {
        return false;
    }
    const lastLine = doc.lineAt(Math.max(0, doc.lineCount - 1));
    return r.end.isEqual(lastLine.rangeIncludingLineBreak.end);
}
