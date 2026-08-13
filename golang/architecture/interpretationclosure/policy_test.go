// SPDX-License-Identifier: AGPL-3.0-only

package interpretationclosure

import "testing"

const testDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func baseInput() Input {
	return Input{
		InterpretationDigestSHA256: testDigest,
		RepositoryRevision:         "deadbeef",
		GraphAuthorityDigestSHA256: testDigest,
		ClosureDigestSHA256:        testDigest,
		TruthFindings:              []TruthFinding{{ClaimID: "claim.arch", CheckKind: "semantic_property", Status: TruthUnknown}},
		Completeness:               CompletenessAssessment{Status: CompletenessComplete},
		Realization:                RealizationAssessment{Status: RealizationUnknown},
		ProofObservations:          []ProofObservation{},
	}
}

func TestTruthUnknownIsNeutralForAuthority(t *testing.T) {
	r, err := Certify(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityGoverning {
		t.Fatalf("authority=%q blockers=%v", r.Authority, r.Blockers)
	}
}

func TestTruthContradictionBlocksAuthority(t *testing.T) {
	in := baseInput()
	in.TruthFindings[0].Status = TruthContradicted
	r, err := Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityAdvisory {
		t.Fatalf("authority=%q", r.Authority)
	}
	if len(r.Blockers) != 1 {
		t.Fatalf("blockers=%v", r.Blockers)
	}
}

func TestCompletenessUnknownBlocksHardAuthority(t *testing.T) {
	in := baseInput()
	in.Completeness.Status = CompletenessUnknown
	r, err := Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityAdvisory {
		t.Fatalf("authority=%q", r.Authority)
	}
}

func TestRealizationUnknownIsNeutralButKnownBreadthBlocks(t *testing.T) {
	r, err := Certify(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityGoverning {
		t.Fatalf("unknown realization unexpectedly blocked: %v", r.Blockers)
	}
	in := baseInput()
	in.Realization.Status = RealizationBroaderThanProven
	r, err = Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityAdvisory {
		t.Fatal("known unjustified breadth did not block")
	}
}

func TestRequiredProofMustBeSatisfied(t *testing.T) {
	in := baseInput()
	in.ProofObservations = []ProofObservation{{ObligationID: "proof.one", RequiredForAuthority: true, Status: ProofUnresolved}}
	r, err := Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityAdvisory {
		t.Fatal("unresolved required proof did not block")
	}
}

func TestVerifyRecomputesAuthorityBindingAndCoverage(t *testing.T) {
	r, err := Certify(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyForGoverning(r, testDigest, "deadbeef", testDigest, testDigest, []string{"claim.arch"}, nil); err != nil {
		t.Fatalf("valid receipt: %v", err)
	}
	tampered := r
	tampered.Authority = AuthorityAdvisory
	if err := VerifyForGoverning(tampered, testDigest, "deadbeef", testDigest, testDigest, []string{"claim.arch"}, nil); err == nil {
		t.Fatal("tampered authority accepted")
	}
	if err := VerifyForGoverning(r, testDigest, "different", testDigest, testDigest, []string{"claim.arch"}, nil); err == nil {
		t.Fatal("wrong repository revision accepted")
	}
	if err := VerifyForGoverning(r, testDigest, "deadbeef", testDigest, stringsRepeat("1", 64), []string{"claim.arch"}, nil); err == nil {
		t.Fatal("wrong task closure digest accepted")
	}
	if err := VerifyForGoverning(r, testDigest, "deadbeef", testDigest, testDigest, []string{"claim.arch", "claim.omitted"}, nil); err == nil {
		t.Fatal("omitted governing claim challenge accepted")
	}
}

func TestVerifyCoverageRequiresDeclaredProofObservation(t *testing.T) {
	in := baseInput()
	in.ProofObservations = []ProofObservation{{ObligationID: "proof.one", RequiredForAuthority: true, Status: ProofSatisfied}}
	r, err := Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyForGoverning(r, testDigest, "deadbeef", testDigest, testDigest, []string{"claim.arch"}, []string{"proof.one"}); err != nil {
		t.Fatalf("covered proof rejected: %v", err)
	}
	if err := VerifyForGoverning(r, testDigest, "deadbeef", testDigest, testDigest, []string{"claim.arch"}, []string{"proof.one", "proof.missing"}); err == nil {
		t.Fatal("missing required proof observation accepted")
	}
}

func TestAssessCompletenessDistinguishesUnknownFromEmpty(t *testing.T) {
	if got := AssessCompleteness([]string{"a.go"}, nil); got.Status != CompletenessUnknown {
		t.Fatalf("nil required=%q", got.Status)
	}
	if got := AssessCompleteness([]string{"a.go"}, []string{}); got.Status != CompletenessComplete {
		t.Fatalf("empty required=%q", got.Status)
	}
	got := AssessCompleteness([]string{"a.go"}, []string{"a.go", "b.go"})
	if got.Status != CompletenessIncomplete || len(got.MissingSurface) != 1 || got.MissingSurface[0] != "b.go" {
		t.Fatalf("assessment=%+v", got)
	}
}

func TestUnsupportedLanguageRemainsUnknown(t *testing.T) {
	f := UnknownTruth("claim.semantic", "rust", "not_implemented", "no Rust checker yet")
	if f.Status != TruthUnknown {
		t.Fatalf("status=%q", f.Status)
	}
	in := baseInput()
	in.TruthFindings = []TruthFinding{f}
	r, err := Certify(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authority != AuthorityGoverning {
		t.Fatalf("unsupported language became blocker: %v", r.Blockers)
	}
}

func stringsRepeat(s string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += s
	}
	return out
}
