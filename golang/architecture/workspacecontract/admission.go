// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture"
	admissionpkg "github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
)

// ProjectDecision projects an already-produced admission.Decision (from
// admission.Evaluate) into a sensei.workspace.admission.v1 decision record.
// It copies fields verbatim — it never re-derives, broadens, or infers any
// decision, capability, reason, limitation, or correctness fact the owner
// did not already report.
func ProjectDecision(d admissionpkg.Decision) Admission {
	a := Admission{
		SchemaVersion:        AdmissionSchemaVersion,
		RecordKind:           RecordKindDecision,
		AdmissionID:          d.AdmissionID,
		DecisionDigestSHA256: d.DecisionDigestSHA256,
		PolicyID:             d.PolicyID,
		PolicyVersion:        d.PolicyVersion,
		Decision:             DecisionOutcome(d.Decision),
		RequestedMode:        RequestedMode(d.RequestedMode),
		Binding:              projectBinding(d.Binding),
		SessionReceipt:       projectSessionReceipt(d.SessionReceipt),
		RequestReceipt:       projectRequestReceipt(d.RequestReceipt),
		InspectionCapability: DecisionOutcome(d.InspectionCapability),
		MutationCapability:   DecisionOutcome(d.MutationCapability),
		Envelope:             projectEnvelope(d.Envelope),
		Reasons:              projectReasons(d.Reasons),
		Limitations:          projectLimitations(d.Limitations),
		ScopeOnly:            d.ScopeOnly,
		CorrectnessCertified: d.CorrectnessCertified,
		Verification:         nil,
	}
	return NormalizeAdmission(a)
}

// ProjectVerification projects an already-produced admission.Verification
// (from admission.Verify) bound to its exact originating admission.Decision
// (loaded via admission.LoadDecision from the same decision_path the
// existing verify_admission tool consumes) into a
// sensei.workspace.admission.v1 verification record. The record's
// admission_id, decision_digest_sha256, policy, binding, session, request,
// capabilities, and envelope all come from the ORIGINAL decision — never
// re-derived or guessed from the verification alone — so a verification
// record is never free-floating.
//
// It fails closed — returning an error and no Admission — when v's own
// AdmissionID, DecisionDigestSHA256, or Binding do not match d. Without
// this check, any caller (including a direct package caller with no
// filesystem race involved at all) could pass a genuinely unrelated
// decision/verification pair and receive a normalized, schema-valid record
// whose verification silently attached itself to the wrong decision's
// identity, policy, binding, session, request, and envelope.
func ProjectVerification(d admissionpkg.Decision, v admissionpkg.Verification) (Admission, error) {
	if v.AdmissionID != d.AdmissionID {
		return Admission{}, fmt.Errorf("verification admission_id %q does not match decision admission_id %q", v.AdmissionID, d.AdmissionID)
	}
	if v.DecisionDigestSHA256 != d.DecisionDigestSHA256 {
		return Admission{}, fmt.Errorf("verification decision_digest_sha256 %q does not match decision decision_digest_sha256 %q", v.DecisionDigestSHA256, d.DecisionDigestSHA256)
	}
	if v.Binding != d.Binding {
		return Admission{}, fmt.Errorf("verification binding does not match the referenced decision %q's binding", d.AdmissionID)
	}

	a := ProjectDecision(d)
	a.RecordKind = RecordKindVerification
	verification := Verification{
		Status:                    VerificationStatus(v.Status),
		VerificationDigestSHA256:  v.VerificationDigestSHA256,
		IterationDigestSHA256:     v.IterationDigestSHA256,
		PatchDigestSHA256:         v.PatchDigestSHA256,
		Changes:                   projectChangeReceipts(v.Changes),
		Violations:                projectViolations(v.Violations),
		PendingConditionIDs:       conditionIDs(v.PendingConditions),
		PendingTestIDs:            guidanceIDs(v.PendingTests),
		PendingProofObligationIDs: proofIDs(v.PendingProofObligations),
		PendingRuntimeEvidenceIDs: guidanceIDs(v.PendingRuntimeEvidence),
		Reasons:                   projectReasons(v.Reasons),
		Limitations:               projectLimitations(v.Limitations),
		ScopeOnly:                 v.ScopeOnly,
		CorrectnessCertified:      v.CorrectnessCertified,
	}
	verification = normalizeVerification(verification)
	a.Verification = &verification
	return a, nil
}

// projectBinding mirrors the same nullable-when-unresolved projection
// ComposeIdentity applies, so both external contracts represent an
// unresolved revision/tree/graph digest identically.
func projectBinding(b architecture.ClaimDocumentBinding) Binding {
	out := Binding{
		RepositoryDomain:  b.RepositoryDomain,
		RevisionStatus:    RevisionStatus(b.RevisionStatus),
		GraphDigestStatus: GraphDigestStatus(b.GraphDigestStatus),
	}
	if b.RevisionStatus == architecture.RevisionResolved && b.Revision != "" {
		rev := b.Revision
		out.Revision = &rev
	}
	if b.TreeDigestSHA256 != "" {
		td := b.TreeDigestSHA256
		out.TreeDigestSHA256 = &td
	}
	if b.GraphDigestStatus == architecture.GraphDigestResolved && b.GraphDigestSHA256 != "" {
		gd := b.GraphDigestSHA256
		out.GraphDigestSHA256 = &gd
	}
	return out
}

func projectSessionReceipt(s admissionpkg.SessionReceipt) SessionReceipt {
	return SessionReceipt{
		SessionID:                 s.SessionID,
		LatestIteration:           s.LatestIteration,
		IterationDigestSHA256:     s.IterationDigestSHA256,
		SemanticStateDigestSHA256: s.SemanticStateDigestSHA256,
		Status:                    s.Status,
		ClosureVerdict:            s.ClosureVerdict,
	}
}

func projectRequestReceipt(r admissionpkg.RequestReceipt) RequestReceipt {
	return RequestReceipt{
		DigestSHA256: r.DigestSHA256,
		Scope:        projectChangeScope(r.Scope),
		Mode:         r.Mode,
		TaskClass:    r.TaskClass,
	}
}

func projectChangeScope(s admissionpkg.ChangeScope) ChangeScope {
	files := make([]FileOperation, 0, len(s.Files))
	for _, f := range s.Files {
		files = append(files, FileOperation{Path: f.Path, Operation: f.Operation})
	}
	return ChangeScope{
		Files:           files,
		Symbols:         s.Symbols,
		Components:      s.Components,
		ClaimIDs:        s.ClaimIDs,
		PropositionKeys: s.PropositionKeys,
	}
}

func projectEnvelope(e admissionpkg.ChangeEnvelope) Envelope {
	return Envelope{
		ReadPaths:             e.ReadPaths,
		ModifyPaths:           e.ModifyPaths,
		Symbols:               e.Symbols,
		Components:            e.Components,
		ClaimIDs:              e.ClaimIDs,
		PropositionKeys:       e.PropositionKeys,
		UnsupportedOperations: e.UnsupportedOperations,
	}
}

func projectReasons(in []admissionpkg.Reason) []Reason {
	out := make([]Reason, 0, len(in))
	for _, r := range in {
		out = append(out, Reason{Code: r.Code, Detail: r.Detail})
	}
	return out
}

func projectLimitations(in []architecture.Limitation) []Limitation {
	out := make([]Limitation, 0, len(in))
	for _, l := range in {
		out = append(out, Limitation{Source: l.Source, Scope: l.Scope, Reason: l.Reason, Blocking: l.Blocking})
	}
	return out
}

func projectChangeReceipts(in []admissionpkg.ChangeReceipt) []ChangeReceipt {
	out := make([]ChangeReceipt, 0, len(in))
	for _, c := range in {
		out = append(out, ChangeReceipt{
			Path:                c.Path,
			OldPath:             c.OldPath,
			ChangeType:          c.ChangeType,
			CurrentDigestSHA256: c.CurrentDigestSHA256,
			CurrentSize:         c.CurrentSize,
		})
	}
	return out
}

func projectViolations(in []admissionpkg.Violation) []Violation {
	out := make([]Violation, 0, len(in))
	for _, v := range in {
		out = append(out, Violation{
			Code:              v.Code,
			Path:              v.Path,
			ObservedOperation: v.ObservedOperation,
			ExpectedOperation: v.ExpectedOperation,
			Detail:            v.Detail,
		})
	}
	return out
}

func conditionIDs(in []closure.Condition) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		out = append(out, c.ID)
	}
	return out
}

func guidanceIDs(in []admissionpkg.GuidanceItem) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		out = append(out, g.ID)
	}
	return out
}

func proofIDs(in []admissionpkg.ProofReceipt) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, p.ID)
	}
	return out
}
