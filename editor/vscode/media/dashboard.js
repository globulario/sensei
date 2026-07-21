/* Project Awareness dashboard — webview client.
 * Talks to the extension host (which holds the gRPC client) via postMessage.
 * No framework, no external deps: an architect's cockpit, not a graph toy. */
(function () {
  'use strict';
  // acquireVsCodeApi() may run only once per webview; share it with controlPanel.js.
  const vscode = window.__vscodeApi || (window.__vscodeApi = acquireVsCodeApi());
  const SVGNS = 'http://www.w3.org/2000/svg';

  // ---- aspects -----------------------------------------------------------
  // cls === null  -> no by_class listing endpoint (forbidden fixes, tests):
  // we show the count and an honest note rather than faking a list.
  const ASPECTS = [
    { phase: 'awareness', key: 'invariant', label: 'Invariants', cls: 'QUERY_CLASS_INVARIANT', count: 'invariant_count' },
    { phase: 'awareness', key: 'failure_mode', label: 'Failure modes', cls: 'QUERY_CLASS_FAILURE_MODE', count: 'failure_mode_count' },
    { phase: 'awareness', key: 'intent', label: 'Intents', cls: 'QUERY_CLASS_INTENT', count: 'intent_count' },
    { phase: 'awareness', key: 'incident_pattern', label: 'Incident patterns', cls: 'QUERY_CLASS_INCIDENT_PATTERN', count: 'incident_pattern_count' },
    { phase: 'awareness', key: 'forbidden_fix', label: 'Forbidden fixes', cls: 'QUERY_CLASS_FORBIDDEN_FIX', count: 'forbidden_fix_count' },
    { phase: 'awareness', key: 'test', label: 'Tests', cls: 'QUERY_CLASS_TEST', count: 'required_test_count' },
    { phase: 'awareness', key: 'source_file', label: 'Files', cls: 'QUERY_CLASS_SOURCE_FILE', count: 'source_file_count' },
    // ── architectural spine + pattern layer (grouped after a separator) ──
    { phase: 'awareness', key: 'component', label: 'Components', cls: 'QUERY_CLASS_COMPONENT', count: 'component_count', group: 'Architecture' },
    { phase: 'awareness', key: 'boundary', label: 'Boundaries', cls: 'QUERY_CLASS_BOUNDARY', count: 'boundary_count' },
    { phase: 'awareness', key: 'contract', label: 'Contracts', cls: 'QUERY_CLASS_CONTRACT', count: 'contract_count' },
    { phase: 'awareness', key: 'decision', label: 'Decisions', cls: 'QUERY_CLASS_DECISION', count: 'decision_count' },
    { phase: 'awareness', key: 'evidence', label: 'Evidence', cls: 'QUERY_CLASS_EVIDENCE', count: 'evidence_count' },
    { phase: 'awareness', key: 'meta_principle', label: 'Meta-principles', cls: 'QUERY_CLASS_META_PRINCIPLE', count: 'meta_principle_count' },
    { phase: 'awareness', key: 'design_pattern', label: 'Design patterns', cls: 'QUERY_CLASS_DESIGN_PATTERN', count: 'design_pattern_count' },
    { phase: 'awareness', key: 'implementation_pattern', label: 'Impl. patterns', cls: 'QUERY_CLASS_IMPLEMENTATION_PATTERN', count: 'implementation_pattern_count' },
    { phase: 'awareness', key: 'pattern_misuse', label: 'Pattern misuses', cls: 'QUERY_CLASS_PATTERN_MISUSE', count: 'pattern_misuse_count' },
    // ── review + corpus layer (evidence-based, read-only) ──
    { phase: 'awareness', key: 'review', label: 'Review', cls: '__review__', count: null, group: 'Review' },
    { phase: 'awareness', key: 'candidates', label: 'Candidates', cls: '__candidates__', count: null },
    // ── Phase 2 closure/control layer ──
    { phase: 'control', key: 'control_overview', label: 'Overview', cls: '__control__', count: null },
    { phase: 'control', key: 'architecture_claim', label: 'Claims', cls: 'QUERY_CLASS_ARCHITECTURE_CLAIM', count: 'architecture_claim_count', group: 'Dialogue' },
    { phase: 'control', key: 'open_question', label: 'Questions', cls: 'QUERY_CLASS_OPEN_QUESTION', count: 'open_question_count' },
    { phase: 'control', key: 'architect_answer', label: 'Answers', cls: 'QUERY_CLASS_ARCHITECT_ANSWER', count: 'architect_answer_count' },
    { phase: 'control', key: 'evidence_probe', label: 'Probes', cls: 'QUERY_CLASS_EVIDENCE_PROBE', count: 'evidence_probe_count' },
    { phase: 'control', key: 'benchmark', label: 'External proof', cls: '__benchmark__', count: null, group: 'Proof' },
  ];

  // UML views (closed set, mirrors ValidUMLViews). null = All.
  const UML_VIEWS = ['structural', 'behavioral', 'interaction', 'deployment', 'awareness'];

  const CLASS_COLOR = {
    intent: '#b180d7', invariant: '#3794ff', failure_mode: '#f14c4c',
    incident_pattern: '#e0a000', forbidden_fix: '#d16969', test: '#2ea043',
    source_file: '#888', symbol: '#4ec9b0', code_symbol: '#4ec9b0', related: '#888',
    // architectural spine + pattern layer
    component: '#5a9bd4', boundary: '#c586c0', contract: '#4ec9b0', decision: '#dcdcaa',
    evidence: '#6a9955', meta_principle: '#9b7bd4', design_pattern: '#56b6c2',
    implementation_pattern: '#7aa6c2', pattern_misuse: '#e06c75',
    architecture_claim: '#4ec9b0', open_question: '#e0a000',
    architect_answer: '#b180d7', evidence_probe: '#56b6c2',
  };
  const SEV_COLOR = {
    critical: '#f14c4c', high: '#e0a000', warning: '#cca700', info: '#3794ff', degraded: '#e0a000',
  };
  // Deterministic direction (screen coords, y-down) per class for the focus graph.
  const DIR = {
    intent: [-0.85, -0.6], invariant: [-1, 0.15], incident_pattern: [0.55, -0.9],
    failure_mode: [1, 0], forbidden_fix: [0.85, 0.7], test: [0, 1],
    source_file: [-0.7, 0.8], symbol: [0.1, -1], code_symbol: [0.1, -1], related: [1, 0.45],
    // architectural spine + pattern layer — spread across the remaining arcs
    component: [-0.45, -0.95], boundary: [0.95, -0.5], contract: [0.7, 0.5],
    decision: [-0.95, -0.35], evidence: [-0.3, 0.95], meta_principle: [-0.6, -0.75],
    design_pattern: [0.4, 0.9], implementation_pattern: [0.9, 0.25], pattern_misuse: [0.25, -0.95],
    architecture_claim: [-0.9, -0.3], open_question: [0.2, -1],
    architect_answer: [0.95, -0.25], evidence_probe: [0.45, 0.9],
  };

  // ---- state -------------------------------------------------------------
  let meta = null;
  let active = 'invariant';
  let listRows = [];
  let listAuthority = null;
  let selectedId = null;
  // Candidate YAML files, fetched lazily. null = not yet scanned; the Review
  // tab reads the count for its "promote candidates carefully" proposal.
  let candidateFiles = null;
  let candidatesMsg = null; // last full 'candidates' message (for re-render)
  // Whether the host may run local sensei writes (opt-in setting). When false the
  // Candidates tab stays read-only and shows the guarded CLI to run by hand.
  let localOps = { enabled: false, hasWorkspace: false, rebuild: { mode: 'single' } };
  let capabilities = {}; // { hasWorkspace, isAwgProject, awgAvailable, candidateCount }
  let lastOp = null; // last promote-approved operation result (survives re-render)
  let lastScan = null; // { kind:'scan'|'apply', m } — scan result, survives re-render
  let lastRefresh = null; // last reload/rebuild result (shown in the banner)
  let viewFilter = null; // active UML view filter (null = All)
  let activeDomain = ''; // the domain the banner/lists are scoped to ('' = all)
  let activePhase = 'awareness';
  let controlState = null;
  let controlStatus = {};
  const graph = { center: null, label: null, depth: 1, nodes: [], edges: [], authority: null, view: null };

  // ---- elements ----------------------------------------------------------
  const $ = (id) => document.getElementById(id);
  const elList = $('list'), elDetail = $('detail'), elNav = $('nav'),
    elSearch = $('search'), elCount = $('listCount'), elSvg = $('graph'), elTip = $('tooltip'),
    elViewFilter = $('viewFilter');

  // ---- boot --------------------------------------------------------------
  vscode.postMessage({ type: 'getMetadata' });
  vscode.postMessage({ type: 'getControlState' });
  renderViewFilter();
  renderPhaseRail();
  selectAspect('invariant');

  elSearch.addEventListener('input', renderList);
  $('graphReset').addEventListener('click', () => fitGraph(true));
  $('depthToggle').addEventListener('click', (e) => {
    const b = e.target.closest('.depth'); if (!b) return;
    const d = Number(b.dataset.depth);
    document.querySelectorAll('.depth').forEach((x) => x.classList.toggle('depth--on', Number(x.dataset.depth) === d));
    graph.depth = d;
    if (graph.center) requestGraph(graph.center, graph.label, d);
  });

  // ---- message handling --------------------------------------------------
  window.addEventListener('message', (ev) => {
    const m = ev.data;
    switch (m.type) {
      case 'metadata': meta = m.data; if (m.localOps) localOps = m.localOps; if (m.activeDomain !== undefined) activeDomain = m.activeDomain; renderBanner(); renderNav(); if (active === 'review') renderReview(); else if (active === 'control_overview') renderControlOverview(); break;
      case 'controlState': controlState = m; if (m.localOps) localOps = m.localOps; renderBanner(); if (active === 'control_overview') renderControlOverview(); break;
      case 'controlStatus': controlStatus[m.kind || 'unknown'] = m; if (active === 'control_overview') renderControlOverview(); break;
      case 'refreshResult': lastRefresh = m; renderRefreshStatus(); break;
      case 'list':
        if (aspectByKey(active).cls === m.cls) {
          listRows = m.rows || [];
          listAuthority = m.authority || null;
          renderList();
          // Populate the cockpit immediately rather than showing an empty pane.
          if (!selectedId && listRows.length) select(listRows[0].id, listRows[0].label);
        }
        break;
      case 'detail': if (m.id === selectedId) renderDetail(m); break;
      case 'graph': if (m.center === graph.center) { graph.nodes = m.nodes; graph.edges = m.edges; graph.depth = m.depth; graph.authority = m.authority || null; renderGraph(); } break;
      case 'candidates':
        candidateFiles = m.files || [];
        candidatesMsg = m;
        if (m.localOps) localOps = m.localOps;
        if (m.capabilities) capabilities = m.capabilities;
        if (active === 'candidates') renderCandidates(m);
        else if (active === 'review') renderReview();
        break;
      case 'candidatePreview': renderCandResult(m.id, 'preview', m); break;
      case 'candidatePromote': renderCandResult(m.id, 'promote', m); break;
      case 'promoteApproved':
        if (!m.cancelled) lastOp = m;
        if (active === 'candidates' && candidatesMsg) renderCandidates(candidatesMsg);
        break;
      case 'candidateScan': lastScan = { kind: 'scan', m }; renderScanResult(); break;
      case 'candidateScanApply':
        lastScan = { kind: 'apply', m }; renderScanResult();
        // Queue changed — refresh it (re-render preserves lastScan).
        if (m.ok) vscode.postMessage({ type: 'getCandidates' });
        break;
      case 'error': renderError(m); break;
    }
  });

  // ---- banner ------------------------------------------------------------
  function effectiveMetadataFreshness(m) {
    if (!m) return 'GRAPH_FRESHNESS_STATE_UNKNOWN';
    const raw = m.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED';
    if (raw !== 'GRAPH_FRESHNESS_STATE_UNSPECIFIED') return raw;
    const triples = Number(m.triple_count || 0);
    if (triples === 0) return 'GRAPH_FRESHNESS_STATE_EMPTY';
    if (m.build_provenance_state === 'BUILD_PROVENANCE_STATE_STAMPED'
      && m.seed_state === 'SEED_STATE_CURRENT'
      && m.live_store_contains_embedded_seed_marker) {
      return 'GRAPH_FRESHNESS_STATE_CURRENT';
    }
    if (m.seed_state === 'SEED_STATE_STALE' || m.live_store_contains_embedded_seed_marker === false) {
      return 'GRAPH_FRESHNESS_STATE_STALE';
    }
    if (m.build_provenance_state === 'BUILD_PROVENANCE_STATE_INCOMPLETE') {
      return 'GRAPH_FRESHNESS_STATE_CHECK_ERROR';
    }
    return 'GRAPH_FRESHNESS_STATE_UNKNOWN';
  }

  function stateLabel(s) {
    return String(s || '')
      .replace(/^GRAPH_FRESHNESS_STATE_/, '')
      .replace(/^BUILD_PROVENANCE_STATE_/, '')
      .replace(/^SEED_STATE_/, '')
      .toLowerCase();
  }

  function metadataAuthority(m) {
    if (!m) {
      return { authoritative: false, verdict: 'unknown', state: 'unknown', summary: 'Graph metadata unavailable', detail: 'The dashboard could not verify the served graph.' };
    }
    const freshness = effectiveMetadataFreshness(m);
    const state = stateLabel(freshness);
    const authoritative =
      freshness === 'GRAPH_FRESHNESS_STATE_CURRENT'
      && m.build_provenance_state === 'BUILD_PROVENANCE_STATE_STAMPED'
      && m.seed_state === 'SEED_STATE_CURRENT'
      && m.live_store_contains_embedded_seed_marker
      && Number(m.triple_count || 0) > 0;
    if (authoritative) {
      return { authoritative: true, verdict: 'authoritative', state, summary: 'Graph authority current', detail: '' };
    }
    let summary = `Graph authority ${state}`;
    if (freshness === 'GRAPH_FRESHNESS_STATE_STALE') summary = 'Live graph stale — authority disabled';
    else if (freshness === 'GRAPH_FRESHNESS_STATE_UNKNOWN') summary = 'Graph freshness unknown — authority disabled';
    else if (freshness === 'GRAPH_FRESHNESS_STATE_EMPTY') summary = 'Graph empty — authority disabled';
    else if (freshness === 'GRAPH_FRESHNESS_STATE_CHECK_ERROR') summary = 'Graph check error — freshness unverified';
    else if (m.build_provenance_state === 'BUILD_PROVENANCE_STATE_DEV' || !m.graph_build_commit) summary = 'Dev build — provenance unstamped';
    let detail = m.graph_freshness_detail || '';
    if (!detail && Number(m.triple_count || 0) === 0) detail = 'The live store is empty and cannot serve graph-backed authority.';
    else if (!detail && m.live_store_contains_embedded_seed_marker === false) detail = 'The live store does not contain the expected embedded seed marker.';
    else if (!detail) detail = 'The dashboard cannot prove the served graph matches the current validated artifact.';
    const verdict = freshness === 'GRAPH_FRESHNESS_STATE_STALE' ? 'stale'
      : freshness === 'GRAPH_FRESHNESS_STATE_EMPTY' ? 'empty'
      : freshness === 'GRAPH_FRESHNESS_STATE_CHECK_ERROR' ? 'degraded'
      : 'unknown';
    return { authoritative: false, verdict, state, summary, detail };
  }

  function bannerState(m) {
    if (!m) return { c: 'unknown', t: 'unknown' };
    const authority = metadataAuthority(m);
    if (!authority.authoritative) {
      return { c: authority.verdict === 'degraded' || authority.verdict === 'stale' ? 'warn' : 'unknown', t: authority.summary };
    }
    const dev = m.build_provenance_state === 'BUILD_PROVENANCE_STATE_DEV'
      || m.server_version === '0.0.0-dev' || !m.graph_build_commit;
    const built = Number(m.graph_build_time_unix || 0);
    if (!dev && built > 0) {
      const ageDays = (Date.now() / 1000 - built) / 86400;
      if (ageDays > 30) return { c: 'warn', t: 'Stale — built ' + Math.round(ageDays) + 'd ago' };
    }
    if (dev) return { c: 'warn', t: 'Dev build — provenance unstamped' };
    return { c: 'ok', t: 'In control' };
  }

  function renderBanner() {
    const b = $('banner'), st = $('bannerState'), stats = $('bannerStats'), mEl = $('bannerMeta');
    b.classList.remove('banner--loading');
    const s = bannerState(meta);
    const authority = metadataAuthority(meta);
    const freshness = effectiveMetadataFreshness(meta);
    st.className = 'banner__state banner__state--' + s.c;
    st.textContent = phaseHeaderText(s);
    renderRefreshBar();
    const cells = [
      ['invariant_count', 'invariants'], ['failure_mode_count', 'failure modes'],
      ['incident_pattern_count', 'incident pat.'], ['intent_count', 'intents'],
      ['forbidden_fix_count', 'forbidden fixes'], ['required_test_count', 'tests'],
      ['source_file_count', 'files'],
      ['component_count', 'components'], ['contract_count', 'contracts'], ['decision_count', 'decisions'],
      ['triple_count', activeDomain ? 'scope triples' : 'store triples'],
    ];
    stats.innerHTML = '';
    for (const [f, lbl] of cells) {
      const d = document.createElement('div'); d.className = 'stat';
      d.innerHTML = `<span class="stat__num">${fmt(meta[f])}</span><span class="stat__lbl">${lbl}</span>`;
      stats.appendChild(d);
    }
    const short = (c) => (c ? c.slice(0, 8) : '—');
    const builtTxt = Number(meta.graph_build_time_unix || 0) > 0
      ? new Date(Number(meta.graph_build_time_unix) * 1000).toLocaleString() : 'unstamped';
    mEl.innerHTML = [
      `version <b>${esc(meta.server_version || '?')}</b>`,
      `graph build <b>${short(meta.graph_build_commit)}</b>`,
      `source commit <b>${short(meta.source_repo_commit)}</b>`,
      `authority <b>${authority.authoritative ? 'authoritative' : 'non-authoritative'}</b>`,
      `seed digest <b>${short(meta.embedded_seed_digest_sha256)}</b>`,
      `live graph <b>${esc(stateLabel(freshness))}</b>`,
      `live digest <b>${short(meta.live_store_graph_digest_sha256)}</b>`,
      `live triples <b>${fmt(Number(meta.live_store_graph_triple_count || 0))}</b>`,
      `coverage <b>${esc((meta.coverage_state || 'COVERAGE_STATE_UNSPECIFIED').replace('COVERAGE_STATE_', '').toLowerCase())}</b>`,
      `candidate queue <b>${esc((meta.candidate_queue_state || 'CANDIDATE_QUEUE_STATE_UNSPECIFIED').replace('CANDIDATE_QUEUE_STATE_', '').toLowerCase())}</b>`,
      `benchmark <b>${esc((meta.benchmark_state || 'BENCHMARK_STATE_UNSPECIFIED').replace('BENCHMARK_STATE_', '').toLowerCase())}</b>`,
      `closure <b>${esc(closureLabel())}</b>`,
      `agent admission <b>${esc(admissionLabel())}</b>`,
      `built <b>${esc(builtTxt)}</b>`,
      `query ${esc(meta.generated_in_ms || '?')}ms`,
      `<span title="The daemon cannot see your live source tree, so drift vs. uncommitted/HEAD changes is not verifiable here.">sync vs live source: not verifiable ⓘ</span>`,
    ].map((x) => `<span>${x}</span>`).join('');
    if (!authority.authoritative && authority.detail) {
      mEl.innerHTML += `<span class="banner__meta-warn">${esc(authority.detail)}</span>`;
    }
  }

  function phaseHeaderText(graphState) {
    if (activePhase === 'awareness') return graphState.t;
    const graph = graphState.c === 'ok' || graphState.c === 'warn' ? 'Graph current' : graphState.t;
    const task = activeTask();
    if (!task || task.kind === 'none') return `${graph} · no active architectural task`;
    if (!taskScopeMatches()) return `${graph} · workspace task belongs to another domain`;
    if (task.kind === 'stale' || task.kind === 'invalid' || task.verified === false) return 'Task stale · admission not trusted';
    const admission = task.admission || 'not established';
    if (/admitted|allowed/i.test(admission)) return `${graph} · bounded mutation admitted`;
    return `${graph} · closure ${task.closure || 'open'} · mutation waiting`;
  }

  function summaryOf(kind, key) {
    const a = controlState && controlState.artifacts && controlState.artifacts[kind];
    return (a && a.summary && a.summary[key]) || '';
  }

  function activeTask() {
    return controlState && controlState.activeTask;
  }

  function taskScopeMatches() {
    const task = activeTask();
    if (!task || task.kind === 'none' || !activeDomain || !task.repositoryDomain) return true;
    if (controlState && controlState.graphDomain === activeDomain && typeof controlState.taskScopeMatches === 'boolean') {
      return controlState.taskScopeMatches;
    }
    return task.repositoryDomain === activeDomain;
  }

  function closureLabel() {
    const task = activeTask();
    if (task && task.kind !== 'none' && taskScopeMatches()) return task.closure || 'open';
    const a = controlState && controlState.artifacts && controlState.artifacts.closure;
    if (!a || !a.exists) return 'not assessed';
    return summaryOf('closure', 'verdict') || 'selected';
  }

  function admissionLabel() {
    const task = activeTask();
    if (task && task.kind !== 'none' && taskScopeMatches()) return task.admission || 'not established';
    const a = controlState && controlState.artifacts && controlState.artifacts.admission;
    if (!a || !a.exists) return 'not established';
    return summaryOf('admission', 'decision') || 'selected';
  }

  function renderPhaseRail() {
    const rail = $('phaseRail');
    if (!rail) return;
    rail.querySelectorAll('.phase-btn').forEach((b) => {
      b.classList.toggle('phase-btn--on', b.dataset.phase === activePhase);
      b.onclick = () => selectPhase(b.dataset.phase);
    });
  }

  function selectPhase(phase) {
    if (!phase || phase === activePhase) return;
    activePhase = phase;
    renderPhaseRail();
    renderNav();
    selectAspect(phase === 'control' ? 'control_overview' : 'invariant');
  }

  // ---- two-mode refresh --------------------------------------------------
  // Reload re-pulls the served graph (Metadata) — cheap, always available.
  // Rebuild runs `sensei rebuild` then reloads — a gated local op with progress
  // and before/after counts. Together they close the read→reload→rebuild loop.
  function renderRefreshBar() {
    const acts = $('bannerActions');
    if (!acts) return;
    const rb = localOps.rebuild || { mode: 'single' };
    const blocked = rb.mode === 'blocked';
    const dis = localOps.enabled && !blocked ? '' : 'disabled';
    // Tooltip explains the exact command (so the user sees single vs combined),
    // or why the button is disabled.
    let hintText;
    if (!localOps.enabled) {
      hintText = 'Enable sensei.enableLocalOperations to rebuild from the dashboard';
    } else if (blocked) {
      hintText = rb.reason || 'Combined graph rebuild requires sensei.servicesRepoPath';
    } else {
      hintText = 'Runs: ' + (rb.command || 'sensei rebuild')
        + (rb.mode === 'combined' ? '  (combined graph)' : '  (single-repo)');
    }
    acts.innerHTML =
      `<button class="rfr" data-refresh="reload" title="Re-pull the served graph (Metadata)">↻ Reload</button>`
      + `<button class="rfr rfr--rebuild" data-refresh="rebuild" ${dis} title="${esc(hintText)}">⟳ Rebuild</button>`
      + domainSelectHtml()
      + `<span class="rfr__status" id="refreshStatus"></span>`;
    acts.querySelectorAll('.rfr').forEach((btn) =>
      btn.addEventListener('click', () => { if (!btn.disabled) onRefresh(btn.dataset.refresh); }));
    const sel = $('domainSelect');
    if (sel) {
      sel.addEventListener('change', () => {
        activeDomain = sel.value;
        vscode.postMessage({ type: 'setDomain', domain: sel.value }); // re-pulls the banner
        const a = aspectByKey(active);
        if (a && a.cls && !String(a.cls).startsWith('__')) {
          vscode.postMessage({ type: 'listClass', cls: a.cls });       // re-scope the open list
        }
      });
    }
    renderRefreshStatus();
  }

  // A domain filter, shown only on a multi-domain graph. '' = All domains
  // (graph-wide). Defaults to the current project (resolved host-side).
  function domainSelectHtml() {
    const domains = (meta && meta.available_domains) || [];
    if (!domains.length) return '';
    const opt = (val, label, sel) => `<option value="${esc(val)}"${sel ? ' selected' : ''}>${esc(label)}</option>`;
    let opts = opt('', 'All domains (store)', activeDomain === '');
    for (const d of domains) opts += opt(d, d, activeDomain === d);
    return `<select id="domainSelect" class="domain-select" title="Scope the banner and lists to a project/domain">${opts}</select>`;
  }

  function onRefresh(mode) {
    if (mode === 'rebuild' && (!localOps.enabled || (localOps.rebuild && localOps.rebuild.mode === 'blocked'))) return;
    lastRefresh = { mode, running: true };
    renderRefreshStatus();
    vscode.postMessage({ type: 'refresh', mode });
  }

  function renderRefreshStatus() {
    const el = $('refreshStatus');
    if (!el || !lastRefresh) return;
    const m = lastRefresh;
    if (m.running) {
      el.className = 'rfr__status';
      el.textContent = m.mode === 'rebuild' ? 'Rebuilding…' : 'Reloading…';
      return;
    }
    if (m.cancelled) { el.textContent = ''; return; }
    if (!m.ok) {
      el.className = 'rfr__status rfr__bad';
      if (m.unreachable) {
        el.textContent = `${m.mode === 'rebuild' ? 'Rebuild' : 'Reload'} finished, but the authority backend is unreachable`;
        return;
      }
      if (m.authority && m.reloaded !== false) {
        const done = m.mode === 'rebuild' ? 'Rebuilt' : 'Reloaded';
        // Surface the *reason* authority is off (e.g. "Dev build — provenance
        // unstamped"), not a self-contradictory "graph is current — authority
        // disabled".
        const why = m.authority.summary || `authority ${m.authority.state}`;
        if (m.authority.state === 'current') {
          // The graph is fresh; only the release provenance is unstamped (a local
          // or dev-built server). That's fine to work against — it's an advisory,
          // not a failure — so don't cry wolf with a red status.
          el.className = 'rfr__status';
          el.textContent = `✓ ${done} — ${why}`;
        } else {
          el.textContent = `${done === 'Rebuilt' ? 'Rebuild' : 'Reload'} finished — ${why}`;
        }
      } else {
        el.textContent = `${m.mode === 'rebuild' ? 'Rebuild' : 'Reload'} failed${m.mode === 'rebuild' ? ' — see Awareness Operations log' : ''}`;
      }
      return;
    }
    el.className = 'rfr__status rfr__ok';
    if (m.mode === 'reload') { el.textContent = '✓ Reloaded'; return; }
    // rebuild
    if (!m.reloaded) {
      el.className = 'rfr__status rfr__bad';
      el.textContent = m.reloadWarning
        ? `Rebuilt on disk; live reload failed — ${m.reloadWarning}`
        : 'Rebuilt on disk; live reload failed';
      return;
    }
    if (m.before && m.after) {
      const dT = m.after.triples - m.before.triples;
      let s = `✓ Rebuilt: ${fmt(m.before.triples)} → ${fmt(m.after.triples)} triples`;
      if (dT) s += ` (${dT > 0 ? '+' : ''}${dT})`;
      el.textContent = s;
    } else {
      el.textContent = m.reloaded ? '✓ Rebuilt' : '✓ Rebuilt (restart sensei serve to reload)';
    }
  }

  // ---- nav ---------------------------------------------------------------
  function renderNav() {
    elNav.innerHTML = '';
    for (const a of ASPECTS.filter((x) => (x.phase || 'awareness') === activePhase)) {
      if (a.group) {
        const sep = document.createElement('span');
        sep.className = 'nav__group';
        sep.textContent = a.group;
        elNav.appendChild(sep);
      }
      const btn = document.createElement('button');
      btn.className = 'tab' + (a.key === active ? ' tab--active' : '');
      const c = a.count && meta ? `<span class="count">${fmt(meta[a.count])}</span>` : '';
      btn.innerHTML = esc(a.label) + c;
      btn.addEventListener('click', () => selectAspect(a.key));
      elNav.appendChild(btn);
    }
  }

  function aspectByKey(k) { return ASPECTS.find((a) => a.key === k); }

  // ---- UML view filter ---------------------------------------------------
  function renderViewFilter() {
    elViewFilter.innerHTML = '<span class="viewbar__label">UML view</span>';
    const mk = (label, val) => {
      const b = document.createElement('button');
      b.className = 'chip-view' + (viewFilter === val ? ' chip-view--on' : '');
      b.textContent = label;
      b.addEventListener('click', () => setViewFilter(val));
      elViewFilter.appendChild(b);
    };
    mk('All', null);
    for (const v of UML_VIEWS) mk(v.charAt(0).toUpperCase() + v.slice(1), v);
  }

  function setViewFilter(v) {
    viewFilter = v;
    renderViewFilter();
    renderList();
    renderGraph();
  }

  // A row/node passes the view filter when no filter is active, or its
  // uml_view matches. Rows without a uml_view appear only under "All".
  function passesView(umlView) {
    if (!viewFilter) return true;
    return (umlView || '').toLowerCase() === viewFilter;
  }

  function selectAspect(key) {
    const selected = aspectByKey(key);
    if (selected && selected.phase && selected.phase !== activePhase) {
      activePhase = selected.phase;
      renderPhaseRail();
    }
    active = key;
    selectedId = null; // each aspect auto-selects its own first row
    renderNav();
    document.body.classList.toggle('candidates-mode', key === 'candidates');
    document.body.classList.toggle('review-mode', key === 'review');
    document.body.classList.toggle('control-mode', activePhase === 'control');
    const a = aspectByKey(key);
    listRows = [];
    listAuthority = null;
    elSearch.value = '';
    if (key === 'review') {
      // Read-only health review computed locally from Metadata + candidate
      // files. We lazily scan candidates once so proposal #5 has real evidence;
      // the 'candidates' message handler re-renders the review when they land.
      elCount.textContent = '';
      if (candidateFiles === null) vscode.postMessage({ type: 'getCandidates' });
      renderReview();
      return;
    }
    if (key === 'candidates') {
      elList.innerHTML = '<div class="notice">Loading candidate queue…</div>';
      elCount.textContent = '';
      vscode.postMessage({ type: 'getCandidates' });
      return;
    }
    if (key === 'control_overview') {
      elCount.textContent = '';
      vscode.postMessage({ type: 'getControlState' });
      renderControlOverview();
      return;
    }
    if (key === 'benchmark') {
      renderBenchmark();
      return;
    }
    if (!a.cls) {
      // Defensive: an aspect with no by_class endpoint shows its count and says
      // so. Every aspect except Candidates currently has an endpoint, so this
      // only fires if a future class is added to the nav without server support.
      elCount.textContent = '';
      const n = meta ? fmt(meta[a.count]) : '…';
      const low = esc(a.label.toLowerCase());
      elList.innerHTML = `<div class="notice"><b>${n} ${low}</b> in the graph.<br><br>`
        + `There is no by-class listing endpoint for ${low} in this graph build.</div>`;
      clearDetailGraph();
      return;
    }
    elList.innerHTML = '<div class="notice">Loading…</div>';
    elCount.textContent = '';
    vscode.postMessage({ type: 'listClass', cls: a.cls });
  }

  // ---- list --------------------------------------------------------------
  function renderList() {
    const a = aspectByKey(active);
    const q = elSearch.value.trim().toLowerCase();
    const filtered = listRows.filter((r) =>
      passesView(r.uml_view)
      && (!q || (r.label || '').toLowerCase().includes(q) || (r.id || '').toLowerCase().includes(q)));
    const total = a && a.count && meta ? Number(meta[a.count] || 0) : listRows.length;
    const auth = inlineAuthority(listAuthority);
    elCount.textContent = `${filtered.length} shown` + (total > listRows.length ? ` · ${total} total (first ${listRows.length})` : ` · ${total}`) + (auth ? ` · ${auth}` : '');
    if (!filtered.length) { elList.innerHTML = '<div class="notice">No matching items.</div>'; return; }
    elList.innerHTML = '';
    for (const r of filtered) {
      const row = document.createElement('div');
      row.className = 'row' + (r.id === selectedId ? ' row--active' : '');
      const color = SEV_COLOR[(r.severity || '').toLowerCase()] || CLASS_COLOR[r.class] || '#888';
      const sev = r.severity ? `<span class="row__sev" style="color:${color}">${esc(r.severity)}</span>` : '';
      row.innerHTML = `<span class="row__dot" style="background:${color}"></span>`
        + `<span class="row__body"><span class="row__label">${esc(r.label || bare(r.id))}</span>`
        + `<span class="row__id">${esc(r.id || '')}</span></span>${sev}`;
      row.addEventListener('click', () => select(r.id, r.label));
      elList.appendChild(row);
    }
  }

  // ---- selection ---------------------------------------------------------
  function select(id, label) {
    if (!id) return;
    selectedId = id;
    document.querySelectorAll('.row').forEach((r) => r.classList.remove('row--active'));
    elDetail.innerHTML = '<p class="muted">Resolving…</p>';
    vscode.postMessage({ type: 'resolve', id });
    requestGraph(id, label, graph.depth);
    renderList();
  }

  function requestGraph(id, label, depth) {
    graph.center = id; graph.label = label || bare(id); graph.view = null; graph.authority = null;
    vscode.postMessage({ type: 'graph', id, label: graph.label, depth });
    elSvg.innerHTML = '';
  }

  // ---- detail ------------------------------------------------------------
  function renderDetail(m) {
    if (m.unsupported) {
      elDetail.innerHTML = `<div class="notice"><b>${esc((m.klass || '').replace(/_/g, ' '))}</b> nodes have no detail endpoint, `
        + `so there's nothing to resolve here. This node still appears in the focus graph and as a related link — `
        + `open the invariant or failure mode it attaches to for full context.</div>`;
      return;
    }
    if (!m.found || !m.node) {
      elDetail.innerHTML = `<div class="notice notice--bad">Could not resolve <code>${esc(m.id)}</code>. `
        + `The node may belong to a different domain scope, or only appears as a relationship target.</div>`;
      return;
    }
    const n = m.node;
    const color = SEV_COLOR[(n.severity || '').toLowerCase()] || CLASS_COLOR[n.class] || '#888';
    let h = `<h2>${esc(n.label || bare(n.id))}</h2>`;
    h += `<div class="sub"><span class="badge" style="border-color:${CLASS_COLOR[n.class] || '#888'}">${esc(n.class || '?')}</span> `;
    if (n.severity) h += `<span class="badge" style="color:${color};border-color:${color}">${esc(n.severity)}</span> `;
    if (n.status) h += `<span class="badge">${esc(n.status)}</span> `;
    h += `<code class="muted">${esc(n.id || '')}</code></div>`;
    h += authorityLine(m.authority);
    h += phase2Honesty(n.class);
    h += umlLine(n);
    if (n.description) h += `<div class="desc">${esc(n.description)}</div>`;

    if (n.anchor && (n.anchor.file || n.anchor.source_yaml)) {
      const a = n.anchor;
      const where = a.file
        ? `${a.file}${a.symbol ? ' · ' + a.symbol : ''}${a.line_start ? ':' + a.line_start : ''}`
        : a.source_yaml;
      const file = a.file || a.source_yaml;
      h += `<div class="section"><h4>Anchor</h4><span class="anchor-link" data-file="${esc(file)}" data-line="${(a.line_start || 1) - 1}">${esc(where)}</span></div>`;
    }

    // Literal rules (a pattern's requiresCall / mustFollow / …) — these govern
    // code by rule, not by an edge to a node, so they never appear in the focus
    // graph. Showing them here makes a link-sparse pattern read as governed.
    if (n.facts && n.facts.length) {
      h += '<div class="section"><h4>Rules</h4>';
      for (const f of n.facts) {
        h += `<span class="fact"><span class="fact__pred">${esc(f.predicate || '')}</span>`
          + `<span class="fact__val">${esc(f.value || '')}</span></span>`;
      }
      h += '</div>';
    }

    const groups = groupRelated(n.related_ids || []);
    for (const [title, key] of REL_SECTIONS) h += relSection(title, groups[key]);

    elDetail.innerHTML = h;
    elDetail.querySelectorAll('.chip').forEach((c) =>
      c.addEventListener('click', () => select(c.dataset.id, c.dataset.label)));
    elDetail.querySelectorAll('.anchor-link').forEach((c) =>
      c.addEventListener('click', () => vscode.postMessage({ type: 'openAnchor', file: c.dataset.file, line: Number(c.dataset.line) })));
  }

  // Renders the optional UML profile line («stereotype» Kind · view) when present.
  function umlLine(n) {
    const parts = [];
    if (n.uml_stereotype) parts.push(`<span class="uml__stereo">«${esc(n.uml_stereotype)}»</span>`);
    if (n.uml_kind) parts.push(esc(n.uml_kind));
    if (n.uml_view) parts.push(`<span class="uml__view">${esc(n.uml_view)} view</span>`);
    return parts.length ? `<div class="uml">${parts.join(' · ')}</div>` : '';
  }

  function authorityLine(a) {
    if (!a) return '';
    const freshness = stateLabel(a.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED');
    const provenance = String(a.build_provenance_state || 'BUILD_PROVENANCE_STATE_UNSPECIFIED')
      .replace('BUILD_PROVENANCE_STATE_', '').toLowerCase();
    let transaction = 'uncertified';
    if (a.embedded_transaction_matches_seed) {
      transaction = 'certified';
    } else if (!a.embedded_transaction_stamp_present) {
      transaction = 'missing';
    }
    const state = a.authoritative ? 'current' : 'non-authoritative';
    let txt = `authority ${state} · freshness ${freshness} · provenance ${provenance} · transaction ${transaction}`;
    if (a.live_store_graph_digest_sha256) {
      txt += ` · live ${esc(String(a.live_store_graph_digest_sha256).slice(0, 8))}`;
    }
    if (a.certified_awareness_graph_commit || a.certified_services_repo_commit) {
      txt += ` · tx graph ${esc(String(a.certified_awareness_graph_commit || 'n/a').slice(0, 8))}`;
      txt += ` · tx svc ${esc(String(a.certified_services_repo_commit || 'n/a').slice(0, 8))}`;
    }
    if (a.embedded_transaction_detail) {
      txt += ` · ${esc(a.embedded_transaction_detail)}`;
    }
    if (a.graph_freshness_detail) {
      txt += ` · ${esc(a.graph_freshness_detail)}`;
    }
    return `<div class="uml">${txt}</div>`;
  }

  function phase2Honesty(cls) {
    const c = String(cls || '');
    if (c === 'architecture_claim') {
      return '<div class="control-warn"><b>Claim is non-authoritative.</b> It remains a maintained proposition until governance promotes or accepts the relevant knowledge.</div>';
    }
    if (c === 'open_question') {
      return '<div class="control-warn"><b>Question records a closure gap.</b> It is not evidence and must stay visible until explicitly resolved.</div>';
    }
    if (c === 'architect_answer') {
      return '<div class="control-warn"><b>Architect answer is not Evidence.</b> Acceptance/adjudication is separate and does not by itself prove implementation truth.</div>';
    }
    if (c === 'evidence_probe') {
      return '<div class="control-warn"><b>Probe is a plan only.</b> This client does not execute probes or tests.</div>';
    }
    return '';
  }

  function inlineAuthority(a) {
    if (!a) return '';
    const freshness = stateLabel(a.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED');
    let transaction = 'uncertified';
    if (a.embedded_transaction_matches_seed) {
      transaction = 'certified';
    } else if (!a.embedded_transaction_stamp_present) {
      transaction = 'missing';
    }
    return a.authoritative
      ? `authority current (${freshness}, ${transaction})`
      : `authority ${freshness} (${transaction})`;
  }

  // Ordered related-node sections — architecture first, then the rule/bug
  // surface. Any token without its own bucket falls into "Other".
  const REL_SECTIONS = [
    ['Components', 'component'], ['Boundaries', 'boundary'], ['Contracts', 'contract'],
    ['Decisions', 'decision'], ['Evidence', 'evidence'], ['Meta-principles', 'meta_principle'],
    ['Claims', 'architecture_claim'], ['Questions', 'open_question'],
    ['Answers', 'architect_answer'], ['Probes', 'evidence_probe'],
    ['Design patterns', 'design_pattern'], ['Impl. patterns', 'implementation_pattern'],
    ['Pattern misuses', 'pattern_misuse'],
    ['Source files', 'source_file'], ['Tests', 'test'], ['Forbidden fixes', 'forbidden_fix'],
    ['Failure modes', 'failure_mode'], ['Intents', 'intent'], ['Invariants', 'invariant'],
    ['Incident patterns', 'incident_pattern'], ['Other', 'other'],
  ];

  function groupRelated(ids) {
    const g = {};
    for (const [, key] of REL_SECTIONS) g[key] = [];
    for (const id of ids) {
      const t = token(id);
      (g[t] || g.other).push(id);
    }
    return g;
  }

  function relSection(title, ids) {
    if (!ids || !ids.length) return '';
    let h = `<div class="section"><h4>${esc(title)} (${ids.length})</h4>`;
    for (const id of ids) {
      const t = token(id);
      h += `<span class="chip" data-id="${esc(id)}" data-label="${esc(bare(id))}">`
        + `<span class="chip__dot" style="background:${CLASS_COLOR[t] || '#888'}"></span>${esc(bare(id))}</span>`;
    }
    return h + '</div>';
  }

  // ---- focus graph -------------------------------------------------------
  function renderGraph() {
    elSvg.innerHTML = '';
    const nodes = graph.nodes;
    if (!nodes.length) { return; }
    const title = $('graphTitle');
    if (title) {
      const auth = inlineAuthority(graph.authority);
      title.textContent = graph.label ? graph.label + (auth ? ` · ${auth}` : '') : (auth || 'Focus graph');
    }
    const center = nodes.find((n) => n.id === graph.center) || nodes[0];
    const pos = layout(nodes, center);

    // Nodes outside the active UML view are dimmed (center always visible).
    const dimmed = new Set();
    if (viewFilter) {
      for (const n of nodes) {
        if (n.id !== center.id && !passesView(n.uml_view)) dimmed.add(n.id);
      }
    }

    const root = document.createElementNS(SVGNS, 'g');
    // edges first
    for (const e of graph.edges) {
      const a = pos[e.from], b = pos[e.to];
      if (!a || !b) continue;
      const dim = dimmed.has(e.from) || dimmed.has(e.to) ? ' gedge--dim' : '';
      const line = mk('line', { x1: a.x, y1: a.y, x2: b.x, y2: b.y, class: 'gedge' + dim });
      root.appendChild(line);
      const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
      if (e.relation && e.relation !== 'related') {
        root.appendChild(mk('text', { x: mx, y: my - 2, class: 'gedge-label', 'text-anchor': 'middle' }, e.relation));
      }
    }
    // nodes
    for (const n of nodes) {
      const p = pos[n.id]; if (!p) continue;
      const g = mk('g', { class: 'gnode' + (n.id === center.id ? ' gnode--center' : '') + (dimmed.has(n.id) ? ' gnode--dim' : '') });
      const r = n.id === center.id ? 11 : 7;
      g.appendChild(mk('circle', { cx: p.x, cy: p.y, r, fill: CLASS_COLOR[n.token] || '#888' }));
      const label = n.label.length > 26 ? n.label.slice(0, 25) + '…' : n.label;
      g.appendChild(mk('text', { x: p.x + r + 3, y: p.y + 4 }, label));
      g.addEventListener('click', () => select(n.id, n.label));
      g.addEventListener('mouseenter', (ev) => showTip(ev, n));
      g.addEventListener('mousemove', moveTip);
      g.addEventListener('mouseleave', hideTip);
      root.appendChild(g);
    }
    elSvg.appendChild(root);
    if (!graph.view) fitGraph(true);
    else applyView();
    ensureLegend();
  }

  function layout(nodes, center) {
    const pos = { [center.id]: { x: 0, y: 0 } };
    const byClass = {};
    for (const n of nodes) {
      if (n.id === center.id) continue;
      (byClass[n.token] = byClass[n.token] || []).push(n);
    }
    for (const tok in byClass) {
      const group = byClass[tok];
      const dir = DIR[tok] || DIR.related;
      const base = Math.atan2(dir[1], dir[0]);
      const spread = Math.min(1.1, 0.32 * (group.length - 1));
      group.forEach((n, i) => {
        const off = group.length === 1 ? 0 : (i / (group.length - 1) - 0.5) * spread * 2;
        const ang = base + off;
        const R = n.level >= 2 ? 360 : 190;
        pos[n.id] = { x: Math.cos(ang) * R, y: Math.sin(ang) * R };
      });
    }
    return pos;
  }

  function fitGraph(reset) {
    const bbox = elSvg.firstChild ? elSvg.getBBox() : { x: -200, y: -200, width: 400, height: 400 };
    const pad = 60;
    graph.view = { x: bbox.x - pad, y: bbox.y - pad, w: bbox.width + pad * 2, h: bbox.height + pad * 2 };
    if (reset) applyView();
  }

  function applyView() {
    const v = graph.view;
    elSvg.setAttribute('viewBox', `${v.x} ${v.y} ${v.w} ${v.h}`);
  }

  // zoom + pan
  elSvg.addEventListener('wheel', (e) => {
    if (!graph.view) return;
    e.preventDefault();
    const v = graph.view, f = e.deltaY < 0 ? 0.9 : 1.1;
    const r = elSvg.getBoundingClientRect();
    const px = v.x + ((e.clientX - r.left) / r.width) * v.w;
    const py = v.y + ((e.clientY - r.top) / r.height) * v.h;
    v.x = px - (px - v.x) * f; v.y = py - (py - v.y) * f; v.w *= f; v.h *= f;
    applyView();
  }, { passive: false });

  let drag = null;
  elSvg.addEventListener('mousedown', (e) => { drag = { x: e.clientX, y: e.clientY }; });
  window.addEventListener('mousemove', (e) => {
    if (!drag || !graph.view) return;
    const r = elSvg.getBoundingClientRect(), v = graph.view;
    v.x -= ((e.clientX - drag.x) / r.width) * v.w;
    v.y -= ((e.clientY - drag.y) / r.height) * v.h;
    drag = { x: e.clientX, y: e.clientY };
    applyView();
  });
  window.addEventListener('mouseup', () => { drag = null; });

  function showTip(ev, n) {
    elTip.hidden = false;
    elTip.innerHTML = `<b>${esc(n.label)}</b><br><span class="t-class">${esc(n.token)}${n.severity ? ' · ' + esc(n.severity) : ''}</span><br><code>${esc(n.id)}</code>`;
    moveTip(ev);
  }
  function moveTip(ev) {
    const wrap = elSvg.parentElement.getBoundingClientRect();
    elTip.style.left = (ev.clientX - wrap.left + 12) + 'px';
    elTip.style.top = (ev.clientY - wrap.top + 12) + 'px';
  }
  function hideTip() { elTip.hidden = true; }

  // Legend reflects the class tokens actually present in the current focus
  // graph (rebuilt each render) rather than a fixed list, so it grows with
  // architecture nodes without becoming a wall of swatches.
  function ensureLegend() {
    let l = document.getElementById('legend');
    if (!l) {
      l = document.createElement('div'); l.id = 'legend'; l.className = 'legend';
      elSvg.parentElement.appendChild(l);
    }
    const present = [];
    const seen = new Set();
    for (const n of graph.nodes) {
      const t = n.token;
      if (t && CLASS_COLOR[t] && !seen.has(t)) { seen.add(t); present.push(t); }
    }
    l.innerHTML = present
      .map((t) => `<span><i style="background:${CLASS_COLOR[t]}"></i>${t.replace(/_/g, ' ')}</span>`).join('');
  }

  function clearDetailGraph() {
    elDetail.innerHTML = '<p class="muted">Select a concern to inspect its reasoning chain.</p>';
    const title = $('graphTitle');
    if (title) title.textContent = 'Reasoning chain';
    elSvg.innerHTML = ''; graph.center = null; selectedId = null;
  }

  // ---- Phase 2: closure and control --------------------------------------
  function artifact(kind) {
    return controlState && controlState.artifacts && controlState.artifacts[kind];
  }

  function artifactValue(kind, key, fallback) {
    const a = artifact(kind);
    return (a && a.summary && a.summary[key]) || fallback || '';
  }

  function artifactCard(kind, title, emptyLabel) {
    const a = artifact(kind);
    const ok = a && a.exists && a.valid;
    let h = `<div class="control-card ${ok ? 'control-card--ok' : ''}">`;
    h += `<div class="control-card__head"><b>${esc(title)}</b>`;
    h += `<span class="badge">${ok ? 'selected' : emptyLabel}</span></div>`;
    if (!a || !a.exists) {
      h += `<div class="muted">${esc((a && a.error) || 'No artifact selected.')}</div>`;
    } else {
      h += `<div class="control-path">${esc(a.path || a.configured || '')}</div>`;
      const rows = Object.entries(a.summary || {}).filter(([, v]) => v);
      if (rows.length) {
        h += '<dl class="control-dl">';
        for (const [k, v] of rows) h += `<dt>${esc(k.replace(/_/g, ' '))}</dt><dd>${esc(v)}</dd>`;
        h += '</dl>';
      } else {
        h += '<div class="muted">Artifact selected; no canonical summary fields recognized by the client.</div>';
      }
      if (a.digest) h += `<div class="muted">digest ${esc(String(a.digest).slice(0, 12))}</div>`;
    }
    h += '<div class="control-actions">';
    h += `<button class="btn-mini control-act" data-control-select="${esc(kind)}">Select</button>`;
    h += `<button class="btn-mini control-act" data-control-status="${esc(kind)}" ${ok ? '' : 'disabled'}>Status</button>`;
    h += '</div></div>';
    return h;
  }

  function renderControlOverview() {
    const task = activeTask();
    renderTaskList(task);
    let h = '<div class="control">';
    if (!task || task.kind === 'none') {
      h += noActiveTaskHtml(task);
    } else {
      h += activeTaskHomeHtml(task);
    }
    h += advancedArtifactSelectionHtml(!task || task.kind === 'none');
    h += '</div>';
    elDetail.innerHTML = h;
    clearControlGraph();
    elDetail.querySelectorAll('[data-control-select]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'selectControlArtifact', kind: b.dataset.controlSelect })));
    elDetail.querySelectorAll('[data-control-status]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'controlStatus', kind: b.dataset.controlStatus })));
    elDetail.querySelectorAll('[data-control-clear]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'clearControlSelection' })));
    elDetail.querySelectorAll('[data-control-refresh]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'getControlState' })));
    elDetail.querySelectorAll('[data-copy-command]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'copy', text: b.dataset.copyCommand })));
  }

  function controlPill(label, value) {
    return `<div class="control-pill"><span>${esc(label)}</span><b>${esc(value || 'unknown')}</b></div>`;
  }

  function renderTaskList(task) {
    if (!task || task.kind === 'none') {
      elCount.textContent = 'No active task';
      elList.innerHTML =
        '<div class="notice"><b>No active architectural task</b><br>Closure is task-bound. Use <code>sensei prepare-change</code> to create and bind closure, convergence, admission, and next-action artifacts.</div>';
      return;
    }
    const c = task.counts || {};
    elCount.textContent = taskScopeMatches()
      ? `${fmt(c.claims)} task claim(s) · ${fmt(c.questions)} question(s) · ${fmt(c.answers)} answer(s) · ${fmt(c.probes)} probe(s)`
      : `Workspace task: ${task.repositoryDomain || 'unknown'} · selected graph: ${activeDomain || 'all domains'}`;
    const rows = [
      ['Task', task.taskId || 'unknown'],
      ['Closure', task.closure || 'open'],
      ['Mutation', task.admission || task.modify || 'waiting'],
      ['Next', nextActionLabel(task.next)],
      ['Active file', task.activeFile && task.activeFile.label],
    ];
    elList.innerHTML = rows.map(([label, value]) =>
      `<div class="control-list-row"><span>${esc(label)}</span><b>${esc(value || 'unknown')}</b></div>`
    ).join('');
  }

  function noActiveTaskHtml(task) {
    let h = '<div class="control-empty">';
    h += '<h2>No active architectural task</h2>';
    h += '<p class="muted">Closure and admission are task-bound. The dashboard will not infer safety from graph-wide Claims, Questions, Answers, or Probes.</p>';
    h += '<div class="control-actions control-actions--top">';
    h += '<button class="btn-mini" data-copy-command="sensei prepare-change --help">Prepare command</button>';
    h += '<button class="btn-mini" data-copy-command="sensei task-status --active --verify">Check active task</button>';
    h += '<button class="btn-mini control-act" data-control-refresh>Advanced artifact selection</button>';
    h += '</div>';
    if (task && task.errors && task.errors.length) {
      h += `<div class="control-warn">${esc(task.errors.join('; '))}</div>`;
    }
    h += '</div>';
    return h;
  }

  function activeTaskHomeHtml(task) {
    const authority = metadataAuthority(meta);
    const verified = task.verified === true ? 'verified' : task.verified === false ? 'not verified' : 'local pointer';
    let h = '<div class="task-home">';
    h += '<div class="task-head">';
    h += `<div><h2>${esc(task.description || 'Active architectural task')}</h2>`;
    h += `<div class="sub"><code>${esc(task.taskId || '')}</code> · ${esc(task.repositoryDomain || 'unknown repo')} · rev ${esc(short(task.revision))} · graph ${esc(short(task.graphDigest))}</div></div>`;
    h += `<span class="badge">${esc(verified)}</span>`;
    h += '</div>';
    if (!taskScopeMatches()) {
      h += `<div class="control-warn"><b>Graph/task scope mismatch.</b> The selected graph is <code>${esc(activeDomain)}</code>, but the active task belongs to <code>${esc(task.repositoryDomain || 'unknown')}</code>. Open that repository workspace to inspect its task dialogue; this task does not establish closure or admission for the selected graph.</div>`;
    }
    h += '<div class="control-strip">';
    h += controlPill('Graph', authority.authoritative ? 'current' : 'not trusted');
    h += controlPill('Closure', task.closure || 'open');
    h += controlPill('Convergence', task.convergence || 'pending');
    h += controlPill('Inspect', task.inspect || 'waiting');
    h += controlPill('Modify', task.modify || task.admission || 'waiting');
    h += '</div>';
    h += '<div class="control-honesty"><b>Bounded closure is not repository-wide understanding.</b> Admission is permission to attempt, not proof of correctness. Verification checks scope only.</div>';
    h += nextActionHtml(task);
    h += '<div class="control-grid">';
    h += taskCountsCard(task);
    h += envelopeCard(task);
    h += governanceBridgeCard(task);
    h += verificationCard(task);
    h += '</div>';
    h += artifactHealthHtml(task);
    return h + '</div>';
  }

  function nextActionHtml(task) {
    const next = task.next || {};
    let h = '<div class="next-action">';
    h += '<span>Next required action</span>';
    h += `<b>${esc(nextActionLabel(next))}</b>`;
    if (next.summary) h += `<p>${esc(next.summary)}</p>`;
    if (next.reference) h += `<code>${esc(next.reference)}</code>`;
    return h + '</div>';
  }

  function nextActionLabel(next) {
    if (!next) return 'prepare change';
    return [next.action, next.reference].filter(Boolean).join(' ') || 'prepare change';
  }

  function taskCountsCard(task) {
    const c = task.counts || {};
    return `<div class="control-card"><div class="control-card__head"><b>Task dialogue</b><span class="badge">task-local</span></div>`
      + `<dl class="control-dl"><dt>claims</dt><dd>${fmt(c.claims)}</dd><dt>questions</dt><dd>${fmt(c.questions)}</dd><dt>answers</dt><dd>${fmt(c.answers)}</dd><dt>probes</dt><dd>${fmt(c.probes)}</dd></dl>`
      + `<div class="muted">Graph tab counts remain repository or domain inventory; these counts come only from the active task artifacts.</div></div>`;
  }

  function envelopeCard(task) {
    const af = task.activeFile || {};
    let h = '<div class="control-card">';
    h += '<div class="control-card__head"><b>Admission envelope</b><span class="badge">exact paths</span></div>';
    h += `<div class="control-big">${esc(af.label || 'No active editor file')}</div>`;
    if (af.relativePath) h += `<div class="control-path">${esc(af.relativePath)}</div>`;
    h += '<div class="section"><h4>Modify</h4>' + pathList(task.modifyEnvelope) + '</div>';
    h += '<div class="section"><h4>Read</h4>' + pathList(task.readEnvelope) + '</div>';
    return h + '</div>';
  }

  function governanceBridgeCard(task) {
    const answers = Number((task.counts && task.counts.answers) || 0);
    return `<div class="control-card"><div class="control-card__head"><b>Governance bridge</b><span class="badge">review</span></div>`
      + `<div class="control-big">${fmt(answers)}</div>`
      + '<div class="muted">Accepted answers are reviewable governance candidates. The dashboard does not promote them automatically.</div></div>';
  }

  function verificationCard(task) {
    let h = '<div class="control-card">';
    h += '<div class="control-card__head"><b>Verification</b><span class="badge">scope only</span></div>';
    h += `<div class="control-big">${esc(task.verified === true ? 'trusted' : task.verified === false ? 'not trusted' : 'pending')}</div>`;
    h += `<dl class="control-dl"><dt>phase</dt><dd>${esc(task.phase || 'unknown')}</dd><dt>status</dt><dd>${esc(task.status || 'unknown')}</dd><dt>proof</dt><dd>pending</dd><dt>correctness certified</dt><dd>no</dd></dl>`;
    if (task.verifyErrors && task.verifyErrors.length) h += `<div class="control-warn">${esc(task.verifyErrors.join('; '))}</div>`;
    return h + '</div>';
  }

  function artifactHealthHtml(task) {
    const rows = task.artifactHealth || [];
    if (!rows.length) return '';
    let h = '<details class="control-advanced"><summary>Task artifact receipts</summary><div class="control-grid">';
    for (const r of rows) {
      h += `<div class="control-card ${r.exists ? 'control-card--ok' : ''}"><div class="control-card__head"><b>${esc(String(r.name).replace(/_/g, ' '))}</b><span class="badge">${r.exists ? 'present' : 'missing'}</span></div>`;
      h += `<div class="control-path">${esc(r.path || '')}</div>`;
      if (r.digest) h += `<div class="muted">digest ${esc(r.digest)}</div>`;
      if (r.error) h += `<div class="muted">${esc(r.error)}</div>`;
      h += '</div>';
    }
    return h + '</div></details>';
  }

  function advancedArtifactSelectionHtml(open) {
    let h = `<details class="control-advanced" ${open ? 'open' : ''}><summary>Advanced artifact selection</summary>`;
    h += '<div class="muted">Manual selections are for inspection only. They do not activate a task and do not establish admission.</div>';
    h += '<div class="control-grid">';
    h += artifactCard('closure', 'Closure assessment', 'not assessed');
    h += artifactCard('convergence', 'Convergence session', 'not selected');
    h += artifactCard('admission', 'Admission decision', 'not established');
    h += artifactCard('verification', 'Admission verification', 'not verified');
    h += '</div>';
    h += '<div class="control-actions control-actions--top">';
    h += '<button class="btn-mini control-act" data-control-clear>Clear selections</button>';
    h += '<button class="btn-mini control-act" data-control-refresh>Refresh artifacts</button>';
    h += '</div>';
    h += controlStatusBlocks();
    return h + '</details>';
  }

  function pathList(paths) {
    if (!paths || !paths.length) return '<div class="muted">none</div>';
    return '<ul class="path-list">' + paths.map((p) => `<li><code>${esc(p)}</code></li>`).join('') + '</ul>';
  }

  function short(v) {
    return v ? String(v).slice(0, 10) : 'unknown';
  }

  function controlStatusBlocks() {
    const entries = Object.entries(controlStatus).filter(([, v]) => v);
    if (!entries.length) return '';
    let h = '<div class="control-statuses">';
    for (const [kind, m] of entries) {
      h += `<div class="opsum ${m.ok ? 'opsum--ok' : 'opsum--bad'}"><b>${esc(kind)} status</b>`;
      if (m.message && !m.ok) h += `<div>${esc(m.message)}</div>`;
      if (m.stdout) h += `<pre>${esc(m.stdout)}</pre>`;
      if (m.stderr) h += `<pre>${esc(m.stderr)}</pre>`;
      h += '</div>';
    }
    return h + '</div>';
  }

  function clearControlGraph() {
    const title = $('graphTitle');
    if (title) title.textContent = 'Phase 2 state';
    elSvg.innerHTML = '';
    graph.center = null;
    selectedId = null;
  }

  function renderBenchmark() {
    elCount.textContent = 'External proof';
    elList.innerHTML = '<div class="notice">Benchmark status is metadata visibility only. There is no network, agent, test, oracle, or evaluation execution button here.</div>';
    let h = '<div class="control">';
    h += '<div class="control-honesty"><b>External proof is separate from closure and admission.</b> The client does not calculate a composite benchmark score.</div>';
    h += '<div class="control-grid">';
    h += benchmarkCard('State', (meta && meta.benchmark_state) || 'BENCHMARK_STATE_UNSPECIFIED');
    h += benchmarkCard('Contracts', fmt(meta && meta.benchmark_contract_count));
    h += benchmarkCard('Learning events', fmt(meta && meta.benchmark_learning_event_count));
    h += benchmarkCard('Latest task', (meta && meta.benchmark_latest_task_id) || 'none');
    h += '</div></div>';
    elDetail.innerHTML = h;
    clearControlGraph();
  }

  function benchmarkCard(title, value) {
    return `<div class="control-card"><div class="control-card__head"><b>${esc(title)}</b></div><div class="control-big">${esc(value)}</div></div>`;
  }

  // ---- candidates --------------------------------------------------------
  // The review→promote surface. Each candidate gets a card with Preview
  // (`sensei promote --dry-run`, no writes) and, when local ops are enabled,
  // Promote (`sensei promote`, which validates → writes canonical YAML → rebuilds,
  // surfaced as a git diff the user commits). When local ops are off, the tab
  // stays read-only and shows the guarded CLI to run by hand.
  function flatCandidates(m) {
    const out = [];
    (m.files || []).forEach((f, fi) =>
      (f.entries || []).forEach((e, ei) => out.push({ e, file: f.path, key: `${fi}-${ei}` })));
    return out;
  }

  function renderCandidates(m) {
    const items = flatCandidates(m);
    const fileCount = (m.files || []).length;
    const approvedCount = items.filter((it) => it.e.decision === 'approved').length;
    elCount.textContent = `${items.length} candidate(s) · ${fileCount} file(s)`;

    // Left list: one row per candidate entry; click scrolls to its card.
    if (!items.length) {
      elList.innerHTML = '<div class="notice">No candidates parsed under <code>docs/awareness/candidates/</code> in this workspace.</div>';
    } else {
      elList.innerHTML = '';
      for (const it of items) {
        const row = document.createElement('div'); row.className = 'row' + (it.e.decision ? ' row--' + it.e.decision : '');
        const color = CLASS_COLOR[it.e.klass] || '#888';
        const mark = it.e.decision === 'approved' ? '✓ ' : it.e.decision === 'rejected' ? '✕ ' : '';
        row.innerHTML = `<span class="row__dot" style="background:${color}"></span>`
          + `<span class="row__body"><span class="row__label">${esc(mark + (it.e.label || it.e.id))}</span>`
          + `<span class="row__id">${esc(it.e.id)}</span></span>`
          + (it.e.klass ? `<span class="row__sev" style="color:${color}">${esc(it.e.klass.replace(/_/g, ' '))}</span>` : '');
        row.addEventListener('click', () => {
          const el = document.getElementById('cx-' + it.key);
          if (el) { el.scrollIntoView({ block: 'nearest' }); el.classList.add('candx--flash'); setTimeout(() => el.classList.remove('candx--flash'), 600); }
        });
        elList.appendChild(row);
      }
    }

    let h = '<div class="cand">';
    h += `<div class="cand__trust"><b>Candidate knowledge ≠ graph truth.</b> Candidates require human approval before promotion. `
      + `Promotion runs the guarded <code>sensei promote</code> → <code>sensei rebuild</code> path locally; nothing enters the graph except through a deterministic rebuild you commit.</div>`;
    h += capabilityBanner();
    h += scanPanel();
    h += batchBar(approvedCount, items.length);
    if (lastOp) h += opSummary(lastOp);

    if (items.length) {
      h += '<h4>Candidate queue</h4>';
      for (const it of items) {
        h += candidateCard(it);
      }
    }

    // Guarded CLI — always available as the reference / manual-recovery flow.
    h += '<details class="cand__cli"><summary>Guarded CLI (run yourself / manual recovery)</summary><div class="cand__cli-body">';
    for (const c of (m.commands || [])) {
      h += `<div class="cmd"><code>${esc(c.cmd)}</code><button data-cmd="${esc(c.cmd)}">copy</button></div><div class="muted" style="margin:-2px 0 8px">${esc(c.label)}</div>`;
    }
    h += '</div></details>';

    // Raw files — full source of truth, collapsed.
    if (fileCount) {
      h += '<details class="cand__cli"><summary>Raw candidate files</summary><div class="cand__cli-body">';
      (m.files || []).forEach((f, i) => {
        h += `<details class="cand__file" id="cand-${i}"><summary>${esc(f.path)}${f.truncated ? ' (truncated)' : ''}</summary><pre>${esc(f.content)}</pre></details>`;
      });
      h += '</div></details>';
    }
    h += '</div>';
    elDetail.innerHTML = h;

    elDetail.querySelectorAll('.cmd button').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'copy', text: b.dataset.cmd })));
    elDetail.querySelectorAll('.candx__act').forEach((b) =>
      b.addEventListener('click', () => onCandAction(b.dataset.act, b.dataset.id, b.dataset.label)));
    elDetail.querySelectorAll('[data-batch]').forEach((b) =>
      b.addEventListener('click', () => { if (!b.disabled) vscode.postMessage({ type: 'promoteApproved' }); }));
    elDetail.querySelectorAll('[data-oplog]').forEach((b) =>
      b.addEventListener('click', () => vscode.postMessage({ type: 'showOpLog' })));
    elDetail.querySelectorAll('.scan__act').forEach((b) =>
      b.addEventListener('click', () => onScanAction(b.dataset.scan)));
    elDetail.querySelectorAll('.candx .anchor-link').forEach((c) =>
      c.addEventListener('click', () => vscode.postMessage({ type: 'openAnchor', file: c.dataset.file, line: 0 })));
    // A prior scan result survives the queue re-render.
    renderScanResult();
  }

  // ---- scan (fill the queue) ---------------------------------------------
  // Audit the codebase for extractable knowledge via the deterministic echo
  // drafter — no LLM, no API key, no cost. Scan previews (dry-run); Apply writes
  // grounded intents + parks candidates for review (a git diff the user commits).
  function scanPanel() {
    const dis = localOps.enabled ? '' : 'disabled';
    const hint = localOps.enabled ? '' : ' title="Enable sensei.enableLocalOperations to run sensei from the dashboard"';
    let h = '<div class="scan">';
    h += '<div class="scan__head"><b>Scan codebase for knowledge</b>'
      + '<span class="scan__sub">Deterministic — echo drafter, no LLM, no cost.</span></div>';
    h += '<div class="scan__desc">Grounds architectural-intent proposals against the workspace tree. '
      + '<b>Scan</b> is a dry-run (nothing written); <b>Apply</b> writes grounded intents and parks weaker proposals + findings under <code>candidates/</code> for review — a git diff you commit. Nothing reaches the graph until you rebuild.</div>';
    h += '<div class="scan__actions">';
    h += `<button class="scan__act btn-mini" data-scan="preview" ${dis}${hint}>Scan (preview)</button>`;
    h += `<button class="scan__act scan__act--apply" data-scan="apply" ${dis}${hint}>Apply to queue…</button>`;
    h += '</div>';
    if (!localOps.enabled) {
      h += '<div class="scan__cli muted">Or run it yourself: <code>sensei intent-mine --repo . --sources docs,comments,schemas,tests --drafter echo</code> (add <code>--apply</code> to write).</div>';
    }
    h += '<div class="scan__result" hidden></div>';
    h += '</div>';
    return h;
  }

  function onScanAction(act) {
    if (!localOps.enabled) return;
    lastScan = { kind: act === 'apply' ? 'apply' : 'scan', m: { running: true } };
    renderScanResult();
    vscode.postMessage({ type: act === 'apply' ? 'candidateScanApply' : 'candidateScan' });
  }

  function renderScanResult() {
    if (!lastScan) return;
    const el = elDetail.querySelector('.scan__result');
    if (!el) return;
    const { kind, m } = lastScan;
    el.hidden = false;
    if (m.running) {
      el.className = 'scan__result';
      el.innerHTML = `<span class="muted">${kind === 'apply' ? 'Applying… (confirm the dialog)' : 'Scanning codebase…'}</span>`;
      return;
    }
    if (m.cancelled) { el.hidden = true; return; }
    if (!m.ok) {
      el.className = 'scan__result scan__result--bad';
      el.innerHTML = `<b>${kind === 'apply' ? 'Apply' : 'Scan'} failed.</b><pre>${esc(m.stderr || m.message || m.stdout || 'unknown error')}</pre>`;
      return;
    }
    el.className = 'scan__result scan__result--ok';
    if (kind === 'apply') {
      let html = '<b>Scan applied.</b> Review the git diff + the refreshed queue, then commit.';
      if (m.diffStat) html += `<pre>${esc(m.diffStat)}</pre>`;
      if (m.stdout) html += `<details><summary>sensei output</summary><pre>${esc(m.stdout)}</pre></details>`;
      el.innerHTML = html;
    } else {
      el.innerHTML = `<b>Scan report (dry-run — nothing written):</b><pre>${esc(m.stdout || '(no output)')}</pre>`
        + (m.stderr ? `<pre class="muted">${esc(m.stderr)}</pre>` : '');
    }
  }

  // Degraded-capability banner: tell the user exactly what's missing and how to
  // proceed manually, rather than failing silently.
  function capabilityBanner() {
    const c = capabilities;
    if (!localOps.enabled) {
      return `<div class="cand__warn"><b>Review only.</b> Approve/reject works, but promotion is off. `
        + `Set <code>sensei.enableLocalOperations: true</code> to promote from here, or use the guarded CLI below.</div>`;
    }
    if (c.hasWorkspace === false) {
      return `<div class="cand__warn"><b>No workspace open.</b> Open the Sensei project folder to run local operations.</div>`;
    }
    if (c.awgAvailable === false) {
      return `<div class="cand__warn"><b><code>sensei</code> not found.</b> The CLI could not be spawned. Install it or set <code>sensei.senseiPath</code>. Until then, use the guarded CLI below.</div>`;
    }
    if (c.isAwgProject === false) {
      return `<div class="cand__warn"><b>Not an Sensei project here.</b> No <code>docs/awareness</code> in this workspace — promotion targets may not resolve.</div>`;
    }
    return `<div class="cand__warn cand__warn--ok"><b>Local operations ready.</b> Approve candidates, then <b>Promote approved</b> to run the guarded promote → rebuild → reload flow. You review the git diff and commit.</div>`;
  }

  // The one explicit batch action: promote all approved candidates.
  function batchBar(approvedCount, total) {
    const canRun = localOps.enabled && capabilities.awgAvailable !== false && approvedCount > 0;
    const dis = canRun ? '' : 'disabled';
    const hint = !localOps.enabled ? ' title="Enable sensei.enableLocalOperations"'
      : approvedCount === 0 ? ' title="Approve at least one candidate first"' : '';
    let h = '<div class="batchbar">';
    h += `<span class="batchbar__count">${approvedCount} approved · ${total} in queue</span>`;
    h += `<button class="batchbar__btn" data-batch ${dis}${hint}>Promote approved (${approvedCount})</button>`;
    h += `<button class="btn-mini" data-oplog title="Open the Awareness Operations log">View log</button>`;
    h += '</div>';
    return h;
  }

  // Summary of the last batch promotion (before/after counts + diff), with
  // expandable detail rather than a wall of text.
  function opSummary(op) {
    if (op.message && !op.promoted) {
      return `<div class="opsum opsum--bad"><b>Promote approved:</b> ${esc(op.message)}</div>`;
    }
    const authorityWarning = op.authority && op.authoritative === false;
    const cls = op.ok ? 'opsum--ok' : 'opsum--bad';
    const n = (op.promoted || []).length;
    let line = op.ok
      ? `Promoted ${n} candidate${n === 1 ? '' : 's'}.`
      : `Promoted ${n}, then stopped at ${esc(op.failedId || '?')}.`;
    if (op.before && op.after) {
      line += ` Rebuilt graph: ${fmt(op.before.triples)} → ${fmt(op.after.triples)} triples`;
      const di = op.after.invariants - op.before.invariants;
      const dt = op.after.tests - op.before.tests;
      const extra = [];
      if (di) extra.push(`${di > 0 ? '+' : ''}${di} invariants`);
      if (dt) extra.push(`${dt > 0 ? '+' : ''}${dt} tests`);
      if (extra.length) line += ` (${extra.join(', ')})`;
      line += '.';
    } else if (op.ok) {
      line += ' Seed rebuilt on disk; restart sensei serve if counts look unchanged.';
    }
    let h = `<div class="opsum ${cls}"><div><b>${op.ok ? '✓' : '✕'} ${line}</b></div>`;
    if (authorityWarning) {
      const label = op.unreachable ? 'Authority backend unreachable' : 'Authority disabled';
      h += `<div class="muted"><b>${label}:</b> ${esc(op.message || op.authority.detail || op.authority.summary || 'served graph not authoritative')}</div>`;
    }
    if (!op.ok && op.error) h += `<pre>${esc(op.error)}</pre>`;
    if (op.diffStat) h += `<details><summary>changed files</summary><pre>${esc(op.diffStat)}</pre></details>`;
    h += `<div class="muted">Full log in the <b>Awareness Operations</b> output channel. Review the git diff and commit.</div>`;
    h += '</div>';
    return h;
  }

  function candidateCard(it) {
    const e = it.e;
    const color = CLASS_COLOR[e.klass] || '#888';
    const decided = e.decision || '';
    let h = `<div class="candx${decided ? ' candx--' + decided : ''}" id="cx-${it.key}" data-cid="${esc(e.id)}">`;
    h += `<div class="candx__head"><span class="candx__title">${esc(e.label || e.id)}</span>`;
    // Review status badge — candidate (default) / approved / rejected.
    const statusLabel = decided === 'approved' ? 'approved' : decided === 'rejected' ? 'rejected' : 'candidate';
    h += `<span class="candx__status candx__status--${statusLabel}">${statusLabel}</span>`;
    if (e.klass) h += `<span class="candx__badge" style="color:${color};border-color:${color}">${esc(e.klass.replace(/_/g, ' '))}</span>`;
    if (e.confidence) h += `<span class="candx__badge">conf: ${esc(e.confidence)}</span>`;
    if (e.review_label) h += `<span class="candx__badge">${esc(e.review_label)}</span>`;
    h += '</div>';
    h += `<code class="candx__id">${esc(e.id)}</code>`;
    if (e.summary) h += `<div class="candx__row"><b>Evidence summary:</b> ${esc(e.summary)}</div>`;
    if (e.evidence) h += `<div class="candx__row"><b>Evidence:</b> ${esc(e.evidence)}</div>`;
    if (e.files && e.files.length) {
      h += '<div class="candx__row"><b>Source anchors:</b> '
        + e.files.map((f) => `<span class="anchor-link" data-file="${esc(f)}">${esc(f)}</span>`).join(', ') + '</div>';
    }
    if (e.target) h += `<div class="candx__row"><b>Proposed target:</b> <code>docs/awareness/${esc(e.target)}</code></div>`;

    h += '<div class="candx__actions">';
    const dis = localOps.enabled ? '' : 'disabled';
    const hint = localOps.enabled ? '' : ' title="Enable sensei.enableLocalOperations to validate via sensei"';
    h += `<button class="candx__act btn-mini" data-act="preview" data-id="${esc(e.id)}" data-label="${esc(e.label || e.id)}" ${dis}${hint}>Preview (dry-run)</button>`;
    if (decided === 'approved') {
      h += `<button class="candx__act candx__act--approved" data-act="undecide" data-id="${esc(e.id)}">✓ Approved (undo)</button>`;
    } else {
      h += `<button class="candx__act candx__act--approve" data-act="approve" data-id="${esc(e.id)}">Approve</button>`;
    }
    if (decided === 'rejected') {
      h += `<button class="candx__act candx__act--rejected" data-act="undecide" data-id="${esc(e.id)}">✕ Rejected (undo)</button>`;
    } else {
      h += `<button class="candx__act" data-act="reject" data-id="${esc(e.id)}">Reject</button>`;
    }
    h += `<button class="candx__act btn-mini" data-act="open" data-id="${esc(e.id)}">Edit</button>`;
    h += '</div>';
    h += `<div class="candx__result" hidden></div>`;
    h += '</div>';
    return h;
  }

  function onCandAction(act, id, label) {
    if (act === 'approve') { vscode.postMessage({ type: 'candidateApprove', id }); return; }
    if (act === 'reject') { vscode.postMessage({ type: 'candidateReject', id }); return; }
    if (act === 'undecide') { vscode.postMessage({ type: 'candidateUndecide', id }); return; }
    if (act === 'open') { vscode.postMessage({ type: 'candidateOpen', id }); return; }
    // preview (dry-run validate) — needs local ops.
    if (!localOps.enabled) return;
    setCandResult(id, '<span class="muted">Running <code>sensei promote --dry-run</code>…</span>', '');
    vscode.postMessage({ type: 'candidatePreview', id, label });
  }

  function candResultEl(id) {
    for (const c of elDetail.querySelectorAll('.candx')) {
      if (c.dataset.cid === id) return c.querySelector('.candx__result');
    }
    return null;
  }
  function setCandResult(id, html, cls) {
    const el = candResultEl(id);
    if (!el) return;
    el.hidden = false;
    el.className = 'candx__result' + (cls ? ' ' + cls : '');
    el.innerHTML = html;
  }

  function renderCandResult(id, kind, m) {
    if (m.cancelled) { const el = candResultEl(id); if (el) el.hidden = true; return; }
    if (kind === 'preview') {
      if (!m.ok) {
        setCandResult(id, `<b>Preview failed.</b><pre>${esc(m.stderr || m.message || 'unknown error')}</pre>`, 'candx__result--bad');
        return;
      }
      setCandResult(id, `<b>Dry-run — would append (no files changed):</b><pre>${esc(m.stdout || '(no output)')}</pre>`
        + (m.stderr ? `<pre class="muted">${esc(m.stderr)}</pre>` : ''), 'candx__result--ok');
      return;
    }
    // promote
    if (!m.ok) {
      if (m.authority || m.reloaded) {
        let html = `<b>${m.unreachable ? 'Promoted, but the authority backend is unreachable.' : 'Promoted, but graph authority is disabled.'}</b>`;
        if (m.before && m.after) {
          const dT = m.after.triples - m.before.triples;
          html += `<div>Rebuilt graph: ${fmt(m.before.triples)} → ${fmt(m.after.triples)} triples`
            + `${dT ? ` (${dT > 0 ? '+' : ''}${dT})` : ''}</div>`;
        }
        if (m.message) html += `<pre>${esc(m.message)}</pre>`;
        if (m.diffStat) html += `<pre>${esc(m.diffStat)}</pre>`;
        html += `<div class="muted">${m.unreachable
          ? 'Review the git diff and do not treat graph-backed answers as authoritative until the backend is reachable again and the dashboard can verify current authority.'
          : 'Review the git diff and do not treat graph-backed answers as authoritative until the dashboard reports current authority.'}</div>`;
        if (m.stdout) html += `<details><summary>sensei output</summary><pre>${esc(m.stdout)}</pre></details>`;
        setCandResult(id, html, 'candx__result--bad');
        vscode.postMessage({ type: 'getCandidates' });
        return;
      }
      setCandResult(id, `<b>Promotion failed.</b><pre>${esc(m.stderr || m.message || m.stdout || 'unknown error')}</pre>`, 'candx__result--bad');
      return;
    }
    let html = `<b>Promoted.</b> Review the git diff and commit when ready.`;
    if (m.before && m.after) {
      const dT = m.after.triples - m.before.triples;
      html += `<div>Rebuilt graph: ${fmt(m.before.triples)} → ${fmt(m.after.triples)} triples`
        + `${dT ? ` (${dT > 0 ? '+' : ''}${dT})` : ''}</div>`;
    }
    if (m.diffStat) html += `<pre>${esc(m.diffStat)}</pre>`;
    html += `<div class="muted">${m.reloaded ? 'Graph metadata reloaded.' : 'Seed rebuilt on disk; restart <code>sensei serve</code> if the served graph looks unchanged.'}</div>`;
    if (m.stdout) html += `<details><summary>sensei output</summary><pre>${esc(m.stdout)}</pre></details>`;
    setCandResult(id, html, 'candx__result--ok');
    // The promoted candidate is gone from the queue — refresh the list.
    vscode.postMessage({ type: 'getCandidates' });
  }

  // ---- review (evidence-based project score + proposals) -----------------
  // Everything here is computed client-side from data the dashboard already
  // holds: Metadata counts + the candidate file list. No new RPC, no mutation,
  // no LLM. The product rule is evidence language only — we report what Sensei can
  // see, never absolute verdicts ("architecture is bad") or fabricated
  // per-file/per-invariant findings the aggregate counts can't support.

  const REVIEW_WEIGHTS = [
    { key: 'coverage', label: 'Graph coverage', weight: 0.15, fn: scoreGraphCoverage },
    { key: 'tests', label: 'Invariant / test evidence', weight: 0.20, fn: scoreInvariantTestEvidence },
    { key: 'freshness', label: 'Drift / freshness', weight: 0.15, fn: scoreFreshness },
    { key: 'spine', label: 'Architecture spine', weight: 0.20, fn: scoreArchitectureSpine },
    { key: 'pattern', label: 'Pattern risk', weight: 0.10, fn: scorePatternRisk },
    { key: 'agent', label: 'Agent readiness', weight: 0.20, fn: scoreAgentReadiness },
  ];

  function num(m, k) { return Number((m && m[k]) || 0); }

  // True when the build has no verifiable provenance (mirrors bannerState).
  function isDevBuild(m) { return m.server_version === '0.0.0-dev' || !m.graph_build_commit; }
  function graphAgeDays(m) {
    const built = num(m, 'graph_build_time_unix');
    return built > 0 ? (Date.now() / 1000 - built) / 86400 : -1;
  }

  function scoreGraphCoverage(m) {
    const dims = [
      'triple_count', 'source_file_count', 'invariant_count', 'intent_count',
      'failure_mode_count', 'required_test_count', 'component_count', 'boundary_count',
      'contract_count', 'decision_count', 'evidence_count', 'meta_principle_count',
      'design_pattern_count', 'implementation_pattern_count', 'pattern_misuse_count',
    ];
    const present = dims.filter((k) => num(m, k) > 0).length;
    const score = Math.round((present / dims.length) * 100);
    return {
      score,
      note: `Sensei sees ${present}/${dims.length} node classes populated across ${fmt(num(m, 'triple_count'))} triples.`,
      strength: score >= 70 ? `The graph spans ${present} of ${dims.length} knowledge classes — broad coverage.` : null,
      risk: score < 45 ? `Only ${present}/${dims.length} knowledge classes are populated; the graph suggests limited coverage.` : null,
    };
  }

  function scoreInvariantTestEvidence(m) {
    const inv = num(m, 'invariant_count'), tests = num(m, 'required_test_count');
    if (inv === 0) {
      return { score: 55, note: 'No invariants authored yet — not enough evidence to judge test linkage.', strength: null, risk: null };
    }
    const ratio = tests / inv;
    let score, strength = null, risk = null;
    if (ratio >= 0.5) { score = 92; strength = `Test evidence looks healthy relative to invariant volume (${tests} tests / ${inv} invariants).`; }
    else if (ratio >= 0.25) { score = 75; }
    else if (ratio >= 0.1) { score = 55; risk = `Test evidence appears thin relative to invariant volume (${tests} tests / ${inv} invariants).`; }
    else { score = 35; risk = `Test evidence appears thin relative to invariant volume (${tests} tests / ${inv} invariants).`; }
    // Aggregate counts only — we deliberately do NOT claim which invariants lack tests.
    return {
      score,
      note: `Sensei metadata reports ${inv} invariants and ${tests} required tests. Counts are aggregate; per-invariant coverage is not asserted here.`,
      strength, risk,
    };
  }

  function scoreFreshness(m) {
    const freshness = m.graph_freshness_state || 'GRAPH_FRESHNESS_STATE_UNSPECIFIED';
    if (freshness === 'GRAPH_FRESHNESS_STATE_CHECK_ERROR') {
      return { score: 10, note: 'Freshness check errored — authority is not proven.', strength: null, risk: m.graph_freshness_detail || 'The daemon could not verify the loaded graph.' };
    }
    if (m.coverage_state === 'COVERAGE_STATE_EMPTY' || num(m, 'triple_count') === 0) {
      return { score: 0, note: 'Graph is empty — no evidence to reason about.', strength: null, risk: 'The graph is empty; Sensei has no evidence to assess.' };
    }
    if (freshness === 'GRAPH_FRESHNESS_STATE_STALE') {
      return { score: 20, note: 'Live graph is stale relative to the verified artifact.', strength: null, risk: m.graph_freshness_detail || 'The daemon reports stale graph authority.' };
    }
    if (freshness === 'GRAPH_FRESHNESS_STATE_UNKNOWN') {
      return { score: 15, note: 'Live graph identity is unknown.', strength: null, risk: m.graph_freshness_detail || 'The daemon cannot prove the loaded graph identity.' };
    }
    if (freshness === 'GRAPH_FRESHNESS_STATE_EMPTY') {
      return { score: 0, note: 'Live graph is empty.', strength: null, risk: 'The live store is empty, so Sensei has no authority to serve.' };
    }
    if (m.build_provenance_state === 'BUILD_PROVENANCE_STATE_DEV' || isDevBuild(m)) {
      return { score: 45, note: 'Dev build — provenance unstamped (no graph build commit).', strength: null, risk: 'Build provenance is unstamped; freshness vs. source cannot be verified.' };
    }
    if (m.build_provenance_state === 'BUILD_PROVENANCE_STATE_INCOMPLETE') {
      return { score: 55, note: 'Build provenance is incomplete.', strength: null, risk: 'One or more provenance fields are missing, so freshness cannot be fully verified.' };
    }
    const age = graphAgeDays(m);
    if (age < 0) {
      return { score: 60, note: 'Build time not stamped; freshness cannot be confirmed.', strength: null, risk: 'Build time is unstamped; freshness cannot be confirmed.' };
    }
    if (age > 30) {
      return { score: 55, note: `Graph was built ${Math.round(age)} days ago — may be stale.`, strength: null, risk: `Graph is ${Math.round(age)} days old; evidence may lag the current source.` };
    }
    return { score: 95, note: `Graph build is stamped and recent (${Math.round(age)}d old).`, strength: 'Graph build is stamped and recent — provenance is verifiable.', risk: null };
  }

  function scoreArchitectureSpine(m) {
    const spine = ['component_count', 'boundary_count', 'contract_count', 'decision_count', 'evidence_count'];
    const present = spine.filter((k) => num(m, k) > 0).length;
    const sf = num(m, 'source_file_count'), inv = num(m, 'invariant_count');
    let score = Math.round((present / spine.length) * 100), strength = null, risk = null;
    if (present >= 4) strength = 'The architectural spine (components, boundaries, contracts, decisions, evidence) is well populated.';
    if (present <= 1 && (sf >= 10 || inv >= 10)) {
      score = Math.min(score, 35);
      risk = 'The graph has many files/invariants but little architectural spine; Sensei suggests adding component/boundary/contract structure.';
    } else if (present <= 2) {
      risk = 'Architectural spine nodes are sparse; the graph suggests room for explicit structure.';
    }
    return {
      score,
      note: `Sensei sees ${fmt(num(m, 'component_count'))} components, ${fmt(num(m, 'boundary_count'))} boundaries, ${fmt(num(m, 'contract_count'))} contracts, ${fmt(num(m, 'decision_count'))} decisions, ${fmt(num(m, 'evidence_count'))} evidence nodes.`,
      strength, risk,
    };
  }

  function scorePatternRisk(m) {
    const misuse = num(m, 'pattern_misuse_count'), dp = num(m, 'design_pattern_count'), ip = num(m, 'implementation_pattern_count');
    if (dp + ip === 0 && misuse === 0) {
      return { score: 65, note: 'No design/implementation patterns or misuses captured yet.', strength: null, risk: null };
    }
    let score = 80, strength = null, risk = null;
    if (dp + ip > 0) { score = 90; strength = `The graph contains reusable design knowledge (${dp} design + ${ip} implementation patterns).`; }
    if (misuse > 0) {
      score -= Math.min(45, misuse * 9);
      risk = `Sensei sees ${misuse} pattern misuse${misuse === 1 ? '' : 's'}; review and convert repeated misuse into a forbidden fix or implementation pattern.`;
    }
    score = Math.max(0, Math.min(100, score));
    return { score, note: `Sensei sees ${dp} design patterns, ${ip} implementation patterns, ${misuse} pattern misuses.`, strength, risk };
  }

  function scoreAgentReadiness(m) {
    const ruleDims = ['intent_count', 'invariant_count', 'failure_mode_count', 'required_test_count', 'forbidden_fix_count'];
    const rules = ruleDims.filter((k) => num(m, k) > 0).length;
    const sf = num(m, 'source_file_count') > 0;
    const spine = ['component_count', 'boundary_count', 'contract_count', 'decision_count', 'evidence_count'].some((k) => num(m, k) > 0);
    const score = Math.round((rules / ruleDims.length) * 70 + (sf ? 10 : 0) + (spine ? 20 : 0));
    return {
      score,
      note: `Sensei sees authored intent/rules in ${rules}/${ruleDims.length} channels${sf ? ', anchored to source files' : ''}${spine ? ', with architectural spine' : ''}.`,
      strength: score >= 75 ? 'The graph carries authored intent, rules, and structure — agents get strong pre-edit guidance.' : null,
      risk: score < 50 ? 'The graph is mostly raw structure with little authored intent/rules; Preflight guidance will be limited.' : null,
    };
  }

  function reviewGrade(score) {
    if (score >= 90) return 'Strong architectural control';
    if (score >= 75) return 'Good, with visible gaps';
    if (score >= 60) return 'Useful but incomplete';
    if (score >= 40) return 'Thin architecture evidence';
    return 'Not enough evidence';
  }

  // Confidence is about how much we can trust the score, not the score itself:
  // it tracks graph freshness and evidence volume, per the product rule.
  function computeConfidence(m) {
    const tc = num(m, 'triple_count');
    if (tc === 0) return 'Low';
    const dev = isDevBuild(m);
    const age = graphAgeDays(m);
    const stale = !dev && age > 30;
    const fresh = !dev && !stale && age >= 0;
    const authored = num(m, 'invariant_count') + num(m, 'intent_count');
    if (fresh && tc >= 500 && authored > 0) return 'High';
    if (dev || stale || tc < 100) return 'Low';
    return 'Medium';
  }

  function reviewSummary(m, overall, confidence) {
    if (num(m, 'triple_count') === 0) {
      return 'The graph is empty — Sensei does not have enough evidence to review this project yet.';
    }
    return `Based on graph metadata and visible structure, Sensei scores this project's architecture evidence at ${overall}/100 `
      + `(${confidence.toLowerCase()} confidence). A low score can mean thin graph evidence, not necessarily weak architecture.`;
  }

  function computeProjectReview(m, candidates) {
    const dimensions = REVIEW_WEIGHTS.map((d) => {
      const r = d.fn(m);
      return { key: d.key, label: d.label, weight: d.weight, score: r.score, note: r.note, strength: r.strength, risk: r.risk };
    });
    const overall = Math.round(dimensions.reduce((s, d) => s + d.score * d.weight, 0));
    const confidence = computeConfidence(m);
    const proposals = buildArchitectureProposals(m, candidates);
    return {
      overall,
      grade: reviewGrade(overall),
      confidence,
      summary: reviewSummary(m, overall, confidence),
      dimensions,
      strengths: dimensions.map((d) => d.strength).filter(Boolean),
      risks: dimensions.map((d) => d.risk).filter(Boolean),
      proposals,
      recommendedActions: proposals.slice(0, 4).map((p) => p.next_step),
    };
  }

  // Client-side proposals, each traceable to a concrete trigger over visible
  // evidence. Severity drives ordering and the card accent.
  function buildArchitectureProposals(m, candidates) {
    const out = [];
    const cand = candidates || [];
    const count = (k) => num(m, k);

    // 1. Missing architectural spine.
    const spineSum = count('component_count') + count('boundary_count') + count('contract_count') + count('decision_count');
    if ((count('source_file_count') >= 10 || count('invariant_count') >= 10) && spineSum < 3) {
      out.push({
        title: 'Add explicit architectural spine for high-value services',
        severity: count('invariant_count') >= 25 ? 'high' : 'warning',
        evidence: `Sensei sees ${fmt(count('source_file_count'))} source files and ${fmt(count('invariant_count'))} invariants but only ${fmt(spineSum)} component/boundary/contract/decision nodes.`,
        why: 'Without an explicit spine, agents see rules but not the structure they protect, so impact analysis stays shallow.',
        next_step: 'Add explicit component/boundary/contract annotations for the highest-value services.',
        confidence: 'medium',
        source: 'Metadata counts only',
      });
    }

    // 2. Increase test evidence for invariants.
    const inv = count('invariant_count'), tests = count('required_test_count');
    if (inv >= 5 && tests < inv * 0.25) {
      out.push({
        title: 'Add required-test evidence for high-value invariants',
        severity: tests < inv * 0.1 ? 'high' : 'warning',
        evidence: `Sensei metadata reports ${inv} invariants and ${tests} required tests.`,
        why: 'Agents can see rules, but weak test linkage makes it harder to verify that a change preserved the rule.',
        next_step: 'Start with critical/high invariants and add aw:requiredTest links or test anchors.',
        confidence: 'medium',
        source: 'Metadata counts only',
      });
    }

    // 3. Resolve pattern misuses.
    const misuse = count('pattern_misuse_count');
    if (misuse > 0) {
      out.push({
        title: 'Resolve recorded pattern misuses',
        severity: misuse > 5 ? 'high' : 'warning',
        evidence: `Sensei sees ${misuse} pattern misuse node${misuse === 1 ? '' : 's'} in the graph.`,
        why: 'Repeated misuse is a signal that a rule or reusable pattern is missing or unclear.',
        next_step: 'Review pattern misuse nodes and convert repeated misuse into either a forbidden fix or an implementation pattern.',
        confidence: 'medium',
        source: 'Metadata counts only',
      });
    }

    // 4. Improve graph freshness / provenance.
    const dev = isDevBuild(m);
    const age = graphAgeDays(m);
    const stale = !dev && age > 30;
    const missingProv = !m.graph_build_commit || !m.source_repo_commit || !num(m, 'graph_build_time_unix');
    if (dev || stale || missingProv) {
      out.push({
        title: 'Stamp graph builds and wire freshness into CI',
        severity: num(m, 'triple_count') === 0 ? 'critical' : (dev || missingProv ? 'high' : 'warning'),
        evidence: dev
          ? 'Sensei reports a dev build with unstamped provenance.'
          : (stale
            ? `Sensei build is ${Math.round(age)} days old.`
            : 'Sensei build is missing one or more provenance fields (build commit, source commit, build time).'),
        why: 'Without stamped provenance, freshness vs. the source tree cannot be verified and reviews lose confidence.',
        next_step: 'Stamp graph builds (build commit, source commit, build time) and wire seed freshness into CI.',
        confidence: 'high',
        source: 'Metadata provenance fields',
      });
    }

    // 5. Promote candidates carefully (dashboard stays read-only).
    if (cand.length > 0) {
      out.push({
        title: 'Review the candidate queue before promoting',
        severity: 'info',
        evidence: `Sensei sees ${cand.length} candidate file${cand.length === 1 ? '' : 's'} under docs/awareness/candidates/.`,
        why: 'Candidates are unverified proposals; promoting without an evidence check can land un-anchored rules.',
        next_step: 'Review the candidate queue; promote only with the guarded CLI after an evidence check (this dashboard stays read-only).',
        confidence: 'high',
        source: 'Local candidate files',
      });
    }

    // 6. Improve agent pre-edit reliability.
    if (count('intent_count') < 5 || count('invariant_count') < 5 || count('source_file_count') < 10) {
      out.push({
        title: 'Improve agent pre-edit reliability',
        severity: 'warning',
        evidence: `Sensei sees ${fmt(count('intent_count'))} intents, ${fmt(count('invariant_count'))} invariants, ${fmt(count('source_file_count'))} source files.`,
        why: 'Preflight is only as useful as the authored intent and file anchors behind it.',
        next_step: 'Add file anchors and human intent so Preflight can give grounded pre-edit guidance.',
        confidence: 'medium',
        source: 'Metadata counts only',
      });
    }

    const rank = { critical: 0, high: 1, warning: 2, info: 3 };
    out.sort((a, b) => (rank[a.severity] ?? 9) - (rank[b.severity] ?? 9));
    return out;
  }

  function listOrEmpty(items, empty) {
    if (!items || !items.length) return `<div class="rev-empty muted">${esc(empty)}</div>`;
    return '<ul class="rev-list">' + items.map((x) => `<li>${esc(x)}</li>`).join('') + '</ul>';
  }

  function renderReview() {
    if (!meta) { elDetail.innerHTML = '<div class="notice">Loading metadata…</div>'; return; }
    const r = computeProjectReview(meta, candidateFiles);
    const sev = (s) => SEV_COLOR[s] || '#888';

    let h = '<div class="review">';

    // Score summary.
    h += `<div class="rev-summary">`
      + `<div class="rev-score"><span class="rev-score__num">${r.overall}</span><span class="rev-score__den">/100</span></div>`
      + `<div class="rev-summary__body">`
      + `<div class="rev-score__cap">Architecture Evidence Score</div>`
      + `<div class="rev-grade">${esc(r.grade)}</div>`
      + `<div class="rev-conf rev-conf--${esc(r.confidence.toLowerCase())}">Confidence: ${esc(r.confidence)}</div>`
      + `<div class="rev-summary__text">${esc(r.summary)}</div>`
      + `</div></div>`;

    // Dimension breakdown.
    h += '<div class="rev-card"><h3>Dimension breakdown</h3><div class="rev-dims">';
    for (const d of r.dimensions) {
      const c = d.score >= 75 ? 'var(--ok)' : d.score >= 50 ? 'var(--high)' : 'var(--crit)';
      h += `<div class="rev-dim">`
        + `<div class="rev-dim__head"><span class="rev-dim__label">${esc(d.label)}</span><span class="rev-dim__score">${d.score}</span></div>`
        + `<div class="rev-bar"><div class="rev-bar__fill" style="width:${d.score}%;background:${c}"></div></div>`
        + `<div class="rev-dim__note">${esc(d.note)}</div>`
        + `</div>`;
    }
    h += '</div></div>';

    // Strengths / Risks.
    h += '<div class="rev-cols">'
      + '<div class="rev-card"><h3>Strengths</h3>' + listOrEmpty(r.strengths, 'Not enough evidence to highlight clear strengths yet.') + '</div>'
      + '<div class="rev-card"><h3>Risks</h3>' + listOrEmpty(r.risks, 'No prominent risks surfaced from the available evidence.') + '</div>'
      + '</div>';

    // Recommended next actions.
    if (r.recommendedActions.length) {
      h += '<div class="rev-card"><h3>Recommended next actions</h3>'
        + '<ul class="rev-list">' + r.recommendedActions.map((a) => `<li>${esc(a)}</li>`).join('') + '</ul></div>';
    }

    // Architecture Enhancement Proposals.
    h += '<div class="rev-card"><h3>Architecture Enhancement Proposals</h3>'
      + '<div class="rev-card__sub">Suggestions traceable to graph evidence — not automatic changes. The dashboard stays read-only.</div>';
    if (!r.proposals.length) {
      h += '<div class="notice">No structural gaps surfaced from current metadata. Sensei has nothing to propose right now.</div>';
    } else {
      for (const p of r.proposals) {
        const col = sev(p.severity);
        h += `<div class="rev-prop" style="border-left-color:${col}">`
          + `<div class="rev-prop__head"><span class="rev-prop__title">${esc(p.title)}</span>`
          + `<span class="rev-prop__sev" style="color:${col};border-color:${col}">${esc(p.severity)}</span></div>`
          + `<div class="rev-prop__row"><b>Evidence:</b> ${esc(p.evidence)}</div>`
          + `<div class="rev-prop__row"><b>Why it matters:</b> ${esc(p.why)}</div>`
          + `<div class="rev-prop__row"><b>Suggested next step:</b> ${esc(p.next_step)}</div>`
          + `<div class="rev-prop__meta"><span>Confidence: ${esc(p.confidence)}</span><span>Source: ${esc(p.source)}</span></div>`
          + `</div>`;
      }
    }
    h += '</div>';

    h += '</div>';
    elDetail.innerHTML = h;
  }

  // ---- errors ------------------------------------------------------------
  function renderError(m) {
    const msg = m.unreachable
      ? `Sensei server not reachable. Start it with <code>sensei serve</code> or set <code>sensei.serverAddr</code>.`
      : esc(m.message);
    const html = `<div class="notice notice--bad">Request failed (${esc(m.context)}): ${msg}</div>`;
    if (m.context === 'getMetadata') {
      $('bannerState').className = 'banner__state banner__state--bad';
      $('bannerState').textContent = m.unreachable ? 'unreachable' : 'error';
    }
    if (m.context === 'listClass' || m.context === 'getCandidates') elList.innerHTML = html;
    else elDetail.innerHTML = html;
  }

  // ---- utils -------------------------------------------------------------
  function mk(tag, attrs, text) {
    const el = document.createElementNS(SVGNS, tag);
    for (const k in attrs) el.setAttribute(k, attrs[k]);
    if (text != null) el.textContent = text;
    return el;
  }
  function token(id) { const i = (id || '').indexOf(':'); return i < 0 ? '' : id.slice(0, i); }
  function bare(id) { const i = (id || '').indexOf(':'); return i < 0 ? (id || '') : decodeURIComponent(id.slice(i + 1)); }
  function fmt(v) { const n = Number(v || 0); return n.toLocaleString(); }
  function esc(s) { return String(s == null ? '' : s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
})();
