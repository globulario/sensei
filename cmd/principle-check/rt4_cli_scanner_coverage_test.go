// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"testing"
)

// RT-4 ratchet for the raw-owner-write scanner's CLI coverage.
//
// RT-1 (docs/design/rt1-direct-write-surface-audit.md) found that golang/globularcli
// was the unswept side door where raw owner-owned etcd writes accumulated — 9+ CLI
// command paths bypassing the owner RPC, invisible because the scanner's
// actor_writer_dirs did not include the CLI. RT-4 added golang/globularcli to the
// swept set so the gate now catches a NEW CLI raw write.
//
// RT-2 / RT-4 relationship (the load-bearing semantic):
//
//	RT-2 (services #106–110) MIGRATED the globularcli raw owner-state writes onto
//	typed owner RPCs — it RESOLVED the debt RT-4 originally baselined. A baseline is
//	a quarantine tag, not a trophy: resolved debt must DISAPPEAR from the baseline,
//	and RT-4 must adapt by shrinking it. The earlier test hardcoded
//	state_cmds.go:916 `cli.Put(...)` as a real "known debt" file and asserted it
//	classified EXCEPTION; once RT-2 healed that write, the assertion became a fossil
//	(it tested the historical presence of one line, not the ratchet) and went red.
//	The behavioral ratchet is now tested against a SYNTHETIC fixture so RT-2 can keep
//	healing real debt without breaking RT-4, plus a regression that locks the new
//	truth: a resolved globularcli file must not remain baselined as EXCEPTION.
const rt4StateMutationPrincipleID = "workflow.every_state_mutation_belongs_to_a_workflow_instance"

// TestRT4_ScannerSweepsGlobularCLI is the coverage lock: golang/globularcli MUST
// stay in the raw-write scanner's actor_writer_dirs. Removing it would silently
// reopen the side door RT-1 found.
func TestRT4_ScannerSweepsGlobularCLI(t *testing.T) {
	root := requireServicesRepo(t)
	loaded, err := loadPrinciple(root, rt4StateMutationPrincipleID)
	if err != nil {
		t.Fatalf("loadPrinciple(%s): %v", rt4StateMutationPrincipleID, err)
	}
	for _, d := range loaded.ActorWriterDirs {
		if d == "golang/globularcli" {
			return
		}
	}
	t.Fatalf("golang/globularcli is not in actor_writer_dirs for %s — the CLI raw-write side door is unswept; dirs=%v",
		rt4StateMutationPrincipleID, loaded.ActorWriterDirs)
}

// TestRT4_ClassifierBucketsBaselinedAsExceptionNewAsDrift is the behavioral ratchet
// tested against a SYNTHETIC principle fixture — it proves the classifier policy (a
// baselined path is tolerated as EXCEPTION; an unlisted raw write is caught as
// DRIFT) WITHOUT depending on any real file's debt that RT-2 may heal. This is the
// durable replacement for the old TestRT4_NewCLIRawWriteIsDriftBaselinedIsException,
// which coupled to state_cmds.go's now-migrated raw write and went stale.
func TestRT4_ClassifierBucketsBaselinedAsExceptionNewAsDrift(t *testing.T) {
	// Synthetic: exactly one baselined exception pattern, nothing else.
	loaded := &loadedPrinciple{
		Exception: []patternEntry{
			{re: regexp.MustCompile(`golang/fixture/baselined_debt\.go$`), reason: "synthetic baselined debt fixture"},
		},
	}
	newRaw := site{ // an unlisted raw write — the gate must catch it
		path: "golang/fixture/newly_added_cmd.go",
		line: 1,
		text: `cli.Put(ctx, key, string(data))`,
	}
	baselined := site{ // a path named in the (synthetic) baseline — tolerated
		path: "golang/fixture/baselined_debt.go",
		line: 1,
		text: `cli.Put(ctx, key, string(data))`,
	}

	got := classify([]site{newRaw, baselined}, loaded)
	if got[0].bucket != bucketDrift {
		t.Errorf("an unlisted raw write must classify as DRIFT (gate must catch it), got %s", got[0].bucket.String())
	}
	if got[1].bucket != bucketException {
		t.Errorf("a baselined path must classify as EXCEPTION (tracked, tolerated), got %s", got[1].bucket.String())
	}
}

// TestRT4_ResolvedGlobularCLIDebtIsNotBaselined locks the new truth after RT-2:
// because RT-2 migrated state_cmds.go's raw owner-state write onto a typed owner
// RPC, the resolved file must NOT remain (or be re-added) as a baselined EXCEPTION.
// Baselines are quarantine tags, not trophies — RT-2 healing real debt must shrink
// the baseline, never leave a fixed file tolerated forever. If a future change
// re-baselines a resolved globularcli file, this fails.
func TestRT4_ResolvedGlobularCLIDebtIsNotBaselined(t *testing.T) {
	root := requireServicesRepo(t)
	loaded, err := loadPrinciple(root, rt4StateMutationPrincipleID)
	if err != nil {
		t.Fatalf("loadPrinciple(%s): %v", rt4StateMutationPrincipleID, err)
	}
	healed := site{ // RT-2 migrated this write away — it is no longer debt
		path: "golang/globularcli/state_cmds.go",
		line: 1,
		text: `cli.Put(ctx, key, string(data))`,
	}
	got := classify([]site{healed}, loaded)
	if got[0].bucket == bucketException {
		t.Errorf("state_cmds.go's raw-write debt was migrated by RT-2; it must NOT remain baselined as " +
			"EXCEPTION (baselines are quarantine tags, not trophies), got EXCEPTION")
	}
}
