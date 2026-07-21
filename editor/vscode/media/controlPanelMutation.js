'use strict';
// controlPanelMutation.js — the guarded architect-answer workflow state machine.
//
// The interaction is painfully literal and NEVER optimistic:
//   choose action → PREPARE → show the owner candidate/consequences → explicit
//   CONFIRM → COMMIT once → show the typed receipt or refusal → REFRESH the
//   artifact state from the owner. The DISPLAYED architectural lifecycle ALWAYS
//   comes from the refreshed owner projection — never inferred from the receipt.
//   A commit may report mutation_applied=true, yet if the follow-up refresh is
//   stale/unavailable the display shows THAT, not an invented "accepted".
//
// Pure + framework-free; loaded by the webview AND by node --test.

function cpGuardedInitial() {
  return { phase: 'idle', candidate: null, refusal: null, receipt: null, ownerState: null, inFlight: false };
}

// cpGuardedReduce is the ONLY way the guarded state advances. It refuses illegal
// transitions (e.g. a second commit while one is in flight, or a commit before
// an explicit confirmation).
function cpGuardedReduce(state, ev) {
  var s = state || cpGuardedInitial();
  switch (ev && ev.type) {
    case 'RESET':
      return cpGuardedInitial();
    case 'PREPARE_START':
      return assign(s, { phase: 'preparing', candidate: null, refusal: null, receipt: null, ownerState: null });
    case 'PREPARE_RESULT':
      if (ev.refusal) {
        return assign(s, { phase: 'refused', refusal: ev.refusal, candidate: null });
      }
      return assign(s, { phase: 'prepared', candidate: ev.candidate || null, refusal: null });
    case 'CONFIRM':
      // Confirmation is only meaningful once a candidate has been shown.
      if (s.phase !== 'prepared') {
        return s;
      }
      return assign(s, { phase: 'confirmed' });
    case 'COMMIT_START':
      // Exactly one commit: only from an explicit confirmation, never while a
      // commit is already in flight (no second click).
      if (s.phase !== 'confirmed' || s.inFlight) {
        return s;
      }
      return assign(s, { phase: 'committing', inFlight: true });
    case 'COMMIT_RESULT':
      if (ev.refusal) {
        return assign(s, { phase: 'refused', refusal: ev.refusal, inFlight: false });
      }
      // A receipt is received — but NO displayed lifecycle is derived from it.
      // The owner refresh is still required before anything is shown as applied.
      return assign(s, { phase: 'committed', receipt: ev.receipt || null, inFlight: false });
    case 'REFRESH_RESULT':
      return assign(s, { phase: 'refreshed', ownerState: ev.ownerState || null });
    default:
      return s;
  }
}

// cpCanSubmit is true only when an explicit confirmation is pending and no commit
// is in flight — the single gate that prevents a duplicate submission.
function cpCanSubmit(state) {
  var s = state || {};
  return s.phase === 'confirmed' && !s.inFlight;
}

// cpDisplayedLifecycle is the lifecycle the UI may render. It comes ONLY from the
// refreshed owner projection — never from the receipt. It is null until a refresh
// has happened, and reflects a stale/unavailable refresh honestly (never optimism).
function cpDisplayedLifecycle(state) {
  var s = state || {};
  if (s.ownerState) {
    if (s.ownerState.unavailable) {
      return { source: 'owner', unavailable: true, reason: s.ownerState.reason || 'unavailable' };
    }
    return { source: 'owner', lifecycle: s.ownerState.lifecycle || null, closure: s.ownerState.closure || null };
  }
  // No owner refresh yet — the UI must NOT invent an "accepted" state, even if a
  // receipt says the mutation was applied.
  return null;
}

// cpReceiptApplied reflects the server's OWN replay authority verbatim (for the
// receipt panel only — never for the architectural lifecycle display).
function cpReceiptApplied(state) {
  var s = state || {};
  if (!s.receipt) {
    return false;
  }
  return s.receipt.mutation_applied === true;
}

function assign(a, b) {
  var out = {};
  for (var k in a) {
    if (Object.prototype.hasOwnProperty.call(a, k)) {
      out[k] = a[k];
    }
  }
  for (var k2 in b) {
    if (Object.prototype.hasOwnProperty.call(b, k2)) {
      out[k2] = b[k2];
    }
  }
  return out;
}

var CP_MUT = {
  cpGuardedInitial: cpGuardedInitial,
  cpGuardedReduce: cpGuardedReduce,
  cpCanSubmit: cpCanSubmit,
  cpDisplayedLifecycle: cpDisplayedLifecycle,
  cpReceiptApplied: cpReceiptApplied,
};

if (typeof module !== 'undefined' && module.exports) {
  module.exports = CP_MUT;
}
