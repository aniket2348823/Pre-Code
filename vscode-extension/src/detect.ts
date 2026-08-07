/**
 * Bulk-insertion detector — pure classifier, no `vscode` imports.
 *
 * An LLM (ChatGPT paste, Copilot/Cursor tab-accept) inserts code as ONE large
 * text edit, whereas a human types many small edits. This classifier turns
 * that observation into a score and decides whether a change is worth routing
 * through the VigilAgent review layer.
 *
 * Vetoes (never scan): undo/redo, deletion-only edits, net-zero whole-file
 * rewrites (format-on-save). Score signals: multi-line insertion, large
 * insertion, selection overwrite, and burst timing (no edits in the trailing
 * window). Scan when score >= SCAN_THRESHOLD.
 */

export interface EditSignal {
    /** Epoch ms of the edit. */
    ts: number;
    /** Newlines in the inserted text — >=1 means a multi-line insertion. */
    newlineCount: number;
    /** Characters inserted. */
    addedChars: number;
    /** Characters replaced (the edit's range length). 0 = pure insertion. */
    replacedChars: number;
    /** Edit came from undo or redo. */
    isUndoRedo: boolean;
    /** Edit rewrote the entire document (format-on-save risk). */
    coversWholeDocument: boolean;
}

export interface DetectionResult {
    scan: boolean;
    score: number;
    reasons: string[];
    /** Set when a veto killed the decision — the human-readable why. */
    veto: string | undefined;
}

/** No edits within this window before an edit ⇒ it arrived as a burst (paste/AI). */
export const BURST_WINDOW_MS = 400;
/** Minimum score to route an edit through the review layer. */
export const SCAN_THRESHOLD = 2;
/** Insertions of this many chars count as "large" even on a single line. */
export const LARGE_INSERT_CHARS = 100;
/** Max events kept per document for cadence analysis. */
export const HISTORY_CAP = 16;

/**
 * Classifies an edit against its prior edit history.
 *
 * `priorEvents` must be the events that happened BEFORE this one (in order);
 * the history is kept by the caller. Pure — deterministic, no I/O.
 */
export function classifyEdit(
    event: EditSignal,
    priorEvents: EditSignal[],
    now: number = event.ts
): DetectionResult {
    const reasons: string[] = [];
    let veto: string | undefined;

    if (event.isUndoRedo) {
        veto = 'undo/redo';
    } else if (event.addedChars === 0) {
        veto = 'deletion-only';
    } else if (event.coversWholeDocument && isNetZeroRewrite(event)) {
        veto = 'format-on-save (net-zero whole-file rewrite)';
    }

    if (veto !== undefined) {
        return { scan: false, score: 0, reasons, veto };
    }

    let score = 0;

    // Multi-line insertion: the signature of a pasted/complete block. This is
    // the strongest signal and catches Copilot tab-accepts of any size.
    if (event.newlineCount >= 1) {
        score += 2;
        reasons.push(`+2 multi-line insertion (${event.newlineCount + 1} lines)`);
    }
    // Large single-line insertion (long URL, config line, JSON blob).
    if (event.addedChars >= LARGE_INSERT_CHARS) {
        score += 2;
        reasons.push(`+2 large insertion (${event.addedChars} chars)`);
    }
    // Overwrote existing code (paste-over-selection).
    if (event.replacedChars > 0) {
        score += 1;
        reasons.push('+1 replaced existing code (selection overwrite)');
    }
    // Burst: nothing was edited shortly before — typing would have produced
    // many small events in this window.
    const lastTs = priorEvents.length > 0 ? priorEvents[priorEvents.length - 1].ts : 0;
    if (now - lastTs >= BURST_WINDOW_MS) {
        score += 1;
        reasons.push('+1 burst insertion (no recent edits)');
    }

    return { scan: score >= SCAN_THRESHOLD, score, reasons, veto };
}

/**
 * A whole-document rewrite that leaves the text length roughly unchanged is
 * almost certainly a formatter/linter pass — scanning it would be wasteful and
 * noisy. Anything with a materially different size (e.g. code pasted into an
 * empty file) is NOT a rewrite and passes through.
 */
function isNetZeroRewrite(event: EditSignal): boolean {
    return (
        Math.abs(event.addedChars - event.replacedChars) <=
        Math.max(1, event.replacedChars * 0.1)
    );
}

/** A hashed snapshot of a document version, for undo/redo detection. */
export interface Snapshot {
    version: number;
    hash: string;
}

/** Number of snapshots kept per document. */
export const SNAPSHOT_CAP = 8;

/**
 * FNV-1a 32-bit hash — fast and collision-safe enough for equality checks
 * against a small ring of recent document versions.
 */
export function hashText(text: string): string {
    let h = 0x811c9dc5;
    for (let i = 0; i < text.length; i++) {
        h ^= text.charCodeAt(i);
        h = Math.imul(h, 0x01000193) >>> 0;
    }
    return h.toString(16);
}

/**
 * Undo/redo restores an older full-document state. If the text matches a
 * snapshot at least 2 versions back, the edit that produced it is almost
 * certainly a revert (typing never reproduces an identical whole-file state).
 */
export function isLikelyUndoRedo(
    hash: string,
    snapshots: Snapshot[],
    currentVersion: number
): boolean {
    for (const s of snapshots) {
        if (s.hash === hash && currentVersion - s.version >= 2) {
            return true;
        }
    }
    return false;
}

// ═══════════════════════════════════════════════════════════════════════════
// GATEWAY OUTPUT REGISTRY
//
// The gateway chat provider registers every code block it generates (with the
// scan id + provenance from the gateway's own scan). When that code is later
// inserted into an editor, AutoVerifier matches the inserted text against this
// registry and scans it even when the generic classifier wouldn't fire (e.g. a
// small one-function insert below the line threshold). Matching is line-set
// overlap on trimmed lines, so it survives re-indentation and partial inserts.
// Pure — no `vscode` imports.
// ═══════════════════════════════════════════════════════════════════════════

export interface GatewayOutputMeta {
    scanId: string;
    provenance: string; // verified | unverified | bypassed
}

/** Minimum ratio of inserted lines present in a registered block to match. */
export const GATEWAY_MATCH_RATIO = 0.7;
/** Inserts shorter than this are never attributed to a gateway output. */
export const GATEWAY_MIN_MATCH_LINES = 2;

/** Extracts the contents of every fenced ``` code block in a chat response. */
export function extractCodeBlocks(text: string): string[] {
    const blocks: string[] = [];
    const re = /```[^\n]*\n([\s\S]*?)(?:```|$)/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(text)) !== null) {
        if (m[1].trim()) {
            blocks.push(m[1]);
        }
    }
    return blocks;
}

/** Trims each line and drops empties — normalization for fingerprinting. */
export function normalizeBlockLines(text: string): string[] {
    return text
        .split('\n')
        .map((l) => l.trim())
        .filter((l) => l.length > 0);
}

/**
 * Remembers recent gateway-generated code blocks. Bounded (FIFO) so it can
 * never grow unboundedly across a long session.
 */
export class GatewayOutputRegistry {
    private entries: Array<GatewayOutputMeta & { lines: string[] }> = [];

    constructor(private readonly cap: number = 16) {}

    /** Register a generated block so later inserts of it can be attributed. */
    register(block: string, meta: GatewayOutputMeta): void {
        const lines = normalizeBlockLines(block);
        if (lines.length === 0) {
            return;
        }
        this.entries.unshift({ ...meta, lines });
        if (this.entries.length > this.cap) {
            this.entries.length = this.cap;
        }
    }

    /**
     * Returns the gateway output metadata if the inserted text looks like a
     * (possibly partial / re-indented) copy of a registered block.
     */
    match(insertedText: string): GatewayOutputMeta | undefined {
        const inserted = normalizeBlockLines(insertedText);
        if (inserted.length < GATEWAY_MIN_MATCH_LINES) {
            return undefined;
        }
        let best: GatewayOutputMeta | undefined;
        let bestRatio = 0;
        for (const e of this.entries) {
            const overlap = inserted.filter((l) => e.lines.includes(l)).length;
            const ratio = overlap / inserted.length;
            if (ratio >= GATEWAY_MATCH_RATIO && ratio > bestRatio) {
                bestRatio = ratio;
                best = { scanId: e.scanId, provenance: e.provenance };
            }
        }
        return best;
    }

    clear(): void {
        this.entries = [];
    }

    get size(): number {
        return this.entries.length;
    }
}
