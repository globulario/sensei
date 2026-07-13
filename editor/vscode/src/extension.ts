// SPDX-License-Identifier: AGPL-3.0-only

// Extension entry point. Wires the "This File" awareness tree to the active
// editor: switching or editing a file (debounced) re-queries the graph, and
// the view subtitle tracks which repo-relative path is being analysed.

import * as vscode from 'vscode';
import * as path from 'path';
import { AwarenessProvider } from './awarenessProvider';
import { DashboardPanel } from './dashboardPanel';
import { disposeClient } from './grpcClient';
import { resetProjectDomainCache } from './projectDomain';
import { resetAwgBinaryCache } from './awgRunner';

const REFRESH_DEBOUNCE_MS = 250;

export function activate(context: vscode.ExtensionContext): void {
  const provider = new AwarenessProvider();
  const view = vscode.window.createTreeView('sensei.fileAwareness', {
    treeDataProvider: provider,
    showCollapseAll: true,
  });
  context.subscriptions.push(view);

  // The file to analyse. Resolved from (in order): the active text editor, the
  // first visible file editor, then the active tab. The tab fallback matters
  // because focusing our own view transiently empties visibleTextEditors —
  // without it, a good result gets overwritten by a spurious "no file" state.
  const currentFileUri = (): vscode.Uri | undefined => {
    const active = vscode.window.activeTextEditor?.document.uri;
    if (active?.scheme === 'file') {
      return active;
    }
    const visible = vscode.window.visibleTextEditors.find(
      (e) => e.document.uri.scheme === 'file'
    );
    if (visible) {
      return visible.document.uri;
    }
    const input = vscode.window.tabGroups.activeTabGroup?.activeTab?.input as
      | { uri?: vscode.Uri }
      | undefined;
    if (input?.uri?.scheme === 'file') {
      return input.uri;
    }
    return undefined;
  };

  let timer: ReturnType<typeof setTimeout> | undefined;
  const scheduleRefresh = (): void => {
    if (timer) {
      clearTimeout(timer);
    }
    timer = setTimeout(() => {
      void provider.refresh(currentFileUri()).then(() => {
        view.description = provider.currentFile;
      });
    }, REFRESH_DEBOUNCE_MS);
  };

  context.subscriptions.push(
    vscode.window.onDidChangeActiveTextEditor(() => scheduleRefresh()),
    // Catches the startup case: editors are restored slightly after the
    // extension activates, firing this rather than an active-editor change.
    vscode.window.onDidChangeVisibleTextEditors(() => scheduleRefresh()),
    // Track the foreground tab even when no text editor is focused.
    vscode.window.tabGroups.onDidChangeTabs(() => scheduleRefresh()),
    // Re-query when the panel is opened, so it reflects the current file even
    // if it was hidden when the file was first opened.
    view.onDidChangeVisibility((e) => {
      if (e.visible) {
        scheduleRefresh();
      }
    }),
    // A save can change which rules detect-match the content; re-query on save.
    vscode.workspace.onDidSaveTextDocument(() => scheduleRefresh()),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('sensei')) {
        resetProjectDomainCache();
        resetAwgBinaryCache();
        scheduleRefresh();
      }
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      resetProjectDomainCache();
      scheduleRefresh();
    }),
    vscode.commands.registerCommand('sensei.refresh', () => scheduleRefresh()),
    vscode.commands.registerCommand('sensei.openDashboard', () =>
      DashboardPanel.show(context)
    ),
    vscode.commands.registerCommand(
      'sensei.revealAnchor',
      (anchor: { file: string; line: number }) => revealAnchor(anchor)
    ),
    { dispose: () => disposeClient() }
  );

  // First-run discovery: surface the panel once so a new user sees it exists.
  // After that it never steals focus — it just tracks the active editor.
  const REVEAL_KEY = 'sensei.firstRunRevealed';
  if (!context.globalState.get<boolean>(REVEAL_KEY)) {
    void context.globalState.update(REVEAL_KEY, true);
    // Delay so the view is registered before we focus it (activation race).
    setTimeout(() => {
      void vscode.commands.executeCommand('sensei.fileAwareness.focus');
    }, 1200);
  }

  // Initial paint for whatever is already open.
  scheduleRefresh();
}

export function deactivate(): void {
  disposeClient();
}

/** Open a repo-relative anchor (file + 0-based line) in an editor. */
async function revealAnchor(anchor: { file: string; line: number }): Promise<void> {
  const folder = anchorWorkspaceFolder();
  if (!folder) {
    void vscode.window.showWarningMessage(
      `Awareness: no workspace folder to resolve "${anchor.file}".`
    );
    return;
  }
  const target = vscode.Uri.file(path.join(folder.uri.fsPath, anchor.file));
  try {
    const doc = await vscode.workspace.openTextDocument(target);
    const editor = await vscode.window.showTextDocument(doc);
    const pos = new vscode.Position(anchor.line, 0);
    editor.selection = new vscode.Selection(pos, pos);
    editor.revealRange(new vscode.Range(pos, pos), vscode.TextEditorRevealType.InCenter);
  } catch {
    void vscode.window.showWarningMessage(
      `Awareness: could not open "${anchor.file}".`
    );
  }
}

function anchorWorkspaceFolder(): vscode.WorkspaceFolder | undefined {
  const active = vscode.window.activeTextEditor;
  if (active) {
    const f = vscode.workspace.getWorkspaceFolder(active.document.uri);
    if (f) {
      return f;
    }
  }
  return vscode.workspace.workspaceFolders?.[0];
}
