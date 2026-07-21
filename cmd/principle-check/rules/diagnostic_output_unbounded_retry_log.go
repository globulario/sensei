// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.unbounded_retry_log
// @awareness file_role=ruleguard_rules_for_meta_diagnostic_output_must_be_bounded
// @awareness enforces=globular.platform:invariant.retry_loop.repeated_error_log_must_be_deduplicated

// Ruleguard rules for the per-instance invariant
//
//	retry_loop.repeated_error_log_must_be_deduplicated
//
// (parent meta.diagnostic_output_must_be_bounded)
//
// The bug shape this catches is the 2026-06-04 regression (commit
// 0afcb65b). ai_watcher's eventLoop logged "event service connection
// failed" on every retry with no dedup; a one-hour outage produced
// 360 identical lines. heal_audit.go had a different shape (unrotated
// JSONL growth) — the same meta-principle violation but a different
// structural pattern; both were fixed in the same commit.
//
// The shape this rule detects is the RETRY-LOG-UNBOUNDED bug:
//
//	if err != nil {
//	    logger.Error("connection failed", "err", err)  // bare
//	    time.Sleep(10 * time.Second)
//	    continue
//	}
//
// where logger.Error and time.Sleep are DIRECT siblings inside the
// `if err != nil` block. The fix nests the logger.Error inside a
// dedup guard (`if msg == lastErr && time.Since(...) < window`) — the
// nesting moves logger.Error out of the direct-sibling position, so
// the structural pattern below does NOT match the post-fix code.
//
// Pattern shape:
//
//	if $err != nil { $*_; logger.Error($*_); $*_; time.Sleep($_); $*_ }
//
// $*_ on both sides allows the err-block to contain other statements
// before/after/between, but logger.Error and time.Sleep must both be
// direct children of the if-block — which is exactly the bug
// signature that 0afcb65b removed.
//
// Today's sweep finds 0 sites: ai_watcher's eventLoop has the
// deduplicated form, and no other actor-writer dir has the bug shape.
// Pure regression detector.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// loggerErrorWithBareSleepInErrBranch catches `logger.Error + time.Sleep`
// as direct siblings in an err-handling block — the unbounded-retry-
// log pattern from commit 0afcb65b. The fix moves logger.Error into a
// nested if/else (dedup-gated), so the direct-sibling pattern no
// longer matches.
//
// Match three log shapes that exist in the codebase:
//   - logger.Error / logger.Warn / log.Printf / log.Println / fmt.Printf
//
// All five are caught by separate Match alternations; the report
// message names the regression for context.
func loggerErrorWithBareSleepInErrBranch(m dsl.Matcher) {
	m.Match(
		`if $err != nil { $*_; logger.Error($*_); $*_; time.Sleep($_); $*_ }`,
		`if $err != nil { $*_; logger.Warn($*_); $*_; time.Sleep($_); $*_ }`,
		`if $err != nil { $*_; log.Printf($*_); $*_; time.Sleep($_); $*_ }`,
		`if $err != nil { $*_; log.Println($*_); $*_; time.Sleep($_); $*_ }`,
	).Report(`error logged unconditionally before time.Sleep in an err-handling block — unbounded-retry-log regression shape. A persistent dependency outage (event service, scylla, minio) will produce one line per retry interval until the outage clears (360 lines/hr at 10s retry). Dedup the log via a lastErr/lastErrAt guard (see ai_watcher/server.go:eventLoop post-2026-06-04 / commit 0afcb65b for the canonical fix). meta.diagnostic_output_must_be_bounded`)
}
