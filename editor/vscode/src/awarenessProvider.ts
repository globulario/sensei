// SPDX-License-Identifier: AGPL-3.0-only

// The "This File" tree: what the awareness graph knows about the file in the
// active editor. On every (debounced) editor change it runs one Preflight
// query for that file and renders the result as a small, glanceable tree.
//
// Design note — visible absence: an empty panel is ambiguous ("is there
// nothing, or did the query fail?"). So zero anchors renders as an explicit
// node that distinguishes confident absence (coverage sufficient → nothing
// governs this file) from a degraded backend (the answer is unreliable).

import * as vscode from 'vscode';
import * as path from 'path';
import {
  AwgError,
  CodeAnchor,
  GraphAuthority,
  KnowledgeNode,
  PreflightResponse,
  preflight,
} from './grpcClient';
import { assessGraphAuthority } from './graphAuthority';

type State =
  | { kind: 'disabled' }
  | { kind: 'noFile' }
  | { kind: 'loading'; file: string }
  | { kind: 'error'; file: string; error: AwgError }
  | { kind: 'ready'; file: string; resp: PreflightResponse };

type Element =
  | { kind: 'message'; label: string; icon?: vscode.ThemeIcon; tooltip?: string | vscode.MarkdownString }
  | { kind: 'risk'; resp: PreflightResponse; file: string }
  | { kind: 'nodeGroup'; label: string; icon: vscode.ThemeIcon; nodes: KnowledgeNode[] }
  | { kind: 'stringGroup'; label: string; icon: vscode.ThemeIcon; items: string[] }
  | { kind: 'node'; node: KnowledgeNode }
  | { kind: 'stringItem'; text: string };

export class AwarenessProvider implements vscode.TreeDataProvider<Element> {
  private readonly _onDidChangeTreeData = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

  private state: State = { kind: 'noFile' };
  private seq = 0; // guards against out-of-order responses when files switch fast

  /** The repo-relative path currently analysed, for the view subtitle. */
  get currentFile(): string | undefined {
    return 'file' in this.state ? this.state.file : undefined;
  }

  /** Re-query for the given file URI (or clear the panel when none applies). */
  async refresh(uri: vscode.Uri | undefined): Promise<void> {
    const cfg = vscode.workspace.getConfiguration('sensei');
    if (!cfg.get<boolean>('enabled', true)) {
      this.setState({ kind: 'disabled' });
      return;
    }

    const rel = uri ? toRepoRelative(uri) : undefined;
    if (!rel) {
      this.setState({ kind: 'noFile' });
      return;
    }

    const ticket = ++this.seq;
    this.setState({ kind: 'loading', file: rel });

    const addr = cfg.get<string>('serverAddr', 'localhost:10120');
    const domain = cfg.get<string>('domain', '') || undefined;
    const mode = cfg.get<string>('mode', 'standard') === 'compact'
      ? 'PREFLIGHT_COMPACT'
      : 'PREFLIGHT_STANDARD';
    const timeout = cfg.get<number>('requestTimeoutMs', 10000);

    try {
      const resp = await preflight(addr, { files: [rel], mode, domain }, timeout);
      if (ticket !== this.seq) {
        return; // a newer file won the race
      }
      this.setState({ kind: 'ready', file: rel, resp });
    } catch (err) {
      if (ticket !== this.seq) {
        return;
      }
      const e = err instanceof AwgError ? err : new AwgError(String(err));
      this.setState({ kind: 'error', file: rel, error: e });
    }
  }

  private setState(s: State): void {
    this.state = s;
    this._onDidChangeTreeData.fire();
  }

  // ---- TreeDataProvider ---------------------------------------------------

  getChildren(element?: Element): Element[] {
    if (!element) {
      return this.rootChildren();
    }
    switch (element.kind) {
      case 'nodeGroup':
        return element.nodes.map((node) => ({ kind: 'node', node }));
      case 'stringGroup':
        return element.items.map((text) => ({ kind: 'stringItem', text }));
      default:
        return [];
    }
  }

  private rootChildren(): Element[] {
    switch (this.state.kind) {
      case 'disabled':
        return [msg('Awareness querying is off', 'circle-slash',
          'Enable `sensei.enabled` to query the graph as you switch files.')];
      case 'noFile':
        return [msg('Open a file to see what governs it', 'info')];
      case 'loading':
        return [msg(`Querying awareness graph…`, 'loading~spin')];
      case 'error':
        return [this.errorRow(this.state.error)];
      case 'ready':
        return this.readyChildren(this.state.resp, this.state.file);
    }
  }

  private errorRow(error: AwgError): Element {
    const addr = vscode.workspace
      .getConfiguration('sensei')
      .get<string>('serverAddr', 'localhost:10120');
    if (error.unreachable) {
      return msg(`Sensei unavailable — no graph briefing`, 'debug-disconnect',
        `Could not reach the awareness-graph server at \`${addr}\`.\n\n` +
        'This is not an empty or low-coverage answer. The file panel has no graph-backed guidance because the authority backend is down or unreachable.\n\n' +
        'Start it with `sensei serve`, or set `sensei.serverAddr`.');
    }
    return msg(`Awareness query failed`, 'warning', error.message);
  }

  private readyChildren(resp: PreflightResponse, file: string): Element[] {
    const out: Element[] = [{ kind: 'risk', resp, file }];

    const groups: Array<[string, string, KnowledgeNode[] | undefined]> = [
      ['Invariants', 'law', resp.direct_invariants],
      ['Failure modes', 'flame', resp.direct_failure_modes],
      ['Intent', 'compass', resp.direct_intents],
      ['Forbidden fixes', 'circle-slash', resp.direct_forbidden_fixes],
      ['Required tests', 'beaker', resp.direct_required_tests],
      ['Architecture', 'symbol-namespace', resp.direct_architecture],
    ];
    let anchorCount = 0;
    for (const [label, icon, nodes] of groups) {
      if (nodes && nodes.length) {
        anchorCount += nodes.length;
        out.push({ kind: 'nodeGroup', label: `${label} (${nodes.length})`, icon: themeIcon(icon), nodes });
      }
    }

    if (resp.required_actions && resp.required_actions.length) {
      out.push(stringGroup('Required actions', 'checklist', resp.required_actions));
    }
    if (resp.blind_spots && resp.blind_spots.length) {
      out.push(stringGroup('Blind spots', 'eye-closed', resp.blind_spots));
    }

    if (anchorCount === 0) {
      out.push(this.absenceRow(resp));
    }
    return out;
  }

  /** The visible-absence row: confident "nothing here" vs. degraded backend. */
  private absenceRow(resp: PreflightResponse): Element {
    if (resp.status === 'PREFLIGHT_STATUS_DEGRADED') {
      return msg('Awareness degraded — answer may be incomplete', 'warning',
        resp.coverage?.note || 'The backend was partially unavailable for this query.');
    }
    const sufficient = resp.coverage?.sufficient ?? true;
    if (sufficient) {
      return msg('No rules anchor to this file', 'pass',
        'The graph has sufficient coverage here and no invariant, failure mode, ' +
        'or intent is anchored to this file. Confident absence — not a missing query.');
    }
    return msg('No anchors found — coverage is thin here', 'question',
      resp.coverage?.note || 'No rules are anchored to this file, but graph coverage ' +
      'in this area is limited, so absence is not conclusive.');
  }

  getTreeItem(element: Element): vscode.TreeItem {
    switch (element.kind) {
      case 'message': {
        const item = new vscode.TreeItem(element.label, vscode.TreeItemCollapsibleState.None);
        if (element.icon) {
          item.iconPath = element.icon;
        }
        item.tooltip = element.tooltip;
        item.contextValue = 'awareness.message';
        return item;
      }
      case 'risk': {
        const meta = riskMeta(element.resp.risk_class);
        const item = new vscode.TreeItem(`Risk: ${meta.label}`, vscode.TreeItemCollapsibleState.None);
        item.iconPath = meta.icon;
        item.description = element.file;
        const conf = element.resp.confidence ? ` · confidence ${element.resp.confidence}` : '';
        const auth = authorityTooltip(element.resp.authority);
        const cov = element.resp.coverage;
        const covLine = cov
          ? `\n\nCoverage: ${cov.sufficient ? 'sufficient' : 'thin'}` +
            ` · ${cov.direct_anchor_count ?? 0} direct anchor(s)` +
            (cov.note ? `\n${cov.note}` : '')
          : '';
        item.tooltip = new vscode.MarkdownString(
          `**Risk classification:** ${meta.label}${conf}${covLine}${auth}`
        );
        item.contextValue = 'awareness.risk';
        return item;
      }
      case 'nodeGroup': {
        const item = new vscode.TreeItem(element.label, vscode.TreeItemCollapsibleState.Expanded);
        item.iconPath = element.icon;
        item.contextValue = 'awareness.group';
        return item;
      }
      case 'stringGroup': {
        const item = new vscode.TreeItem(
          `${element.label} (${element.items.length})`,
          vscode.TreeItemCollapsibleState.Collapsed
        );
        item.iconPath = element.icon;
        item.contextValue = 'awareness.group';
        return item;
      }
      case 'node':
        return nodeTreeItem(element.node);
      case 'stringItem': {
        const item = new vscode.TreeItem(element.text, vscode.TreeItemCollapsibleState.None);
        item.tooltip = element.text;
        return item;
      }
    }
  }
}

// ---- Tree item construction ------------------------------------------------

function nodeTreeItem(node: KnowledgeNode): vscode.TreeItem {
  const label = node.label || node.id || node.iri || '(unnamed)';
  const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
  item.iconPath = severityIcon(node.severity);
  if (node.id) {
    item.description = node.id;
  }
  item.tooltip = nodeTooltip(node);
  item.contextValue = 'awareness.node';

  const anchor = bestAnchor(node.anchor);
  if (anchor) {
    item.command = {
      command: 'sensei.revealAnchor',
      title: 'Open Source Anchor',
      arguments: [anchor],
    };
  }
  return item;
}

function nodeTooltip(node: KnowledgeNode): vscode.MarkdownString {
  const md = new vscode.MarkdownString();
  md.appendMarkdown(`**${node.label || node.id || 'node'}**\n\n`);
  const facts: string[] = [];
  if (node.class) facts.push(`class \`${node.class}\``);
  if (node.severity) facts.push(`severity \`${node.severity}\``);
  if (node.status) facts.push(`status \`${node.status}\``);
  if (facts.length) {
    md.appendMarkdown(facts.join(' · ') + '\n\n');
  }
  if (node.description) {
    md.appendMarkdown(node.description + '\n\n');
  }
  const a = node.anchor;
  if (a && (a.source_yaml || a.file)) {
    const where = a.file
      ? `${a.file}${a.symbol ? ` · ${a.symbol}` : ''}${a.line_start ? `:${a.line_start}` : ''}`
      : a.source_yaml!;
    md.appendMarkdown(`_anchored in_ \`${where}\``);
  }
  return md;
}

// ---- Anchor resolution -----------------------------------------------------

interface ResolvedAnchor {
  file: string;
  line: number;
}

function bestAnchor(a?: CodeAnchor): ResolvedAnchor | undefined {
  if (!a) {
    return undefined;
  }
  const file = a.file || a.source_yaml;
  if (!file) {
    return undefined;
  }
  return { file, line: Math.max(0, (a.line_start ?? 1) - 1) };
}

function authorityTooltip(authority?: GraphAuthority): string {
  const assessed = assessGraphAuthority(authority);
  if (!authority) {
    return `\n\nAuthority: ${assessed.summary}`;
  }
  const freshness = (authority.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED')
    .replace('GRAPH_FRESHNESS_STATE_', '')
    .toLowerCase();
  const provenance = (authority.build_provenance_state || 'BUILD_PROVENANCE_STATE_UNSPECIFIED')
    .replace('BUILD_PROVENANCE_STATE_', '')
    .toLowerCase();
  const prefix = assessed.authoritative ? 'current' : 'non-authoritative';
  let transaction = 'uncertified';
  if (authority.embedded_transaction_matches_seed) {
    transaction = 'certified';
  } else if (!authority.embedded_transaction_stamp_present) {
    transaction = 'missing';
  }
  let out = `\n\nAuthority: ${prefix} · freshness ${freshness} · provenance ${provenance} · transaction ${transaction}`;
  if (authority.certified_awareness_graph_commit || authority.certified_services_repo_commit) {
    out += `\ntransaction commits: graph=${authority.certified_awareness_graph_commit || 'n/a'} services=${authority.certified_services_repo_commit || 'n/a'}`;
  }
  if (authority.embedded_transaction_detail) {
    out += `\n${authority.embedded_transaction_detail}`;
  }
  if (authority.graph_freshness_detail) {
    out += `\n${authority.graph_freshness_detail}`;
  }
  if (assessed.detail && assessed.detail !== authority.graph_freshness_detail) {
    out += `\n${assessed.detail}`;
  }
  return out;
}

// ---- Visual mapping --------------------------------------------------------

function severityIcon(sev?: string): vscode.ThemeIcon {
  switch ((sev || '').toLowerCase()) {
    case 'critical':
    case 'high':
      return new vscode.ThemeIcon('error', new vscode.ThemeColor('charts.red'));
    case 'warning':
    case 'degraded':
      return new vscode.ThemeIcon('warning', new vscode.ThemeColor('charts.yellow'));
    case 'info':
      return new vscode.ThemeIcon('info', new vscode.ThemeColor('charts.blue'));
    default:
      return new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.foreground'));
  }
}

function riskMeta(risk?: string): { label: string; icon: vscode.ThemeIcon } {
  switch (risk) {
    case 'LOW_RISK':
      return { label: 'Low', icon: new vscode.ThemeIcon('pass', new vscode.ThemeColor('charts.green')) };
    case 'ARCHITECTURE_SENSITIVE':
      return { label: 'Architecture-sensitive', icon: new vscode.ThemeIcon('warning', new vscode.ThemeColor('charts.yellow')) };
    case 'CONVERGENCE_RISK':
      return { label: 'Convergence', icon: new vscode.ThemeIcon('warning', new vscode.ThemeColor('charts.orange')) };
    case 'SECURITY_RISK':
      return { label: 'Security', icon: new vscode.ThemeIcon('shield', new vscode.ThemeColor('charts.red')) };
    case 'DATA_LOSS_RISK':
      return { label: 'Data-loss', icon: new vscode.ThemeIcon('error', new vscode.ThemeColor('charts.red')) };
    case 'UNKNOWN_IMPACT':
      return { label: 'Unknown impact', icon: new vscode.ThemeIcon('question') };
    default:
      return { label: '—', icon: new vscode.ThemeIcon('circle-outline') };
  }
}

// ---- Small helpers ---------------------------------------------------------

function themeIcon(id: string): vscode.ThemeIcon {
  return new vscode.ThemeIcon(id);
}

function msg(label: string, icon?: string, tooltip?: string): Element {
  return {
    kind: 'message',
    label,
    icon: icon ? new vscode.ThemeIcon(icon) : undefined,
    tooltip,
  };
}

function stringGroup(label: string, icon: string, items: string[]): Element {
  return { kind: 'stringGroup', label, icon: themeIcon(icon), items };
}

/**
 * Map a document URI to a repo-relative POSIX path, the form Sensei anchors on
 * (e.g. "golang/server/impact.go"). Returns undefined for non-file documents
 * (output panels, git diffs, untitled) and files outside any workspace folder.
 */
function toRepoRelative(uri: vscode.Uri): string | undefined {
  if (uri.scheme !== 'file') {
    return undefined;
  }
  const folder = vscode.workspace.getWorkspaceFolder(uri);
  if (!folder) {
    return undefined;
  }
  const rel = path.relative(folder.uri.fsPath, uri.fsPath);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel)) {
    return undefined;
  }
  return rel.split(path.sep).join('/');
}
