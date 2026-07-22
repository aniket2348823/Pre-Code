import * as vscode from 'vscode';

export class VigilAgentStatusBar {
    private statusBarItem: vscode.StatusBarItem;

    constructor() {
        this.statusBarItem = vscode.window.createStatusBarItem(
            vscode.StatusBarAlignment.Right,
            100
        );
        this.statusBarItem.text = '$(shield) VigilAgent';
        this.statusBarItem.tooltip = 'VigilAgent: Click to configure';
        this.statusBarItem.command = 'vigilagent.configure';
        this.statusBarItem.show();
    }

    showConfidence(grade: string, score: number): void {
        const icon = grade?.toUpperCase() === 'A' ? '$(pass)' :
                     grade?.toUpperCase() === 'B' ? '$(warning)' :
                     '$(error)';
        this.statusBarItem.text = `${icon} VigilAgent: ${grade} (${(score * 100).toFixed(0)}%)`;
        this.statusBarItem.tooltip = `Confidence: ${grade} (${(score * 100).toFixed(0)}%)`;
    }

    reset(): void {
        this.statusBarItem.text = '$(shield) VigilAgent';
        this.statusBarItem.tooltip = 'VigilAgent: Click to configure';
    }

    dispose(): void {
        this.statusBarItem.dispose();
    }
}
