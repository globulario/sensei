'use strict';
// controlPanelFmt.js — pure, framework-free formatters for the Phase 9.5
// architectural control panel. Loaded BOTH as a plain webview <script> (defines
// globals used by dashboard.js) AND via require() from node --test proofs
// (module.exports). It contains NO semantic tables: it never decides closure,
// severity, lifecycle, class membership, or applicability. It only:
//   - reflects a server enum string as a CSS token + a human text label
//     (so color is never the sole carrier of meaning), and
//   - preserves the unknown-versus-zero distinction the wire encodes.
// All meaning originates from the typed owner projection; this file styles it.

// The closed set of proto enum prefixes the panel renders. Stripping a prefix
// yields the bare token used ONLY as a CSS class + (title-cased) text label.
var CP_ENUM_PREFIXES = [
  'ARCHITECTURE_ARTIFACT_CLOSURE_',
  'ARCHITECTURE_ATTENTION_SEVERITY_',
  'ARCHITECTURE_LIFECYCLE_STATE_',
  'ARCHITECTURE_ASSESSMENT_COVERAGE_',
  'ARCHITECTURE_AVAILABILITY_',
  'ARCHITECTURE_SOURCE_AVAILABILITY_',
  'ARCHITECTURE_SOURCE_IMPACT_',
  'ARCHITECTURE_DIMENSION_STATE_',
];

// cpEnumToken maps a full proto enum name to its lowercase bare token
// ('ARCHITECTURE_ARTIFACT_CLOSURE_CLOSED' -> 'closed'). Empty string for a
// missing value. Used ONLY as a CSS class suffix — not as a decision input.
function cpEnumToken(name) {
  if (!name || typeof name !== 'string') {
    return '';
  }
  for (var i = 0; i < CP_ENUM_PREFIXES.length; i++) {
    var p = CP_ENUM_PREFIXES[i];
    if (name.indexOf(p) === 0) {
      return name.slice(p.length).toLowerCase();
    }
  }
  return name.toLowerCase();
}

// cpEnumLabel produces the human text label carried alongside every badge, so
// the badge never relies on color alone ('not_applicable' -> 'Not applicable').
function cpEnumLabel(name) {
  var token = cpEnumToken(name);
  if (!token) {
    return 'Unknown';
  }
  var words = token.split('_').join(' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

// cpIsUnspecified is true for a missing value or any *_UNSPECIFIED enum. Every
// closed vocabulary uses UNSPECIFIED=0 which is ALWAYS invalid on the wire — the
// panel must render it as an explicit invalid state, never a neutral/OK default.
function cpIsUnspecified(name) {
  if (!name || typeof name !== 'string') {
    return true;
  }
  return name.indexOf('_UNSPECIFIED') === name.length - '_UNSPECIFIED'.length;
}

// cpCount preserves unknown-versus-zero. A proto3 `optional` int64 decodes to a
// numeric string when observed and to undefined/null when the source was not
// observed. Returns a Number when observed (including a real 0) and null when
// unknown. NEVER coerce absence to zero.
function cpCount(value) {
  if (value === undefined || value === null || value === '') {
    return null;
  }
  var n = Number(value);
  return isNaN(n) ? null : n;
}

// cpCountText renders a count honestly: an em dash for unknown, the number for
// observed (including "0").
function cpCountText(value) {
  var n = cpCount(value);
  return n === null ? '—' : String(n);
}

// cpIsUnknownCount is true only when the source was not observed.
function cpIsUnknownCount(value) {
  return cpCount(value) === null;
}

function cpEsc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

// cpBadge renders one owner enum as a badge: a visual class derived from the
// enum TOKEN plus a text label (always present, so color is never the sole
// carrier). A missing / *_UNSPECIFIED value renders an explicit "Invalid" badge
// — it is never mapped to a neutral or OK default.
function cpBadge(kind, enumName) {
  var invalid = cpIsUnspecified(enumName);
  var cls = invalid
    ? 'cp-badge cp-' + kind + '--invalid'
    : 'cp-badge cp-' + kind + '-' + cpEnumToken(enumName);
  var text = invalid ? 'Invalid' : cpEnumLabel(enumName);
  return '<span class="' + cls + '" title="' + cpEsc(enumName || '') + '">' + cpEsc(text) + '</span>';
}

// cpSeverityCount reads a severity tally by key. A count is a real 0 only when
// the projection was observed; when the source was not observed it is Unknown.
function cpSeverityCount(counts, sevKey, observed) {
  if (!Array.isArray(counts)) {
    return observed ? '0' : 'Unknown';
  }
  for (var i = 0; i < counts.length; i++) {
    if (counts[i] && counts[i].key === sevKey) {
      return String(Number(counts[i].count || 0));
    }
  }
  return observed ? '0' : 'Unknown';
}

// ── Checkpoint 4 artifact-inspector builders (pure; owner state rendered verbatim) ──

// cpIdList renders a labeled list of owner id strings verbatim. An empty list
// reads "none observed" — never a synthesized value. `ids` are shown as-is.
function cpIdList(label, ids) {
  var arr = Array.isArray(ids) ? ids : [];
  var body = arr.length
    ? '<ul class="cp-idlist">' + arr.map(function (x) { return '<li><code>' + cpEsc(x) + '</code></li>'; }).join('') + '</ul>'
    : '<span class="cp-none">none observed</span>';
  return '<div class="cp-insp-row"><span class="cp-insp-k">' + cpEsc(label) + '</span>' + body + '</div>';
}

// cpExplanation renders the owner-projected actionable incompleteness for a
// non-positive dimension: what is Known, what is Missing, Why it cannot improve,
// and the Next evidence needed. It is PRESENTATION ONLY — every string is the
// owner's verbatim projection (escaped); the panel composes no wording and holds
// no decision table. The stable semantic identity (e.kind) rides as a data
// attribute so distinctions are testable without parsing prose. Empty fields are
// omitted (never rendered as a blank row). A null explanation yields ''.
function cpExplanation(e) {
  if (!e || !e.kind) {
    return '';
  }
  function row(k, v) {
    return v ? '<div class="cp-explain-row"><span class="cp-explain-k">' + cpEsc(k) + '</span><span class="cp-explain-v">' + cpEsc(v) + '</span></div>' : '';
  }
  return (
    '<div class="cp-dim-explain" data-explain-kind="' + cpEsc(e.kind) + '">' +
    row('Known', e.known) +
    row('Missing', e.missing) +
    row('Why not improvable', e.why_not_improvable) +
    row('Next evidence', e.next_evidence) +
    '</div>'
  );
}

// cpDimensionRow renders ONE dimension. Applicable-only is enforced HERE: a
// non-applicable dimension yields '' and can never be shown as open. The state
// is an owner enum -> badge (no client computation); required/reason and the
// per-dimension blockers/evidence/questions/owner/next-action are verbatim. When
// the owner supplied an actionable incompleteness explanation (non-positive
// states only), it is rendered verbatim beneath the head.
function cpDimensionRow(d) {
  if (!d || d.applicable === false) {
    return '';
  }
  var head =
    '<div class="cp-dim-head">' +
    '<span class="cp-dim-label">' + cpEsc(d.label || d.dimension || '') + '</span>' +
    (d.required ? '<span class="cp-dim-req">required</span>' : '<span class="cp-dim-opt">optional</span>') +
    cpBadge('dim', d.state) +
    (d.reason_code ? '<span class="cp-reason">' + cpEsc(d.reason_code) + '</span>' : '') +
    '</div>';
  var body =
    cpExplanation(d.explanation) +
    cpIdList('Blockers', d.blockers) +
    cpIdList('Evidence', d.evidence) +
    cpIdList('Questions', d.questions) +
    '<div class="cp-insp-row"><span class="cp-insp-k">Owner</span><span>' + cpEsc(d.owner || '—') + '</span></div>' +
    '<div class="cp-insp-row"><span class="cp-insp-k">Next action owner</span><span>' + cpEsc(d.next_action_owner || '—') + '</span></div>';
  return '<div class="cp-dim">' + head + '<div class="cp-dim-body">' + body + '</div></div>';
}

// cpKeyedCount reads one key's count from a wire keyed-count array. Returns null
// when the array is ABSENT (the population was not observed — never coerce to 0),
// 0 when the collection was observed but the key is absent (an observed zero).
function cpKeyedCount(arr, key) {
  if (!Array.isArray(arr)) {
    return null;
  }
  for (var i = 0; i < arr.length; i++) {
    if (arr[i] && arr[i].key === key) {
      return Number(arr[i].count || 0);
    }
  }
  return 0;
}

// cpKeyedTotal sums an observed keyed-count array; null when unobserved (absent/empty).
function cpKeyedTotal(arr) {
  if (!Array.isArray(arr) || arr.length === 0) {
    return null;
  }
  var t = 0;
  for (var i = 0; i < arr.length; i++) {
    t += Number((arr[i] && arr[i].count) || 0);
  }
  return t;
}

// cpRatio is the HONEST numerator/denominator rule: a percentage is emitted ONLY when
// the denominator is an observed integer strictly greater than zero. An unobserved side
// (null) yields "unavailable"; an observed zero population yields "no eligible items" —
// never 0% and never 100%. Coverage is never fabricated from a missing denominator.
function cpRatio(num, den) {
  if (num === null || den === null) {
    return { text: 'unavailable', percent: null };
  }
  if (den === 0) {
    return { text: '0 of 0 (no eligible items)', percent: null };
  }
  return { text: num + ' of ' + den, percent: Math.round((num / den) * 100) };
}

// cpGroundingSummary renders a compact, deterministic grounding summary from tallies that
// ALREADY exist under a single owner (the controlstate catalog + per-class assessment). It
// adds no census engine and invents no denominator: when the catalog was not observed it
// renders an explicit "unavailable" (never 0%/100%). It is grounding, NOT correctness — and
// it never suppresses attention (it is an additional block, not a replacement).
function cpGroundingSummary(snapshot) {
  var snap = snapshot || {};
  var cov = snap.assessment_coverage_counts;
  var clo = snap.closure_counts;
  if (!Array.isArray(cov) || cov.length === 0) {
    return (
      '<div class="cp-grounding"><span class="cp-grounding-h">Grounding</span>' +
      '<span class="cp-none">coverage unavailable — catalog not observed</span></div>'
    );
  }
  // Assessment coverage: assessable artifacts / all enumerated artifacts (owner: controlstate catalog).
  var assessR = cpRatio(cpKeyedCount(cov, 'assessable'), cpKeyedTotal(cov));
  // Closure grounding: closed artifacts / assessable artifacts (owner: controlstate per-class assessment).
  var closeR = cpRatio(cpKeyedCount(clo, 'closed'), cpKeyedCount(cov, 'assessable'));
  function line(label, r) {
    var pct = r.percent === null ? '' : ' <span class="cp-grounding-pct">(' + r.percent + '%)</span>';
    return (
      '<div class="cp-grounding-row"><span class="cp-grounding-k">' + cpEsc(label) + '</span>' +
      '<span class="cp-grounding-v">' + cpEsc(r.text) + pct + '</span></div>'
    );
  }
  return (
    '<div class="cp-grounding"><span class="cp-grounding-h">Grounding</span>' +
    line('Assessable / enumerated', assessR) +
    line('Closed / assessable', closeR) +
    '<div class="cp-grounding-note">Grounding, not correctness — high coverage never certifies and never suppresses attention.</div>' +
    '</div>'
  );
}

// cpFeedbackProvenance renders the EXACT-SCOPE Phase 9.6 feedback reference as
// provenance ONLY — never a repository-wide scan and never authoritative truth.
// A null reference reads "no exact-scope feedback".
function cpFeedbackProvenance(ref) {
  if (!ref) {
    return '<div class="cp-insp-row"><span class="cp-none">no exact-scope feedback</span></div>';
  }
  return (
    '<div class="cp-prov-block">' +
    '<div class="cp-insp-row"><span class="cp-insp-k">Scope</span><code>' + cpEsc(ref.scope_identity || '—') + '</code></div>' +
    '<div class="cp-insp-row"><span class="cp-insp-k">Availability</span><span>' + cpEsc(ref.availability || '—') + '</span></div>' +
    cpIdList('Verified records', ref.verified_record_ids) +
    cpIdList('Lineage', ref.lineage_ids) +
    cpIdList('Limitations', ref.limitations) +
    '<div class="cp-prov-note">Exact-scope provenance only — not a repo-wide scan, not authority.</div>' +
    '</div>'
  );
}

// cpUnavailableSection is the honest "no owner projection" placeholder used by
// relationships and the focus graph (which have no owner-projected data). It
// never falls back to legacy graph adjacency.
function cpUnavailableSection(title, reason) {
  return (
    '<div class="cp-insp-unavailable">' +
    '<span class="cp-insp-k">' + cpEsc(title) + '</span>' +
    '<span class="cp-reason">' + cpEsc(reason || 'no owner-projected data') + '</span>' +
    '</div>'
  );
}

var CP_FMT = {
  ENUM_PREFIXES: CP_ENUM_PREFIXES,
  cpEnumToken: cpEnumToken,
  cpEnumLabel: cpEnumLabel,
  cpIsUnspecified: cpIsUnspecified,
  cpCount: cpCount,
  cpCountText: cpCountText,
  cpIsUnknownCount: cpIsUnknownCount,
  cpEsc: cpEsc,
  cpBadge: cpBadge,
  cpSeverityCount: cpSeverityCount,
  cpIdList: cpIdList,
  cpExplanation: cpExplanation,
  cpKeyedCount: cpKeyedCount,
  cpKeyedTotal: cpKeyedTotal,
  cpRatio: cpRatio,
  cpGroundingSummary: cpGroundingSummary,
  cpDimensionRow: cpDimensionRow,
  cpFeedbackProvenance: cpFeedbackProvenance,
  cpUnavailableSection: cpUnavailableSection,
};

if (typeof module !== 'undefined' && module.exports) {
  module.exports = CP_FMT;
}
