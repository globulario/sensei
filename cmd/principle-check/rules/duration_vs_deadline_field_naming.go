// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.duration_vs_deadline_field_naming
// @awareness file_role=ruleguard_rules_for_meta_duration_versus_deadline_is_not_interchangeable

// Ruleguard rules for the per-instance invariant
//
//	ambiguous_int_typed_time_field_must_carry_unit_qualifier
//
// (parent meta.duration_versus_deadline_is_not_interchangeable)
//
// The bug shape: a struct field named Timeout / Delay / Wait /
// Backoff / TTL with an INT-family type, with no name qualifier
// (Seconds / Ms / Duration / Deadline / Unix). Consumers cannot
// tell whether the value is a duration (units since some moment)
// or a deadline (an absolute moment) or even what units it carries.
// The two encodings age differently across process restart and NTP
// correction; mixing them silently corrupts retry/expiry logic.
//
// Today's sweep: the codebase is largely clean — fields named
// Timeout/Delay/Wait/Backoff are either typed time.Duration (units
// unambiguous) or carry the _Seconds suffix in the name. This rule
// is a pure regression detector for new fields that get added
// without the qualifier.
//
// Canonical good shapes:
//
//	Timeout       time.Duration       // unambiguous via type
//	TimeoutSeconds int               // unambiguous via name
//	BackoffMs     int               // unambiguous via name
//	DeadlineUnix  int64             // explicit deadline semantics
//
// Bug shape:
//
//	Timeout int          // ambiguous: seconds? ms? deadline?
//	Backoff int64        // same
//	TTL     uint32       // is this seconds or milliseconds?
//
// Note: gogrep struct-field matching is limited; we match the
// declaration via the field's textual shape. This catches the
// common cases.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// ambiguousIntTimeField catches struct fields with names matching
// the time-keyword set but with an int-family type and no unit
// suffix in the name. The Where clause requires the type to be
// one of int/int64/uint/uint64/int32/uint32.
//
// gogrep's struct-field matching is best-effort; we use the
// field-declaration pattern shape. ruleguard validates the type
// via Type.Is.
func ambiguousIntTimeField(m dsl.Matcher) {
	m.Match(
		`type $S struct { $*_; $name $T; $*_ }`,
	).
		Where(
			m["name"].Text.Matches(`^(Timeout|Delay|Wait|Backoff|TTL|Expiry|Cooldown)$`) &&
				(m["T"].Type.Is("int") ||
					m["T"].Type.Is("int64") ||
					m["T"].Type.Is("int32") ||
					m["T"].Type.Is("uint") ||
					m["T"].Type.Is("uint64") ||
					m["T"].Type.Is("uint32")),
		).
		Report(`struct field "$name" has int-family type "$T" without unit qualifier — operators and downstream code cannot tell whether the value is seconds, milliseconds, or an absolute deadline. Rename to add a unit qualifier ($name + Seconds / Ms / Unix) OR change the type to time.Duration. See meta.duration_versus_deadline_is_not_interchangeable`)
}
