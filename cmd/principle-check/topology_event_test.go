// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.topology_event
// @awareness file_role=artifact_gate_for_meta_topology_change_is_first_class_event

// Artifact gate for meta.topology_change_is_a_first_class_event.
//
// Topology-mutating operations (node join, node remove, profile change,
// etcd member add/remove, MinIO pool resize, ScyllaDB topology change)
// MUST emit a cluster event. If a topology change happens silently,
// downstream consumers (doctor rules, xDS reconciler, DNS publisher,
// monitoring) cannot react — they discover the change only on their
// next periodic sweep, which may be minutes later.
//
// The gate scans Go source files in the controller for topology-
// mutating function signatures and verifies that each file also
// contains an event emission call (emitClusterEvent or PublishEvent).
// Files that perform topology mutations without emitting events are
// flagged.
//
// Carve-outs exist for files where the event is emitted by the CALLER
// (not the mutating function itself) — documented with a reason.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// topologyMutationPatterns are function/method signatures that indicate
// a topology-mutating operation. If a file contains one of these, it
// should also contain an event emission.
var topologyMutationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`func.*[Rr]emoveNode`),
	regexp.MustCompile(`func.*[Aa]ddNode`),
	regexp.MustCompile(`func.*[Jj]oinNode`),
	regexp.MustCompile(`func.*[Ss]etNodeProfiles`),
	regexp.MustCompile(`func.*[Aa]ddEtcdMember`),
	regexp.MustCompile(`func.*[Rr]emoveEtcdMember`),
	regexp.MustCompile(`func.*[Rr]esizePool`),
	regexp.MustCompile(`func.*[Aa]pplyTopology`),
	regexp.MustCompile(`func.*[Rr]econcileTopology`),
}

// eventEmissionPatterns indicate a cluster event is being emitted.
var eventEmissionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`emitClusterEvent`),
	regexp.MustCompile(`PublishEvent`),
	regexp.MustCompile(`publishEvent`),
	regexp.MustCompile(`event\.Publish`),
	regexp.MustCompile(`eventClient\.\w+`),
}

// Files where topology mutation happens but the event is emitted by the
// caller or a separate coordination path. Each must carry a reason.
var topologyEventCarveOuts = map[string]string{
	"etcd_members.go":         "etcd member add/remove is called by the join workflow actor; the actor emits the event after the RPC succeeds. The low-level function is intentionally event-free to avoid double-emit.",
	"reconcile_minio.go":      "MinIO topology apply is orchestrated by the reconciler tick; topology-change events are emitted by the reconciler after the apply succeeds, not inside the apply function.",
	"profiles_normalize.go":   "Profile normalization is a pure computation (no side effects); the caller (SetNodeProfiles RPC handler) emits the event.",
	"bootstrap_phases.go":     "Bootstrap is Day-0 — the event service may not be running yet. Events are emitted post-bootstrap by the first reconciler tick.",
	"repair_node_workflow.go": "repairRejoinNode is called from the repair workflow run; the workflow step receipt IS the event. The repair workflow's completion event is emitted by the workflow engine.",
	"scylla_ring_remove.go":   "removeNodeFromScyllaRing is called from the node-removal workflow actor; the actor emits the topology event after the ring operation succeeds.",
	"scylla_schema_guard.go":  "callScyllaRemoveNode is a low-level Scylla nodetool wrapper called from scylla_ring_remove.go; the event is emitted by the caller's caller (the workflow actor).",
	"day1_join_isolation.go":  "activeDay1JoinNode is a read-only predicate — it reports whether a node is still in the Day-1 bootstrap lane and mutates no topology. Its name coincidentally matches the JoinNode pattern; there is no mutation, so there is no event to emit.",
}

func TestTopologyChangeEmitsEvent(t *testing.T) {
	repo := requireServicesRepo(t)
	controllerDir := filepath.Join(repo, "golang", "cluster_controller", "cluster_controller_server")

	entries, err := os.ReadDir(controllerDir)
	if err != nil {
		t.Fatalf("read controller dir: %v", err)
	}

	total := 0
	emitting := 0
	carved := 0
	var silent []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(controllerDir, e.Name()))
		if err != nil {
			continue
		}
		content := string(raw)

		// Check if this file has any topology-mutating function.
		hasMutation := false
		for _, pat := range topologyMutationPatterns {
			if pat.MatchString(content) {
				hasMutation = true
				break
			}
		}
		if !hasMutation {
			continue
		}

		total++
		baseName := e.Name()

		if reason, ok := topologyEventCarveOuts[baseName]; ok {
			carved++
			t.Logf("CARVE-OUT: %s — %s", baseName, reason)
			continue
		}

		// Check if the file also emits an event.
		hasEvent := false
		for _, pat := range eventEmissionPatterns {
			if pat.MatchString(content) {
				hasEvent = true
				break
			}
		}

		if hasEvent {
			emitting++
		} else {
			silent = append(silent, baseName)
		}
	}

	for _, name := range silent {
		t.Errorf("file %s contains topology-mutating operations but no event emission. "+
			"Topology changes must emit a cluster event so downstream consumers (doctor, xDS, DNS, "+
			"monitoring) can react promptly instead of discovering the change on their next sweep. "+
			"Add emitClusterEvent() after the mutation, or add the file to topologyEventCarveOuts "+
			"with a reason if the event is emitted by the caller. "+
			"See meta.topology_change_is_a_first_class_event.", name)
	}

	t.Logf("topology event coverage: %d topology-mutating files, %d with events, %d carved-out, %d silent",
		total, emitting, carved, len(silent))
}
