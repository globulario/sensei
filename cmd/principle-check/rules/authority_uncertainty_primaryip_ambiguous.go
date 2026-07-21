// SPDX-License-Identifier: AGPL-3.0-only

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.authority_uncertainty
// @awareness file_role=ruleguard_rules_for_meta_authority_must_express_uncertainty
// @awareness enforces=globular.platform:invariant.netutil.identity_getter_must_express_vip_ambiguity

// Ruleguard rules for the per-instance invariant
//
//	netutil.identity_getter_must_express_vip_ambiguity
//
// (parent meta.authority_must_express_uncertainty)
//
// The bug shape this catches: callers using `node.PrimaryIP()` for
// identity-critical comparisons (pool membership, self-identification,
// etcd endpoint registration, scylla hosts publish). PrimaryIP returns
// the floating VIP when the node holds it — and there is no signal in
// the return value telling the caller "this IP belongs to my floating
// VIP role, not my stable identity." The owner (PrimaryIP) cannot
// express the uncertainty, so the caller fabricates a stable identity
// from an unstable value.
//
// Three concrete incidents traced to this bug class:
//
//  1. VIP/keepalived evicted healthy etcd members because etcd
//     endpoint publish read PrimaryIP() — on the VIP-holder, the
//     published endpoint was the VIP, and a VIP move broke etcd
//     membership. Fix: StableIP(clusterVIP).
//
//  2. publishScyllaHostsIfNeeded historically used PrimaryIP(),
//     contaminating Scylla cluster hosts with the VIP. Fix:
//     StableIP(clusterVIP).
//
//  3. /etc/hosts entries and scylla/hosts written from PrimaryIP()
//     caused cross-node contamination when the VIP moved.
//
// The fix in every case was the same: `node.StableIP(clusterVIP)`
// returns the node's stable identity, excluding the VIP. The bug
// shape — `node.PrimaryIP()` in identity-critical context — is what
// this rule catches.
//
// Why "authority_must_express_uncertainty" is the right meta-parent:
// PrimaryIP CANNOT tell the caller "the answer depends on whether I
// hold the VIP right now" — it just returns a string. The caller
// has no way to express "I want the stable identity, not the
// transient role." StableIP forces that question to the caller
// by name. This is the principle's correctness criterion:
// authority surfaces must let callers distinguish "real value" from
// "value with hidden ambiguity."
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// primaryIPInIdentityContext catches `$_.PrimaryIP()` callsites. The
// per-instance invariant's actor_writer_dirs restricts scope to the
// dirs where this call is identity-critical (cluster controller,
// reconcilers, deploy paths). Legitimate uses (logging, display)
// would be classified under exception_files with a reason.
func primaryIPInIdentityContext(m dsl.Matcher) {
	m.Match(`$_.PrimaryIP()`).
		Report(`node.PrimaryIP() in an identity-critical context — returns the floating VIP when the node holds it, causing self-identification and pool-membership comparisons to drift when the VIP moves. Use StableIP(clusterVIP) for identity comparisons. See meta.authority_must_express_uncertainty + netutil.identity_getter_must_express_vip_ambiguity`)
}
