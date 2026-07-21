'use strict';
// controlPanel.js — Phase 9.5 Checkpoint 3 read-only architectural control panel.
//
// It RENDERS owner projections verbatim and NEVER classifies: no closure,
// severity, lifecycle, applicability, class membership, capability, or ontology
// grouping is decided here. The left rail comes from the navigation descriptor;
// the top strip + attention queue from the control snapshot; the "all objects"
// list + filters from the artifact index; the selection header from artifact
// state (thin — the deep dimension/evidence inspector is Checkpoint 4). All
// styling is enum-token -> CSS class via controlPanelFmt.js; every badge also
// carries a text label so color is never the sole carrier.
//
// It is self-contained: its own message listener (dashboard.js ignores these
// types) and a shared VS Code API handle. No mutation is ever posted.

(function () {
  // Shared VS Code API — acquireVsCodeApi() may run only once per webview.
  const vscode = window.__vscodeApi || (window.__vscodeApi = acquireVsCodeApi());
  const $ = (id) => document.getElementById(id);
  const esc = (s) =>
    String(s == null ? '' : s).replace(/[&<>"']/g, (c) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])
    );
  // Announce a transient guarded-action result to assistive tech through a
  // persistent live region (proof 21) — no focus jump, no fabricated state.
  // No-op when the region is absent (older host shell).
  const announce = (msg) => { const el = $('cpLive'); if (el) el.textContent = String(msg || ''); };
  // A div[role="button"] row is NOT a native button, so it must honor BOTH Enter
  // and Space (proof 21). Native <button> elements (chips, rail, guarded panel)
  // already do this themselves and need no keydown wiring.
  const activateKey = (e, fn) => {
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') { e.preventDefault(); fn(); }
  };

  // ---- state (all server-sourced; nothing synthesized) --------------------
  let descriptor = null; // OntologyNavigationDescriptor
  let snapshot = null; // ArchitectureControlSnapshot
  let snapshotUnavailable = null; // { reason } when repo context missing
  let index = null; // ArchitectureArtifactIndex (artifact mode)
  let indexUnavailable = null;
  let artifact = null; // { state } | { unavailable, reason } | null
  let mode = 'attention'; // 'attention' | 'artifacts'
  let activeFilter = { chip: '' }; // current filter chip id
  let indexFilters = {}; // { family_filter, class_filter, closure_filter, severity_filter }
  let indexCursor = '';
  let selectedNodeIri = '';
  let showProvenance = false;
  let pendingInspectorFocus = false; // move focus to the inspector heading after a fresh selection
  // Guarded architect-answer action (at most one at a time). Its lifecycle is the
  // pure state machine in controlPanelMutation.js — never optimistic.
  let mutation = null; // { questionId, form, gs }

  // Attention filter chips SELECT owner-provided enum/class values — they never
  // reinterpret them. This is a filter definition, not an ontology/severity/
  // closure/capability table.
  const ATTENTION_CLASS_FILTER = {
    questions: 'architect_question_open',
    contradictions: 'contradiction_present',
    missing_evidence: 'evidence_missing',
  };
  const ATTENTION_SEVERITY_FILTER = {
    critical: 'ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL',
    warnings: 'ARCHITECTURE_ATTENTION_SEVERITY_WARNING',
  };
  // Artifact-index closure filter chips -> the exact server closure enum.
  const CLOSURE_FILTER = {
    open: 'ARCHITECTURE_ARTIFACT_CLOSURE_OPEN',
    degraded: 'ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED',
    unknown: 'ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN',
  };

  // ---- boot ---------------------------------------------------------------
  vscode.postMessage({ type: 'getNavigationDescriptor' });
  requestSnapshot();
  wireModeToggle();

  function requestSnapshot() {
    snapshot = null;
    snapshotUnavailable = null;
    vscode.postMessage({ type: 'getControlSnapshot' });
  }
  function requestIndex() {
    index = null;
    indexUnavailable = null;
    vscode.postMessage({ type: 'listArtifacts', filters: indexFilters, cursor: indexCursor });
  }

  window.addEventListener('message', (ev) => {
    const m = ev.data;
    switch (m && m.type) {
      case 'navigationDescriptor':
        descriptor = m.descriptor || null;
        renderRail();
        break;
      case 'controlSnapshot':
        snapshot = m.snapshot || null;
        snapshotUnavailable = m.unavailable ? { reason: m.reason } : null;
        renderTopStrip();
        renderCenter();
        break;
      case 'artifactIndex':
        index = m.index || null;
        indexUnavailable = m.unavailable ? { reason: m.reason } : null;
        if (mode === 'artifacts') renderCenter();
        break;
      case 'artifactState':
        artifact = m.unavailable ? { unavailable: true, reason: m.reason } : { state: m.state };
        // After a commit, feed the REFRESHED owner state into the guarded machine
        // so the displayed lifecycle comes ONLY from the owner — never the receipt.
        if (mutation && mutation.gs && mutation.gs.phase === 'committed') {
          const owner = m.unavailable
            ? { unavailable: true, reason: m.reason || 'unavailable' }
            : { lifecycle: (m.state && m.state.lifecycle && m.state.lifecycle.state) || null, closure: m.state && m.state.closure };
          mutation.gs = cpGuardedReduce(mutation.gs, { type: 'REFRESH_RESULT', ownerState: owner });
          announce(owner.unavailable
            ? 'Refreshed owner state unavailable — no lifecycle shown.'
            : 'Owner state refreshed. Lifecycle ' + (owner.lifecycle || 'none') + '.');
        }
        renderHeader();
        break;
      case 'dispositionPrepared':
        if (mutation && mutation.questionId === m.questionId) {
          mutation.gs = cpGuardedReduce(mutation.gs, { type: 'PREPARE_RESULT', candidate: m.candidate, refusal: m.refusal });
          announce(m.refusal
            ? 'Prepare refused, nothing written: ' + ((m.refusal && m.refusal.reason_code) || 'refused') + '.'
            : 'Owner candidate ready. Nothing written yet — confirm to commit.');
          renderHeader();
        }
        break;
      case 'dispositionCommitted':
        if (mutation && mutation.questionId === m.questionId) {
          mutation.gs = cpGuardedReduce(mutation.gs, { type: 'COMMIT_RESULT', receipt: m.receipt, refusal: m.refusal });
          announce(m.refusal
            ? 'Refused, nothing written: ' + ((m.refusal && m.refusal.reason_code) || 'refused') + '.'
            : 'Recorded. Outcome ' + ((m.receipt && m.receipt.outcome) || 'unknown') + '. Awaiting owner refresh.');
          renderHeader(); // the follow-up artifactState refresh re-renders with owner truth
        }
        break;
      case 'error':
        // A transport failure is honest state, never fake data.
        if (mutation && mutation.gs && mutation.gs.inFlight) {
          mutation.gs = cpGuardedReduce(mutation.gs, { type: 'COMMIT_RESULT', refusal: { reason_code: m.unreachable ? 'server_unreachable' : 'transport_error', owner: 'client', mutation_applied: false } });
          announce('Transport error — nothing was written.');
          renderHeader();
        } else if (mode === 'artifacts' && (!index || indexUnavailable)) {
          indexUnavailable = { reason: m.unreachable ? 'server_unreachable' : 'error', message: m.message };
          renderCenter();
        }
        break;
    }
  });

  // ---- mode toggle (control panel <-> legacy explorer) --------------------
  function wireModeToggle() {
    const cp = $('cpModeControl');
    const lg = $('cpModeLegacy');
    if (!cp || !lg) return;
    cp.onclick = () => setView('control');
    lg.onclick = () => setView('legacy');
    setView('control');
  }
  function setView(which) {
    const panel = $('controlPanel');
    const legacy = $('legacyView');
    const onControl = which === 'control';
    if (panel) panel.hidden = !onControl;
    if (legacy) legacy.hidden = onControl;
    const cp = $('cpModeControl');
    const lg = $('cpModeLegacy');
    if (cp) cp.classList.toggle('cp-modebtn--on', onControl);
    if (lg) lg.classList.toggle('cp-modebtn--on', !onControl);
    if (cp) cp.setAttribute('aria-pressed', String(onControl));
    if (lg) lg.setAttribute('aria-pressed', String(!onControl));
  }

  // ---- badges: shared pure builder (enum token -> css class + text label) --
  const badge = cpBadge;

  // ---- top strip ----------------------------------------------------------
  function renderTopStrip() {
    const host = $('cpTopStrip');
    if (!host) return;
    if (snapshotUnavailable) {
      host.innerHTML =
        `<div class="cp-strip-unavailable" role="status">Repository context unavailable` +
        ` — <span class="cp-reason">${esc(snapshotUnavailable.reason || '')}</span>.` +
        ` No architectural state is claimed.</div>`;
      return;
    }
    if (!snapshot) {
      host.innerHTML = `<div class="cp-strip-loading" role="status">Loading control snapshot…</div>`;
      return;
    }
    const meta = snapshot.meta || {};
    const avail = meta.availability;
    const ga = snapshot.graph_authority || {};
    const cells = [];

    // repository / domain
    cells.push(cell('Repository', esc(meta.repository_identity || '—')));
    cells.push(cell('Domain', esc(meta.requested_domain || '(all)')));

    // graph authority — three independent booleans, NEVER a single healthy badge.
    if (ga.observed) {
      const parts =
        `<span class="cp-auth cp-auth-observed">observed</span>` +
        tri('current', ga.current) +
        tri('integrity', ga.integrity) +
        `<span class="cp-auth-id" title="${esc(ga.identity || '')}">${esc(shortId(ga.identity))}</span>`;
      cells.push(cell('Graph authority', parts));
    } else {
      cells.push(cell('Graph authority', `<span class="cp-auth cp-auth--unobserved">not observed</span>`));
    }

    // projection availability — explicit, never collapsed to healthy.
    cells.push(cell('Projection', badge('avail', avail)));

    // counts (unknown != zero)
    cells.push(cell('Critical', severityCountText('critical', avail)));
    cells.push(cell('Warnings', severityCountText('warning', avail)));
    cells.push(cell('Open questions', cpCountText(snapshot.open_question_count)));

    // active task / completion — null (unobserved) reads unavailable, never OK.
    cells.push(cell('Active task', taskText(snapshot.active_task)));
    cells.push(cell('Completion', completionText(snapshot.completion)));

    let html = `<div class="cp-strip-cells">${cells.join('')}</div>`;

    // grounding summary — honest coverage ratios from already-owned catalog tallies only
    // (no denominator → no percentage; never suppresses attention).
    html += CP_FMT.cpGroundingSummary(snapshot);

    // availability degradation banner (partial / unavailable / invalid)
    if (avail && avail !== 'ARCHITECTURE_AVAILABILITY_AVAILABLE') {
      const lims = (meta.limitations || []).map((l) => `<li>${esc(l)}</li>`).join('');
      const degraded = (meta.sources || [])
        .filter((s) => s.availability && s.availability !== 'ARCHITECTURE_SOURCE_AVAILABILITY_AVAILABLE')
        .map((s) => `<li><code>${esc(s.owner)}</code> ${badge('savail', s.availability)} <span class="cp-reason">${esc(s.reason_code || '')}</span></li>`)
        .join('');
      html +=
        `<div class="cp-strip-degraded" role="status">` +
        `<strong>${esc(cpEnumLabel(avail))}</strong> — some sources were not fully observed.` +
        (lims ? `<ul class="cp-limits">${lims}</ul>` : '') +
        (degraded ? `<ul class="cp-sources">${degraded}</ul>` : '') +
        `</div>`;
    }

    // provenance / digests behind an expander (no raw digests unless expanded)
    html +=
      `<button class="cp-prov-toggle" aria-expanded="${showProvenance}">` +
      `${showProvenance ? 'Hide' : 'Show'} provenance &amp; digests</button>`;
    if (showProvenance) {
      const rows = [
        ['schema', meta.schema_version],
        ['producer', (meta.producer_name || '') + ' ' + (meta.producer_version || '')],
        ['projection digest', meta.digest_sha256],
        ['registry digest', snapshot.registry_digest],
        ['non-authoritative', String(meta.non_authoritative_projection === true)],
      ]
        .map(([k, v]) => `<div><span class="cp-prov-k">${esc(k)}</span><code>${esc(v || '—')}</code></div>`)
        .join('');
      html += `<div class="cp-prov">${rows}</div>`;
    }
    host.innerHTML = html;
    const tog = host.querySelector('.cp-prov-toggle');
    if (tog) tog.onclick = () => { showProvenance = !showProvenance; renderTopStrip(); };
  }

  function cell(label, valueHtml) {
    return `<div class="cp-cell"><div class="cp-cell-k">${esc(label)}</div><div class="cp-cell-v">${valueHtml}</div></div>`;
  }
  function tri(label, v) {
    const state = v ? 'yes' : 'no';
    return `<span class="cp-tri cp-tri--${state}">${esc(label)} ${v ? '✓' : '✗'}</span>`;
  }
  function shortId(id) {
    if (!id) return '(no identity)';
    return id.length > 14 ? id.slice(0, 12) + '…' : id;
  }
  // A count is a real 0 only when the projection was observed; otherwise unknown.
  function severityCountText(sevKey, avail) {
    const observed =
      avail === 'ARCHITECTURE_AVAILABILITY_AVAILABLE' || avail === 'ARCHITECTURE_AVAILABILITY_PARTIAL';
    return cpSeverityCount(snapshot && snapshot.attention_counts_by_severity, sevKey, observed);
  }
  function taskText(t) {
    if (t == null) return 'Unavailable';
    if (!t.task_id) return 'No active task';
    const bits = [esc(t.task_id)];
    if (t.closure) bits.push('closure ' + esc(t.closure));
    if (t.admission) bits.push('admission ' + esc(t.admission));
    return bits.join(' · ');
  }
  function completionText(c) {
    if (c == null) return 'Unavailable';
    const st = c.terminal_state ? esc(c.terminal_state) : 'none';
    // Completion is a workflow terminal state, NOT correctness (proof 4). Phase 6
    // alone certifies; the word "authoritative" qualifies completion, never truth.
    return st + (c.authoritative_completion ? ' (authoritative completion — not correctness)' : '');
  }

  // ---- left rail (descriptor-driven only) ---------------------------------
  function renderRail() {
    const host = $('cpRail');
    if (!host) return;
    if (!descriptor) {
      host.innerHTML = `<div class="cp-rail-loading">Loading navigation…</div>`;
      return;
    }
    // Server order preserved exactly — no client sort.
    const families = descriptor.families || [];
    let html = '';
    for (const fam of families) {
      html += `<div class="cp-fam"><div class="cp-fam-h">${esc(fam.label || fam.id)}</div>`;
      for (const c of fam.classes || []) {
        if (c.default_visible === false) continue;
        const active = indexFilters.class_filter === c.class_iri ? ' cp-cls--on' : '';
        html +=
          `<button class="cp-cls${active}" role="button" tabindex="0"` +
          ` data-class="${esc(c.class_iri)}" data-family="${esc(fam.id)}"` +
          ` title="${esc(c.class_iri)}">` +
          `<span class="cp-cls-label">${esc(c.label || c.class_iri)}</span>` +
          (c.assessable_artifact ? `<span class="cp-cls-tag">assessable</span>` : '') +
          `</button>`;
      }
      html += `</div>`;
    }
    host.innerHTML = html;
    host.querySelectorAll('.cp-cls').forEach((b) => {
      const activate = () => selectClass(b.dataset.class, b.dataset.family);
      b.addEventListener('click', activate);
      b.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); activate(); }
      });
    });
  }

  function selectClass(classIri, family) {
    mode = 'artifacts';
    activeFilter = { chip: '' };
    indexFilters = { class_filter: classIri, family_filter: family };
    indexCursor = '';
    renderRail();
    renderChips();
    requestIndex();
    renderCenter();
  }

  // ---- filter chips -------------------------------------------------------
  const ATTENTION_CHIPS = [
    { id: 'critical', label: 'Critical' },
    { id: 'warnings', label: 'Warnings' },
    { id: 'questions', label: 'Questions' },
    { id: 'contradictions', label: 'Contradictions' },
    { id: 'missing_evidence', label: 'Missing evidence' },
  ];
  const ARTIFACT_CHIPS = [
    { id: 'open', label: 'Open' },
    { id: 'degraded', label: 'Degraded' },
    { id: 'unknown', label: 'Unknown' },
    { id: 'all', label: 'All artifacts' },
  ];

  function renderChips() {
    const host = $('cpChips');
    if (!host) return;
    const chip = (c, group) => {
      const on = activeFilter.chip === c.id ? ' cp-chip--on' : '';
      return `<button class="cp-chip cp-chip--${group}${on}" data-chip="${c.id}" data-group="${group}" aria-pressed="${activeFilter.chip === c.id}">${esc(c.label)}</button>`;
    };
    host.innerHTML =
      `<span class="cp-chip-group-label">Attention</span>` +
      ATTENTION_CHIPS.map((c) => chip(c, 'attn')).join('') +
      `<span class="cp-chip-sep"></span><span class="cp-chip-group-label">Artifacts</span>` +
      ARTIFACT_CHIPS.map((c) => chip(c, 'art')).join('');
    host.querySelectorAll('.cp-chip').forEach((b) => {
      b.onclick = () => onChip(b.dataset.chip, b.dataset.group);
    });
  }

  function onChip(id, group) {
    if (activeFilter.chip === id) {
      // toggle off -> attention-queue default
      activeFilter = { chip: '' };
      mode = 'attention';
      renderChips();
      renderCenter();
      return;
    }
    activeFilter = { chip: id };
    if (group === 'art') {
      mode = 'artifacts';
      indexCursor = '';
      indexFilters = id === 'all' ? {} : { closure_filter: CLOSURE_FILTER[id] };
      requestIndex();
    } else {
      mode = 'attention'; // client-select over the observed queue
    }
    renderChips();
    renderCenter();
  }

  // ---- center: attention queue (default) or artifact index ----------------
  function renderCenter() {
    const host = $('cpList');
    const pager = $('cpPager');
    if (!host) return;
    if (pager) pager.innerHTML = '';
    if (mode === 'attention') return renderAttentionQueue(host);
    return renderArtifactIndex(host, pager);
  }

  function renderAttentionQueue(host) {
    if (snapshotUnavailable) {
      host.innerHTML = `<div class="cp-empty">No attention queue — repository context unavailable.</div>`;
      return;
    }
    if (!snapshot) {
      host.innerHTML = `<div class="cp-empty">Loading…</div>`;
      return;
    }
    let items = snapshot.top_attention || [];
    // Client-SELECT by owner-provided enum/class value (never reinterpret).
    const f = activeFilter.chip;
    if (ATTENTION_SEVERITY_FILTER[f]) {
      items = items.filter((it) => it.severity === ATTENTION_SEVERITY_FILTER[f]);
    } else if (ATTENTION_CLASS_FILTER[f]) {
      items = items.filter((it) => it.attention_class === ATTENTION_CLASS_FILTER[f]);
    }
    if (!items.length) {
      host.innerHTML = `<div class="cp-empty">No attention items in the observed bounded queue${f ? ' for this filter' : ''}.</div>`;
      return;
    }
    // Server order preserved — no client severity sort.
    host.innerHTML = items.map(attnRow).join('');
    host.querySelectorAll('.cp-row').forEach((r) => {
      const iri = r.dataset.iri;
      if (!iri) return;
      r.addEventListener('click', () => selectArtifact(iri));
      r.addEventListener('keydown', (e) => activateKey(e, () => selectArtifact(iri)));
    });
  }

  function attnRow(it) {
    const affected = it.affected_artifacts || [];
    const iri = affected.length === 1 ? affected[0] : '';
    return (
      `<div class="cp-row cp-row--attn" ${iri ? `data-iri="${esc(iri)}" tabindex="0" role="button"` : ''}>` +
      badge('sev', it.severity) +
      `<span class="cp-row-class">${esc((it.attention_class || '').replace(/_/g, ' '))}</span>` +
      `<span class="cp-row-reason">${esc(it.reason_code || '')}</span>` +
      (it.blocking ? `<span class="cp-tag cp-tag--blocking">blocking</span>` : '') +
      (it.architect_input_required ? `<span class="cp-tag cp-tag--architect">architect input</span>` : '') +
      `<span class="cp-row-owner">${esc(it.next_action_owner || '')}</span>` +
      `</div>`
    );
  }

  function renderArtifactIndex(host, pager) {
    if (indexUnavailable) {
      const r = indexUnavailable.reason || 'unavailable';
      host.innerHTML = `<div class="cp-empty">Artifacts unavailable — <span class="cp-reason">${esc(r)}</span>.</div>`;
      return;
    }
    if (!index) {
      host.innerHTML = `<div class="cp-empty">Loading artifacts…</div>`;
      return;
    }
    const rows = index.page || [];
    if (!rows.length) {
      host.innerHTML = `<div class="cp-empty">No artifacts for this scope/filter (observed).</div>`;
    } else {
      host.innerHTML = rows.map(artifactRow).join('');
      host.querySelectorAll('.cp-row').forEach((r) => {
        const iri = r.dataset.iri;
        r.addEventListener('click', () => selectArtifact(iri));
        r.addEventListener('keydown', (e) => activateKey(e, () => selectArtifact(iri)));
      });
    }
    if (pager) {
      let p = '';
      if (index.truncated) p += `<span class="cp-trunc">Results truncated by the owner.</span>`;
      if (index.next_cursor) p += `<button id="cpMore" class="cp-more">Show more</button>`;
      pager.innerHTML = p;
      const more = $('cpMore');
      if (more) more.onclick = () => { indexCursor = index.next_cursor; requestIndex(); };
    }
  }

  // Row: label · class · closure token · open-dimension count · highest
  // severity · optional owner. No local synthesis from missing fields.
  function artifactRow(a) {
    const iri = (a.identity && a.identity.node_iri) || '';
    const sev = a.highest_severity ? badge('sev', a.highest_severity) : `<span class="cp-nosev">—</span>`;
    return (
      `<div class="cp-row cp-row--art" data-iri="${esc(iri)}" tabindex="0" role="button">` +
      `<span class="cp-row-label">${esc(a.label || bare(iri))}</span>` +
      `<span class="cp-row-cls">${esc(a.class || '')}</span>` +
      badge('closure', a.closure) +
      `<span class="cp-row-dims" title="open required dimensions">${esc(String(Number(a.open_required_dimensions || 0)))}</span>` +
      sev +
      (a.owner_summary ? `<span class="cp-row-owner">${esc(a.owner_summary)}</span>` : '') +
      `</div>`
    );
  }
  function bare(iri) {
    if (!iri) return '(no identity)';
    const h = iri.split('#').pop();
    return h || iri;
  }

  // ---- selection header (thin — deep inspector is CP4) --------------------
  function selectArtifact(iri) {
    selectedNodeIri = iri;
    artifact = null;
    pendingInspectorFocus = true; // land the keyboard on the inspector once content arrives
    renderHeader();
    vscode.postMessage({ type: 'getArtifactState', nodeIri: iri });
  }

  // Move focus to the inspector heading exactly once, after a fresh selection's
  // real content (full or unavailable) first renders — never during the loading
  // placeholder and never on a guarded-panel re-render (which must not steal focus).
  function focusInspectorIfPending(host) {
    if (!pendingInspectorFocus) return;
    pendingInspectorFocus = false;
    const h = host.querySelector('.cp-header-id');
    if (h && h.focus) h.focus();
  }

  function renderHeader() {
    const host = $('cpHeader');
    if (!host) return;
    if (!selectedNodeIri) {
      host.innerHTML = `<div class="cp-header-empty">Select an artifact to see its owner-derived header.</div>`;
      return;
    }
    if (!artifact) {
      host.innerHTML = `<div class="cp-header-empty">Loading ${esc(bare(selectedNodeIri))}…</div>`;
      return;
    }
    if (artifact.unavailable) {
      host.innerHTML =
        `<div class="cp-header"><div class="cp-header-id" tabindex="-1">${esc(bare(selectedNodeIri))}</div>` +
        `<div class="cp-header-unavailable">Artifact state unavailable — <span class="cp-reason">${esc(artifact.reason || '')}</span>.</div></div>`;
      focusInspectorIfPending(host);
      return;
    }
    // Full read-only inspector (Checkpoint 4), in the design §16 order. Every
    // section is owner-sourced or explicitly unavailable; nothing is inferred.
    const st = artifact.state || {};
    const id = st.identity || {};
    const lc = st.lifecycle || {};
    const meta = st.meta || {};
    const dims = Array.isArray(st.dimensions) ? st.dimensions : [];
    const applicable = dims.filter((d) => d && d.applicable !== false);
    const notApplicable = dims.length - applicable.length;

    // Open dimensions — applicable only; a non-applicable dimension is never open.
    let dimHtml = applicable.map(cpDimensionRow).join('');
    if (!applicable.length) dimHtml = `<span class="cp-none">no applicable dimensions observed</span>`;
    if (notApplicable > 0) dimHtml += `<div class="cp-dim-na">${notApplicable} dimension(s) not applicable to this class</div>`;

    // Warnings & blockers — the owner's attention items + verbatim dimension blockers.
    const attnHtml = Array.isArray(st.attention) && st.attention.length
      ? st.attention.map(attnRow).join('')
      : `<span class="cp-none">none observed</span>`;

    host.innerHTML =
      `<div class="cp-insp">` +
      `<div class="cp-header-id" tabindex="-1" title="${esc(id.node_iri || '')}">${esc(bare(id.node_iri || selectedNodeIri))}</div>` +
      section('Identity',
        row('Canonical class', esc(st.canonical_class || id.canonical_class || '—')) +
        row('Observed classes', esc((id.observed_classes || []).join(', ') || '—')) +
        row('Repository', esc(id.repository_identity || '—')) +
        row('Domain', esc(id.domain_identity || '(all)'))) +
      section('Authority',
        row('Bound authority', `<code>${esc(id.graph_authority_identity || '—')}</code>`)) +
      section('Lifecycle',
        row('State', lc.applicable === false ? 'not applicable' : badge('lifecycle', lc.state)) +
        row('Source', esc(lc.source_owner || '—') + (lc.source_availability ? ' ' : '') +
          (lc.source_availability ? badge('savail', lc.source_availability) : '')) +
        (lc.reason_code ? row('Reason', `<span class="cp-reason">${esc(lc.reason_code)}</span>`) : '')) +
      section('Artifact closure',
        row('Closure', badge('closure', st.closure) + (st.closure_reason ? ` <span class="cp-reason">${esc(st.closure_reason)}</span>` : '')) +
        row('Assessment coverage', badge('coverage', st.assessment_coverage)) +
        row('Availability', badge('avail', meta.availability))) +
      section('Open dimensions', dimHtml) +
      section('Warnings &amp; blockers', attnHtml) +
      section('Architect questions', questionLinkage(st.questions, dims)) +
      section('Next permitted action',
        `<span class="cp-nextaction">${esc(st.next_action_owner || 'none')}</span>` +
        `<div class="cp-readonly-note">The next action owner is who acts next; guarded answer/accept actions live in Architect questions.</div>`) +
      section('Evidence &amp; tests', cpIdList('Evidence', st.evidence)) +
      section('Promoted-feedback provenance', cpFeedbackProvenance(st.feedback)) +
      section('Relationships', cpUnavailableSection('Relationships', 'no owner-projected relationship data')) +
      section('Focus graph', cpUnavailableSection('Focus graph', 'no owner-projected graph data')) +
      `</div>`;
    wireGuardedPanel();
    focusInspectorIfPending(host);
  }
  function row(k, v) {
    return `<div class="cp-h-row"><span class="cp-h-k">${esc(k)}</span><span class="cp-h-v">${v}</span></div>`;
  }
  function section(title, inner) {
    return `<section class="cp-insp-sec"><h3 class="cp-insp-h">${title}</h3><div class="cp-insp-b">${inner}</div></section>`;
  }
  // Questions are display-only ids, each annotated with the dimension(s) that
  // reference it (a lookup over dimension.questions — never state inference).
  function questionLinkage(questions, dims) {
    const qs = Array.isArray(questions) ? questions : [];
    if (!qs.length) return `<span class="cp-none">none observed</span>`;
    const list = qs.map((q) => {
      const inDims = (Array.isArray(dims) ? dims : [])
        .filter((d) => d && Array.isArray(d.questions) && d.questions.indexOf(q) !== -1)
        .map((d) => d.label || d.dimension)
        .filter(Boolean);
      const link = inDims.length ? `<span class="cp-qdim">${esc(inDims.join(', '))}</span>` : '';
      const active = mutation && mutation.questionId === q ? ' cp-qanswer--on' : '';
      return `<li><code>${esc(q)}</code> ${link} <button class="cp-qanswer${active}" data-q="${esc(q)}" type="button">Answer…</button></li>`;
    }).join('');
    return `<ul class="cp-idlist">${list}</ul>` + guardedActionPanel();
  }

  // The guarded architect-answer panel: choose → prepare → confirm → commit once
  // → receipt/refusal → the owner refresh drives the display (never optimistic).
  function guardedActionPanel() {
    if (!mutation) {
      return `<div class="cp-readonly-note">Choose "Answer…" on a question to record a guarded disposition.</div>`;
    }
    const gs = mutation.gs || {};
    const f = mutation.form || {};
    let body = '';
    if (gs.phase === 'idle') {
      body =
        `<div class="cp-mut-form">` +
        `<label>Disposition <select id="cpMutDisp">` +
        opt('ARCHITECTURE_DISPOSITION_ANSWERED', 'answered', f.disposition) +
        opt('ARCHITECTURE_DISPOSITION_DISMISSED', 'dismissed', f.disposition) +
        opt('ARCHITECTURE_DISPOSITION_DEFERRED', 'deferred', f.disposition) +
        `</select></label>` +
        `<label>Reusability <select id="cpMutReuse">` +
        opt('ARCHITECTURE_REUSABILITY_REUSABLE_CANDIDATE', 'reusable candidate', f.reusability) +
        opt('ARCHITECTURE_REUSABILITY_NONE', 'none', f.reusability) +
        opt('ARCHITECTURE_REUSABILITY_TASK_LOCAL', 'task-local', f.reusability) +
        `</select></label>` +
        `<label>Answer id <input id="cpMutAnswerId" type="text" value="${esc(f.answerId || '')}"/></label>` +
        `<label>Answer <textarea id="cpMutAnswer" rows="3">${esc(f.answerText || '')}</textarea></label>` +
        `<label>Rationale <input id="cpMutRationale" type="text" value="${esc(f.rationale || '')}"/></label>` +
        `<div class="cp-mut-actions"><button id="cpMutPrepare" type="button">Prepare</button>` +
        `<button id="cpMutCancel" type="button">Cancel</button></div>` +
        `<div class="cp-readonly-note">The raw answer is hashed by the owner and never becomes governed prose.</div>` +
        `</div>`;
    } else if (gs.phase === 'preparing') {
      body = `<div class="cp-mut-status">Preparing… (no state is written)</div>`;
    } else if (gs.phase === 'prepared') {
      const c = gs.candidate || {};
      body =
        `<div class="cp-mut-candidate"><strong>Owner candidate (nothing written yet)</strong>` +
        row('Expected ledger head', `<code>${esc(c.expected_ledger_head_digest_sha256 || '—')}</code>`) +
        row('Receipt digest', `<code>${esc(c.receipt_digest_sha256 || '—')}</code>`) +
        `<div class="cp-mut-actions"><button id="cpMutCommit" type="button">Confirm &amp; commit once</button>` +
        `<button id="cpMutCancel" type="button">Cancel</button></div></div>`;
    } else if (gs.phase === 'committing') {
      body = `<div class="cp-mut-status">Committing… <span class="cp-reason">(commit in flight — duplicate submission disabled)</span></div>`;
    } else if (gs.phase === 'committed' || gs.phase === 'refreshed') {
      const r = gs.receipt || {};
      const disp = cpDisplayedLifecycle(gs);
      let ownerLine = `<div class="cp-reason">awaiting owner refresh…</div>`;
      if (disp) {
        ownerLine = disp.unavailable
          ? `<div class="cp-mut-owner">Refreshed owner state: <span class="cp-reason">unavailable (${esc(disp.reason)})</span> — no optimistic lifecycle shown.</div>`
          : `<div class="cp-mut-owner">Refreshed owner lifecycle: ${disp.lifecycle ? badge('lifecycle', disp.lifecycle) : '<span class="cp-none">none</span>'}</div>`;
      }
      body =
        `<div class="cp-mut-receipt"><strong>Receipt</strong>` +
        row('Outcome', esc(r.outcome || '')) +
        row('Mutation applied', cpReceiptApplied(gs) ? 'yes' : 'no (server replay authority)') +
        row('Receipt digest', `<code>${esc(r.receipt_digest_sha256 || '—')}</code>`) +
        ownerLine +
        `<div class="cp-readonly-note">Governed promotion is deferred in this build — an accepted answer stays a reusable candidate; nothing is promoted here.</div>` +
        `<div class="cp-mut-actions"><button id="cpMutDone" type="button">Done</button></div></div>`;
    } else if (gs.phase === 'refused') {
      const ref = gs.refusal || {};
      body =
        `<div class="cp-mut-refusal"><strong>Refused — nothing was written</strong>` +
        row('Reason', `<span class="cp-reason">${esc(ref.reason_code || '')}</span>`) +
        row('Owner', esc(ref.owner || '')) +
        row('Mutation applied', 'no') +
        `<div class="cp-mut-actions"><button id="cpMutBack" type="button">Back</button>` +
        `<button id="cpMutCancel" type="button">Cancel</button></div></div>`;
    }
    return `<div class="cp-mut" data-q="${esc(mutation.questionId)}">${body}</div>`;
  }
  function opt(value, label, cur) {
    return `<option value="${esc(value)}"${cur === value ? ' selected' : ''}>${esc(label)}</option>`;
  }

  function readMutForm() {
    const g = (id) => { const el = $(id); return el ? el.value : ''; };
    mutation.form = {
      disposition: g('cpMutDisp') || 'ARCHITECTURE_DISPOSITION_ANSWERED',
      reusability: g('cpMutReuse') || 'ARCHITECTURE_REUSABILITY_NONE',
      answerId: g('cpMutAnswerId'),
      answerText: g('cpMutAnswer'),
      rationale: g('cpMutRationale'),
    };
  }

  function wireGuardedPanel() {
    // Per-question "Answer…" starts a guarded action (resets any prior one).
    const host = $('cpHeader');
    if (!host) return;
    host.querySelectorAll('.cp-qanswer').forEach((b) => {
      b.onclick = () => {
        mutation = { questionId: b.dataset.q, form: {}, gs: cpGuardedInitial() };
        renderHeader();
      };
    });
    if (!mutation) return;
    const on = (id, fn) => { const el = $(id); if (el) el.onclick = fn; };
    on('cpMutCancel', () => { mutation = null; renderHeader(); });
    on('cpMutBack', () => { mutation.gs = cpGuardedInitial(); renderHeader(); });
    on('cpMutDone', () => { mutation = null; renderHeader(); });
    on('cpMutPrepare', () => {
      readMutForm();
      mutation.gs = cpGuardedReduce(mutation.gs, { type: 'PREPARE_START' });
      renderHeader();
      vscode.postMessage(Object.assign({ type: 'prepareDisposition', questionId: mutation.questionId }, mutation.form));
    });
    on('cpMutCommit', () => {
      // Explicit confirmation, then exactly one commit; the button is removed on
      // re-render (committing phase) so a second click cannot fire.
      mutation.gs = cpGuardedReduce(mutation.gs, { type: 'CONFIRM' });
      mutation.gs = cpGuardedReduce(mutation.gs, { type: 'COMMIT_START' });
      if (!mutation.gs.inFlight) { renderHeader(); return; }
      const cand = mutation.gs.candidate || {};
      renderHeader();
      vscode.postMessage(Object.assign({
        type: 'commitDisposition', questionId: mutation.questionId, nodeIri: selectedNodeIri,
        expectedHead: cand.expected_ledger_head_digest_sha256 || '',
      }, mutation.form));
    });
  }

  // initial paints
  renderRail();
  renderChips();
  renderTopStrip();
  renderCenter();
  renderHeader();
})();
