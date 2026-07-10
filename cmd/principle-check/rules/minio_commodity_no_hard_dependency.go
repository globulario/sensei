// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.minio_commodity_dep
// @awareness file_role=ruleguard_rules_for_minio_is_commodity_not_a_pillar
// @awareness enforces=globular.platform:invariant.minio.is_commodity_not_a_pillar

// Ruleguard rules for the per-instance invariant
//
//	minio_commodity_no_hard_dependency
//
// (parent minio.is_commodity_not_a_pillar)
//
// The bug shape: a primary service registers MinIO as a health-gating
// dependency in its dephealth watchdog —
//
//	deps = append(deps, dephealth.Dep("minio", ...))
//
// dephealth.Watchdog.RequireHealthy() then gates ALL of that service's RPCs
// with codes.Unavailable whenever MinIO is down. But MinIO is a commodity
// object-store tier for secondary user data — an unreliable friend you don't
// lean on — NOT a pillar like etcd, scylla, or envoy. Below the 3-node
// object-store quorum the pool never forms, so gating a primary service on it
// wedges a perfectly healthy cluster.
//
// Past incident (globule-nuc, 2026-06-20): rbac registered
// dephealth.Dep("minio", ...) → MinIO (correctly) absent on a 2-node cluster →
// rbac RPCs returned Unavailable → globular-rbac.service went failed → the node
// was reported unhealthy → Day-1 convergence stalled and ~10 services never
// installed.
//
// The fix (already in rbac): MinIO removed from the watchdog; the narrow
// storageExists/storageStat lookups stay best-effort and degrade to "not found"
// when MinIO is absent. Legitimate degraded-mode MinIO use (repository's own
// depHealthWatchdog mirror tier, which never blocks reads) does NOT use
// dephealth.Dep and so does not match this pattern.
//
// There are currently ZERO legitimate dephealth.Dep("minio", ...) sites, so the
// invariant's exception_files is empty — any match is drift.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// minioHardDependency catches MinIO registered as a dephealth (stop-mode)
// dependency — the exact shape that gated rbac's RPCs on a commodity tier.
func minioHardDependency(m dsl.Matcher) {
	m.Match(
		`dephealth.Dep("minio", $*_)`,
	).Report(`MinIO registered as a health-gating dependency (dephealth.Dep("minio", ...)) — MinIO is a commodity object-store tier for secondary user data, NEVER a pillar like etcd/scylla/envoy, and must not gate a primary service's RPCs. Below the 3-node object-store quorum the MinIO pool never forms, so this wedges a healthy cluster (globule-nuc, 2026-06-20). Use MinIO best-effort / degraded, never stop-mode. See invariant minio.is_commodity_not_a_pillar`)
}
