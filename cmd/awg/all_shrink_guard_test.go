package main

// Law 11: a generation must not be destroyed while an ACTIVE pointer, served store,
// marker or governed run still refers to it.
//
// `rebuild` guards this (cmd_rebuild.go:223 calls guardAgainstLiveShrink). `build
// --all` — the DESTRUCTIVE whole-graph replace, and the very path `import` was
// recommending on a fabricated diagnosis — did not. It printed a warning and PUT.
//
// The stakes are measured. Three stores were live on 2026-09-13 holding 237,049 /
// 142,739 / 35,268 triples, and a scoped build of the sensei-code corpus compiles to
// 35,255. So `--all` against the wrong endpoint could replace a 237k generation with
// a 35k one, and `rebuild` would have refused that exact shrink.
//
// The guard is reused, not reinvented: same function, same thresholds, same
// documented tolerance for an empty or unreachable store so cold starts are
// unaffected.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAllRefusesToClobberASubstantiallyLargerLiveStore(t *testing.T) {
	// A live store already holding far more than this build will produce.
	srv := countOnlyServer(t, 237049)
	defer srv.Close()

	root := t.TempDir()
	awareness := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(awareness, 0o755); err != nil {
		t.Fatal(err)
	}
	// A tiny corpus: whatever it compiles to will be far under half of 237,049.
	if err := os.WriteFile(filepath.Join(awareness, "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "marker.json")

	code := runBuild([]string{
		"--input", awareness, "--all",
		// Named explicitly: without it the build refuses earlier, for lacking a
		// canonical repository identity, and this test would pass without ever
		// reaching the guard it exists to exercise. That is how its first version
		// let both mutants survive.
		"--repository-identity", "github.com/globulario/sensei-code",
		"--store-url", srv.URL + "/store?default",
		"--graph-marker-file", marker,
	})
	if code == 0 {
		t.Fatal("--all replaced a substantially larger live store; law 11 requires refusal")
	}
	// And it must not have written a marker certifying a generation it refused to
	// create: a marker is the claim that a generation is active.
	if _, err := os.Stat(marker); err == nil {
		t.Error("a marker was written for a load that was refused")
	}
}

// The guard must keep its documented tolerances, or a cold start becomes impossible.
func TestBuildAllStillAllowsAColdStart(t *testing.T) {
	if err := guardAgainstLiveShrink("http://127.0.0.1:1/store?default", 35255); err != nil {
		t.Errorf("an unreachable store was treated as a shrink: %v", err)
	}
	srv := countOnlyServer(t, 0)
	defer srv.Close()
	if err := guardAgainstLiveShrink(srv.URL+"/store?default", 35255); err != nil {
		t.Errorf("an empty store was treated as a shrink: %v", err)
	}
}

// The refusal has to name the numbers, or an operator cannot tell a real shrink from
// a misdirected endpoint — which, with three live stores, is the likelier cause.
func TestTheShrinkRefusalNamesBothCounts(t *testing.T) {
	srv := countOnlyServer(t, 237049)
	defer srv.Close()
	err := guardAgainstLiveShrink(srv.URL+"/store?default", 35255)
	if err == nil {
		t.Fatal("a 237049 -> 35255 replacement was allowed")
	}
	for _, want := range []string{"237049", "35255"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal omits %s: %v", want, err)
		}
	}
}
