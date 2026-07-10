// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.doctor_rule_must_consume_snap_errors
// @awareness file_role=ruleguard_rules_for_meta_harvest_and_yield_are_distinct_availability_dimensions

// Ruleguard rules for the per-instance invariant
//
//	doctor_rule_evaluate_must_consult_snap_errors
//
// (parent meta.harvest_and_yield_are_distinct_availability_dimensions)
//
// GATING STATUS: NOT in principle-check-all (deliberately). This static
// matcher is too broad — it flags ~55 of the 111 cluster_doctor Evaluate
// methods, most of which legitimately do not read snapshot data and so
// have nothing to guard. The SAME contract is already gated, precisely
// and behaviorally, in services CI: TestNoRuleEmitsConfidentFailureOnErroredSnapshot,
// TestClusterScopedRulesRefuseOnSourceError, and
// TestEvaluateAll_EmptyFindings_SurfacesSourceUnavailable. Keep this rule
// as an exploratory lens (run it by hand to enumerate candidate sites);
// do not gate it until it is narrowed to "reads a snapshot field AND
// emits a confident verdict WITHOUT consulting snap.Errors."
//
// The bug shape: a cluster_doctor rule's Evaluate function reads
// snap.Nodes, snap.NodeHealths, snap.ObjectStoreDesired, or any
// other Snapshot field WITHOUT first checking snap.Errors to see
// whether the source of that field had a failed fetch. The
// collector records failed sub-fetches in snap.Errors via
// snap.addError(source, op, err), but the rules ignore the record
// and reason on the data as if it were complete.
//
// The result: when a collector sub-fetch fails (timeout, RBAC
// reject, transient gRPC error), the rule reads an EMPTY snapshot
// field and concludes "this thing doesn't exist" — instead of
// "I cannot tell." This produces both false negatives (missing
// findings the doctor should have raised) and false positives
// (findings that name absence as drift when it's actually missing
// data).
//
// INC-2026-0004 hit this in the partial-snapshot path specifically;
// the fix at that one site became objectstore.partial_snapshot_
// unknown_not_down. The proactive review of 2026-06-06 showed the
// gap extends to ALL 62 doctor rule files in cluster_doctor_server/
// rules/, none of which currently consult snap.Errors.
//
// Today's sweep: every Evaluate function that consumes a Snapshot
// field will match this rule. The scanner currently produces 62
// findings — exactly the structural debt the harvest_yield
// promotion record names. A future infrastructure fix (rule
// registry consults snap.Errors before dispatch) closes the gap
// across all files at once; until then this rule documents the
// scope of the debt.
//
// Bug shape:
//
//	func (myRule) Evaluate(snap *collector.Snapshot, cfg Config) []Finding {
//	    var findings []Finding
//	    for _, node := range snap.Nodes {            // ← reads field
//	        // ... no check on snap.Errors before reasoning ...
//	    }
//	    return findings
//	}
//
// Canonical good shape (proposed):
//
//	func (myRule) Evaluate(snap *collector.Snapshot, cfg Config) []Finding {
//	    if snap.HadError("cluster_controller", "ListNodes") {
//	        return nil   // cannot tell; do not produce findings on partial data
//	    }
//	    var findings []Finding
//	    for _, node := range snap.Nodes {
//	        ...
//	    }
//	    return findings
//	}
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// evaluateConsumesSnapshotWithoutErrorCheck catches Evaluate
// methods that read snap.Nodes / snap.NodeHealths / snap.* and
// whose body does NOT also reference snap.Errors. ruleguard cannot
// negate "does not appear" cleanly on a multi-statement body, so
// we match the broad shape (Evaluate method consuming snap.<field>)
// and rely on the AT-LEAST-ONE finding it produces to escalate to
// a manual audit. The cluster_doctor rule infrastructure fix is
// the structural remediation.
//
// gogrep $*_ wildcard matches "zero or more statements", so the
// pattern below catches any Evaluate body that uses snap.Nodes
// regardless of what else it does.
func evaluateConsumesSnapshotWithoutErrorCheck(m dsl.Matcher) {
	// Broader pattern: any Evaluate method on a doctor rule whose
	// body references snap.<anyField> at any depth. We then exclude
	// the (rare) bodies that also reference snap.Errors / snap.HadError
	// via a Where-clause file-level check is not available — instead
	// we rely on the fact that today no rule consults snap.Errors,
	// so every Evaluate match is a candidate. When the structural
	// fix lands and rules start consulting snap.Errors, the rule
	// will need refinement (or removal).
	m.Match(
		// Receiver with a name: func (r ruleType) Evaluate(...)
		`func ($_ $_) Evaluate($snap *collector.Snapshot, $_ $_) []Finding { $*body }`,
		// Anonymous receiver: func (ruleType) Evaluate(...) — Go
		// allows omitting the receiver name when it's unused; the
		// gogrep pattern needs a separate clause for this shape.
		`func ($_) Evaluate($snap *collector.Snapshot, $_ $_) []Finding { $*body }`,
	).
		Where(
			// Filter out trivial rules that don't actually consume
			// snapshot data (none today, but defensive)...
			m["snap"].Text == "snap" &&
				// ...and exclude bodies that DO consult a per-source error
				// signal. As of 2026-06-09 the structural fix landed (registry
				// emits one INVARIANT_UNKNOWN finding per errored source, and
				// the TestNoRuleEmitsConfidentFailureOnErroredSnapshot ratchet
				// gates confident FAILs). A rule is conformant if it consults
				// ANY of the snapshot's error signals — not only snap.HadError,
				// but also the dedicated per-source error fields the collector
				// populates:
				//   HadError     — generic (service,rpc) error check
				//   QueryError   — CriticalKeyQueryError[key] per-key etcd error
				//   LoadError    — ObjectStoreDesiredLoadError / IngressSpecLoadError
				//   ReachError   — RepositoryOperationalStatus.ReachError
				//   DataErrors   — rules that iterate the raw error list directly
				// Matching these tokens makes the count track real conformance.
				// Source-independent rules (local fs, static config, probe-flag
				// bools) carry NO RPC source and are exempted via the invariant's
				// exception_files list instead.
				!m["body"].Text.Matches(`HadError|QueryError|LoadError|ReachError|DataErrors`),
		).
		Report(`doctor rule Evaluate consumes a Snapshot without consulting snap.HadError — when the collector's fetch for a source failed, this rule reads an empty field and may produce a wrong finding (or suppress a real one). FALSE_POSITIVE (confident FAIL on errored data) is gated by TestNoRuleEmitsConfidentFailureOnErroredSnapshot; FALSE_NEGATIVE (silent) is covered structurally by the registry's snapshot_source_unavailable findings. Add a snap.HadError guard for rules whose verdict depends on a single source. See meta.harvest_and_yield_are_distinct_availability_dimensions`)
}
