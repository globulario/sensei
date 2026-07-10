// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.graph_truth_readonly
// @awareness file_role=behavioral_guard_for_graph_truth_must_come_from_approved_corpus

// Behavioral guard for meta.graph_truth_must_come_from_approved_corpus.
//
// The principle: trusted graph truth enters ONLY through the approved corpus
// plus a deterministic rebuild — never through a live write path. The served
// gRPC surface is the authority boundary for that prohibition: if the graph
// exposed a fact-writing / store-mutating RPC, truth could enter at runtime,
// bypassing the corpus -> rebuild -> review gates that the whole governed-memory
// system depends on.
//
// This guard pins the BOUNDARY, not the implementation. It parses the served
// proto contract and fails if ANY rpc carries a mutation verb. New READ rpcs
// pass freely (the surface may grow); a new write rpc — the actual risk — fails
// and forces an explicit architecture review. The complementary corpus+rebuild
// half of the principle is held by the seed-freshness and reference-validation
// gates (meta.generated_artifacts_must_be_fresh_before_merge,
// meta.cross_repo_reference_must_be_validated).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/emicklei/proto"
)

// graphWriteVerbs mirrors protoscan's (unexported) write-verb vocabulary: a
// leading CamelCase word in this set denotes a state mutation. Kept in sync
// deliberately — if protoscan's vocabulary grows, this guard should too.
var graphWriteVerbs = map[string]bool{
	"create": true, "update": true, "delete": true, "set": true, "put": true,
	"add": true, "remove": true, "write": true, "promote": true, "apply": true,
	"mutate": true, "register": true, "deregister": true, "patch": true, "save": true,
	"restore": true, "publish": true, "start": true, "stop": true, "enable": true,
	"disable": true, "reload": true, "drain": true, "rotate": true, "sync": true,
	"push": true, "insert": true, "upsert": true, "provision": true, "deprovision": true,
	"attach": true, "detach": true, "grant": true, "revoke": true, "approve": true,
	"reject": true, "cancel": true, "retry": true, "trigger": true, "execute": true,
	"run": true, "advance": true, "rollback": true, "commit": true, "abort": true,
	"scale": true, "migrate": true, "import": true, "export": true, "install": true,
	"uninstall": true, "deploy": true, "send": true, "store": true,
}

// leadingWordLower returns the first CamelCase word of s, lowercased
// (e.g. "EditCheck" -> "edit", "StoreFacts" -> "store").
func leadingWordLower(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	out := []rune{r[0]}
	for i := 1; i < len(r); i++ {
		if unicode.IsUpper(r[i]) {
			break
		}
		out = append(out, r[i])
	}
	return strings.ToLower(string(out))
}

func TestGraphTruth_ServedSurfaceHasNoFactWritingRPC(t *testing.T) {
	path := filepath.Join(agRepoRoot(), "proto", "awareness_graph.proto")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open served proto contract %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	def, err := proto.NewParser(f).Parse()
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var services, rpcs, offenders []string
	proto.Walk(def,
		proto.WithService(func(s *proto.Service) { services = append(services, s.Name) }),
		proto.WithRPC(func(r *proto.RPC) {
			rpcs = append(rpcs, r.Name)
			if graphWriteVerbs[leadingWordLower(r.Name)] {
				offenders = append(offenders, r.Name)
			}
		}),
	)

	// Non-vacuous: the guard must have actually inspected a real surface, or a
	// renamed/moved proto would make it silently pass (a porcelain trap).
	if len(services) == 0 || len(rpcs) == 0 {
		t.Fatalf("no service/rpc parsed from %s (services=%d rpcs=%d) — guard would be vacuous",
			path, len(services), len(rpcs))
	}

	if len(offenders) > 0 {
		t.Errorf("meta.graph_truth_must_come_from_approved_corpus VIOLATED: the served AwarenessGraph "+
			"surface exposes fact-writing/store-mutating RPC(s): %v.\n"+
			"Graph truth must enter only via the approved corpus + deterministic rebuild, never a live "+
			"write path. If the RPC is genuinely read-only, rename it to a read verb; if it must mutate "+
			"the store, that is an architecture change requiring explicit review — do NOT weaken this guard.",
			offenders)
	}

	t.Logf("graph_truth read-only guard: %d service(s), %d rpc(s), 0 mutation surface — %v",
		len(services), len(rpcs), rpcs)
}
