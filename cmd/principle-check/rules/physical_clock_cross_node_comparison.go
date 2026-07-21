// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.physical_clock_cross_node_comparison
// @awareness file_role=ruleguard_rules_for_meta_physical_clocks_disagree_use_logical_ordering
// @awareness enforces=globular.platform:invariant.cross_node_staleness_must_use_server_clock

// Ruleguard rules for the per-instance invariant
//
//	cross_node_staleness_must_use_server_clock
//
// (parent meta.physical_clocks_disagree_use_logical_ordering)
//
// The bug shape this catches: `time.Since(time.Unix(...))` — converting
// a stored Unix timestamp back to time.Time and comparing it against
// the local wall clock via time.Since. This is Go's idiomatic pattern
// for "how stale is this record?" and it is only correct when the
// timestamp was produced by the SAME node's clock.
//
// When the timestamp comes from a remote node's report (heartbeat,
// status update, workflow receipt), the comparison is against two
// independent physical clocks that drift independently. NTP corrects
// them but not atomically, so the staleness calculation can be
// negative, wildly wrong, or transiently correct — depending on the
// skew at the moment of comparison.
//
// The correct pattern (already implemented for LastSeen in
// ReportNodeStatus): the SERVER stamps the observation at receipt
// time using its own clock, and all staleness checks compare against
// that server-stamped value. This eliminates cross-node clock
// comparison entirely.
//
// The same shape also appears as `time.Since(time.UnixMilli(...))`
// for millisecond-precision timestamps.
//
// Sites in the scoped directories fall into three categories:
//
//  1. SERVER-STAMPED: the Unix timestamp was written by the same
//     process now calling time.Since — e.g. controller wrote
//     LastTransitionUnixMs when it processed a status change, and
//     later checks elapsed time against its own clock. These are
//     CONFORMANT (safe, same clock).
//
//  2. LEADER-LOCAL: the value was written and read within the same
//     leader epoch — e.g. leader_liveness.go's lastNano is the
//     leader's own atomic counter. CONFORMANT.
//
//  3. REMOTE-SOURCED: the Unix timestamp was produced by a remote
//     node's clock (heartbeat timestamp, install time, cert
//     NotBefore). These are DRIFT unless a documented skew bound
//     or server-side re-stamping makes them safe.
//
// The exception_files list in the per-instance invariant classifies
// the known sites. A new `time.Since(time.Unix(...))` callsite in
// a scoped directory appears as UNKNOWN until classified.
//
// Concrete incidents this shape protects against:
//   - TestReportNodeStatus_LastSeenUsesServerClockNotNodeClock:
//     caught the original bug where LastSeen used the reporting
//     node's clock, making staleness checks clock-skew-dependent.
//   - Cert NotBefore validation: verifiers on nodes with skewed
//     clocks see "not yet valid" for freshly issued certs.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// timeSinceUnix catches `time.Since(time.Unix($secs, $nsecs))` —
// the most common shape for wall-clock staleness of a stored record.
func timeSinceUnix(m dsl.Matcher) {
	m.Match(`time.Since(time.Unix($_, $_))`).
		Report(`time.Since(time.Unix(...)) compares a stored Unix timestamp against the local wall clock — if the timestamp was produced by a different node's clock, the result is skewed by the physical clock difference between the two nodes. Use a server-stamped timestamp (written at receipt time by the comparing node's own clock) or a logical ordering (etcd revision, Raft index). See meta.physical_clocks_disagree_use_logical_ordering`)
}

// timeSinceUnixMilli catches `time.Since(time.UnixMilli($ms))` —
// the millisecond-precision variant of the same shape.
func timeSinceUnixMilli(m dsl.Matcher) {
	m.Match(`time.Since(time.UnixMilli($_))`).
		Report(`time.Since(time.UnixMilli(...)) compares a stored millisecond timestamp against the local wall clock — same cross-node skew risk as time.Since(time.Unix(...)). Verify the timestamp was produced by THIS node's clock, or use server-side re-stamping. See meta.physical_clocks_disagree_use_logical_ordering`)
}
