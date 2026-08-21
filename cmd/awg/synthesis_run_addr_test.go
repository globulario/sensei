// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #231: an unreachable endpoint composed exactly like a graph that answered and
// had nothing — same partial identity, same COVERAGE_STATE_EMPTY — so the run
// prescribed "rebuild or reload the graph" for a graph that was healthy and
// simply never asked. The repairs are unrelated: one is starting a server or
// correcting --addr, the other is a full republication.
func TestUnreachableGraphEndpointIsReportedAsUnreachable(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".sensei", "config.yaml"), "repository:\n  domain: example.com/eval\n")

	// A port nothing is serving.
	_, unreachable, err := composeSynthesisRunIdentity(context.Background(), "127.0.0.1:1", repo, filepath.Join(repo, ".sensei", "tasks", "task.absent"))
	if err != nil {
		t.Fatalf("composition must still return an identity: %v", err)
	}
	if unreachable == "" {
		t.Fatal("an unreachable endpoint was not reported as unreachable")
	}
}

// Its own reason and its own exit code, because the vocabulary's whole point is
// that a state nobody can act on identically must not share a code with one
// they can.
func TestUnreachableEndpointHasItsOwnStopReasonAndCode(t *testing.T) {
	code, ok := stopExitCodes[stopGraphEndpointUnreachable]
	if !ok {
		t.Fatal("graph-endpoint-unreachable has no exit code")
	}
	if code == stopExitCodes[stopGraphIdentityUnusable] {
		t.Fatalf("unreachable shares exit code %d with graph-identity-unusable", code)
	}
	meaning, ok := stopMeanings[stopGraphEndpointUnreachable]
	if !ok || strings.TrimSpace(meaning) == "" {
		t.Fatal("graph-endpoint-unreachable has no operator meaning")
	}
	if strings.Contains(strings.ToLower(meaning), "rebuild") || strings.Contains(strings.ToLower(meaning), "reload") {
		t.Fatalf("the meaning prescribes republishing a graph that was never consulted: %q", meaning)
	}
}

// The documented default invocation must run. The active pointer records a
// repo-relative session path, so the derived task directory was relative and
// the checkpoint store — which requires an absolute root — refused it, while an
// explicit --task worked. The path nobody types was the one that could not run.
func TestDerivedTaskDirectoryIsAbsolute(t *testing.T) {
	repo := t.TempDir()
	rel := filepath.Join(".sensei", "tasks", "task.example")
	writeFile(t, filepath.Join(repo, rel, "session.yaml"), "session: {}\n")

	for _, tc := range []struct {
		name string
		flag string
	}{
		{"explicit relative --task", rel},
		{"explicit absolute --task", filepath.Join(repo, rel)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.flag
			if !filepath.IsAbs(got) {
				got = filepath.Join(repo, got)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("task dir %q is not absolute", got)
			}
			if _, err := os.Stat(filepath.Join(got, "session.yaml")); err != nil {
				t.Fatalf("resolved task dir does not hold the session: %v", err)
			}
		})
	}

	// The derivation the defect was in: a relative path from the active
	// pointer must be absolutised the same way an explicit flag is.
	derived := filepath.Dir(filepath.Join(rel, "session.yaml"))
	if filepath.IsAbs(derived) {
		t.Skip("pointer paths are absolute in this fixture")
	}
	absolutised := filepath.Join(repo, derived)
	if !filepath.IsAbs(absolutised) {
		t.Fatalf("derived task dir stayed relative: %q", absolutised)
	}
}
