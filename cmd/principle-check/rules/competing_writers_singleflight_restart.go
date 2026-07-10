// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.competing_writers
// @awareness file_role=ruleguard_rules_for_meta_competing_writers_must_converge_or_be_fenced
// @awareness enforces=globular.platform:invariant.service.restart_singleflight

// Ruleguard rules for the per-instance invariant
//
//	service.restart_singleflight
//
// (parent meta.competing_writers_must_converge_or_be_fenced)
//
// The bug shape this catches: a convergence / reconcile / workflow path
// that calls `systemctl restart <unit>` directly via os/exec, bypassing
// the singleflight gate. The gate exists because two concurrent
// convergence ticks each deciding to restart the same service produces
// a cascade of SIGTERMs → start-limit-hit → service down until manual
// reset.
//
// The legitimate restart sites are operations procedures (cert rotation,
// Day-0 bootstrap, backup-manager restore) — those run serially under
// operator control and don't compete with convergence ticks. They are
// classified as EXCEPTION in the per-instance invariant.
//
// Direct restart calls in convergence / reconcile / workflow dirs are
// regression candidates. Today (2026-06-05) there are zero such sites
// in the scanned scope; this rule guards against a future PR adding
// one without first wiring through the singleflight gate.
//
// Scope (actor_writer_dirs in invariants.yaml):
//   - cluster_controller/cluster_controller_server  (reconcile loop)
//   - workflow/workflow_server                       (workflow engine)
//   - node_agent/node_agent_server                   (reconcile/apply +
//     ops procedures; added 2026-06-11 after a scope audit found node
//     agent's convergence-path restarts were unwatched)
//
// node_agent_server's two known ops-procedure restart files —
// certificate.go (cert rotation) and workflow_day0.go (Day-0 bootstrap) —
// are named in exception_files, so they classify EXCEPTION, not DRIFT. A
// restart added to any OTHER node_agent_server file (e.g. a reconcile /
// apply handler) is caught.
//
// We still do NOT scan backup_manager/ — it has no convergence loop, so
// every restart there is categorically an ops procedure and scanning it
// would only generate future false positives. (Caveat surfaced in the
// audit: backup_manager restore.go::restartAllServices restarts core
// convergence-managed units; its race-safety rests on the operational
// invariant "restore runs only on a quiesced cluster," not a code fence.)
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// directExecRestart catches `exec.Command(... "systemctl" ... "restart" ...)`
// callsites — the most common shape in Go.
func directExecRestart(m dsl.Matcher) {
	m.Match(`exec.Command($*_, "systemctl", $*_, "restart", $*_)`,
		`exec.CommandContext($*_, "systemctl", $*_, "restart", $*_)`).
		Report(`direct "systemctl restart" exec in a convergence/workflow path — competing-writers risk. The legitimate restart sites are ops procedures (cert rotation, Day-0, backup restore); convergence-loop restarts must go through the singleflight gate (see invariant service.restart_singleflight, repair_actions.go::repairVerifyRuntime)`)
}

// runSystemctlRestart catches the node_agent's internal `runSystemctl`
// helper. This wrapper exists to centralize the privilege-drop logic;
// using it from a convergence path is still a singleflight bypass.
func runSystemctlRestart(m dsl.Matcher) {
	m.Match(`runSystemctl($*_, "restart", $*_)`).
		Report(`runSystemctl with "restart" verb in a convergence/workflow path — competing-writers risk. The wrapper centralizes privilege drop but does not provide singleflight; convergence-driven restarts must go through the singleflight gate`)
}
