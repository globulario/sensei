// SPDX-License-Identifier: AGPL-3.0-only

package main

// Slice G5: "one read-only command that reports the entire canonical state in one
// place... If an equivalent command already exists, fix that rather than adding
// another one. Its answer must come from the same graph authority used by production
// readers."
//
// Two commands already answer "is this graph current", and they answer it over
// DIFFERENT TRANSPORTS:
//
//	sensei metadata --domain D   asks the awareness service, resolving the endpoint
//	                            through the registry (G2) -- the production path
//	sensei seed-status          opens Oxigraph directly on --oxigraph-url, whose
//	                            default is a hardcoded port
//
// So seed-status is the most detailed report in the codebase and it describes
// whichever store answers that port, with no domain in the picture. Under law 13 a
// matching digest and count from the wrong store reads as agreement.
//
// It stays: opening the store directly is exactly what a maintenance instrument
// should do, and law 14 keeps that authority while requiring it be "explicit and
// visibly unsafe/non-canonical". What it must stop doing is looking like the
// canonical answer.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

// seed-status is a maintenance instrument and must say so, naming the command that
// does answer canonically. A reader who cannot tell which of two reports governs will
// believe whichever they ran.
// writeAgreeingMarker writes a marker that certifies exactly the served graph the
// report is being given. Without it the marker verdict complains — correctly — and the
// "everything agrees" case would be testing an incomplete fixture instead.
func writeAgreeingMarker(t *testing.T, root, digest string, triples int64) {
	t.Helper()
	path := filepath.Join(root, ".sensei", "graph-authority.json")
	m := seedmeta.Marker{IRI: "urn:sensei:test", Digest: digest, TripleCount: triples}
	if err := seedmeta.WriteMarkerFile(path, m); err != nil {
		t.Fatal(err)
	}
}

func TestSeedStatusDeclaresItselfNonCanonical(t *testing.T) {
	notice := seedStatusTransportNotice("http://localhost:7878/query")
	if notice == "" {
		t.Fatal("seed-status reports no transport caveat at all")
	}
	lower := strings.ToLower(notice)
	for _, want := range []string{"not", "canonical"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the notice does not say it is non-canonical (%q): %s", want, notice)
		}
	}
	// It must name the canonical report, not merely disclaim itself: a caveat with no
	// alternative leaves the reader with the same single answer they had.
	if !strings.Contains(notice, "sensei metadata") {
		t.Errorf("the notice does not name the canonical reporter: %s", notice)
	}
	// And it must name the endpoint it actually opened, since that is the fact the
	// caveat is about.
	if !strings.Contains(notice, "localhost:7878") {
		t.Errorf("the notice does not name the store it opened: %s", notice)
	}
}

// The consolidated verdict line. Every disagreement the canonical report can detect
// must appear in ONE place, because a reader checking a graph should not have to know
// which of five lines carries a complaint.
//
// Each case below is ISOLATED so the complaint under test is the ONLY one present. An
// earlier version asserted merely that the summary was not "none", and three mutants
// that deleted a single gatherer survived it: the other complaints in those scenarios
// kept the summary non-empty, so the assertion could not tell which gatherer ran.
func TestTheCanonicalReportGathersEveryDisagreementInOnePlace(t *testing.T) {
	const declared = "c0b660fcaaaa"
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: `+declared+`
`)
	absentRegistry := filepath.Join(t.TempDir(), "absent.yaml")

	// Everything agrees: the summary must say so POSITIVELY. "No complaints printed"
	// and "checked, and everything agrees" are different claims, and only one of them
	// is evidence a check ran.
	root := projectRoot(t, t.TempDir())
	writeAgreeingMarker(t, root, declared, 10)
	out := renderBlock(t, endpointReport{Root: root, Domain: "example.com/acme/thing",
		ResolvedAddr: "a", RegistryPath: registry, LiveDigest: declared, LiveTriples: 10})
	if !strings.Contains(out, "Disagreements:") {
		t.Fatalf("the report has no consolidated verdict line:\n%s", out)
	}
	if !strings.Contains(out, "Disagreements:       none") {
		t.Fatalf("a fully agreeing state is not reported as agreeing:\n%s", out)
	}

	cases := []struct {
		name    string
		want    string
		prepare func(t *testing.T) endpointReport
	}{{
		name: "the served graph is not the declared ACTIVE generation",
		want: "not the declared ACTIVE generation",
		prepare: func(t *testing.T) endpointReport {
			// The marker certifies what IS served, so the generation is the only
			// thing that disagrees.
			r := projectRoot(t, t.TempDir())
			writeAgreeingMarker(t, r, "999999999999", 10)
			return endpointReport{Root: r, Domain: "example.com/acme/thing", ResolvedAddr: "a",
				RegistryPath: registry, LiveDigest: "999999999999", LiveTriples: 10}
		},
	}, {
		name: "no ACTIVE generation is declared",
		want: "no ACTIVE generation is declared",
		prepare: func(t *testing.T) endpointReport {
			r := projectRoot(t, t.TempDir())
			writeAgreeingMarker(t, r, declared, 10)
			return endpointReport{Root: r, Domain: "example.com/acme/thing", ResolvedAddr: "a",
				RegistryPath: absentRegistry, LiveDigest: declared, LiveTriples: 10}
		},
	}, {
		name: "the marker certifies a different graph",
		want: "the marker DOES NOT match the served graph",
		prepare: func(t *testing.T) endpointReport {
			r := projectRoot(t, t.TempDir())
			writeAgreeingMarker(t, r, "aaaaaaaaaaaa", 7)
			return endpointReport{Root: r, Domain: "example.com/acme/thing", ResolvedAddr: "a",
				RegistryPath: registry, LiveDigest: declared, LiveTriples: 10}
		},
	}, {
		name: "no marker file can be named at all",
		want: "no marker file can be named",
		prepare: func(t *testing.T) endpointReport {
			return endpointReport{Root: "", RootError: errors.New("not inside a project"),
				Domain: "example.com/acme/thing", ResolvedAddr: "a",
				RegistryPath: registry, LiveDigest: declared, LiveTriples: 10}
		},
	}, {
		name: "an orphaned legacy marker exists",
		want: "orphaned legacy marker",
		prepare: func(t *testing.T) endpointReport {
			r := projectRoot(t, t.TempDir())
			writeAgreeingMarker(t, r, declared, 10)
			writeOrphanMarker(t, r)
			return endpointReport{Root: r, Domain: "example.com/acme/thing", ResolvedAddr: "a",
				RegistryPath: registry, LiveDigest: declared, LiveTriples: 10}
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderBlock(t, tc.prepare(t))
			if !strings.Contains(out, "Disagreements:       1\n") {
				t.Fatalf("want exactly one disagreement so this case is isolated, got:\n%s", out)
			}
			summary := out[strings.Index(out, "Disagreements:"):]
			if !strings.Contains(summary, tc.want) {
				t.Errorf("the summary does not carry %q:\n%s", tc.want, summary)
			}
		})
	}
}

// Every complaint the report prints in a per-field line must also reach the summary.
// A summary that can say "none" while a field above it says otherwise is worse than
// no summary, because it is the line a reader will trust.
func TestTheSummaryNeverContradictsTheFieldsAboveIt(t *testing.T) {
	root := projectRoot(t, t.TempDir())
	registry := writeRegistryFixture(t, `domains:
  example.com/acme/thing:
    repository_identity: acme/thing
    active_generation: c0b660fcaaaa
`)
	for name, r := range map[string]endpointReport{
		"wrong generation": {Root: root, Domain: "example.com/acme/thing", ResolvedAddr: "a", RegistryPath: registry, LiveDigest: "zzz"},
		"no pointer":       {Root: root, Domain: "example.com/acme/thing", ResolvedAddr: "a", RegistryPath: filepath.Join(t.TempDir(), "absent.yaml"), LiveDigest: "c0b660fcaaaa"},
		"no marker":        {Root: "", RootError: errors.New("nope"), Domain: "example.com/acme/thing", ResolvedAddr: "a", RegistryPath: registry, LiveDigest: "c0b660fcaaaa"},
	} {
		out := renderBlock(t, r)
		saysNone := strings.Contains(out, "Disagreements:       none")
		complained := strings.Contains(out, "DOES NOT") || strings.Contains(out, "cannot be verified") ||
			strings.Contains(out, "cannot be resolved") || strings.Contains(out, "ambiguous")
		if complained && saysNone {
			t.Errorf("%s: a field complained and the summary said none:\n%s", name, out)
		}
	}
}

func writeOrphanMarker(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".awg")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "graph-authority.json"), []byte(`{"digest":"stale"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The caveat must actually be PRINTED, in both renderings. Testing the function that
// composes the sentence proves nothing about whether anything emits it — a mutant that
// simply stopped setting the field survived exactly that gap.
func TestSeedStatusPrintsItsTransportCaveatInBothRenderings(t *testing.T) {
	res := seedStatusResult{
		SeedPath:        "/x/awareness.nt",
		QueryURL:        "http://localhost:7878/query",
		TransportNotice: seedStatusTransportNotice("http://localhost:7878/query"),
		OverallState:    "current",
	}
	var text, asJSON bytes.Buffer
	writeSeedStatusResult(&text, res, false)
	writeSeedStatusResult(&asJSON, res, true)

	// Text: at the TOP. A caveat under forty lines of detail is read after the reader
	// has already formed a conclusion.
	got := text.String()
	if !strings.Contains(got, "NOT the canonical graph state") {
		t.Errorf("the text output carries no transport caveat:\n%s", got)
	}
	if i, j := strings.Index(got, "NOT the canonical"), strings.Index(got, "Seed file:"); i > j {
		t.Errorf("the caveat appears after the detail it qualifies (%d > %d)", i, j)
	}

	// JSON: a machine reader that treats this as canonical makes the same mistake a
	// person would, so the field travels with the data.
	if !strings.Contains(asJSON.String(), "transport_notice") {
		t.Errorf("the JSON output carries no transport_notice:\n%s", asJSON.String())
	}
	if !strings.Contains(asJSON.String(), "sensei metadata") {
		t.Errorf("the JSON caveat does not name the canonical reporter:\n%s", asJSON.String())
	}
}

// The COMMAND must set the caveat, not merely be capable of composing one. The test
// above builds its own result, so a mutant that simply stopped populating the field in
// runSeedStatus survived it — the same gap in a different place: proving a sentence can
// be written proves nothing about whether anything writes it.
func TestTheSeedStatusCommandSetsTheTransportCaveat(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d", code)
	}
	seedPath, _ := seedArtifactPaths(true, agRepo)
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	storeURL := seedStatusStore(t, seedBytes)

	out := captureStdout(t, func() {
		if code := runSeedStatus([]string{
			"--json",
			"--seed", seedPath,
			"--ag-repo", agRepo,
			"--services-repo", svcRepo,
			"--oxigraph-url", storeURL,
		}); code != 0 {
			t.Fatalf("runSeedStatus code=%d", code)
		}
	})
	var got seedStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.TransportNotice == "" {
		t.Fatal("the command produced a report with no transport caveat")
	}
	if !strings.Contains(got.TransportNotice, "sensei metadata") {
		t.Errorf("the caveat does not name the canonical reporter: %s", got.TransportNotice)
	}
	// It must be about the store this invocation actually opened, not a generic line.
	if !strings.Contains(got.TransportNotice, got.QueryURL) {
		t.Errorf("the caveat names %q while the command queried %q", got.TransportNotice, got.QueryURL)
	}
}
