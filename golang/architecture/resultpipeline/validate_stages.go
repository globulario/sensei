// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"bytes"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/seedmeta"
)

// decodeCanonical strictly decodes a JSON stage artifact into ptr, proves the
// bytes are canonical by re-rendering the decoded value byte-identically, and
// proves the receipt's semantic digest is the digest of that exact decoded value.
func decodeCanonical(art PipelineArtifact, ptr any) error {
	if err := strictDecode(art.Bytes, ptr); err != nil {
		return verr(CodeStageBytesInvalid, art.Stage, "strict decode: %v", err)
	}
	rendered, err := stageJSONBytes(ptr)
	if err != nil {
		return verr(CodeStageBytesInvalid, art.Stage, "re-render: %v", err)
	}
	if !bytes.Equal(rendered, art.Bytes) {
		return verr(CodeStageBytesInvalid, art.Stage, "artifact bytes are not canonical for the decoded value")
	}
	sem, err := closureprotocol.SemanticDigest(ptr)
	if err != nil {
		return verr(CodeStageSemanticDigestMismatch, art.Stage, "semantic digest: %v", err)
	}
	if sem != art.Receipt.SemanticDigestSHA256 {
		return verr(CodeStageSemanticDigestMismatch, art.Stage, "semantic digest of decoded value does not match receipt")
	}
	return nil
}

// decodeAndCheckStage strictly decodes one first-nine stage and enforces its
// stage-specific cross-stage consistency laws.
func decodeAndCheckStage(dc *decodeContext, art PipelineArtifact) error {
	switch art.Stage {
	case closureprotocol.StageGovernedSourceManifest:
		return checkStage1(dc, art)
	case closureprotocol.StageGeneratedRepositoryArtifacts:
		return checkStage2(dc, art)
	case closureprotocol.StageArchitectureGraph:
		return checkStage3(dc, art)
	case closureprotocol.StageInferredClaims:
		return checkStage4(dc, art)
	case closureprotocol.StageMaintainedClaims:
		return checkStage5(dc, art)
	case closureprotocol.StagePlaneAssessment:
		return checkStage6(dc, art)
	case closureprotocol.StageClosureAssessment:
		return checkStage7(dc, art)
	case closureprotocol.StageArchitectQuestions:
		return checkStage8(dc, art)
	case closureprotocol.StageProofRequirements:
		return checkStage9(dc, art)
	}
	return verr(CodeStageContractInvalid, art.Stage, "no decoder for stage")
}

func checkStage1(dc *decodeContext, art PipelineArtifact) error {
	var b GovernedSourceManifestBundle
	if err := decodeCanonical(art, &b); err != nil {
		return err
	}
	if strings.TrimSpace(b.RepositoryDomain) == "" {
		return verr(CodeStageContentMismatch, art.Stage, "empty repository domain")
	}
	if strings.TrimSpace(b.GraphInputPolicyID) == "" {
		return verr(CodeStageContentMismatch, art.Stage, "empty graph input policy id")
	}
	if !isHex64(b.GraphInputSnapshotDigestSHA256) {
		return verr(CodeStageContentMismatch, art.Stage, "graph input snapshot digest is not a 64-hex sha256")
	}
	// Deterministic, duplicate-free, filesystem-clean source roots.
	seenRoot := map[string]bool{}
	for _, r := range b.LogicalSourceRoots {
		if err := cleanLogicalPath(art.Stage, r.LogicalPath); err != nil {
			return err
		}
		if seenRoot[r.LogicalPath] {
			return verr(CodeStageContentMismatch, art.Stage, "duplicate source root %q", r.LogicalPath)
		}
		seenRoot[r.LogicalPath] = true
	}
	seenSup := map[string]bool{}
	for _, s := range b.SupplementalGraphs {
		if seenSup[s.ID] {
			return verr(CodeStageContentMismatch, art.Stage, "duplicate supplemental graph %q", s.ID)
		}
		seenSup[s.ID] = true
	}
	dc.repoDomain = b.RepositoryDomain
	return nil
}

// cleanLogicalPath rejects absolute, temp, and active-pointer paths in Stage 1.
func cleanLogicalPath(stage closureprotocol.ResultPipelineStage, p string) error {
	if strings.HasPrefix(p, "/") {
		return verr(CodeStageContentMismatch, stage, "absolute path %q", p)
	}
	if strings.HasPrefix(p, "/tmp/") || strings.Contains(p, "/tmp/") {
		return verr(CodeStageContentMismatch, stage, "temporary path %q", p)
	}
	if strings.Contains(p, "/.sensei/active") || strings.Contains(p, "/current") {
		return verr(CodeStageContentMismatch, stage, "active-pointer path %q", p)
	}
	return nil
}

func checkStage2(dc *decodeContext, art PipelineArtifact) error {
	var m generatedartifact.VerificationManifest
	if err := decodeCanonical(art, &m); err != nil {
		return err
	}
	if !m.AllRequiredMatched {
		return verr(CodeStageContentMismatch, art.Stage, "generated artifacts did not all verify")
	}
	// RequiredPaths sorted, duplicate-free.
	for i := 1; i < len(m.RequiredPaths); i++ {
		if m.RequiredPaths[i] <= m.RequiredPaths[i-1] {
			return verr(CodeStageContentMismatch, art.Stage, "required paths are not sorted and duplicate-free")
		}
	}
	// Result-binding generated-artifact set, exact (path -> byte digest).
	rbArtifacts := map[string]string{}
	for _, ga := range dc.result.ResultBinding.GeneratedArtifacts {
		rbArtifacts[ga.Path] = ga.DigestSHA256
	}
	verifiedBytes := map[string]string{}
	proofFound := false
	for _, e := range m.Entries {
		if e.Status != "verified" {
			return verr(CodeStageContentMismatch, art.Stage, "entry %q status %q, want verified", e.Path, e.Status)
		}
		if !isHex64(e.ExpectedSemanticDigestSHA256) || !isHex64(e.ExpectedByteDigestSHA256) {
			return verr(CodeStageContentMismatch, art.Stage, "entry %q expected digests are not 64-hex", e.Path)
		}
		// Byte equality is the regeneration proof: identical regenerated bytes imply
		// identical content. (The engine's observed semantic digest for a non-graph
		// artifact is its byte digest, so byte equality is the meaningful check.)
		if e.ObservedByteDigestSHA256 != e.ExpectedByteDigestSHA256 {
			return verr(CodeStageContentMismatch, art.Stage, "entry %q byte digest not regenerated to equality", e.Path)
		}
		verifiedBytes[e.Path] = e.ExpectedByteDigestSHA256
		if e.Path == generatedartifact.ProofObligationsPath {
			proofFound = true
			dc.proofObligationSem = e.ExpectedSemanticDigestSHA256
		}
	}
	// Exact correspondence with the result binding's generated artifacts.
	if len(verifiedBytes) != len(rbArtifacts) {
		return verr(CodeStageContentMismatch, art.Stage, "generated-artifact set differs from the result binding")
	}
	for p, d := range rbArtifacts {
		if verifiedBytes[p] != d {
			return verr(CodeStageContentMismatch, art.Stage, "generated artifact %q differs from the result binding", p)
		}
	}
	if !proofFound {
		return verr(CodeStageContentMismatch, art.Stage, "no verified entry for %q", generatedartifact.ProofObligationsPath)
	}
	dc.stage2Semantic = art.Receipt.SemanticDigestSHA256
	dc.generatedArtifactsSem = art.Receipt.SemanticDigestSHA256
	return nil
}

func checkStage3(dc *decodeContext, art PipelineArtifact) error {
	marker, ok := seedmeta.ParseMarker(art.Bytes)
	if !ok {
		return verr(CodeStageBytesInvalid, art.Stage, "architecture graph carries no seed marker")
	}
	if marker.Digest != art.Receipt.SemanticDigestSHA256 {
		return verr(CodeStageSemanticDigestMismatch, art.Stage, "graph marker digest does not match the Stage 3 receipt")
	}
	if marker.Digest != dc.result.ResultBinding.GraphDigestSHA256 {
		return verr(CodeStageContentMismatch, art.Stage, "graph digest does not match the frozen result binding")
	}
	dc.graphSemantic = art.Receipt.SemanticDigestSHA256
	return nil
}

func checkStage4(dc *decodeContext, art PipelineArtifact) error {
	var b InferredClaimsBundle
	if err := decodeCanonical(art, &b); err != nil {
		return err
	}
	if err := architecture.ValidateClaimDocument(b.Document); err != nil {
		return verr(CodeStageContentMismatch, art.Stage, "invalid claim document: %v", err)
	}
	return checkResultClaimBinding(dc, art, b.Document.Binding)
}

func checkStage5(dc *decodeContext, art PipelineArtifact) error {
	var b MaintainedClaimsBundle
	if err := decodeCanonical(art, &b); err != nil {
		return err
	}
	if err := architecture.ValidateClaimDocument(b.Document); err != nil {
		return verr(CodeStageContentMismatch, art.Stage, "invalid claim document: %v", err)
	}
	if err := checkResultClaimBinding(dc, art, b.Document.Binding); err != nil {
		return err
	}
	if !bindingIsResult(dc, b.Report.CurrentBinding) {
		return verr(CodeStageBindingMismatch, art.Stage, "maintenance current binding is not the current result")
	}
	if !bindingIsResult(dc, b.Report.ObservedBinding) {
		return verr(CodeStageBindingMismatch, art.Stage, "maintenance observed binding is not the current result")
	}
	if strings.TrimSpace(b.Report.EvaluatedAt) != strings.TrimSpace(dc.result.EvaluatedAt) {
		return verr(CodeStageContentMismatch, art.Stage, "maintenance evaluated_at differs from the build")
	}
	return nil
}

func checkStage6(dc *decodeContext, art PipelineArtifact) error {
	var r plane.Report
	if err := decodeCanonical(art, &r); err != nil {
		return err
	}
	if r.ClaimBinding.RepositoryDomain != dc.repoDomain ||
		r.ClaimBinding.TreeDigestSHA256 != dc.result.ResultBinding.ResultTreeDigestSHA256 ||
		r.ClaimBinding.GraphDigestSHA256 != dc.result.ResultBinding.GraphDigestSHA256 {
		return verr(CodeStageBindingMismatch, art.Stage, "plane claim binding is not the current result")
	}
	if r.GraphSnapshot.DigestSHA256 != dc.result.ResultBinding.GraphDigestSHA256 {
		return verr(CodeStageContentMismatch, art.Stage, "plane graph snapshot digest differs from the result graph")
	}
	if strings.TrimSpace(r.GraphSnapshot.DigestStatus) != architecture.GraphDigestResolved {
		return verr(CodeStageContentMismatch, art.Stage, "plane graph digest status is not resolved")
	}
	return nil
}

func checkStage7(dc *decodeContext, art PipelineArtifact) error {
	var r closure.Report
	if err := decodeCanonical(art, &r); err != nil {
		return err
	}
	if !bindingIsResult(dc, r.Request.Binding) {
		return verr(CodeStageBindingMismatch, art.Stage, "closure request binding is not the current result")
	}
	if !bindingIsResult(dc, r.ObservedBinding) {
		return verr(CodeStageBindingMismatch, art.Stage, "closure observed binding is not the current result")
	}
	switch r.Verdict {
	case closure.VerdictClosed, closure.VerdictConditionallyClosed, closure.VerdictOpen:
	case closure.VerdictUncertifiable:
		return verr(CodeProofExtractionUncertifiable, art.Stage, "closure verdict is uncertifiable")
	default:
		return verr(CodeStageContentMismatch, art.Stage, "closure verdict %q is outside the closed vocabulary", r.Verdict)
	}
	seenBlk := map[string]bool{}
	for _, blk := range r.Blockers {
		if seenBlk[blk.ID] {
			return verr(CodeStageContentMismatch, art.Stage, "duplicate closure blocker id %q", blk.ID)
		}
		seenBlk[blk.ID] = true
	}
	// Duplicated top-level view: the receipt-bound Stage 7 report is the same as
	// BuildResult.ClosureReport.
	if closureprotocol.MustSemanticDigest(r) != closureprotocol.MustSemanticDigest(dc.result.ClosureReport) {
		return verr(CodeStageContentMismatch, art.Stage, "top-level closure report differs from the Stage 7 artifact")
	}
	dc.closureSemantic = art.Receipt.SemanticDigestSHA256
	dc.closureBlockers = seenBlk
	return nil
}

func checkStage8(dc *decodeContext, art PipelineArtifact) error {
	var b ArchitectQuestionsBundle
	if err := decodeCanonical(art, &b); err != nil {
		return err
	}
	if !bindingIsResult(dc, b.Report.Binding) {
		return verr(CodeStageBindingMismatch, art.Stage, "questions report binding is not the current result")
	}
	if !bindingIsResult(dc, b.Dialogue.Binding) {
		return verr(CodeStageBindingMismatch, art.Stage, "questions dialogue binding is not the current result")
	}
	// Current blocker IDs equal the exact Stage 7 blocker set.
	if len(b.CurrentBlockerIDs) != len(dc.closureBlockers) {
		return verr(CodeStageContentMismatch, art.Stage, "current blocker set differs from Stage 7")
	}
	seen := map[string]bool{}
	for _, id := range b.CurrentBlockerIDs {
		if seen[id] {
			return verr(CodeStageContentMismatch, art.Stage, "duplicate current blocker id %q", id)
		}
		seen[id] = true
		if !dc.closureBlockers[id] {
			return verr(CodeStageContentMismatch, art.Stage, "current blocker %q is not a Stage 7 blocker", id)
		}
	}
	// Duplicated top-level dialogue view.
	if closureprotocol.MustSemanticDigest(b.Dialogue) != closureprotocol.MustSemanticDigest(dc.result.Dialogue) {
		return verr(CodeStageContentMismatch, art.Stage, "top-level dialogue differs from the Stage 8 artifact")
	}
	dc.questionsSemantic = art.Receipt.SemanticDigestSHA256
	return nil
}

func checkStage9(dc *decodeContext, art PipelineArtifact) error {
	var doc proofrequirements.Document
	if err := decodeCanonical(art, &doc); err != nil {
		return err
	}
	if err := proofrequirements.ValidateDocument(doc); err != nil {
		return verr(CodeStageContentMismatch, art.Stage, "invalid proof document: %v", err)
	}
	// Duplicated top-level proof view.
	if closureprotocol.MustSemanticDigest(doc) != closureprotocol.MustSemanticDigest(dc.result.ProofRequirements) {
		return verr(CodeStageContentMismatch, art.Stage, "top-level proof requirements differ from the Stage 9 artifact")
	}
	if doc.ResultBindingDigestSHA256 != dc.result.ResultBindingDigestSHA256 {
		return verr(CodeStageBindingMismatch, art.Stage, "proof document result binding digest mismatch")
	}
	// Cross-source digest checks.
	b := dc.result.BoundRepositoryResult
	for _, c := range []struct {
		name, got, want string
	}{
		{"authority_resolution", doc.SourceAuthorityResolutionDigestSHA256, b.AuthorityResolutionDigestSHA256},
		{"admission_decision", doc.SourceAdmissionDecisionDigestSHA256, b.AdmissionDecisionDigestSHA256},
		{"generated_artifacts", doc.SourceGeneratedArtifactsDigestSHA256, dc.generatedArtifactsSem},
		{"repository_proof", doc.SourceRepositoryProofDigestSHA256, dc.proofObligationSem},
		{"graph", doc.SourceGraphDigestSHA256, dc.graphSemantic},
		{"closure", doc.SourceClosureDigestSHA256, dc.closureSemantic},
		{"questions", doc.SourceQuestionsDigestSHA256, dc.questionsSemantic},
	} {
		if c.got != c.want {
			return verr(CodeStageContentMismatch, art.Stage, "proof source digest %s does not match its stage", c.name)
		}
	}
	if doc.CompletionPolicyID != strings.TrimSpace(b.AdmissionDecision.CompletionPolicyID) {
		return verr(CodeStageContentMismatch, art.Stage, "proof completion policy differs from the admission decision")
	}
	// Admission monotonic floor: no carried admission requirement may vanish.
	if err := checkAdmissionFloor(art.Stage, b.AdmissionDecision, doc); err != nil {
		return err
	}
	// Completeness gate.
	switch doc.ExtractionCompleteness {
	case proofrequirements.ExtractionComplete:
	case proofrequirements.ExtractionIncomplete:
		return verr(CodeProofExtractionIncomplete, art.Stage, "proof extraction is incomplete")
	case proofrequirements.ExtractionUncertifiable:
		return verr(CodeProofExtractionUncertifiable, art.Stage, "proof extraction is uncertifiable")
	default:
		return verr(CodeStageContentMismatch, art.Stage, "unknown extraction completeness %q", doc.ExtractionCompleteness)
	}
	for _, cov := range doc.SourceCoverage {
		if cov.Status != proofrequirements.CoverageConsulted {
			return verr(CodeProofExtractionIncomplete, art.Stage, "source %q not consulted (%s)", cov.Source, cov.Status)
		}
	}
	// Disposition: ready or blocked; uncertifiable refused; blocked needs a reason.
	switch doc.ProvingDisposition {
	case proofrequirements.ProvingReady:
	case proofrequirements.ProvingBlocked:
		if !blockedReasonRepresented(doc) {
			return verr(CodeStageContentMismatch, art.Stage, "proving is blocked with no represented reason")
		}
	case proofrequirements.ProvingUncertifiable:
		return verr(CodeProofExtractionUncertifiable, art.Stage, "proving disposition is uncertifiable")
	default:
		return verr(CodeStageContentMismatch, art.Stage, "unknown proving disposition %q", doc.ProvingDisposition)
	}
	return nil
}

func checkAdmissionFloor(stage closureprotocol.ResultPipelineStage, dec closureprotocol.AdmissionDecision, doc proofrequirements.Document) error {
	slotIDs := map[string]bool{}
	for _, r := range doc.RequiredSlots {
		slotIDs[r.ID] = true
	}
	for _, id := range dec.RequiredProofSlots {
		if id = strings.TrimSpace(id); id != "" && !slotIDs[id] {
			return verr(CodeStageContentMismatch, stage, "admission proof slot %q vanished from the requirements", id)
		}
	}
	evIDs := map[string]bool{}
	for _, r := range doc.RuntimeEvidenceProfiles {
		evIDs[r.ID] = true
	}
	for _, id := range dec.RequiredEvidenceProfiles {
		if id = strings.TrimSpace(id); id != "" && !evIDs[id] {
			return verr(CodeStageContentMismatch, stage, "admission evidence profile %q vanished from the requirements", id)
		}
	}
	rbIDs := map[string]bool{}
	for _, r := range doc.RequiredResultRebuilds {
		rbIDs[r.ID] = true
	}
	for _, id := range dec.RequiredResultRebuilds {
		if id = strings.TrimSpace(id); id != "" && !rbIDs[id] {
			return verr(CodeStageContentMismatch, stage, "admission result rebuild %q vanished from the requirements", id)
		}
	}
	return nil
}

// blockedReasonRepresented reports whether a complete-but-blocked document carries
// at least one represented reason for the block.
func blockedReasonRepresented(doc proofrequirements.Document) bool {
	if len(doc.ArchitectQuestions) > 0 || len(doc.ClosureBlockers) > 0 {
		return true
	}
	for _, ch := range doc.RequirementChanges {
		if ch.Disposition == "governance_review_required" {
			return true
		}
	}
	return false
}

func checkResultClaimBinding(dc *decodeContext, art PipelineArtifact, b architecture.ClaimDocumentBinding) error {
	if !bindingIsResult(dc, b) {
		return verr(CodeStageBindingMismatch, art.Stage, "claim binding is not the current result")
	}
	return nil
}

// bindingIsResult reports whether a claim binding identifies the exact current
// result by domain, result tree digest, and result graph digest.
func bindingIsResult(dc *decodeContext, b architecture.ClaimDocumentBinding) bool {
	return b.RepositoryDomain == dc.repoDomain &&
		b.TreeDigestSHA256 == dc.result.ResultBinding.ResultTreeDigestSHA256 &&
		b.GraphDigestSHA256 == dc.result.ResultBinding.GraphDigestSHA256
}

// validateManifest implements §17: the Stage 10 manifest is exact — nine entries
// in canonical order, each matching its source artifact field-for-field, and its
// derivation inputs equal all nine receipt digests in order, with no self-view.
func validateManifest(result BuildResult, byStage map[closureprotocol.ResultPipelineStage]PipelineArtifact, receiptDigest map[closureprotocol.ResultPipelineStage]string) error {
	art := byStage[closureprotocol.StageArtifactManifest]
	if err := validateDerivationManifest(art, receiptDigest); err != nil {
		return err
	}
	var m ArtifactManifestBundle
	if err := decodeCanonical(art, &m); err != nil {
		return err
	}
	if m.ResultBindingDigestSHA256 != result.ResultBindingDigestSHA256 {
		return verr(CodeArtifactManifestMismatch, art.Stage, "manifest bound to a different result")
	}
	first9 := closureprotocol.ResultPipelineStages[:9]
	if len(m.Stages) != len(first9) {
		return verr(CodeArtifactManifestMismatch, art.Stage, "manifest lists %d entries, want %d", len(m.Stages), len(first9))
	}
	for i, stage := range first9 {
		e := m.Stages[i]
		if e.Stage != string(stage) {
			return verr(CodeArtifactManifestMismatch, art.Stage, "entry %d is %q, want %q", i, e.Stage, stage)
		}
		src := byStage[stage]
		if e.LogicalPath != src.LogicalPath ||
			e.MediaType != src.MediaType ||
			e.SemanticDigestSHA256 != src.Receipt.SemanticDigestSHA256 ||
			e.ByteDigestSHA256 != src.Receipt.ByteDigestSHA256 ||
			e.ProducerID != src.Receipt.Producer.ID ||
			e.ProducerVersion != src.Receipt.Producer.Version ||
			e.ReceiptDigestSHA256 != src.Receipt.ReceiptDigestSHA256 {
			return verr(CodeArtifactManifestMismatch, art.Stage, "entry %q does not match its source artifact", stage)
		}
		if !equalStrings(e.DerivationInputs, src.Derivation.InputArtifactReceiptDigestsSHA256) {
			return verr(CodeArtifactManifestMismatch, art.Stage, "entry %q derivation inputs are stale", stage)
		}
		if stage == closureprotocol.StageArtifactManifest {
			return verr(CodeArtifactManifestMismatch, art.Stage, "manifest lists itself")
		}
	}
	return nil
}

// validateDerivationManifest checks the Stage 10 derivation: it names the manifest
// stage, outputs the manifest receipt, and its inputs are all nine prior receipt
// digests in canonical order.
func validateDerivationManifest(art PipelineArtifact, receiptDigest map[closureprotocol.ResultPipelineStage]string) error {
	d := art.Derivation
	if d.Stage != closureprotocol.StageArtifactManifest {
		return verr(CodeStageDerivationMismatch, art.Stage, "manifest derivation stage %q", d.Stage)
	}
	if d.OutputArtifactReceiptDigestSHA256 != art.Receipt.ReceiptDigestSHA256 {
		return verr(CodeStageDerivationMismatch, art.Stage, "manifest derivation output is not its receipt")
	}
	if len(d.InputBindingDigestsSHA256) != 1 || d.InputBindingDigestsSHA256[0] != art.Receipt.ResultBindingDigestSHA256 {
		return verr(CodeStageDerivationMismatch, art.Stage, "manifest derivation input binding is not the current result")
	}
	want := make([]string, 0, 9)
	for _, stage := range closureprotocol.ResultPipelineStages[:9] {
		want = append(want, receiptDigest[stage])
	}
	if !equalStrings(d.InputArtifactReceiptDigestsSHA256, want) {
		return verr(CodeStageDerivationMismatch, art.Stage, "manifest derivation inputs are not the nine prior receipts in order")
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
