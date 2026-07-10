// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.identity_field_writers
// @awareness file_role=ruleguard_rules_for_meta_identity_computation_must_be_invariant
// @awareness enforces=globular.platform:invariant.installed_package.timestamp_writers_must_preserve_during_observe

// Ruleguard rules for the per-instance invariant
//
//	installed_package.timestamp_writers_must_preserve_during_observe
//
// (parent meta.identity_computation_must_be_invariant)
//
// The bug shape this catches is the v1.2.164 regression:
//
//	// file: heartbeat.go  (observer-only context)
//	existing.UpdatedUnix = time.Now().Unix()  // ← stomps the install
//	                                          //   anchor with an observe
//	                                          //   timestamp; verifier
//	                                          //   sees max(UpdatedUnix,
//	                                          //   InstalledUnix) > PID
//	                                          //   start and flags
//	                                          //   service.old_pid_after_upgrade
//
// The principle: an identity-timestamp field has ONE semantic (here: the
// install anchor — when the PID started). A second writer that treats it
// as "what I see now" silently corrupts the contract. The fix in
// v1.2.164 made heartbeat preserve existing.UpdatedUnix; this rule
// regresses if any new writer is added that doesn't preserve.
//
// The rule is intentionally narrow — it only catches assignment to
// .UpdatedUnix with time.Now() (the wall-clock substitution shape).
// Other writers that legitimately set UpdatedUnix to an install anchor
// (apply_package_release.go's `time.Now().Unix()` at install time,
// process_fingerprint.go's PID-start derivation) are classified under
// exception_files because they ARE the canonical writers.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// updatedUnixWallClockStomp catches `_.UpdatedUnix = time.Now().Unix()`
// assignments — the v1.2.164 regression shape. The fix preserves the
// existing value; any new writer that takes this shape outside the
// canonical install path is a regression.
func updatedUnixWallClockStomp(m dsl.Matcher) {
	m.Match(`$_.UpdatedUnix = time.Now().Unix()`).
		Report(`UpdatedUnix stomped with wall-clock time.Now() — v1.2.164 regression shape; this field carries the install anchor (PID-start derivation in proof writer, install time in apply path). Observer contexts (heartbeat, snapshot) must preserve existing.UpdatedUnix. See meta.identity_computation_must_be_invariant`)
}

// installedUnixWallClockStomp catches the sibling shape on
// InstalledUnix — same anchor semantics, same regression risk.
func installedUnixWallClockStomp(m dsl.Matcher) {
	m.Match(`$_.InstalledUnix = time.Now().Unix()`).
		Report(`InstalledUnix stomped with wall-clock time.Now(); this field is the install anchor — observer contexts must preserve existing.InstalledUnix. See meta.identity_computation_must_be_invariant`)
}
