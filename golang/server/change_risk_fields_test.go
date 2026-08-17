// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// The drift guard. blastRadiusOrder / approvalGateOrder are the classifier's
// vocabulary; the enum maps are the wire projection. Adding a severity level to
// one without the other would silently downgrade it to UNSPECIFIED — a NEW,
// WIDER blast radius reaching consumers as "unclassified" is exactly the kind of
// quiet loss this issue is about.
func TestEveryVocabularyValueHasAnEnum(t *testing.T) {
	for _, v := range blastRadiusOrder {
		if got := blastRadiusProto(v); got == awarenesspb.BlastRadius_BLAST_RADIUS_UNSPECIFIED {
			t.Errorf("blast radius %q has no enum; it would reach consumers as unclassified", v)
		}
	}
	for _, v := range approvalGateOrder {
		if got := approvalGateProto(v); got == awarenesspb.ApprovalGate_APPROVAL_GATE_UNSPECIFIED {
			t.Errorf("approval gate %q has no enum; it would reach consumers as unclassified", v)
		}
	}
	if len(blastRadiusEnums) != len(blastRadiusOrder) {
		t.Errorf("blastRadiusEnums has %d entries, vocabulary has %d", len(blastRadiusEnums), len(blastRadiusOrder))
	}
	if len(approvalGateEnums) != len(approvalGateOrder) {
		t.Errorf("approvalGateEnums has %d entries, vocabulary has %d", len(approvalGateEnums), len(approvalGateOrder))
	}
}

// The enum numbers must ascend with severity, matching the order of the
// vocabulary slices, so "widest wins" is a numeric comparison on the wire too.
func TestEnumNumbersAscendWithSeverity(t *testing.T) {
	prev := awarenesspb.BlastRadius_BLAST_RADIUS_UNSPECIFIED
	for _, v := range blastRadiusOrder {
		got := blastRadiusProto(v)
		if got <= prev {
			t.Fatalf("blast radius %q = %d does not exceed the previous level %d", v, got, prev)
		}
		prev = got
	}
	prevGate := awarenesspb.ApprovalGate_APPROVAL_GATE_UNSPECIFIED
	for _, v := range approvalGateOrder {
		got := approvalGateProto(v)
		if got <= prevGate {
			t.Fatalf("approval gate %q = %d does not exceed the previous level %d", v, got, prevGate)
		}
		prevGate = got
	}
}

// An unknown value must read as UNSPECIFIED, never as the safest member. A
// consumer can refuse "unclassified"; it would proceed on "local/none".
func TestUnknownValueIsUnclassifiedNotSafe(t *testing.T) {
	if got := blastRadiusProto("galaxy_wide"); got != awarenesspb.BlastRadius_BLAST_RADIUS_UNSPECIFIED {
		t.Fatalf("unknown blast radius = %v, want UNSPECIFIED", got)
	}
	if got := approvalGateProto("ask_nicely"); got != awarenesspb.ApprovalGate_APPROVAL_GATE_UNSPECIFIED {
		t.Fatalf("unknown approval gate = %v, want UNSPECIFIED", got)
	}
	// And the safe members must not be the zero value, or "unset" and "safe"
	// would be indistinguishable on the wire.
	if awarenesspb.BlastRadius_BLAST_RADIUS_LOCAL == 0 {
		t.Error("LOCAL is the zero value; an unset field would read as a safe classification")
	}
	if awarenesspb.ApprovalGate_APPROVAL_GATE_NONE == 0 {
		t.Error("NONE is the zero value; an unset field would read as no approval required")
	}
}

// The structured fields and the prose line must describe the SAME verdict — that
// is the entire point of deriving both from one assessment.
func TestStructuredFieldsAgreeWithTheProseLine(t *testing.T) {
	a := changeAssessment{
		BlastRadius:  "security",
		ApprovalGate: "human_approval_required",
		Reasons:      []string{"touches an authority boundary"},
	}

	cr := changeRiskProto(a)
	line := changeAssessmentAction(a)

	if cr.GetBlastRadius() != awarenesspb.BlastRadius_BLAST_RADIUS_SECURITY {
		t.Fatalf("blast radius = %v", cr.GetBlastRadius())
	}
	if cr.GetApprovalGate() != awarenesspb.ApprovalGate_APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED {
		t.Fatalf("approval gate = %v", cr.GetApprovalGate())
	}
	if !strings.Contains(line, "blast=security") || !strings.Contains(line, "approval=human_approval_required") {
		t.Fatalf("the prose line describes something else: %s", line)
	}
	if len(cr.GetReasons()) != 1 || cr.GetReasons()[0] != "touches an authority boundary" {
		t.Fatalf("reasons = %v", cr.GetReasons())
	}
}

// Reasons must be copied, not aliased: a caller mutating the response must not
// reach back into the assessment the server still holds.
func TestReasonsAreCopiedNotAliased(t *testing.T) {
	reasons := []string{"original"}
	cr := changeRiskProto(changeAssessment{BlastRadius: "local", ApprovalGate: "none", Reasons: reasons})
	cr.Reasons[0] = "mutated"
	if reasons[0] != "original" {
		t.Fatal("the projection aliases the assessment's reasons slice")
	}
}

// End to end through the real RPC: the field must actually be POPULATED, and it
// must agree with the prose line in the same response. A correct projection
// helper that nothing calls would leave the issue unfixed while every unit test
// above still passed.
func TestPreflightPopulatesChangeRiskAndAgreesWithProse(t *testing.T) {
	facts := loadCorpusAuthorityFacts(t)
	s := newAuthorityTestServer(t, facts)

	resp, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{
		Task:  "adjust the workflow resume path",
		Files: []string{goldenAuthorityCases[0].file},
		Mode:  awarenesspb.PreflightMode_PREFLIGHT_STANDARD,
	})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	cr := resp.GetChangeRisk()
	if cr == nil {
		t.Fatal("change_risk is absent; consumers are still left parsing prose")
	}
	if cr.GetBlastRadius() == awarenesspb.BlastRadius_BLAST_RADIUS_UNSPECIFIED {
		t.Error("blast_radius is UNSPECIFIED for a classified change")
	}
	if cr.GetApprovalGate() == awarenesspb.ApprovalGate_APPROVAL_GATE_UNSPECIFIED {
		t.Error("approval_gate is UNSPECIFIED for a classified change")
	}

	// The prose line must still be there, unchanged, for existing consumers —
	// and must describe the same verdict as the structured fields.
	var prose string
	for _, line := range resp.GetRequiredActions() {
		if strings.HasPrefix(line, "Change risk: ") {
			prose = line
			break
		}
	}
	if prose == "" {
		t.Fatal("the Change risk prose line disappeared; this was meant to be additive")
	}
	for value, label := range map[string]string{
		strings.ToLower(cr.GetBlastRadius().String()):  "blast",
		strings.ToLower(cr.GetApprovalGate().String()): "approval",
	} {
		// Enum names carry their prefix; compare the vocabulary tail.
		tail := value[strings.LastIndex(value, "radius_")+len("radius_"):]
		if label == "approval" {
			tail = value[strings.LastIndex(value, "gate_")+len("gate_"):]
		}
		if !strings.Contains(prose, label+"="+tail) {
			t.Errorf("structured %s=%q does not match the prose line %q", label, tail, prose)
		}
	}
}
