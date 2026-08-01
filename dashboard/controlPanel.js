'use strict';
// controlPanel.js — static, read-only fork of
// editor/vscode/media/controlPanel.js for the GitHub Pages architecture
// dashboard (dashboard/). Renders the same owner projections verbatim: no
// closure, severity, lifecycle, applicability, class membership, capability,
// or ontology grouping is decided here — see controlPanelFmt.js.
//
// Differences from the VS Code original:
//   - Data comes from a prebuilt JSON snapshot (fetched once at load), not a
//     live gRPC connection through a VS Code webview message bridge. The
//     snapshot's ListArchitectureArtifacts pages are already fully crawled at
//     build time, so pagination/filtering here is a client-side slice over
//     the already-loaded array, never a new network request.
//   - There is no mutation UI. The one guarded architect-answer action from
//     the VS Code panel has no backend on a static site and is not included
//     here at all — not disabled, just absent.
(function () {
  const $ = (id) => document.getElementById(id);
  const esc = (s) =>
    String(s == null ? '' : s).replace(/[&<>"']/g, (c) =>
      ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])
    );
  const activateKey = (e, fn) => {
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') { e.preventDefault(); fn(); }
  };

  const PAGE_STEP = 100;

  // ---- state (all snapshot-sourced; nothing synthesized) -------------------
  let descriptor = null; // OntologyNavigationDescriptor
  let snapshot = null; // ArchitectureControlSnapshot
  let snapshotUnavailable = null; // { reason } when the snapshot itself couldn't be loaded
  let index = null; // ArchitectureArtifactIndex-shaped object, fully crawled at build time
  let indexUnavailable = null;
  let attentionStates = {}; // node_iri -> ArchitectureArtifactState, bounded to top_attention's affected artifacts
  let artifact = null; // { state } | { unavailable, reason } | null
  let mode = 'attention'; // 'attention' | 'artifacts'
  let activeFilter = { chip: '' };
  let indexFilters = {}; // { class_filter, closure_filter } — applied client-side over the loaded index
  let visibleCount = PAGE_STEP;
  let selectedNodeIri = '';
  let showProvenance = false;
  let pendingInspectorFocus = false;

  const ATTENTION_CLASS_FILTER = {
    questions: 'architect_question_open',
    contradictions: 'contradiction_present',
    missing_evidence: 'evidence_missing',
  };
  const ATTENTION_SEVERITY_FILTER = {
    critical: 'ARCHITECTURE_ATTENTION_SEVERITY_CRITICAL',
    warnings: 'ARCHITECTURE_ATTENTION_SEVERITY_WARNING',
  };
  const CLOSURE_FILTER = {
    open: 'ARCHITECTURE_ARTIFACT_CLOSURE_OPEN',
    degraded: 'ARCHITECTURE_ARTIFACT_CLOSURE_DEGRADED',
    unknown: 'ARCHITECTURE_ARTIFACT_CLOSURE_UNKNOWN',
  };

  // ---- boot: fetch the prebuilt snapshot once -------------------------------
  loadSnapshot();

  function fetchJSON(path) {
    return fetch(path).then((r) => {
      if (!r.ok) throw new Error(path + ': HTTP ' + r.status);
      return r.json();
    });
  }

  async function loadSnapshot() {
    try {
      const [nav, ctrl, idx, states] = await Promise.all([
        fetchJSON('./snapshot/navigation.json'),
        fetchJSON('./snapshot/control_snapshot.json'),
        fetchJSON('./snapshot/artifacts.json'),
        fetchJSON('./snapshot/attention_states.json'),
      ]);
      descriptor = nav;
      snapshot = ctrl;
      index = idx;
      attentionStates = states || {};
      fetchJSON('./snapshot/meta.json').then(renderMeta).catch(() => {});
    } catch (e) {
      const reason = 'snapshot unavailable: ' + ((e && e.message) || e);
      snapshotUnavailable = { reason };
      indexUnavailable = { reason };
    }
    renderRail();
    renderChips();
    renderTopStrip();
    renderCenter();
    renderHeader();
  }

  function renderMeta(meta) {
    const host = $('cpSnapshotMeta');
    if (!host || !meta) return;
    const sha = meta.commit ? String(meta.commit).slice(0, 12) : 'unknown';
    host.textContent = 'Snapshot as of commit ' + sha + (meta.generated_at ? ' · generated ' + meta.generated_at : '');
  }

  // ---- badges: shared pure builder (enum token -> css class + text label) --
  const badge = cpBadge;

  // ---- top strip ------------------------------------------------------------
  function renderTopStrip() {
    const host = $('cpTopStrip');
    if (!host) return;
    if (snapshotUnavailable) {
      host.innerHTML =
        `<div class="cp-strip-unavailable" role="status">Snapshot unavailable` +
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

    cells.push(cell('Repository', esc(meta.repository_identity || '—')));
    cells.push(cell('Domain', esc(meta.requested_domain || '(all)')));

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

    cells.push(cell('Projection', badge('avail', avail)));
    cells.push(cell('Critical', severityCountText('critical', avail)));
    cells.push(cell('Warnings', severityCountText('warning', avail)));
    cells.push(cell('Open questions', cpCountText(snapshot.open_question_count)));
    cells.push(cell('Active task', taskText(snapshot.active_task)));
    cells.push(cell('Completion', completionText(snapshot.completion)));

    let html = `<div class="cp-strip-cells">${cells.join('')}</div>`;
    html += CP_FMT.cpGroundingSummary(snapshot);

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
    return st + (c.authoritative_completion ? ' (authoritative completion — not correctness)' : '');
  }

  // ---- left rail (descriptor-driven only) -----------------------------------
  function renderRail() {
    const host = $('cpRail');
    if (!host) return;
    if (!descriptor) {
      host.innerHTML = `<div class="cp-rail-loading">Loading navigation…</div>`;
      return;
    }
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
    visibleCount = PAGE_STEP;
    renderRail();
    renderChips();
    renderCenter();
  }

  // ---- filter chips -----------------------------------------------------------
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
      activeFilter = { chip: '' };
      mode = 'attention';
      renderChips();
      renderCenter();
      return;
    }
    activeFilter = { chip: id };
    if (group === 'art') {
      mode = 'artifacts';
      visibleCount = PAGE_STEP;
      indexFilters = id === 'all' ? {} : { closure_filter: CLOSURE_FILTER[id] };
    } else {
      mode = 'attention';
    }
    renderChips();
    renderCenter();
  }

  // ---- center: attention queue (default) or artifact index -------------------
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
      host.innerHTML = `<div class="cp-empty">No attention queue — snapshot unavailable.</div>`;
      return;
    }
    if (!snapshot) {
      host.innerHTML = `<div class="cp-empty">Loading…</div>`;
      return;
    }
    let items = snapshot.top_attention || [];
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

  // filteredArtifacts applies the current filter chips over the already-fully-
  // crawled index (the CI snapshot job pages ListArchitectureArtifacts to
  // exhaustion at build time) — this is a client-side SELECT, never a new
  // network request, since there is no live server behind this page.
  function filteredArtifacts() {
    const rows = (index && index.page) || [];
    return rows.filter((a) => {
      if (indexFilters.class_filter && a.class !== indexFilters.class_filter) return false;
      if (indexFilters.closure_filter && a.closure !== indexFilters.closure_filter) return false;
      return true;
    });
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
    const all = filteredArtifacts();
    const rows = all.slice(0, visibleCount);
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
      if (all.length > visibleCount) p += `<button id="cpMore" class="cp-more">Show more</button>`;
      pager.innerHTML = p;
      const more = $('cpMore');
      if (more) more.onclick = () => { visibleCount += PAGE_STEP; renderCenter(); };
    }
  }

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

  // ---- selection header (thin — deep inspector) ------------------------------
  // Full per-artifact state is only available in this snapshot for IRIs that
  // appeared in the control snapshot's top_attention list (attentionStates) —
  // the CI job deliberately doesn't crawl full state for every artifact (an
  // unbounded, unmeasured cost). Selecting anything else shows an honest
  // "not captured in this snapshot" note rather than spinning forever.
  function selectArtifact(iri) {
    selectedNodeIri = iri;
    pendingInspectorFocus = true;
    if (Object.prototype.hasOwnProperty.call(attentionStates, iri)) {
      artifact = { state: attentionStates[iri] };
    } else {
      artifact = { unavailable: true, reason: 'full detail not captured in this snapshot (only attention-linked artifacts include it)' };
    }
    renderHeader();
  }

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
    const st = artifact.state || {};
    const id = st.identity || {};
    const lc = st.lifecycle || {};
    const meta = st.meta || {};
    const dims = Array.isArray(st.dimensions) ? st.dimensions : [];
    const applicable = dims.filter((d) => d && d.applicable !== false);
    const notApplicable = dims.length - applicable.length;

    let dimHtml = applicable.map(cpDimensionRow).join('');
    if (!applicable.length) dimHtml = `<span class="cp-none">no applicable dimensions observed</span>`;
    if (notApplicable > 0) dimHtml += `<div class="cp-dim-na">${notApplicable} dimension(s) not applicable to this class</div>`;

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
        `<div class="cp-readonly-note">This is a read-only snapshot — architect answer/accept actions aren't available here.</div>`) +
      section('Evidence &amp; tests', cpIdList('Evidence', st.evidence)) +
      section('Promoted-feedback provenance', cpFeedbackProvenance(st.feedback)) +
      section('Relationships', cpUnavailableSection('Relationships', 'no owner-projected relationship data')) +
      section('Focus graph', cpUnavailableSection('Focus graph', 'no owner-projected graph data')) +
      `</div>`;
    focusInspectorIfPending(host);
  }
  function row(k, v) {
    return `<div class="cp-h-row"><span class="cp-h-k">${esc(k)}</span><span class="cp-h-v">${v}</span></div>`;
  }
  function section(title, inner) {
    return `<section class="cp-insp-sec"><h3 class="cp-insp-h">${title}</h3><div class="cp-insp-b">${inner}</div></section>`;
  }
  // Questions are display-only ids, each annotated with the dimension(s) that
  // reference it. No "Answer…" action — there is no mutation path here.
  function questionLinkage(questions, dims) {
    const qs = Array.isArray(questions) ? questions : [];
    if (!qs.length) return `<span class="cp-none">none observed</span>`;
    const list = qs.map((q) => {
      const inDims = (Array.isArray(dims) ? dims : [])
        .filter((d) => d && Array.isArray(d.questions) && d.questions.indexOf(q) !== -1)
        .map((d) => d.label || d.dimension)
        .filter(Boolean);
      const link = inDims.length ? `<span class="cp-qdim">${esc(inDims.join(', '))}</span>` : '';
      return `<li><code>${esc(q)}</code> ${link}</li>`;
    }).join('');
    return `<ul class="cp-idlist">${list}</ul>`;
  }
})();
