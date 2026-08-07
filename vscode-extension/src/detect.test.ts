/**
 * Pure-function test matrix for the bulk-insertion detector (detect.ts).
 *
 * These tests exercise the classifier, vetoes and snapshot helpers without
 * any VS Code dependency — they encode the manual-testing expectations:
 *   - Copilot/Cursor tab-accepts (2+ lines) must be caught
 *   - ChatGPT-style pastes (multi-line, large, burst) must be caught
 *   - Human typing must NOT be flagged
 *   - Undo/redo, deletion-only and format-on-save must be vetoed
 */
import { describe, it, expect } from 'vitest';
import {
    EditSignal,
    classifyEdit,
    hashText,
    isLikelyUndoRedo,
    Snapshot,
    SCAN_THRESHOLD,
    BURST_WINDOW_MS,
    extractCodeBlocks,
    GatewayOutputRegistry,
    normalizeBlockLines,
} from './detect';

/** Build an EditSignal with sensible defaults. */
function edit(overrides: Partial<EditSignal>): EditSignal {
    return {
        ts: 1_000_000,
        newlineCount: 0,
        addedChars: 0,
        replacedChars: 0,
        isUndoRedo: false,
        coversWholeDocument: false,
        ...overrides,
    };
}

describe('classifyEdit — bulk-insertion scoring', () => {
    it('is a pure function (no history → deterministic burst bonus)', () => {
        const signal = edit({ newlineCount: 2, addedChars: 60 });
        const a = classifyEdit(signal, []);
        const b = classifyEdit(signal, []);
        expect(a).toEqual(b);
    });

    it('catches a 3-line Copilot tab-accept (2-line threshold)', () => {
        // The most common AI path: a small multi-line completion with no
        // recent typing. newlineCount 2 → +2, burst → +1 ⇒ scan.
        const result = classifyEdit(edit({ newlineCount: 2, addedChars: 90 }), [], 2_000_000);
        expect(result.scan).toBe(true);
        expect(result.score).toBeGreaterThanOrEqual(SCAN_THRESHOLD);
        expect(result.reasons.some(r => r.includes('multi-line'))).toBe(true);
    });

    it('catches a 2-line insertion even without burst (selection overwrite)', () => {
        const signal = edit({ newlineCount: 1, addedChars: 40, replacedChars: 12 });
        // +2 multi-line, +1 selection overwrite; no burst (prior edit 100ms ago).
        const prior = [edit({ ts: 2_000_000 - 100 })];
        const result = classifyEdit(signal, prior, 2_000_000);
        expect(result.scan).toBe(true);
        expect(result.score).toBe(3);
    });

    it('catches a large single-line insertion (>= 100 chars)', () => {
        const signal = edit({ addedChars: 150 });
        const result = classifyEdit(signal, []);
        expect(result.scan).toBe(true);
        expect(result.reasons.some(r => r.includes('large'))).toBe(true);
    });

    it('catches a big ChatGPT-style paste (multi-line + large + burst)', () => {
        const signal = edit({ newlineCount: 24, addedChars: 1800 });
        const result = classifyEdit(signal, []);
        expect(result.scan).toBe(true);
        // All three signals contribute.
        expect(result.score).toBe(5);
    });

    it('does NOT flag one-line human typing of moderate length', () => {
        const signal = edit({ addedChars: 30, ts: 2_000_000 });
        const prior = [edit({ ts: 2_000_000 - 100 }), edit({ ts: 2_000_000 - 250 })];
        const result = classifyEdit(signal, prior, 2_000_000);
        expect(result.scan).toBe(false);
        expect(result.score).toBeLessThan(SCAN_THRESHOLD);
    });

    it('does NOT flag a one-line insert while typing is continuous (no burst)', () => {
        // 50-char single-line edit right after another edit: burst gives 0,
        // everything else 0 ⇒ no scan.
        const signal = edit({ addedChars: 50, ts: 2_000_000 });
        const prior = [edit({ ts: 2_000_000 - 50 })];
        const result = classifyEdit(signal, prior, 2_000_000);
        expect(result.scan).toBe(false);
    });

    it('flags a one-line insert as burst only when typing paused', () => {
        const signal = edit({ addedChars: 50, ts: 2_000_000 });
        const prior = [edit({ ts: 2_000_000 - BURST_WINDOW_MS - 10 })];
        const result = classifyEdit(signal, prior, 2_000_000);
        expect(result.reasons.some(r => r.includes('burst'))).toBe(true);
        // +1 burst only ⇒ still below threshold (burst alone never scans).
        expect(result.scan).toBe(false);
    });

    it('treats burst as relative to the LAST prior event', () => {
        const signal = edit({ newlineCount: 1, addedChars: 40, ts: 2_000_000 });
        const prior = [
            edit({ ts: 2_000_000 - 5000 }),
            edit({ ts: 2_000_000 - 100 }), // recent edit — no burst
        ];
        const result = classifyEdit(signal, prior, 2_000_000);
        expect(result.reasons.some(r => r.includes('burst'))).toBe(false);
        // Multi-line only ⇒ +2 ⇒ scan.
        expect(result.scan).toBe(true);
    });
});

describe('classifyEdit — vetoes', () => {
    it('vetoes undo/redo edits regardless of size', () => {
        const result = classifyEdit(
            edit({ newlineCount: 20, addedChars: 900, isUndoRedo: true }),
            []
        );
        expect(result.scan).toBe(false);
        expect(result.veto).toBe('undo/redo');
        expect(result.score).toBe(0);
    });

    it('vetoes deletion-only edits (nothing added)', () => {
        const result = classifyEdit(edit({ addedChars: 0, replacedChars: 40 }), []);
        expect(result.scan).toBe(false);
        expect(result.veto).toBe('deletion-only');
    });

    it('vetoes format-on-save (net-zero whole-file rewrite)', () => {
        // Prettier rewrites the whole file with roughly the same length.
        const result = classifyEdit(
            edit({ newlineCount: 30, addedChars: 5000, replacedChars: 5005, coversWholeDocument: true }),
            []
        );
        expect(result.scan).toBe(false);
        expect(result.veto).toContain('format-on-save');
    });

    it('does NOT veto a large paste into an empty file (whole-doc, not net-zero)', () => {
        const result = classifyEdit(
            edit({ newlineCount: 19, addedChars: 600, replacedChars: 0, coversWholeDocument: true }),
            []
        );
        expect(result.scan).toBe(true);
        expect(result.veto).toBeUndefined();
    });

    it('veto wins even when other signals would scan', () => {
        const result = classifyEdit(
            edit({ newlineCount: 5, addedChars: 300, isUndoRedo: true }),
            []
        );
        expect(result.scan).toBe(false);
        expect(result.veto).toBeDefined();
    });
});

describe('extractCodeBlocks — gateway response parsing', () => {
    it('extracts a single fenced block, dropping the language tag', () => {
        const blocks = extractCodeBlocks('Here you go:\n```python\nprint(1)\n```');
        expect(blocks).toEqual(['print(1)\n']);
    });

    it('extracts multiple blocks in one response', () => {
        const blocks = extractCodeBlocks('```go\nfunc a() {}\n```\nand:\n```js\nconst b = 1;\n```');
        expect(blocks).toHaveLength(2);
        expect(blocks[0]).toContain('func a() {}');
        expect(blocks[1]).toContain('const b = 1;');
    });

    it('returns nothing when there are no fences', () => {
        expect(extractCodeBlocks('plain prose only')).toEqual([]);
    });

    it('handles an unterminated fence (captures to end)', () => {
        const blocks = extractCodeBlocks('```sh\necho hi');
        expect(blocks).toHaveLength(1);
        expect(blocks[0]).toContain('echo hi');
    });

    it('skips empty blocks', () => {
        expect(extractCodeBlocks('```\n```')).toEqual([]);
    });
});

describe('normalizeBlockLines', () => {
    it('trims each line and drops empties', () => {
        expect(normalizeBlockLines('  a  \n\n b\n')).toEqual(['a', 'b']);
    });
});

describe('GatewayOutputRegistry — gateway-code attribution', () => {
    const BLOCK = 'func scan(code string) {\n\treturn code\n}\n\nfunc main() {\n\tfmt.Println(scan("x"))\n}';

    it('matches an exact re-insert of a registered block', () => {
        const reg = new GatewayOutputRegistry();
        reg.register(BLOCK, { scanId: 'scan-1', provenance: 'verified' });
        const m = reg.match(BLOCK);
        expect(m).toEqual({ scanId: 'scan-1', provenance: 'verified' });
    });

    it('matches a re-indented insert (chat Insert into File adds indentation)', () => {
        const reg = new GatewayOutputRegistry();
        reg.register(BLOCK, { scanId: 'scan-1', provenance: 'verified' });
        const indented = BLOCK.split('\n').map(l => `    ${l}`).join('\n');
        const m = reg.match(indented);
        expect(m).toBeDefined();
        expect(m!.scanId).toBe('scan-1');
    });

    it('matches a partial insert (one function pasted out of several)', () => {
        const reg = new GatewayOutputRegistry();
        reg.register(BLOCK, { scanId: 'scan-1', provenance: 'verified' });
        const partial = 'func scan(code string) {\n\treturn code\n}';
        expect(reg.match(partial)).toBeDefined();
    });

    it('does NOT match unrelated code', () => {
        const reg = new GatewayOutputRegistry();
        reg.register(BLOCK, { scanId: 'scan-1', provenance: 'verified' });
        expect(reg.match('const x = 1;\nconst y = 2;')).toBeUndefined();
    });

    it('does not match inserts shorter than the 2-line minimum', () => {
        const reg = new GatewayOutputRegistry();
        reg.register(BLOCK, { scanId: 'scan-1', provenance: 'verified' });
        expect(reg.match('func scan(code string) {')).toBeUndefined();
    });

    it('returns the most-recent matching block when several are registered', () => {
        const reg = new GatewayOutputRegistry();
        reg.register('func a() {\n x()\n}', { scanId: 'old', provenance: 'verified' });
        reg.register(BLOCK, { scanId: 'new', provenance: 'verified' });
        const m = reg.match(BLOCK);
        expect(m).toBeDefined();
        expect(m!.scanId).toBe('new');
    });

    it('is bounded (FIFO cap) and clearable', () => {
        const reg = new GatewayOutputRegistry(2);
        reg.register('block one', { scanId: '1', provenance: 'verified' });
        reg.register('block two two two', { scanId: '2', provenance: 'verified' });
        reg.register('block three three', { scanId: '3', provenance: 'verified' });
        expect(reg.size).toBe(2);
        reg.clear();
        expect(reg.size).toBe(0);
    });

    it('ignores empty/blank registrations', () => {
        const reg = new GatewayOutputRegistry();
        reg.register('   \n\n  ', { scanId: '1', provenance: 'verified' });
        expect(reg.size).toBe(0);
    });
});

describe('snapshot helpers (undo/redo detection)', () => {
    it('hashes are stable and length-sensitive', () => {
        expect(hashText('func main() {}')).toBe(hashText('func main() {}'));
        expect(hashText('a')).not.toBe(hashText('b'));
    });

    it('detects a revert to a state 2+ versions back', () => {
        const snapshots: Snapshot[] = [
            { version: 4, hash: hashText('original code') },
            { version: 5, hash: hashText('original code + paste') },
        ];
        // Version 7 is back at the original state — user pasted then undid twice.
        expect(isLikelyUndoRedo(hashText('original code'), snapshots, 7)).toBe(true);
    });

    it('ignores the immediately-preceding version (typing normally edits forward)', () => {
        const snapshots: Snapshot[] = [{ version: 6, hash: hashText('current text') }];
        expect(isLikelyUndoRedo(hashText('current text'), snapshots, 7)).toBe(false);
    });

    it('requires a matching hash — unrelated text is never undo', () => {
        const snapshots: Snapshot[] = [{ version: 4, hash: hashText('something else') }];
        expect(isLikelyUndoRedo(hashText('brand new text'), snapshots, 7)).toBe(false);
    });
});
