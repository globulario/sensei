// SPDX-License-Identifier: AGPL-3.0-only

package phase10score

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testProtocolDigest = "protocol-digest"
	testManifestID     = "manifest-declared-identity"
)

type fixture struct {
	root   string
	items  []SampleItem
	files  map[string]*LabelFile
	strata []Stratum
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{root: t.TempDir(), files: map[string]*LabelFile{}}
}

func (f *fixture) precision(t *testing.T, world, provider string, population, multiplicity int, labels ...string) {
	t.Helper()
	name := world + ".precision.labels.json"
	lf := f.files[name]
	if lf == nil {
		lf = &LabelFile{
			SchemaVersion: "sensei.eval_labels.v1", ProtocolID: "p", ProtocolDigestSHA256: testProtocolDigest,
			SampleManifestDigestSHA256: testManifestID, World: world, Lane: LanePrecision,
		}
		f.files[name] = lf
	}
	for i, l := range labels {
		key := fmt.Sprintf("%s:%s:%d:%d", world, provider, len(f.items), i)
		f.items = append(f.items, SampleItem{
			ItemKey: key, World: world, Lane: LanePrecision, ProviderID: provider, Multiplicity: multiplicity,
		})
		rec := LabelRecord{ItemKey: key, World: world, Lane: LanePrecision}
		if l != "" {
			v := l
			rec.Label = &v
		}
		lf.Labels = append(lf.Labels, rec)
		lf.ItemCount++
		if l != "" {
			lf.LabelledCount++
		}
	}
	f.populations(world, provider, population)
}

func (f *fixture) populations(world, provider string, population int) {
	for i := range f.strata {
		if f.strata[i].World == world && f.strata[i].ProviderID == provider {
			return
		}
	}
	f.strata = append(f.strata, Stratum{World: world, Lane: LanePrecision, ProviderID: provider, Population: population, Status: "sampled"})
}

func (f *fixture) write(t *testing.T) *ReferenceSet {
	t.Helper()
	worlds := map[string]bool{}
	for _, it := range f.items {
		worlds[it.World] = true
	}
	m := SampleManifest{
		SchemaVersion: "sensei.eval_sample_manifest.v1", ProtocolID: "p",
		ProtocolDigestSHA256: testProtocolDigest, SelectionSeed: "seed",
		Items: f.items, Strata: f.strata, DigestSHA256: testManifestID,
	}
	for w := range worlds {
		m.Worlds = append(m.Worlds, World{World: w, RepositoryDomain: "example.test/" + w, Revision: "rev", WorldBindingDigestSHA256: "wb-" + w})
	}
	mustWrite(t, filepath.Join(f.root, "sample", "sample-manifest.json"), m)
	for name, lf := range f.files {
		mustWrite(t, filepath.Join(f.root, "labels", name), lf)
	}
	mustWrite(t, filepath.Join(f.root, "adjudicator-overlap.json"), Overlap{
		SchemaVersion: "sensei.eval_overlap.v1", ProtocolID: "p", ProtocolDigestSHA256: testProtocolDigest,
		SampleManifestDigestSHA256: testManifestID, TotalOverlapItems: 0,
		Worlds: map[string]struct {
			ItemKeys []string `json:"item_keys"`
		}{},
	})
	rs, err := Load(f.root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rs
}

func mustWrite(t *testing.T, path string, payload any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(payload, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func world(t *testing.T, s Score, name string) WorldScore {
	t.Helper()
	for _, w := range s.Worlds {
		if w.World == name {
			return w
		}
	}
	t.Fatalf("world %s missing from the score", name)
	return WorldScore{}
}

// Only `supported` and `unsupported` enter the primary precision denominator.
// Every other label is present in this fixture and none may move the ratio.
func TestCompute_PrecisionDenominatorExcludesUnresolvedLabels(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "contract_extractor", 100, 1,
		LabelSupported, LabelSupported, LabelUnsupported,
		LabelAmbiguous, LabelOutsideScope, LabelCannotAdjudicate, "")
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	p := world(t, s, "w1").Providers[0]
	if p.Precision.Denominator != 3 || p.Precision.Numerator != 2 {
		t.Fatalf("precision=%d/%d, want 2/3", p.Precision.Numerator, p.Precision.Denominator)
	}
	c := p.Labels
	if c.Ambiguous != 1 || c.OutsideScope != 1 || c.CannotAdjudicate != 1 || c.Unlabelled != 1 {
		t.Fatalf("the excluded labels were collapsed somewhere: %+v", c)
	}
}

// A high-volume provider must not mathematically erase a weak thin one. The
// macro figure is the headline precisely because the micro figure can.
func TestCompute_MacroKeepsAThinProviderVisibleWhereMicroDoesNot(t *testing.T) {
	f := newFixture(t)
	// A dominant provider, perfect, each sampled row standing for 1000 emissions.
	f.precision(t, "w1", "state_extractor", 160000, 1000, LabelSupported, LabelSupported, LabelSupported, LabelSupported)
	// A thin architecture-relevant provider, wrong every time, one emission each.
	f.precision(t, "w1", "contract_extractor", 4, 1, LabelUnsupported, LabelUnsupported, LabelUnsupported, LabelUnsupported)
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	w := world(t, s, "w1")
	if *w.MacroPrecision.Value != 0.5 {
		t.Fatalf("macro precision=%v, want 0.5 — the two strata weigh the same", *w.MacroPrecision.Value)
	}
	if *w.MicroPrecision.Value <= 0.9 {
		t.Fatalf("micro precision=%v; the fixture must show micro flattering the dominant provider", *w.MicroPrecision.Value)
	}
	if len(w.Providers) != 2 {
		t.Fatalf("both provider strata must stay visible, got %d", len(w.Providers))
	}
}

// An unlabelled reference set has no precision. Reporting 0.0 would read as
// total failure of the system being graded.
func TestCompute_UnlabelledSetReportsAbsentNotZero(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, "", "", "")
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	w := world(t, s, "w1")
	if w.MacroPrecision.Value != nil {
		t.Fatalf("macro precision=%v, want absent", *w.MacroPrecision.Value)
	}
	if w.MacroPrecision.Availability != NoAdjudicableSample {
		t.Fatalf("availability=%s, want %s", w.MacroPrecision.Availability, NoAdjudicableSample)
	}
	if s.HeadlineMacroPrecision.Value != nil {
		t.Fatal("a headline was produced from no adjudicable sample")
	}
}

// Primary recall needs the frozen expected-fact set. Where the container
// cannot hold one, the metric is reported missing with the reason — never
// silently scored, and never omitted.
func TestCompute_RecallReportsContainerGapRatherThanAZero(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, LabelSupported)
	name := "w1.recall_unit.labels.json"
	f.files[name] = &LabelFile{
		SchemaVersion: "sensei.eval_labels.v1", ProtocolID: "p", ProtocolDigestSHA256: testProtocolDigest,
		SampleManifestDigestSHA256: testManifestID, World: "w1", Lane: LaneRecallUnit,
		ItemCount: 1,
		Labels:    []LabelRecord{{ItemKey: "w1:recall:0", World: "w1", Lane: LaneRecallUnit}},
	}
	f.items = append(f.items, SampleItem{ItemKey: "w1:recall:0", World: "w1", Lane: LaneRecallUnit, Multiplicity: 1})
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	w := world(t, s, "w1")
	if w.Recall.Availability != NotCapturedByContainer {
		t.Fatalf("recall availability=%s, want %s", w.Recall.Availability, NotCapturedByContainer)
	}
	if w.Recall.Value != nil {
		t.Fatalf("recall=%v, want absent", *w.Recall.Value)
	}
	found := false
	for _, u := range s.Uncomputable {
		if strings.Contains(u, "recall") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the uncomputable list does not name recall: %+v", s.Uncomputable)
	}
}

// With expected facts present, recall is the protocol's ratio over
// expected_supported only.
func TestCompute_RecallCountsExpectedSupportedOnly(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, LabelSupported)
	yes, no := true, false
	f.files["w1.recall_unit.labels.json"] = &LabelFile{
		SchemaVersion: "sensei.eval_labels.v1", ProtocolID: "p", ProtocolDigestSHA256: testProtocolDigest,
		SampleManifestDigestSHA256: testManifestID, World: "w1", Lane: LaneRecallUnit, ItemCount: 1, LabelledCount: 1,
		Labels: []LabelRecord{{
			ItemKey: "w1:recall:0", World: "w1", Lane: LaneRecallUnit,
			ExpectedFacts: []ExpectedFact{
				{ID: "f1", State: ExpectedSupported, Matched: &yes},
				{ID: "f2", State: ExpectedSupported, Matched: &no},
				{ID: "f3", State: ExpectedAmbiguous, Matched: &no},
				{ID: "f4", State: ExpectedOutsideScope, Matched: &no},
			},
		}},
	}
	f.items = append(f.items, SampleItem{ItemKey: "w1:recall:0", World: "w1", Lane: LaneRecallUnit, Multiplicity: 1})
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	w := world(t, s, "w1")
	if w.Recall.Numerator != 1 || w.Recall.Denominator != 2 {
		t.Fatalf("recall=%d/%d, want 1/2 over expected_supported only", w.Recall.Numerator, w.Recall.Denominator)
	}
}

// Section 13's absence is typed, never manufactured agreement.
func TestCompute_SecondAdjudicatorAbsenceIsTyped(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, LabelSupported)
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if s.SecondAdjudicator.Availability != SecondAdjudicatorUnavailable {
		t.Fatalf("second adjudicator=%s, want %s", s.SecondAdjudicator.Availability, SecondAdjudicatorUnavailable)
	}
	if s.SecondAdjudicator.RawAgreement.Value != nil {
		t.Fatal("an agreement figure was produced with one adjudicator")
	}
}

// The section 17 identity moves when a label file's bytes move. A score that
// could not tell two label sets apart could not name what it consumed.
func TestReferenceSetDigest_TracksLabelFileBytes(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, LabelSupported)
	rs := f.write(t)
	before, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	lf := f.files["w1.precision.labels.json"]
	other := LabelUnsupported
	lf.Labels[0].Label = &other
	mustWrite(t, filepath.Join(f.root, "labels", "w1.precision.labels.json"), lf)
	rs2, err := Load(f.root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Compute(rs2, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if before.ReferenceSetDigestSHA256 == after.ReferenceSetDigestSHA256 {
		t.Fatal("the reference-set digest did not change when a label did")
	}
}

// Labels bound to another sample are answers to a question nobody asked.
func TestLoad_RefusesLabelsBoundToAnotherManifest(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "state_extractor", 10, 1, LabelSupported)
	f.files["w1.precision.labels.json"].SampleManifestDigestSHA256 = "some-other-manifest"
	rs, err := Load(f.rootAfterWrite(t))
	if err == nil {
		t.Fatalf("a mismatched label container was accepted: %+v", rs)
	}
	if !strings.Contains(err.Error(), "different sample") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// rootAfterWrite writes the fixture and returns its root without loading it.
func (f *fixture) rootAfterWrite(t *testing.T) string {
	t.Helper()
	m := SampleManifest{
		SchemaVersion: "sensei.eval_sample_manifest.v1", ProtocolID: "p",
		ProtocolDigestSHA256: testProtocolDigest, SelectionSeed: "seed",
		Items: f.items, Strata: f.strata, DigestSHA256: testManifestID,
		Worlds: []World{{World: "w1", RepositoryDomain: "example.test/w1", Revision: "rev", WorldBindingDigestSHA256: "wb"}},
	}
	mustWrite(t, filepath.Join(f.root, "sample", "sample-manifest.json"), m)
	for name, lf := range f.files {
		mustWrite(t, filepath.Join(f.root, "labels", name), lf)
	}
	return f.root
}

// The report must carry section 20's required fields and say outright that no
// aggregate in it is sufficient evidence for completion.
func TestRender_CarriesSection20Requirements(t *testing.T) {
	f := newFixture(t)
	f.precision(t, "w1", "contract_extractor", 100, 1, LabelSupported, LabelUnsupported, LabelAmbiguous)
	rs := f.write(t)
	s, err := Compute(rs, nil)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	out := Render(s)
	for _, want := range []string{
		s.ReferenceSetDigestSHA256, s.SampleManifestDeclaredID, s.SampleManifestFileDigest,
		"contract_extractor", "macro precision", "micro precision", "unsupported rate",
		"Operator burden", "Second adjudicator", "contradiction preservation",
		"optional-model delta", "Metrics this reference-set version cannot produce",
		"No single aggregate score in this report is sufficient evidence",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report omits %q", want)
		}
	}
}
