// SPDX-License-Identifier: AGPL-3.0-only

package prospective

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// sentinelAnchor is a value that exists ONLY in the corpus anchors. If it
// appears anywhere in an emitted adjudication package, the package leaked
// Sensei's account of which files an item governs — which is the answer key.
const sentinelAnchor = "SENTINEL-ANCHOR-MUST-NOT-LEAK/path.go"

func testCorpus(t *testing.T) Corpus {
	t.Helper()
	c, err := NormalizeCorpus(Corpus{
		RepositoryDomain:  "github.com/globulario/sensei",
		GraphDigestSHA256: "deadbeef",
		ProducedBy:        "sensei query --mode by_class --class invariant",
		Items: []CorpusItem{
			{ID: "inv.b", Class: "invariant", Title: "B", Statement: "b holds", Anchors: []string{sentinelAnchor}},
			{ID: "inv.a", Class: "invariant", Title: "A", Statement: "a holds", Anchors: []string{sentinelAnchor}},
		},
	})
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	return c
}

func testIndex(t *testing.T, anchored ...string) AnchorIndex {
	t.Helper()
	idx, err := NormalizeAnchorIndex(AnchorIndex{
		RepositoryDomain:  "github.com/globulario/sensei",
		GraphDigestSHA256: "deadbeef",
		ProducedBy:        "sensei query --mode by_file",
		AnchoredPaths:     anchored,
	})
	if err != nil {
		t.Fatalf("anchor index: %v", err)
	}
	return idx
}

func change(id string, paths ...PathChange) Change {
	return Change{
		ID: id, Revision: id, BaseRevision: "base-" + id,
		BaseTreeDigestSHA256: "tree-" + id,
		Paths:                paths,
		ContentDigestSHA256:  mustDigest("diff of " + id),
	}
}

func mustDigest(s string) string {
	d, err := DigestOf(s)
	if err != nil {
		panic(err)
	}
	return d
}

func lookupContent(changeID string) (string, error) { return "diff of " + changeID, nil }

func newFile(p string) PathChange      { return PathChange{Path: p, ExistedBefore: false, Status: "A"} }
func existingFile(p string) PathChange { return PathChange{Path: p, ExistedBefore: true, Status: "M"} }

// A world binding is an identity, not a label. A checkout that has drifted off
// the pinned revision must refuse and NAME the drift: a run that reported an
// empty inventory instead is indistinguishable from a world with nothing in it.
func TestDriftIsRefusedAndNamed(t *testing.T) {
	wb := Bind("world1_sensei_self", "github.com/globulario/sensei", "aaaa", "tree")
	if err := VerifyRevision(wb, "aaaa"); err != nil {
		t.Fatalf("the pinned revision was refused: %v", err)
	}
	err := VerifyRevision(wb, "bbbb")
	if err == nil {
		t.Fatal("a drifted checkout was accepted, so a result would silently carry across worlds")
	}
	for _, want := range []string{"aaaa", "bbbb", "drift"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	if err := VerifyRevision(Bind("w", "d", "", ""), "aaaa"); err == nil {
		t.Fatal("a binding pinning no revision was accepted")
	}
}

// Every change lands in exactly one stratum. A change that landed in none
// would leave every denominator while still appearing in the population.
func TestStratumAssignmentIsTotalAndSingleValued(t *testing.T) {
	idx := testIndex(t, "anchored.go")
	cases := []struct {
		name string
		c    Change
		want string
	}{
		{"all new", change("c1", newFile("new.go")), StratumA},
		{"new plus unanchored", change("c2", newFile("new.go"), existingFile("unknown.go")), StratumA},
		{"existing unanchored only", change("c3", existingFile("unknown.go")), StratumB},
		{"anchored only", change("c4", existingFile("anchored.go")), StratumC},
		{"anchored plus new", change("c5", existingFile("anchored.go"), newFile("new.go")), StratumD},
		{"anchored plus unanchored", change("c6", existingFile("anchored.go"), existingFile("unknown.go")), StratumD},
	}
	for _, tc := range cases {
		got, err := Classify(tc.c, idx)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Stratum != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got.Stratum, tc.want)
		}
		found := 0
		for _, s := range Strata {
			if s == got.Stratum {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("%s: stratum %q is not exactly one member of the closed vocabulary", tc.name, got.Stratum)
		}
	}
	if _, err := Classify(change("empty"), idx); err == nil {
		t.Fatal("a change touching no path was classified rather than refused")
	}
}

// A and B answer different questions — creation-time context versus missing
// anchors generally — and the protocol's whole interpretation table rests on
// their being separate. Nothing may merge them.
func TestAAndBAreNeverMerged(t *testing.T) {
	if StratumA == StratumB {
		t.Fatal("the stratum constants collapsed")
	}
	idx := testIndex(t, "anchored.go")
	inv := mustInventory(t, idx, []Change{
		change("c1", newFile("new.go")),
		change("c2", existingFile("unknown.go")),
	})
	if len(inv.InStratum(StratumA)) != 1 || len(inv.InStratum(StratumB)) != 1 {
		t.Fatalf("A and B did not stay separate populations: A=%d B=%d",
			len(inv.InStratum(StratumA)), len(inv.InStratum(StratumB)))
	}
	if inv.StratumDigests[StratumA] == inv.StratumDigests[StratumB] {
		t.Fatal("A and B share a population digest, so one denominator could be presented as the other")
	}
	m, _, err := Build(inv, testCorpus(t), testOptions(), lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range m.Strata {
		seen[s.Stratum] = true
	}
	for _, s := range Strata {
		if !seen[s] {
			t.Fatalf("stratum %q is missing from the manifest, so an empty or weak stratum could vanish from a report", s)
		}
	}
}

func testOptions() Options {
	return Options{
		ProtocolID:           "prospective-recall-protocol-v1",
		ProtocolDigestSHA256: "ade91a42",
		Seed:                 "seed-1",
		GeneratedAt:          "2026-08-22T00:00:00Z",
		TargetPerStratum:     2,
		OverlapFraction:      0.2,
		RetrievalSurface: RetrievalSurface{
			ID: "sensei.preflight.task_and_path.v1", Invocation: "sensei preflight --file <path> --task <text>",
			Rationale: "not BY_FILE", NoChannelStatus: StatusNoProspectiveChannel,
		},
	}
}

func mustInventory(t *testing.T, idx AnchorIndex, changes []Change) Inventory {
	t.Helper()
	wb := Bind("world1_sensei_self", "github.com/globulario/sensei", "rev-1", "tree-1")
	inv, err := BuildInventory(wb, idx, changes, nil)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	return inv
}

// An exclusion is a counted row with a reason. A change that simply failed to
// appear reads as a population that never contained it, and a shrinking
// denominator is the cheapest available way to raise a recall figure.
func TestExclusionsAreCountedWithReasons(t *testing.T) {
	idx := testIndex(t, "anchored.go")
	wb := Bind("w", "d", "rev-1", "tree-1")
	prior := []Exclusion{
		{ChangeID: "m1", Reason: ExcludedNoSingleBase, Detail: "merge commit"},
		{ChangeID: "m2", Reason: ExcludedNoSingleBase, Detail: "root commit"},
		{ChangeID: "b1", Reason: ExcludedUnreconstructable, Detail: "binary"},
	}
	inv, err := BuildInventory(wb, idx, []Change{change("c1", newFile("new.go")), change("c2")}, prior)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	counts := map[string]int{}
	for _, e := range inv.ExclusionCounts() {
		counts[e.Reason] = e.Count
	}
	if counts[ExcludedNoSingleBase] != 2 || counts[ExcludedUnreconstructable] != 1 || counts[ExcludedNoPaths] != 1 {
		t.Fatalf("exclusion counts lost a reason: %#v", counts)
	}
	for _, e := range inv.Exclusions {
		if e.Reason == "" || e.ChangeID == "" {
			t.Fatalf("an exclusion carries no reason or no identity: %#v", e)
		}
	}
	if len(inv.Classified) != 1 {
		t.Fatalf("an unclassifiable change entered the population: %d classified", len(inv.Classified))
	}
}

// The draw is a pure function of the committed seed and the change identity.
// A seed recorded in the manifest but never reaching the draw would mint a new
// sample identity over an unchanged sample.
func TestSelectionIsSeededAndReproducible(t *testing.T) {
	idx := testIndex(t)
	var changes []Change
	for i := 0; i < 20; i++ {
		changes = append(changes, change(fmt.Sprintf("c%02d", i), newFile(fmt.Sprintf("f%02d.go", i))))
	}
	inv := mustInventory(t, idx, changes)
	corpus := testCorpus(t)

	first, _, err := Build(inv, corpus, testOptions(), lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	again, _, err := Build(inv, corpus, testOptions(), lookupContent)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if first.DigestSHA256 != again.DigestSHA256 {
		t.Fatal("two builds of the same inputs disagree, so the sample is not reproducible")
	}

	other := testOptions()
	other.Seed = "seed-2"
	changed, _, err := Build(inv, corpus, other, lookupContent)
	if err != nil {
		t.Fatalf("reseed: %v", err)
	}
	if sameItems(first.Items, changed.Items) {
		t.Fatal("changing the seed did not change the draw, so the seed reaches the manifest without reaching the selection")
	}
}

func sameItems(a, b []Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ChangeID != b[i].ChangeID {
			return false
		}
	}
	return true
}

// A stratum smaller than the target reports its true denominator rather than
// borrowing from another stratum to reach a round number.
func TestASmallStratumReportsItsRealDenominator(t *testing.T) {
	idx := testIndex(t, "anchored.go")
	inv := mustInventory(t, idx, []Change{
		change("c1", newFile("new.go")),
		change("c2", existingFile("anchored.go")),
		change("c3", existingFile("anchored.go"), existingFile("anchored.go")),
	})
	opts := testOptions()
	opts.TargetPerStratum = 12
	m, _, err := Build(inv, testCorpus(t), opts, lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, s := range m.Strata {
		switch s.Stratum {
		case StratumA:
			if s.Population != 1 || s.Selected != 1 || s.Status != StatusSampledAll {
				t.Fatalf("stratum A did not report its real denominator: %#v", s)
			}
			if !strings.Contains(s.Reason, "rather than topped up") {
				t.Fatalf("stratum A does not say it was not topped up: %q", s.Reason)
			}
		case StratumB:
			if s.Population != 0 || s.Status != StatusAbsent {
				t.Fatalf("an empty stratum was not reported as empty: %#v", s)
			}
		}
	}
	total := 0
	for _, s := range m.Strata {
		total += s.Selected
	}
	if total != len(m.Items) {
		t.Fatalf("selected counts (%d) disagree with emitted items (%d)", total, len(m.Items))
	}
}

// The package a human adjudicates must carry no Sensei retrieval output and no
// statement about what Sensei knows. Asserted against the serialized bytes,
// because a type can grow a field faster than a reviewer can notice one.
func TestTheAdjudicationPackageLeaksNothingSenseiKnows(t *testing.T) {
	idx := testIndex(t, "anchored.go")
	inv := mustInventory(t, idx, []Change{change("c1", newFile("new.go"), existingFile("anchored.go"))})
	_, pkgs, err := Build(inv, testCorpus(t), testOptions(), lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected one package, got %d", len(pkgs))
	}
	raw, err := json.Marshal(pkgs[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), sentinelAnchor) {
		t.Fatal("the package leaked a corpus anchor — that is Sensei's own account of which files an item governs, i.e. the answer key")
	}
	var walked map[string]any
	if err := json.Unmarshal(raw, &walked); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	forbidden := []string{"stratum", "anchors", "anchored_paths", "unanchored_paths", "new_paths", "surfaced", "retrieval", "preflight", "briefing", "applicable", "label", "verdict", "score"}
	assertNoKey(t, walked, forbidden)

	items, ok := walked["eligible_corpus"].([]any)
	if !ok || len(items) == 0 {
		t.Fatal("the package carries no eligible corpus, so the adjudicator has nothing to mark applicable")
	}
	for _, it := range items {
		obj := it.(map[string]any)
		for k := range obj {
			switch k {
			case "id", "class", "title", "statement":
			default:
				t.Fatalf("a blind corpus item carries the unexpected field %q", k)
			}
		}
	}
	if pkgs[0].Change.Content == "" || pkgs[0].DigestSHA256 == "" {
		t.Fatal("the package is missing the change contents or its identity")
	}
}

func assertNoKey(t *testing.T, node any, forbidden []string) {
	t.Helper()
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			for _, bad := range forbidden {
				if k == bad {
					t.Fatalf("the adjudication package carries the field %q, which states something about what Sensei knows or would answer", k)
				}
			}
			assertNoKey(t, child, forbidden)
		}
	case []any:
		for _, child := range v {
			assertNoKey(t, child, forbidden)
		}
	}
}

// Contents are bound to the digest the inventory froze. Otherwise the sample
// names one change while the adjudicator reads another, and every label
// collected is attached to the wrong question.
func TestContentThatDoesNotMatchTheFrozenDigestIsRefused(t *testing.T) {
	idx := testIndex(t)
	inv := mustInventory(t, idx, []Change{change("c1", newFile("new.go"))})
	_, _, err := Build(inv, testCorpus(t), testOptions(), func(string) (string, error) { return "a different diff", nil })
	if err == nil {
		t.Fatal("a package was built from contents the inventory never froze")
	}
	if !strings.Contains(err.Error(), "different changes") {
		t.Fatalf("the refusal does not explain what went wrong: %v", err)
	}
}

// The overlap subset is fixed before either adjudicator's labels exist, and
// depends only on the seed and the item keys. Selecting it later would let it
// be chosen where the two humans happened to agree.
func TestOverlapSubsetIsDeterministicAndIndependentOfLabels(t *testing.T) {
	idx := testIndex(t)
	var changes []Change
	for i := 0; i < 10; i++ {
		changes = append(changes, change(fmt.Sprintf("c%02d", i), newFile(fmt.Sprintf("f%02d.go", i))))
	}
	inv := mustInventory(t, idx, changes)
	opts := testOptions()
	opts.TargetPerStratum = 10
	m, _, err := Build(inv, testCorpus(t), opts, lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(m.OverlapItemKeys) != 2 {
		t.Fatalf("20%% of 10 items should be 2, got %d", len(m.OverlapItemKeys))
	}
	again, _, err := Build(inv, testCorpus(t), opts, lookupContent)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if strings.Join(m.OverlapItemKeys, ",") != strings.Join(again.OverlapItemKeys, ",") {
		t.Fatal("the overlap subset is not reproducible")
	}
	keys := map[string]bool{}
	for _, it := range m.Items {
		keys[it.ItemKey] = true
	}
	for _, k := range m.OverlapItemKeys {
		if !keys[k] {
			t.Fatalf("overlap names %q, which is not in the sample", k)
		}
	}
	// Rounding up matters: 20% of a small sample truncates to zero, typing the
	// overlap away exactly when one adjudicator's idiosyncrasy carries most of
	// the result.
	small := overlapSubset("seed-1", m.Items[:1], 0.2)
	if len(small) != 1 {
		t.Fatalf("a one-item sample produced %d overlap items; the fraction truncated to nothing", len(small))
	}
}

// The manifest is the artifact a later reader defends the score with. Every
// identity it depends on has to be in it.
func TestTheManifestBindsEveryIdentityItDependsOn(t *testing.T) {
	idx := testIndex(t, "anchored.go")
	inv := mustInventory(t, idx, []Change{
		change("c1", newFile("new.go")),
		change("c2", existingFile("anchored.go")),
	})
	corpus := testCorpus(t)
	m, _, err := Build(inv, corpus, testOptions(), lookupContent)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if m.ProtocolID == "" || m.ProtocolDigestSHA256 == "" {
		t.Fatal("the manifest does not name the protocol it serves")
	}
	if m.SelectionSeed != "seed-1" || m.GeneratedAt == "" {
		t.Fatal("the manifest does not carry its own selection inputs")
	}
	if m.World.DigestSHA256 == "" || m.World.Revision == "" {
		t.Fatal("the manifest does not pin its world")
	}
	if m.AnchorIndexDigestSHA256 != idx.DigestSHA256 {
		t.Fatal("the manifest does not bind the anchor index that cut its strata")
	}
	if m.CorpusDigestSHA256 != corpus.DigestSHA256 {
		t.Fatal("the manifest does not bind the eligible corpus that bounds its denominator")
	}
	if m.ClassificationRuleID != ClassificationRuleID || m.ClassificationRuleDescription == "" {
		t.Fatal("the manifest does not state the rule that cut its strata")
	}
	if m.RetrievalSurface.ID == "" || m.RetrievalSurface.NoChannelStatus != StatusNoProspectiveChannel {
		t.Fatal("the manifest does not freeze the retrieval surface decision")
	}
	for _, s := range m.Strata {
		if s.InventoryDigestSHA256 == "" {
			t.Fatalf("stratum %q publishes no population digest, so its denominator cannot be reproduced", s.Stratum)
		}
	}
	if m.DigestSHA256 == "" {
		t.Fatal("the manifest has no identity of its own")
	}
	// Nothing in the manifest may express an answer.
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var walked map[string]any
	if err := json.Unmarshal(raw, &walked); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	assertNoKey(t, walked, []string{"applicable", "not_applicable", "label", "labels", "verdict", "score", "recall", "nuisance", "surfaced"})
}

// Missing inputs fail closed rather than defaulting to something permissive.
func TestBuildRefusesIncompleteInputs(t *testing.T) {
	idx := testIndex(t)
	inv := mustInventory(t, idx, []Change{change("c1", newFile("new.go"))})
	corpus := testCorpus(t)
	for name, mutate := range map[string]func(*Options){
		"no seed":              func(o *Options) { o.Seed = "" },
		"no timestamp":         func(o *Options) { o.GeneratedAt = "" },
		"no protocol digest":   func(o *Options) { o.ProtocolDigestSHA256 = "" },
		"no retrieval surface": func(o *Options) { o.RetrievalSurface = RetrievalSurface{} },
	} {
		opts := testOptions()
		mutate(&opts)
		if _, _, err := Build(inv, corpus, opts, lookupContent); err == nil {
			t.Fatalf("%s: the build was accepted", name)
		}
	}
	if _, _, err := Build(inv, Corpus{}, testOptions(), lookupContent); err == nil {
		t.Fatal("a sample was built against an uncontent-addressed corpus")
	}
	if _, err := BuildInventory(Bind("w", "d", "r", "t"), AnchorIndex{}, nil, nil); err == nil {
		t.Fatal("an inventory was built against an anchor index with no identity")
	}
	if _, err := NormalizeAnchorIndex(AnchorIndex{AnchoredPaths: []string{"a.go"}}); err == nil {
		t.Fatal("an anchor index with no producing command was accepted")
	}
	if _, err := NormalizeCorpus(Corpus{Items: []CorpusItem{{ID: "x"}}}); err == nil {
		t.Fatal("a corpus with no producing command was accepted")
	}
}
