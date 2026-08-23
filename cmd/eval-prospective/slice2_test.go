// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prospective"
	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
	"github.com/globulario/sensei/golang/architecture/prospectivescore"
)

// The protocol's fifth arrow is enforced mechanically, not by convention: a
// run with no frozen answer key must refuse.
func TestRun_RefusesWithoutFrozenLabels(t *testing.T) {
	err := runRun(context.Background(), []string{
		"--reference-set", t.TempDir(),
		"--domain", "github.com/globulario/sensei",
		"--graph-digest", "def94857",
		"--executed-at", "2026-08-23T00:00:00Z",
		"--out", filepath.Join(t.TempDir(), "run.json"),
	})
	if err == nil {
		t.Fatal("run executed with no --labels: retrieval must not run before the answer key is frozen")
	}
	if !strings.Contains(err.Error(), "labels") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_RefusesALabelsFileThatIsNotThere(t *testing.T) {
	err := runRun(context.Background(), []string{
		"--reference-set", "../../docs/evaluation/prospective-v1-reference-set",
		"--labels", filepath.Join(t.TempDir(), "absent.json"),
		"--domain", "github.com/globulario/sensei",
		"--graph-digest", "def94857",
		"--executed-at", "2026-08-23T00:00:00Z",
		"--out", filepath.Join(t.TempDir(), "run.json"),
		"--repo", "../..",
	})
	if err == nil {
		t.Fatal("run executed against a labels file that does not exist")
	}
	if !strings.Contains(err.Error(), "frozen labels") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A bypass flag is how the one ordering constraint the experiment rests on
// gets undone by somebody in a hurry. Assert the flag set has none.
func TestRun_HasNoBypassFlag(t *testing.T) {
	forbidden := []string{"force", "skip", "no-labels", "allow-unlabelled", "unsafe", "ignore-labels", "dry-run", "no-verify", "allow-drift"}
	for _, name := range []string{"run", "score", "report"} {
		fs := flagSetFor(t, name)
		fs.VisitAll(func(f *flag.Flag) {
			for _, bad := range forbidden {
				if strings.Contains(f.Name, bad) {
					t.Fatalf("%s carries the bypass flag --%s", name, f.Name)
				}
			}
		})
	}
}

// flagSetFor drives each subcommand with an argument it must reject, and
// captures the flag set it defined. Parsing "--help" makes the command define
// its flags and stop before doing anything.
func flagSetFor(t *testing.T, name string) *flag.FlagSet {
	t.Helper()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(devnull)
	switch name {
	case "run":
		defineRunFlags(fs)
	case "score":
		defineScoreFlags(fs)
	case "report":
		defineReportFlags(fs)
	}
	return fs
}

// A MetaPrinciple node is a dual-typed meta.* invariant and reaches production
// through the invariant partition. Without the unqualified fallback every
// meta_principle item in the corpus would be permanently unsurfaceable, and
// the resulting zero would read as a retrieval failure.
func TestCorpusIndex_ResolvesMetaPrincipleSurfacedAsInvariant(t *testing.T) {
	idx := NewCorpusIndex([]string{"meta_principle:meta.fail_closed", "invariant:graph.reload_is_atomic"})

	id, rule, ok := idx.Resolve(knowledgeNode{ID: "graph.reload_is_atomic", Class: "invariant"})
	if !ok || id != "invariant:graph.reload_is_atomic" || rule != prospectivescore.MatchExact {
		t.Fatalf("qualified match failed: id=%q rule=%q ok=%v", id, rule, ok)
	}

	id, rule, ok = idx.Resolve(knowledgeNode{ID: "meta.fail_closed", Class: "invariant"})
	if !ok || id != "meta_principle:meta.fail_closed" {
		t.Fatalf("meta principle surfaced as an invariant did not resolve: id=%q ok=%v", id, ok)
	}
	if rule != prospectivescore.MatchIDOnly {
		t.Fatalf("match rule=%q, want the fallback recorded as %q", rule, prospectivescore.MatchIDOnly)
	}
}

// An ambiguous short id is not guessed at. A wrong hit is worse than a
// recorded miss, because it inflates recall silently.
func TestCorpusIndex_RefusesAnAmbiguousShortID(t *testing.T) {
	idx := NewCorpusIndex([]string{"invariant:shared.id", "contract:shared.id"})
	if _, _, ok := idx.Resolve(knowledgeNode{ID: "shared.id", Class: "failure_mode"}); ok {
		t.Fatal("an ambiguous short id was resolved to one of its candidates")
	}
	if id, _, ok := idx.Resolve(knowledgeNode{ID: "shared.id", Class: "contract"}); !ok || id != "contract:shared.id" {
		t.Fatalf("an exact qualified match must still resolve: id=%q ok=%v", id, ok)
	}
}

// The task text is composed from the frozen paths alone, deterministically.
func TestTaskTextFor_IsMechanicalAndDeterministic(t *testing.T) {
	change := prospective.BlindChange{
		ChangeID: "abc",
		Paths: []prospective.PathChange{
			{Path: "cmd/new/main.go", ExistedBefore: false, Status: "A"},
			{Path: "golang/server/reload.go", ExistedBefore: true, Status: "M"},
			{Path: "golang/server/old.go", ExistedBefore: true, Status: "D"},
		},
	}
	got := TaskTextFor(change)
	want := "add cmd/new/main.go; modify golang/server/reload.go; delete golang/server/old.go"
	if got != want {
		t.Fatalf("task text=%q, want %q", got, want)
	}
	if TaskTextFor(change) != got {
		t.Fatal("task text is not deterministic")
	}
}

// A change whose paths do not exist yet, for which production cannot be asked
// at all, is recorded as no_prospective_channel — not as an empty result and
// not as a drop.
func TestInvoke_UnaskableNewSeamIsNoProspectiveChannel(t *testing.T) {
	r := Retriever{Bin: filepath.Join(t.TempDir(), "sensei-that-does-not-exist"), Addr: "localhost:1", Domain: "d", Repo: "."}
	pkg := prospective.BlindPackage{
		ItemKey: "pr1:a",
		Change: prospective.BlindChange{
			ChangeID: "abc",
			Paths:    []prospective.PathChange{{Path: "cmd/brand/new.go", ExistedBefore: false, Status: "A"}},
		},
	}
	got := r.Invoke(context.Background(), pkg, NewCorpusIndex([]string{"invariant:x"}))
	if got.RetrievalStatus != prospectivescore.StatusNoProspectiveChannel {
		t.Fatalf("status=%s, want %s", got.RetrievalStatus, prospectivescore.StatusNoProspectiveChannel)
	}
	if len(got.Invocations) != 1 || got.Invocations[0].ExitOK {
		t.Fatalf("the failed invocation must be recorded verbatim: %+v", got.Invocations)
	}
}

// The same failure on an existing file is `unavailable`, not a claim that
// production has no channel for it.
func TestInvoke_FailureOnAnExistingPathIsUnavailable(t *testing.T) {
	r := Retriever{Bin: filepath.Join(t.TempDir(), "sensei-that-does-not-exist"), Addr: "localhost:1", Domain: "d", Repo: "."}
	pkg := prospective.BlindPackage{
		ItemKey: "pr1:b",
		Change: prospective.BlindChange{
			ChangeID: "def",
			Paths:    []prospective.PathChange{{Path: "golang/server/reload.go", ExistedBefore: true, Status: "M"}},
		},
	}
	got := r.Invoke(context.Background(), pkg, NewCorpusIndex([]string{"invariant:x"}))
	if got.RetrievalStatus != prospectivescore.StatusUnavailable {
		t.Fatalf("status=%s, want %s", got.RetrievalStatus, prospectivescore.StatusUnavailable)
	}
}

// End to end over a synthetic reference set: score and report read what the
// runner writes. Nothing here touches the real #259 reference set, and no
// retrieval is executed.
func TestScoreAndReport_EndToEndOverASyntheticReferenceSet(t *testing.T) {
	dir := t.TempDir()
	corpus := prospective.BlindCorpus{
		SchemaVersion: prospective.BlindCorpusSchemaVersion,
		Items: []prospective.BlindCorpusItem{
			{ID: "invariant:one", Class: "invariant", Title: "One", Statement: "must hold"},
			{ID: "failure_mode:two", Class: "failure_mode", Title: "Two", Statement: "broke once"},
		},
	}
	d, err := prospective.DigestOf(prospective.BlindCorpus{SchemaVersion: corpus.SchemaVersion, Items: corpus.Items})
	if err != nil {
		t.Fatal(err)
	}
	corpus.DigestSHA256 = d

	m := prospective.Manifest{
		SchemaVersion:           prospective.SchemaVersion,
		ProtocolID:              "prospective-recall-protocol-v1",
		BlindCorpusDigestSHA256: corpus.DigestSHA256,
		RetrievalSurface:        prospective.RetrievalSurface{ID: "sensei.preflight.file_and_task.v1"},
		Items: []prospective.Item{
			{ItemKey: "pr1:a", Stratum: prospective.StratumA},
		},
		Strata: []prospective.Stratum{{Stratum: prospective.StratumA, Population: 1, Target: 12, Selected: 1, Status: prospective.StatusSampledAll}},
	}
	m.World.Revision = "eac9603e"
	m.DigestSHA256 = "synthetic-manifest"

	writeTestJSON(t, filepath.Join(dir, "sample-manifest.json"), m)
	writeTestJSON(t, filepath.Join(dir, "blind-corpus.json"), corpus)
	writeTestJSON(t, filepath.Join(dir, "packages", "pr1-a.json"), prospective.BlindPackage{
		ItemKey:                 "pr1:a",
		BlindCorpusDigestSHA256: corpus.DigestSHA256,
		Change: prospective.BlindChange{
			ChangeID: "abc",
			Paths:    []prospective.PathChange{{Path: "cmd/new/main.go", ExistedBefore: false, Status: "A"}},
		},
	})

	labels := labelSetFixture(t, m, corpus)
	labelsPath := filepath.Join(dir, "labels.json")
	writeTestJSON(t, labelsPath, labels)

	run := prospectivescore.Run{
		SchemaVersion:              prospectivescore.RunSchemaVersion,
		ProtocolID:                 m.ProtocolID,
		SampleManifestDigestSHA256: m.DigestSHA256,
		BlindCorpusDigestSHA256:    corpus.DigestSHA256,
		LabelsDigestSHA256:         labels.DigestSHA256,
		WorldRevision:              m.World.Revision,
		GraphDigestSHA256:          "def94857",
		RetrievalSurface:           m.RetrievalSurface,
		ExecutedAt:                 "2026-08-23T02:00:00Z",
		Changes: []prospectivescore.ChangeRun{{
			ItemKey: "pr1:a", Stratum: prospective.StratumA,
			RetrievalStatus:  prospectivescore.StatusEmpty,
			ContextAvailable: []string{prospectivescore.CtxChangeContents},
		}},
	}
	sealedRun, err := run.Seal()
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join(dir, "run.json")
	writeTestJSON(t, runPath, sealedRun)

	scorePath := filepath.Join(dir, "score.json")
	if err := runScore([]string{"--reference-set", dir, "--labels", labelsPath, "--run", runPath, "--out", scorePath}); err != nil {
		t.Fatalf("score: %v", err)
	}
	reportPath := filepath.Join(dir, "report.md")
	if err := runReport([]string{"--reference-set", dir, "--score", scorePath, "--out", reportPath}); err != nil {
		t.Fatalf("report: %v", err)
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(body)
	// The applicable item was not surfaced, and the report must say so rather
	// than round it away.
	if !strings.Contains(report, "invariant:one") {
		t.Fatalf("the missed applicable item is absent from the report:\n%s", report)
	}
	if !strings.Contains(report, "absent (0/0)") {
		t.Fatalf("a stratum with no data must report an absent rate, not zero:\n%s", report)
	}
}

func labelSetFixture(t *testing.T, m prospective.Manifest, corpus prospective.BlindCorpus) prospectivelabel.LabelSet {
	t.Helper()
	ls := prospectivelabel.LabelSet{
		SchemaVersion:              prospectivelabel.LabelSetSchemaVersion,
		ProtocolID:                 m.ProtocolID,
		SampleManifestDigestSHA256: m.DigestSHA256,
		BlindCorpusDigestSHA256:    corpus.DigestSHA256,
		WorldRevision:              m.World.Revision,
		Adjudicator:                "test-human",
		SecondAdjudicatorStatus:    prospectivelabel.SecondAdjudicatorUnavailable,
		FrozenAt:                   "2026-08-23T01:00:00Z",
		Labels: []prospectivelabel.Label{
			{ItemKey: "pr1:a", CorpusItemID: "invariant:one", Label: prospectivelabel.LabelApplicable, AssignmentMode: prospectivelabel.ModeIndividual},
			{ItemKey: "pr1:a", CorpusItemID: "failure_mode:two", Label: prospectivelabel.LabelNotApplicable, AssignmentMode: prospectivelabel.ModeBulkSweep},
		},
	}
	sealed, err := ls.Seal()
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func writeTestJSON(t *testing.T, path string, payload any) {
	t.Helper()
	if err := writeSealedJSON(path, payload); err != nil {
		t.Fatal(err)
	}
}
