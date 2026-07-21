// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.timestamp_field_must_be_qualified
// @awareness file_role=ruleguard_rules_for_meta_timestamp_is_an_observation_not_an_event_time

// Ruleguard rules for the per-instance invariant
//
//	timestamp_field_must_carry_semantic_qualifier
//
// (parent meta.timestamp_is_an_observation_not_an_event_time)
//
// The bug shape: a struct field named bare "Timestamp" or
// "CreatedAt" / "UpdatedAt" / "Time" with a *timestamppb.Timestamp
// or int64 type. The unqualified name carries no information about
// WHOSE clock recorded it (event source, observer, transport,
// storage) or WHAT moment it captures (event-actual, observation,
// commit, etc.).
//
// The principle distinguishes four moments any timestamp could
// represent: event-actual, observer-recorded, transport-arrived,
// storage-committed. Consumers that interpret an unqualified
// timestamp as one of these without explicit semantics get the
// answer wrong by whatever lag exists between the moments —
// which is unbounded in distributed systems.
//
// Canonical good shapes:
//
//	CapturedAtUnix       int64  // observer clock when capture ran
//	ProducedAtUnix       int64  // event-source clock when emitted
//	CommittedAtUnix      int64  // storage clock when committed
//	InstalledUnix        int64  // /proc/<pid> mtime, not wall-clock
//	ObserverClockUnixMs  int64  // explicit observer + units
//
// Bug shape:
//
//	Timestamp     *timestamppb.Timestamp   // whose clock? what moment?
//	CreatedAt     int64                     // by whom? for what event?
//	Time          string                    // ambiguous on every axis
//
// Today's sweep: this rule scans Go struct definitions (proto-
// generated and hand-written) for bare timestamp field names.
// The known-good patterns above all match because they include
// qualifying words (At*, Unix, *Ms, etc.).
//
// Note: proto-generated fields will likely have many hits because
// proto field naming conventions allow bare `timestamp`. Those
// hits are best addressed at the .proto definition level; the
// scanner surfaces them so the proto author knows.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// bareTimestampField catches struct fields named Timestamp /
// CreatedAt / UpdatedAt / Time that are NOT qualified by an
// At + verb (CapturedAt, ProducedAt, ObservedAt) or by units
// (Unix, UnixMs, UnixNano). The rule's regex is intentionally
// restrictive: it matches ONLY the exact bare names.
func bareTimestampField(m dsl.Matcher) {
	m.Match(
		`type $S struct { $*_; $name $T; $*_ }`,
	).
		// NOTE: an earlier version also tested
		// m["T"].Type.Is("*timestamppb.Timestamp"). ruleguard cannot
		// resolve that package-qualified type standalone, so its rule
		// loader aborted with a parse error — which silently killed the
		// ENTIRE rule (including the int64/uint64/string cases) and made
		// it report zero findings in production: a dead scanner that
		// looks clean. (meta.negative_result_requires_coverage_attestation.)
		// Proto-generated timestamp fields are better caught at the
		// .proto level anyway, as this file's header comment notes.
		Where(
			m["name"].Text.Matches(`^(Timestamp|CreatedAt|UpdatedAt|Time)$`) &&
				(m["T"].Type.Is("int64") ||
					m["T"].Type.Is("uint64") ||
					m["T"].Type.Is("string")),
		).
		Report(`struct field "$name" has bare temporal name with no semantic qualifier — the reader cannot tell whose clock (event source, observer, transport, storage) or what moment (event-actual, observation, commit) it captures. Rename to CapturedAt / ProducedAt / CommittedAt + Unix / UnixMs / UnixNano. See meta.timestamp_is_an_observation_not_an_event_time`)
}
