// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/extractbudget"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// The distinction this whole checkpoint rests on: a caller-declared limit that
// nothing reads must not be mistaken for one that binds. An unbounded run says
// so with an all-zero Budget -- which is a plainer statement than an absent
// field, and keeps the disposition available on every run -- while still
// recording what it measured.
func TestUnboundedRunSaysSoRatherThanClaimingBoundedness(t *testing.T) {
	root := deterministicFixture(t)
	doc, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	rb := doc.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt at all; the disposition must be available on every run")
	}
	if rb.Budget.Bounded() {
		t.Fatalf("an unbounded run reported a bound budget: %+v", rb.Budget)
	}
	if rb.Status != extractbudget.StatusCompleted {
		t.Errorf("status = %q, want completed", rb.Status)
	}
	if rb.Consumption.Files == 0 || rb.Consumption.Observations == 0 {
		t.Errorf("an unbounded run still has to report what it did: %+v", rb.Consumption)
	}
	if len(doc.Receipt.ResourceLimits) == 0 {
		t.Error("the legacy caller-declared limits are no longer recorded")
	}
	if len(doc.Observations) == 0 {
		t.Fatal("the fixture produced no observations; later assertions would be vacuous")
	}
}

// A receipt must carry SOME resource statement, and an enforced budget is a
// strictly stronger one than a declared string map. Before this, the only way
// past the validator was the declared map -- which is why a surface that
// bounded nothing injected the literal {"surface": "bounded"} to satisfy it.
func TestEnforcedBudgetSatisfiesTheResourceStatementRequirement(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.ResourceLimits = nil
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatalf("a run with no declared limits but a real budget receipt was refused: %v", err)
	}
	if err := investigation.Validate(doc); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(doc.Receipt.ResourceLimits) != 0 {
		t.Error("a declared limit was fabricated to satisfy the validator")
	}
}

// Adopting the contract must not change what an unbounded extraction produces.
// If it did, every existing receipt's digest would move for reasons unrelated
// to the repository it describes.
func TestBoundedAndUnboundedAgreeWhenNothingIsCut(t *testing.T) {
	root := deterministicFixture(t)
	unbounded, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxFiles: 10_000, MaxObservations: 100_000, MaxWallClock: 10 * time.Minute}
	bounded, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Observations) != len(unbounded.Observations) {
		t.Fatalf("a budget nothing reached changed the observations: %d vs %d", len(bounded.Observations), len(unbounded.Observations))
	}
	if bounded.Receipt.ResourceBudget == nil {
		t.Fatal("a bounded run produced no budget receipt")
	}
	if got := bounded.Receipt.ResourceBudget.Status; got != extractbudget.StatusCompleted {
		t.Errorf("status = %q, want completed", got)
	}
	if c := bounded.Receipt.ResourceBudget.Consumption; c.Files == 0 || c.Observations == 0 {
		t.Errorf("consumption was not measured: %+v", c)
	}
}

// The load-bearing assertion for this checkpoint: a limit that is reached must
// actually reduce the work, be reported as the reason, and say what was left
// unsearched. Recording the limit and extracting everything anyway is the
// defect the issue names.
func TestFileCeilingActuallyReducesTheSearchedSet(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxFiles: 1}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	rb := doc.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt")
	}
	if rb.Status != extractbudget.StatusBudgetExhausted {
		t.Fatalf("status = %q, want budget_exhausted", rb.Status)
	}
	if rb.Consumption.Files != 1 {
		t.Errorf("searched %d files under a ceiling of 1", rb.Consumption.Files)
	}
	if len(rb.ExhaustedDimensions) == 0 || rb.ExhaustedDimensions[0] != "max_files" {
		t.Errorf("exhausted dimensions = %v, want max_files", rb.ExhaustedDimensions)
	}

	var named bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "were NOT searched") {
			named = true
			if l.Blocking {
				t.Error("a bounded extraction's limitation must not be blocking; it is still valid evidence")
			}
		}
	}
	if !named {
		t.Fatalf("a truncated run did not say what it failed to search: %+v", doc.Limitations)
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("a partial document must remain a valid one: %v", err)
	}
}

// Scopes narrow the search and are recorded, because a run over a deliberately
// narrowed repository and a run that ran out of budget produce the same
// observations for entirely different reasons.
func TestScopesNarrowTheSearchAndAreRecorded(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{ExcludePaths: []string{"impl"}}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	rb := doc.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt")
	}
	if len(rb.ExcludePaths) != 1 || rb.ExcludePaths[0] != "impl" {
		t.Fatalf("the scope in force was not recorded: %+v", rb.ExcludePaths)
	}
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "impl/") {
			t.Fatalf("an excluded path produced an observation: %s", obs.Evidence.SourceFile)
		}
	}
	var said bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "outside the bound include/exclude scopes") {
			said = true
		}
	}
	if !said {
		t.Error("a scoped run did not disclose that files were out of scope")
	}
}

// An observation ceiling binds on the normalized set and is reported.
func TestObservationCeilingBinds(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxObservations: 2}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Observations) > 2 {
		t.Fatalf("kept %d observations under a ceiling of 2", len(doc.Observations))
	}
	if doc.Receipt.ResourceBudget.Status != extractbudget.StatusBudgetExhausted {
		t.Errorf("status = %q", doc.Receipt.ResourceBudget.Status)
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("a truncated document must stay valid: %v", err)
	}
}

// An evidence ceiling has to CUT receipts, not merely count them. A receipt
// reporting budget_exhausted over a set nothing trimmed would be the same lie
// as the string map, wearing a typed struct.
func TestEvidenceCeilingCutsReceiptsRatherThanCountingThem(t *testing.T) {
	root := deterministicFixture(t)
	full, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(full.RawEvidence) < 2 {
		t.Skip("fixture produces too little evidence for a meaningful ceiling")
	}

	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxEvidenceReceipts: 1}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.RawEvidence) != 1 {
		t.Fatalf("kept %d evidence receipts under a ceiling of 1", len(doc.RawEvidence))
	}
	if doc.Receipt.ResourceBudget.Consumption.EvidenceReceipts != 1 {
		t.Errorf("consumption disagrees with the document: %+v", doc.Receipt.ResourceBudget.Consumption)
	}
	var said bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "max_evidence_receipts") {
			said = true
		}
	}
	if !said {
		t.Error("discarded evidence was not disclosed")
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("validate: %v", err)
	}
}

// A cancelled run is reported as cancelled -- never as a zero-observation
// success, and never blamed on a budget limit it may not have reached.
// Widening a limit that was not the constraint is the wrong response, and a
// receipt that suggested it would cause exactly that.
func TestCancellationIsNotReportedAsBudgetExhaustion(t *testing.T) {
	root := deterministicFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxFiles: 1}
	doc, err := ExtractContext(ctx, root, opts)
	if err != nil {
		t.Fatalf("a cancelled run must still produce a truthful document: %v", err)
	}
	rb := doc.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt")
	}
	if rb.Status != extractbudget.StatusCancelled {
		t.Fatalf("status = %q, want cancelled (a ceiling was also reached; it must not take the blame)", rb.Status)
	}
	if len(rb.ExhaustedDimensions) != 0 {
		t.Errorf("a cancelled run named budget dimensions as the cause: %v", rb.ExhaustedDimensions)
	}
}

// A budget that cannot be honoured as written is refused before any work,
// rather than repaired into a plausible one that searches somewhere else.
func TestUnhonourableBudgetIsRefusedBeforeExtraction(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{IncludePaths: []string{"/etc"}}
	if _, err := Extract(root, opts); err == nil {
		t.Fatal("an absolute include scope was accepted")
	}
}

// End-to-end: a scope naming a directory that does not exist must not yield a
// clean "completed" document with no observations.
func TestScopeMatchingNothingIsPartialNotComplete(t *testing.T) {
	root := deterministicFixture(t)
	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{IncludePaths: []string{"nowhere"}}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	rb := doc.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt")
	}
	if rb.Status == extractbudget.StatusCompleted {
		t.Fatalf("a scope that matched no file reported %q over %d observations", rb.Status, len(doc.Observations))
	}
	if rb.Status != extractbudget.StatusPartial {
		t.Errorf("status = %q, want partial", rb.Status)
	}
	var said bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "matched no source file") {
			said = true
		}
	}
	if !said {
		t.Errorf("the document does not say the scope matched nothing: %+v", doc.Limitations)
	}
}

// A provider whose evidence the budget discarded must NOT be reported as
// "searched, no result". That sentence, in a governed document, says the
// provider looked and found nothing — when it found something the budget threw
// away. Coverage has to carry the difference or the document lies about the
// repository rather than about itself.
func TestBudgetDroppedEvidenceIsNotReportedAsNoResult(t *testing.T) {
	root := deterministicFixture(t)
	full, err := Extract(root, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}

	discovering := map[string]bool{}
	for _, rec := range full.RawEvidence {
		discovering[rec.Provider.ID] = true
	}
	if len(discovering) < 2 {
		t.Skip("fixture exercises too few providers for this to be meaningful")
	}

	opts := defaultOpts()
	opts.Budget = extractbudget.Budget{MaxEvidenceReceipts: 1}
	doc, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}

	surviving := map[string]bool{}
	for _, rec := range doc.RawEvidence {
		surviving[rec.Provider.ID] = true
	}
	var checked int
	for _, cov := range doc.Coverage {
		if !discovering[cov.ProviderID] || surviving[cov.ProviderID] {
			continue
		}
		checked++
		if cov.Status == investigation.CoverageNoResult {
			t.Errorf("provider %q found evidence the budget discarded, but coverage says searched_no_result", cov.ProviderID)
		}
		if cov.Status != investigation.CoverageSkipped {
			t.Errorf("provider %q coverage = %q, want skipped_with_reason", cov.ProviderID, cov.Status)
		}
		if !strings.Contains(cov.Reason, "discarded by the extraction budget") {
			t.Errorf("provider %q gives no reason for the skip: %q", cov.ProviderID, cov.Reason)
		}
	}
	if checked == 0 {
		t.Fatal("no provider lost all its evidence; the assertion never ran")
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("validate: %v", err)
	}
}
