// SPDX-License-Identifier: AGPL-3.0-only

package briefingfeedback

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// ---------------------------------------------------------------------------
// Test fixtures: a valid verified promotion + injectable deps. These exercise
// the owner's classification/scope/debris matrix without a real tampered
// promotion world; the questionpromotion boundary is faked, never bypassed.
// ---------------------------------------------------------------------------

const (
	testDomain = "github.com/globulario/sensei"
	testFile   = "golang/server/reload.go"
)

// validReceipt is a structurally complete receipt bound to (testDomain, testFile).
func validReceipt(lineage string) qp.QuestionPromotionReceipt {
	return qp.QuestionPromotionReceipt{
		Task:                                   closureprotocol.TaskBinding{ID: "task.origin", SessionID: "session.origin"},
		QuestionDispositionReceiptDigestSHA256: "disp-" + lineage,
		QuestionID:                             "q." + lineage,
		AnswerID:                               "a." + lineage,
		GovernedTargetKind:                     "failure_mode",
		CanonicalRecordID:                      "fm." + lineage,
		SourceDocument:                         "docs/awareness/failure_modes.yaml",
		ReceiptDigestSHA256:                    "rcpt-" + lineage,
		EffectiveScopeDomain:                   testDomain,
		EffectiveScopeFiles:                    []string{testFile},
		GovernedNodeIRI:                        "aw:FailureMode/fm." + lineage,
	}
}

func verified(lineage string, mutate func(*qp.QuestionPromotionReceipt)) qp.VerifiedPromotion {
	rc := validReceipt(lineage)
	if mutate != nil {
		mutate(&rc)
	}
	return qp.VerifiedPromotion{
		PromotionLineageID: lineage,
		Receipt:            rc,
		GovernedNodeIRI:    rc.GovernedNodeIRI,
	}
}

// relevantDesc is a readable descriptor whose untrusted claims route to the scope.
func relevantDesc(lineage string) qp.CandidateDescriptor {
	return qp.CandidateDescriptor{
		LineageID:     lineage,
		ClaimedDomain: testDomain,
		ClaimedFiles:  []string{testFile},
		ClaimedTaskID: "task.origin",
		Readable:      true,
	}
}

type outcome struct {
	vp  qp.VerifiedPromotion
	err error
}

// fakeDeps builds an injectable deps from an ordered lineage list, a per-lineage
// verification outcome map, and an optional descriptor map (default: relevant).
func fakeDeps(lineages []string, out map[string]outcome, desc map[string]qp.CandidateDescriptor) deps {
	return deps{
		resolveRoot: func(r string) (string, error) { return r, nil },
		discover:    func(string) ([]string, error) { return lineages, nil },
		descriptor: func(_, lin string) qp.CandidateDescriptor {
			if desc != nil {
				if d, ok := desc[lin]; ok {
					return d
				}
			}
			return relevantDesc(lin)
		},
		verify: func(_ context.Context, _, lin string) (qp.VerifiedPromotion, error) {
			o := out[lin]
			return o.vp, o.err
		},
	}
}

func validReq() Request {
	return Request{
		RepositoryRoot:     "/repo",
		RepositoryIdentity: testDomain,
		RequestedDomain:    testDomain,
		RequestedFiles:     []string{testFile},
		Task:               &TaskBinding{TaskID: "task.origin", SessionID: "session.origin", RepositoryDomain: testDomain, Files: []string{testFile}},
	}
}

func mustBuild(t *testing.T, req Request, d deps) Projection {
	t.Helper()
	p, err := build(context.Background(), req, d)
	if err != nil {
		t.Fatalf("build returned a Go error (should be impossible): %v", err)
	}
	if verr := ValidateProjection(p); verr != nil {
		t.Fatalf("build produced an invalid projection: %v", verr)
	}
	return p
}

// ---------------------------------------------------------------------------
// Happy path & projection shape
// ---------------------------------------------------------------------------

func TestBuild_VerifiedAdmittedRecord(t *testing.T) {
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil)
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackAvailable {
		t.Fatalf("availability = %q, want feedback_available", p.Availability)
	}
	if len(p.Records) != 1 || len(p.Findings) != 0 {
		t.Fatalf("want 1 record 0 findings, got %d records %d findings", len(p.Records), len(p.Findings))
	}
	r := p.Records[0]
	if r.VerificationClass != PromotionVerified {
		t.Fatalf("record class = %q, want promotion_verified", r.VerificationClass)
	}
	if r.PromotionLineageID != "L1" || r.QuestionID != "q.L1" || r.AnswerID != "a.L1" {
		t.Fatalf("lineage provenance not preserved: %+v", r)
	}
	if r.EffectiveDomain != testDomain || len(r.EffectiveFileScope) != 1 || r.EffectiveFileScope[0] != testFile {
		t.Fatalf("verified scope not preserved exactly: %+v", r)
	}
	if r.ProvenanceInterpretation != provenanceInterpretation {
		t.Fatalf("provenance interpretation not stamped")
	}
	if !p.NonAuthoritativeProjection || p.Bound != BoundStatement {
		t.Fatalf("non-authoritative bound not asserted")
	}
}

func TestBuild_NoPromotionsIsEmpty(t *testing.T) {
	d := fakeDeps(nil, nil, nil)
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackEmpty {
		t.Fatalf("availability = %q, want feedback_empty", p.Availability)
	}
	if len(p.Records) != 0 || len(p.Findings) != 0 {
		t.Fatalf("empty scope must carry no records/findings")
	}
}

func TestBuild_DomainNeutralPromotionAdmittedOnFileScope(t *testing.T) {
	d := fakeDeps([]string{"L1"}, map[string]outcome{
		"L1": {vp: verified("L1", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeDomain = "" })},
	}, nil)
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackAvailable || len(p.Records) != 1 {
		t.Fatalf("domain-neutral promotion must qualify on exact file scope: %q recs=%d", p.Availability, len(p.Records))
	}
	if p.Records[0].EffectiveDomain != "" {
		t.Fatalf("domain-neutral record must keep empty effective domain")
	}
}

// ---------------------------------------------------------------------------
// Verified-scope admission law (exact identity, no fallback)
// ---------------------------------------------------------------------------

func TestBuild_ScopeAdmissionLaw(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*qp.QuestionPromotionReceipt)
		req    func(Request) Request
		admit  bool
	}{
		{"exact match admits", nil, nil, true},
		{"foreign domain excluded", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeDomain = "github.com/other/repo" }, nil, false},
		{"domain case mismatch excluded", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeDomain = strings.ToUpper(testDomain) }, nil, false},
		{"absent effective file scope never global", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeFiles = nil }, nil, false},
		{"disjoint file scope excluded", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeFiles = []string{"golang/other/x.go"} }, nil, false},
		{"domain-scoped promotion under empty requested domain excluded", nil, func(r Request) Request {
			r.RequestedDomain = ""
			return r
		}, false},
		{"task-file intersection admits", nil, func(r Request) Request {
			r.RequestedFiles = nil // rely on the task file set only
			return r
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq()
			if tc.req != nil {
				req = tc.req(req)
			}
			d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", tc.mutate)}}, nil)
			p := mustBuild(t, req, d)
			got := len(p.Records) == 1
			if got != tc.admit {
				t.Fatalf("admit = %v, want %v (avail %q recs %d)", got, tc.admit, p.Availability, len(p.Records))
			}
			// An out-of-scope verified promotion is silently excluded — never debris.
			if !tc.admit && len(p.Findings) != 0 {
				t.Fatalf("out-of-scope verified promotion must not produce findings, got %d", len(p.Findings))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Typed failure classification (never error text) + relevant-debris routing
// ---------------------------------------------------------------------------

func TestBuild_TypedFailureClassification(t *testing.T) {
	cases := []struct {
		class qp.VerificationFailureClass
		want  FindingClass
		avail Availability
	}{
		{qp.VerifyIncomplete, PromotionIncomplete, FeedbackDegraded},
		{qp.VerifyIntegrityFailure, PromotionIntegrityFailure, FeedbackDegraded},
		{qp.VerifyStale, PromotionStale, FeedbackDegraded},
		{qp.VerifyUnverifiable, PromotionUnverifiable, FeedbackDegraded},
	}
	for _, tc := range cases {
		t.Run(string(tc.class), func(t *testing.T) {
			verr := verErr(tc.class, "reason_x")
			d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: verr}}, nil)
			p := mustBuild(t, validReq(), d)
			if p.Availability != tc.avail {
				t.Fatalf("availability = %q, want %q", p.Availability, tc.avail)
			}
			if len(p.Findings) != 1 || p.Findings[0].Class != tc.want {
				t.Fatalf("finding class = %+v, want %q", p.Findings, tc.want)
			}
			if p.Findings[0].ReasonCode != "reason_x" {
				t.Fatalf("reason code not carried from typed error: %q", p.Findings[0].ReasonCode)
			}
			// The finding must never embed the raw human-readable error text.
			if strings.Contains(p.Findings[0].Detail, "boom") {
				t.Fatalf("finding must not carry raw error text")
			}
		})
	}
}

func TestBuild_UntypedFailureIsUnknownClassificationAndInvalid(t *testing.T) {
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: errors.New("mystery failure")}}, nil)
	p := mustBuild(t, validReq(), d)
	if len(p.Findings) != 1 || p.Findings[0].Class != PromotionUnknownClassification {
		t.Fatalf("untyped error must map to promotion_unknown_classification: %+v", p.Findings)
	}
	if p.Availability != FeedbackInvalid {
		t.Fatalf("unknown classification must fail closed to feedback_invalid, got %q", p.Availability)
	}
	if strings.Contains(p.Findings[0].ReasonCode+p.Findings[0].Detail, "mystery") {
		t.Fatalf("unknown classification must not parse the error text")
	}
}

// ---------------------------------------------------------------------------
// Unrelated broken-promotion debris isolation
// ---------------------------------------------------------------------------

func TestBuild_UnrelatedFailureIsDroppedNotDegraded(t *testing.T) {
	// An integrity failure whose untrusted claim targets a different domain/file
	// must not appear as a scoped finding and must not degrade this scope.
	desc := map[string]qp.CandidateDescriptor{
		"L1": {LineageID: "L1", ClaimedDomain: "github.com/foreign/repo", ClaimedFiles: []string{"z/z.go"}, Readable: true},
	}
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: verErr(qp.VerifyIntegrityFailure, "tampered")}}, desc)
	p := mustBuild(t, validReq(), d)
	if len(p.Findings) != 0 {
		t.Fatalf("unrelated broken promotion must be isolated, got findings %+v", p.Findings)
	}
	if p.Availability != FeedbackEmpty {
		t.Fatalf("unrelated debris must not degrade scope: %q", p.Availability)
	}
}

func TestBuild_CrossTaskFailureIsStillRelevant(t *testing.T) {
	// A broken promotion whose ORIGINATING task differs from the consuming task, but
	// which targets a requested file, is still relevant debris (governed promotions are
	// reusable across tasks; originating task id is provenance, not a relevance filter).
	desc := map[string]qp.CandidateDescriptor{
		"L1": {LineageID: "L1", ClaimedDomain: testDomain, ClaimedFiles: []string{testFile}, ClaimedTaskID: "task.some.other", Readable: true},
	}
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: verErr(qp.VerifyStale, "drift")}}, desc)
	p := mustBuild(t, validReq(), d)
	if len(p.Findings) != 1 || p.Findings[0].Class != PromotionStale {
		t.Fatalf("cross-task relevant failure must still surface: %+v", p.Findings)
	}
	if p.Availability != FeedbackDegraded {
		t.Fatalf("availability = %q, want feedback_degraded", p.Availability)
	}
}

func TestBuild_UnreadableFailedCandidateIsUnrelated(t *testing.T) {
	desc := map[string]qp.CandidateDescriptor{"L1": {LineageID: "L1", Readable: false}}
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: verErr(qp.VerifyStale, "drift")}}, desc)
	p := mustBuild(t, validReq(), d)
	if len(p.Findings) != 0 || p.Availability != FeedbackEmpty {
		t.Fatalf("unreadable failed candidate must be unrelated debris: %q %+v", p.Availability, p.Findings)
	}
}

func TestBuild_RelevantAndUnrelatedFailuresCoexist(t *testing.T) {
	desc := map[string]qp.CandidateDescriptor{
		"REL":  relevantDesc("REL"),
		"UNRL": {LineageID: "UNRL", ClaimedDomain: "github.com/foreign/repo", ClaimedFiles: []string{"z.go"}, Readable: true},
	}
	out := map[string]outcome{
		"REL":  {err: verErr(qp.VerifyIncomplete, "not_committed")},
		"UNRL": {err: verErr(qp.VerifyIntegrityFailure, "tampered")},
	}
	p := mustBuild(t, validReq(), fakeDeps([]string{"REL", "UNRL"}, out, desc))
	if len(p.Findings) != 1 || p.Findings[0].LineageID != "REL" {
		t.Fatalf("only the relevant failure should surface: %+v", p.Findings)
	}
	if p.Availability != FeedbackDegraded {
		t.Fatalf("availability = %q, want feedback_degraded", p.Availability)
	}
}

// ---------------------------------------------------------------------------
// Malformed request → feedback_invalid (never silent repair)
// ---------------------------------------------------------------------------

func TestBuild_MalformedRequest(t *testing.T) {
	cases := []struct {
		name   string
		req    func(Request) Request
		reason string
	}{
		{"absent repo root", func(r Request) Request { r.RepositoryRoot = "  "; return r }, "repository_root_absent"},
		{"whitespace domain", func(r Request) Request { r.RequestedDomain = "has space"; return r }, "domain_malformed"},
		{"leading/trailing space domain", func(r Request) Request { r.RequestedDomain = " x"; return r }, "domain_malformed"},
		{"unsafe requested file", func(r Request) Request { r.RequestedFiles = []string{"../escape"}; return r }, "unsafe_requested_file"},
		{"absolute requested file", func(r Request) Request { r.RequestedFiles = []string{"/etc/passwd"}; return r }, "unsafe_requested_file"},
		{"unsafe task file", func(r Request) Request { r.Task.Files = []string{"../../x"}; return r }, "unsafe_task_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req(validReq())
			// discover should never be reached for a malformed request.
			d := deps{
				resolveRoot: func(r string) (string, error) { return r, nil },
				discover: func(string) ([]string, error) {
					t.Fatal("discover must not run for malformed request")
					return nil, nil
				},
				descriptor: func(_, l string) qp.CandidateDescriptor { return relevantDesc(l) },
				verify: func(context.Context, string, string) (qp.VerifiedPromotion, error) {
					return qp.VerifiedPromotion{}, nil
				},
			}
			p := mustBuild(t, req, d)
			if p.Availability != FeedbackInvalid {
				t.Fatalf("availability = %q, want feedback_invalid", p.Availability)
			}
			if len(p.Findings) != 1 || p.Findings[0].Class != PromotionScopeIdentityInvalid || p.Findings[0].ReasonCode != tc.reason {
				t.Fatalf("want scope_identity_invalid/%s, got %+v", tc.reason, p.Findings)
			}
		})
	}
}

func TestBuild_EmptyDomainIsNotMalformed(t *testing.T) {
	req := validReq()
	req.RequestedDomain = ""
	d := fakeDeps([]string{"L1"}, map[string]outcome{
		"L1": {vp: verified("L1", func(rc *qp.QuestionPromotionReceipt) { rc.EffectiveScopeDomain = "" })},
	}, nil)
	p := mustBuild(t, req, d)
	if p.Availability == FeedbackInvalid {
		t.Fatalf("empty domain must not be malformed")
	}
	if len(p.Records) != 1 {
		t.Fatalf("domain-neutral promotion must still admit under empty domain: %+v", p)
	}
}

// ---------------------------------------------------------------------------
// Discovery outage → feedback_unavailable
// ---------------------------------------------------------------------------

func TestBuild_DiscoveryOutageIsUnavailable(t *testing.T) {
	d := fakeDeps(nil, nil, nil)
	d.discover = func(string) ([]string, error) { return nil, errors.New("store offline") }
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackUnavailable {
		t.Fatalf("availability = %q, want feedback_unavailable", p.Availability)
	}
	if len(p.Findings) != 1 || p.Findings[0].Class != PromotionDiscoveryUnavailable {
		t.Fatalf("want discovery_unavailable finding, got %+v", p.Findings)
	}
	if p.Findings[0].Disposition != DispositionUnavailable {
		t.Fatalf("discovery outage disposition must be unavailable")
	}
}

// ---------------------------------------------------------------------------
// Contradiction semantics
// ---------------------------------------------------------------------------

func TestBuild_DuplicateVerifiedLineageIsContradictory(t *testing.T) {
	// Two discovered lineages that both verify to the SAME promotion lineage.
	out := map[string]outcome{
		"A": {vp: verified("DUP", nil)},
		"B": {vp: verified("DUP", nil)},
	}
	desc := map[string]qp.CandidateDescriptor{"A": relevantDesc("A"), "B": relevantDesc("B")}
	p := mustBuild(t, validReq(), fakeDeps([]string{"A", "B"}, out, desc))
	if p.Availability != FeedbackInvalid {
		t.Fatalf("duplicate verified lineage must be feedback_invalid, got %q", p.Availability)
	}
	found := false
	for _, f := range p.Findings {
		if f.Class == PromotionContradictory {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a contradictory finding: %+v", p.Findings)
	}
}

func TestBuild_ConflictingRelevantDescriptorsAreContradictory(t *testing.T) {
	a := relevantDesc("L1")
	b := relevantDesc("L1")
	b.ClaimedSessionID = "different-session" // same lineage id, conflicting claims
	// Deliver the same lineage id twice with conflicting descriptors.
	descSeq := []qp.CandidateDescriptor{a, b}
	i := 0
	d := deps{
		resolveRoot: func(r string) (string, error) { return r, nil },
		discover:    func(string) ([]string, error) { return []string{"L1", "L1"}, nil },
		descriptor: func(_, _ string) qp.CandidateDescriptor {
			dd := descSeq[i]
			i++
			return dd
		},
		verify: func(context.Context, string, string) (qp.VerifiedPromotion, error) {
			return qp.VerifiedPromotion{}, verErr(qp.VerifyIncomplete, "x")
		},
	}
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackInvalid {
		t.Fatalf("conflicting relevant descriptors must be feedback_invalid, got %q", p.Availability)
	}
}

func TestBuild_ConflictingUnrelatedDescriptorsDoNotPoison(t *testing.T) {
	a := qp.CandidateDescriptor{LineageID: "L1", ClaimedDomain: "github.com/foreign/x", ClaimedFiles: []string{"z.go"}, Readable: true}
	b := a
	b.ClaimedSessionID = "diff"
	descSeq := []qp.CandidateDescriptor{a, b}
	i := 0
	d := deps{
		resolveRoot: func(r string) (string, error) { return r, nil },
		discover:    func(string) ([]string, error) { return []string{"L1", "L1"}, nil },
		descriptor:  func(_, _ string) qp.CandidateDescriptor { dd := descSeq[i]; i++; return dd },
		verify: func(context.Context, string, string) (qp.VerifiedPromotion, error) {
			return qp.VerifiedPromotion{}, verErr(qp.VerifyIncomplete, "x")
		},
	}
	p := mustBuild(t, validReq(), d)
	if p.Availability == FeedbackInvalid {
		t.Fatalf("unrelated conflicting descriptors must not poison the projection")
	}
}

// ---------------------------------------------------------------------------
// Determinism, digest, ordering
// ---------------------------------------------------------------------------

func TestBuild_DeterministicRegardlessOfDiscoveryOrder(t *testing.T) {
	out := map[string]outcome{
		"L1": {vp: verified("L1", nil)},
		"L2": {vp: verified("L2", func(rc *qp.QuestionPromotionReceipt) {
			rc.GovernedNodeIRI = "aw:FailureMode/fm.L2"
			rc.EffectiveScopeFiles = []string{testFile}
		})},
		"L3": {err: verErr(qp.VerifyStale, "drift")},
	}
	desc := map[string]qp.CandidateDescriptor{"L1": relevantDesc("L1"), "L2": relevantDesc("L2"), "L3": relevantDesc("L3")}
	forward := mustBuild(t, validReq(), fakeDeps([]string{"L1", "L2", "L3"}, out, desc))
	reverse := mustBuild(t, validReq(), fakeDeps([]string{"L3", "L2", "L1"}, out, desc))
	if forward.DigestSHA256 != reverse.DigestSHA256 {
		t.Fatalf("digest not order-independent: %q vs %q", forward.DigestSHA256, reverse.DigestSHA256)
	}
	if !recordsSorted(forward.Records) || !findingsSorted(forward.Findings) {
		t.Fatalf("records/findings not in canonical order")
	}
}

func TestBuild_DigestIsSelfExcludingAndTamperEvident(t *testing.T) {
	p := mustBuild(t, validReq(), fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil))
	recomputed, err := ComputeDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed != p.DigestSHA256 {
		t.Fatalf("self-excluding digest does not verify: %q vs %q", recomputed, p.DigestSHA256)
	}
	// Any post-digest mutation must be caught by ValidateProjection.
	tampered := p
	tampered.Records = append([]VerifiedRecord(nil), p.Records...)
	tampered.Records[0].QuestionID = "forged"
	if ValidateProjection(tampered) == nil {
		t.Fatal("mutated projection must fail validation")
	}
}

func TestValidateProjection_ZeroValueFails(t *testing.T) {
	if ValidateProjection(Projection{}) == nil {
		t.Fatal("zero-value projection must fail closed")
	}
}

func TestValidateProjection_OffVocabularyFails(t *testing.T) {
	p := mustBuild(t, validReq(), fakeDeps(nil, nil, nil))
	bad := p
	bad.Availability = "made_up"
	bad.DigestSHA256 = ""
	dig, _ := ComputeDigest(bad)
	bad.DigestSHA256 = dig
	if ValidateProjection(bad) == nil {
		t.Fatal("off-vocabulary availability must fail")
	}
}

// ---------------------------------------------------------------------------
// Path parity (Windows/Unix)
// ---------------------------------------------------------------------------

func TestBuild_BackslashPathParity(t *testing.T) {
	req := validReq()
	req.RequestedFiles = []string{`golang\server\reload.go`}
	req.Task.Files = []string{`golang\server\reload.go`}
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil)
	p := mustBuild(t, req, d)
	if len(p.Records) != 1 {
		t.Fatalf("backslash-form request must admit the same promotion: %+v", p)
	}
	if p.RequestedFiles[0] != testFile {
		t.Fatalf("requested file not canonicalized to slash form: %q", p.RequestedFiles[0])
	}
}

// ---------------------------------------------------------------------------
// Privacy boundary: the projection serializes no filesystem repo root and no
// caller-supplied prose; the record carries provenance IDs, not question text.
// ---------------------------------------------------------------------------

func TestBuild_ProjectionOmitsRepositoryRoot(t *testing.T) {
	req := validReq()
	req.RepositoryRoot = "/secret/checkout/path"
	p := mustBuild(t, req, fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil))
	blob, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "/secret/checkout/path") {
		t.Fatalf("projection must never serialize the filesystem repository root")
	}
}

// ---------------------------------------------------------------------------
// Helpers to mint a typed verification error without depending on qp internals.
// ---------------------------------------------------------------------------

func verErr(class qp.VerificationFailureClass, reason string) error {
	return &qp.VerificationError{Class: class, ReasonCode: reason, Impact: qp.VerificationCandidateLocal, Cause: errors.New("boom")}
}

func facilityErr(reason string) error {
	return &qp.VerificationError{Class: qp.VerifyUnverifiable, ReasonCode: reason, Impact: qp.VerificationFacilityUnavailable, Cause: errors.New("boom-io")}
}

// ---------------------------------------------------------------------------
// Repair proofs (Checkpoint 1 review): identity, root, padded paths, facility
// impact, order-independence, duplicate-finding refusal.
// ---------------------------------------------------------------------------

// A task-scoped request missing task id or session id fails closed as feedback_invalid.
func TestBuild_MissingTaskIdentityFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*TaskBinding)
		reason string
	}{
		{"no task id", func(tb *TaskBinding) { tb.TaskID = "" }, "task_identity_absent"},
		{"no session id", func(tb *TaskBinding) { tb.SessionID = "" }, "session_identity_absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq()
			tc.mutate(req.Task)
			p := mustBuild(t, req, fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil))
			if p.Availability != FeedbackInvalid || len(p.Findings) != 1 || p.Findings[0].ReasonCode != tc.reason {
				t.Fatalf("want feedback_invalid/%s, got %q %+v", tc.reason, p.Availability, p.Findings)
			}
			if p.TaskID != "" || p.SessionID != "" {
				t.Fatalf("invalid projection must not be task-bound: %+v", p)
			}
		})
	}
}

// A task/repository domain mismatch fails closed (no fallback, no case/prefix repair).
func TestBuild_RepositoryDomainMismatchFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
		reason string
	}{
		{"requested domain != identity", func(r *Request) { r.RequestedDomain = "github.com/other/repo" }, "repository_identity_incoherent"},
		{"task domain != identity", func(r *Request) { r.Task.RepositoryDomain = "github.com/other/repo" }, "task_domain_incoherent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq()
			tc.mutate(&req)
			p := mustBuild(t, req, fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil))
			if p.Availability != FeedbackInvalid || p.Findings[0].ReasonCode != tc.reason {
				t.Fatalf("want feedback_invalid/%s, got %q %+v", tc.reason, p.Availability, p.Findings)
			}
		})
	}
}

// Relative, padded, and absent repository roots are rejected at the owner boundary — never
// resolved against the process working directory.
func TestBuild_RepositoryRootIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		root   string
		reason string
	}{
		{"relative root", "relative/checkout", "repository_root_relative"},
		{"padded root", " /repo ", "repository_root_padded"},
		{"absent root", "   ", "repository_root_absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq()
			req.RepositoryRoot = tc.root
			d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil)
			d.resolveRoot = func(string) (string, error) { t.Fatal("resolveRoot must not run for an invalid root"); return "", nil }
			p := mustBuild(t, req, d)
			if p.Availability != FeedbackInvalid || p.Findings[0].ReasonCode != tc.reason {
				t.Fatalf("want feedback_invalid/%s, got %q %+v", tc.reason, p.Availability, p.Findings)
			}
		})
	}
}

// An absolute-but-unresolvable root (does not exist / not a directory) is feedback_invalid.
func TestBuild_UnresolvableRootIsInvalid(t *testing.T) {
	d := fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil)
	d.resolveRoot = func(string) (string, error) { return "", errors.New("no such directory") }
	p := mustBuild(t, validReq(), d)
	if p.Availability != FeedbackInvalid || p.Findings[0].ReasonCode != "repository_root_unresolvable" {
		t.Fatalf("want feedback_invalid/repository_root_unresolvable, got %q %+v", p.Availability, p.Findings)
	}
}

// resolveRepoRoot (the production resolver) resolves symlinks once, requires a directory, and
// never calls filepath.Abs.
func TestResolveRepoRoot_RealFilesystem(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveRepoRoot(dir)
	if err != nil {
		t.Fatalf("resolveRepoRoot(%q) = %v", dir, err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("resolved root not absolute: %q", got)
	}
	// A file, not a directory, is rejected.
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRepoRoot(f); err == nil {
		t.Fatal("resolveRepoRoot must reject a non-directory")
	}
	// A missing path is rejected.
	if _, err := resolveRepoRoot(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("resolveRepoRoot must reject a missing path")
	}
}

// A padded file identity is rejected, never trimmed into a distinct canonical path.
func TestBuild_PaddedFileIdentityRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
		reason string
	}{
		{"padded requested file", func(r *Request) { r.RequestedFiles = []string{" golang/server/reload.go"} }, "unsafe_requested_file"},
		{"trailing-space requested file", func(r *Request) { r.RequestedFiles = []string{"golang/server/reload.go "} }, "unsafe_requested_file"},
		{"padded task file", func(r *Request) { r.Task.Files = []string{"golang/server/reload.go\t"} }, "unsafe_task_file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := validReq()
			tc.mutate(&req)
			p := mustBuild(t, req, fakeDeps([]string{"L1"}, map[string]outcome{"L1": {vp: verified("L1", nil)}}, nil))
			if p.Availability != FeedbackInvalid || p.Findings[0].ReasonCode != tc.reason {
				t.Fatalf("want feedback_invalid/%s, got %q %+v", tc.reason, p.Availability, p.Findings)
			}
		})
	}
}

// A candidate-local unverifiable result degrades (excluded); a shared-facility outage is
// unavailable — the two are distinct.
func TestBuild_CandidateLocalVsFacilityImpact(t *testing.T) {
	local := mustBuild(t, validReq(), fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: verErr(qp.VerifyUnverifiable, "governed_record_unreadable")}}, nil))
	if local.Availability != FeedbackDegraded || local.Findings[0].Disposition != DispositionExcluded {
		t.Fatalf("candidate-local unverifiable must degrade+exclude: %q %+v", local.Availability, local.Findings)
	}
	facility := mustBuild(t, validReq(), fakeDeps([]string{"L1"}, map[string]outcome{"L1": {err: facilityErr("graph_reverify_failed")}}, nil))
	if facility.Availability != FeedbackUnavailable {
		t.Fatalf("facility outage must be unavailable, got %q", facility.Availability)
	}
	if len(facility.Findings) != 1 || facility.Findings[0].Disposition != DispositionUnavailable || facility.Findings[0].Class != PromotionUnverifiable {
		t.Fatalf("facility finding malformed: %+v", facility.Findings)
	}
}

// A global facility outage classifies as unavailable regardless of discovery order and is
// reported exactly once.
func TestBuild_FacilityOutageOrderIndependent(t *testing.T) {
	out := map[string]outcome{
		"A": {err: facilityErr("graph_reverify_failed")},
		"B": {err: verErr(qp.VerifyIntegrityFailure, "tampered")},
	}
	desc := map[string]qp.CandidateDescriptor{"A": relevantDesc("A"), "B": relevantDesc("B")}
	forward := mustBuild(t, validReq(), fakeDeps([]string{"A", "B"}, out, desc))
	reverse := mustBuild(t, validReq(), fakeDeps([]string{"B", "A"}, out, desc))
	if forward.Availability != FeedbackUnavailable || reverse.Availability != FeedbackUnavailable {
		t.Fatalf("facility outage must be unavailable both orders: %q / %q", forward.Availability, reverse.Availability)
	}
	if forward.DigestSHA256 != reverse.DigestSHA256 {
		t.Fatalf("facility-outage projection not order-independent")
	}
	facilityFindings := 0
	for _, f := range forward.Findings {
		if f.Disposition == DispositionUnavailable {
			facilityFindings++
		}
	}
	if facilityFindings != 1 {
		t.Fatalf("facility outage must be reported exactly once, got %d", facilityFindings)
	}
}

// ValidateProjection refuses a duplicate finding identity; finalize deduplicates.
func TestValidateProjection_DuplicateFindingRefused(t *testing.T) {
	base := mustBuild(t, validReq(), fakeDeps(nil, nil, nil))
	dup := base
	f := Finding{Class: PromotionStale, ReasonCode: "drift", LineageID: "L1", AffectedDomain: testDomain, Disposition: DispositionExcluded}
	dup.Findings = []Finding{f, f}
	dup.Availability = FeedbackDegraded
	dup.DigestSHA256 = ""
	dig, _ := ComputeDigest(dup)
	dup.DigestSHA256 = dig
	if ValidateProjection(dup) == nil {
		t.Fatal("duplicate finding identity must be refused")
	}
	if got := dedupeFindings([]Finding{f, f}); len(got) != 1 {
		t.Fatalf("dedupeFindings must collapse duplicates, got %d", len(got))
	}
}

// No raw verification error text or answer text is serialized into the projection.
func TestBuild_NoRawTextSerialized(t *testing.T) {
	p := mustBuild(t, validReq(), fakeDeps([]string{"L1", "L2"}, map[string]outcome{
		"L1": {vp: verified("L1", nil)},
		"L2": {err: verErr(qp.VerifyIntegrityFailure, "tampered")},
	}, map[string]qp.CandidateDescriptor{"L1": relevantDesc("L1"), "L2": relevantDesc("L2")}))
	blob, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "boom") {
		t.Fatalf("raw verification error text leaked into projection")
	}
}
