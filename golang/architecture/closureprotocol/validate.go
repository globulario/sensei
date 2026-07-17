// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func ValidateTaskTransition(from, to TaskPhase) error {
	if !validTaskPhase(from) || !validTaskPhase(to) {
		return errors.New("unknown task phase")
	}
	for _, allowed := range AllowedTaskTransitions[from] {
		if allowed == to {
			return nil
		}
	}
	return fmt.Errorf("illegal task transition %s -> %s", from, to)
}

func ValidateActorBinding(in ActorBinding) error {
	var errs []string
	if strings.TrimSpace(in.PrincipalID) == "" {
		errs = append(errs, "principal_id is required")
	}
	if !validActorKind(in.ActorKind) {
		errs = append(errs, "actor_kind is invalid")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func ValidateAuthenticationReceipt(in AuthenticationReceipt) error {
	if strings.TrimSpace(in.ReceiptID) == "" {
		return errors.New("receipt_id is required")
	}
	if strings.TrimSpace(in.PrincipalID) == "" {
		return errors.New("principal_id is required")
	}
	if strings.TrimSpace(in.Issuer) == "" {
		return errors.New("issuer is required")
	}
	if err := ValidateLedgerPayloadRef(in.AuthenticationArtifact); err != nil {
		return fmt.Errorf("authentication_artifact: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, in.AuthenticatedAt); err != nil {
		return errors.New("authenticated_at must be RFC3339")
	}
	if strings.TrimSpace(in.ExpiresAt) != "" {
		if _, err := time.Parse(time.RFC3339, in.ExpiresAt); err != nil {
			return errors.New("expires_at must be RFC3339")
		}
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	return nil
}

func ValidateRoleAttestationReceipt(in RoleAttestationReceipt) error {
	if strings.TrimSpace(in.ReceiptID) == "" {
		return errors.New("receipt_id is required")
	}
	if strings.TrimSpace(in.PrincipalID) == "" {
		return errors.New("principal_id is required")
	}
	if !validActorKind(in.ActorKind) {
		return errors.New("actor_kind is invalid")
	}
	if strings.TrimSpace(in.Issuer) == "" {
		return errors.New("issuer is required")
	}
	if len(NormalizeSet(in.RoleIDs)) == 0 {
		return errors.New("role_ids are required")
	}
	if _, err := time.Parse(time.RFC3339, in.IssuedAt); err != nil {
		return errors.New("issued_at must be RFC3339")
	}
	if strings.TrimSpace(in.ValidUntil) != "" {
		if _, err := time.Parse(time.RFC3339, in.ValidUntil); err != nil {
			return errors.New("valid_until must be RFC3339")
		}
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	return nil
}

func ValidateDelegationReceipt(in DelegationReceipt) error {
	if strings.TrimSpace(in.DelegationID) == "" {
		return errors.New("delegation_id is required")
	}
	if strings.TrimSpace(in.DelegatorPrincipalID) == "" || strings.TrimSpace(in.DelegatedPrincipalID) == "" {
		return errors.New("delegator_principal_id and delegated_principal_id are required")
	}
	if strings.TrimSpace(in.ParentGrantID) == "" && strings.TrimSpace(in.ParentDelegationID) == "" {
		return errors.New("parent_grant_id or parent_delegation_id is required")
	}
	if strings.TrimSpace(in.PolicyID) == "" || strings.TrimSpace(in.Issuer) == "" {
		return errors.New("policy_id and issuer are required")
	}
	if _, err := time.Parse(time.RFC3339, in.IssuedAt); err != nil {
		return errors.New("issued_at must be RFC3339")
	}
	if _, err := time.Parse(time.RFC3339, in.ValidFrom); err != nil {
		return errors.New("valid_from must be RFC3339")
	}
	if strings.TrimSpace(in.ValidUntil) != "" {
		if _, err := time.Parse(time.RFC3339, in.ValidUntil); err != nil {
			return errors.New("valid_until must be RFC3339")
		}
	}
	for _, action := range in.Actions {
		if !validOperationKind(action) {
			return errors.New("actions contains invalid operation kind")
		}
	}
	for _, mechanism := range in.MechanismKinds {
		if !validMechanismKind(mechanism) {
			return errors.New("mechanism_kinds contains invalid mechanism kind")
		}
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	return nil
}

func ValidateRepositorySnapshot(in RepositorySnapshot) error {
	if strings.TrimSpace(in.Domain) == "" {
		return errors.New("repository domain is required")
	}
	if strings.TrimSpace(in.RevisionStatus) == "" {
		return errors.New("repository revision_status is required")
	}
	if strings.TrimSpace(in.RevisionStatus) == "resolved" && strings.TrimSpace(in.Revision) == "" {
		return errors.New("repository revision is required when revision_status is resolved")
	}
	return nil
}

func ValidateGraphSnapshot(in GraphSnapshot) error {
	if strings.TrimSpace(in.DigestStatus) == "" {
		return errors.New("graph digest_status is required")
	}
	if strings.TrimSpace(in.DigestStatus) == "resolved" && strings.TrimSpace(in.DigestSHA256) == "" {
		return errors.New("graph digest is required when digest_status is resolved")
	}
	return nil
}

func ValidateLedgerPayloadRef(in LedgerPayloadRef) error {
	path := filepath.ToSlash(strings.TrimSpace(in.Path))
	if path == "" {
		return errors.New("payload path is required")
	}
	if strings.HasPrefix(path, "/") || path == "." || path == ".." || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") {
		return errors.New("payload path must be confined and relative")
	}
	if strings.TrimSpace(in.MediaType) == "" {
		return errors.New("payload media_type is required")
	}
	if strings.TrimSpace(in.DigestSHA256) == "" {
		return errors.New("payload digest_sha256 is required")
	}
	return nil
}

func ValidateBaseBinding(in BaseBinding) error {
	if err := ValidateRepositorySnapshot(in.Repository); err != nil {
		return err
	}
	if err := ValidateGraphSnapshot(in.Graph); err != nil {
		return err
	}
	if strings.TrimSpace(in.Task.ID) == "" || strings.TrimSpace(in.Task.SessionID) == "" {
		return errors.New("task binding requires id and session_id")
	}
	if strings.TrimSpace(in.Policies.Canonicalization) == "" || strings.TrimSpace(in.Policies.Completion) == "" {
		return errors.New("policy binding is incomplete")
	}
	return nil
}

func ValidateLedgerEntry(in LedgerEntry) error {
	if in.Sequence < 1 {
		return errors.New("sequence must be >= 1")
	}
	if !validLedgerEventType(in.EventType) {
		return errors.New("event_type is invalid")
	}
	if strings.TrimSpace(in.Task.ID) == "" || strings.TrimSpace(in.Task.SessionID) == "" {
		return errors.New("task binding requires id and session_id")
	}
	if err := ValidateLedgerPayloadRef(in.Payload); err != nil {
		return err
	}
	if strings.TrimSpace(in.Producer) == "" {
		return errors.New("producer is required")
	}
	if _, err := time.Parse(time.RFC3339, in.ProducedAt); err != nil {
		return errors.New("produced_at must be RFC3339")
	}
	return nil
}

func ValidateChangePlan(in ChangePlan) error {
	if strings.TrimSpace(in.PlanID) == "" {
		return errors.New("plan_id is required")
	}
	if len(in.Operations) == 0 {
		return errors.New("operations are required")
	}
	for _, op := range in.Operations {
		if strings.TrimSpace(op.OperationID) == "" {
			return errors.New("operation_id is required")
		}
		if !validOperationKind(op.Kind) {
			return fmt.Errorf("invalid operation kind for %s", op.OperationID)
		}
		// rename is a reserved but unsupported operation kind in v1: ChangeOperation
		// carries a single Target and cannot encode distinct source and destination
		// endpoints, so it must fail closed rather than be approximated.
		if op.Kind == OperationRename {
			return errors.New("protocol.rename_requires_explicit_source_and_destination")
		}
		if !validMechanismKind(op.SelectedMechanism) {
			return fmt.Errorf("invalid mechanism kind for %s", op.OperationID)
		}
		if strings.TrimSpace(op.TargetKind) == "" || strings.TrimSpace(op.Target) == "" {
			return fmt.Errorf("operation %s target is incomplete", op.OperationID)
		}
	}
	return nil
}

func ValidateAuthorityResolution(in AuthorityResolution) error {
	if strings.TrimSpace(in.ActorBindingDigestSHA256) == "" {
		return errors.New("actor_binding_digest_sha256 is required")
	}
	if strings.TrimSpace(in.BaseBindingDigestSHA256) == "" {
		return errors.New("base_binding_digest_sha256 is required")
	}
	if strings.TrimSpace(in.ClosureAssessmentDigestSHA256) == "" {
		return errors.New("closure_assessment_digest_sha256 is required")
	}
	if strings.TrimSpace(in.OperationSetDigestSHA256) == "" {
		return errors.New("operation_set_digest_sha256 is required")
	}
	if strings.TrimSpace(in.AuthorityPolicyGraphDigestSHA256) == "" {
		return errors.New("authority_policy_graph_digest_sha256 is required")
	}
	if strings.TrimSpace(in.PolicyID) == "" {
		return errors.New("policy_id is required")
	}
	if _, err := time.Parse(time.RFC3339, in.EvaluatedAt); err != nil {
		return errors.New("evaluated_at must be RFC3339")
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	if len(in.OperationResults) == 0 {
		return errors.New("operation_results are required")
	}
	for _, result := range in.OperationResults {
		if strings.TrimSpace(result.OperationID) == "" {
			return errors.New("operation_result operation_id is required")
		}
		if !validReceiptStatus(result.Status) {
			return fmt.Errorf("operation_result %s has invalid status", result.OperationID)
		}
		if !validMechanismKind(result.SelectedMechanism) {
			return fmt.Errorf("operation_result %s has invalid selected_mechanism", result.OperationID)
		}
	}
	return nil
}

func ValidateAdmissionRequest(in AdmissionRequest) error {
	if err := ValidateActorBinding(in.ActorBinding); err != nil {
		return err
	}
	if err := ValidateBaseBinding(in.BaseBinding); err != nil {
		return err
	}
	if err := ValidateChangePlan(in.ChangePlan); err != nil {
		return err
	}
	if strings.TrimSpace(in.AuthorityResolutionDigestSHA256) == "" {
		return errors.New("authority_resolution_digest_sha256 is required")
	}
	if strings.TrimSpace(in.PolicyID) == "" {
		return errors.New("policy_id is required")
	}
	return nil
}

func ValidateAdmissionDecision(in AdmissionDecision) error {
	if strings.TrimSpace(in.RequestDigestSHA256) == "" {
		return errors.New("request_digest_sha256 is required")
	}
	if strings.TrimSpace(in.PolicyID) == "" {
		return errors.New("policy_id is required")
	}
	if len(in.OperationVerdicts) == 0 {
		return errors.New("operation_verdicts are required")
	}
	if strings.TrimSpace(in.CapabilityID) == "" || strings.TrimSpace(in.CompletionPolicyID) == "" {
		return errors.New("capability_id and completion_policy_id are required")
	}
	return nil
}

func ValidateCapabilityConsumption(in CapabilityConsumption) error {
	if strings.TrimSpace(in.CapabilityID) == "" {
		return errors.New("capability_id is required")
	}
	if err := ValidateActorBinding(in.ConsumerActor); err != nil {
		return err
	}
	if in.OneUseStatus != ReceiptValid {
		return errors.New("one_use_status must be valid for first use")
	}
	if _, err := time.Parse(time.RFC3339, in.ConsumedAt); err != nil {
		return errors.New("consumed_at must be RFC3339")
	}
	return nil
}

func ValidateEvidenceProfile(in EvidenceProfile) error {
	if strings.TrimSpace(in.ProfileID) == "" || strings.TrimSpace(in.Owner) == "" || strings.TrimSpace(in.LegalObservationPath) == "" {
		return errors.New("profile_id, owner, and legal_observation_path are required")
	}
	if !validEvidenceKind(in.EvidenceKind) || !validReceiptStatus(in.Status) {
		return errors.New("evidence profile kind or status is invalid")
	}
	return nil
}

func ValidateEvidenceReceipt(in EvidenceReceipt) error {
	if strings.TrimSpace(in.ReceiptID) == "" || strings.TrimSpace(in.ProfileID) == "" {
		return errors.New("receipt_id and profile_id are required")
	}
	if !validEvidenceKind(in.EvidenceKind) || !validReceiptStatus(in.Status) {
		return errors.New("evidence receipt kind or status is invalid")
	}
	if _, err := time.Parse(time.RFC3339, in.ObservedAt); err != nil {
		return errors.New("observed_at must be RFC3339")
	}
	if in.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, in.ExpiresAt); err != nil {
			return errors.New("expires_at must be RFC3339")
		}
	}
	if strings.TrimSpace(in.PayloadDigestSHA256) == "" {
		return errors.New("payload_digest_sha256 is required")
	}
	return nil
}

func ValidateProofDischarge(in ProofDischarge) error {
	if strings.TrimSpace(in.ObligationID) == "" {
		return errors.New("obligation_id is required")
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	if len(in.SlotResults) == 0 {
		return errors.New("slot_results are required")
	}
	for _, slot := range in.SlotResults {
		if slot.SlotID == "" || !validDimensionStatus(slot.Status) {
			return errors.New("slot result is invalid")
		}
		if (slot.Status == DimensionPass || slot.Status == DimensionPassWithException) && len(slot.ReceiptIDs) == 0 {
			return errors.New("pass proof slot must map at least one receipt")
		}
	}
	return nil
}

func ValidateCertificationReceipt(in CertificationReceipt) error {
	if strings.TrimSpace(in.CertificationPolicy) == "" {
		return errors.New("certification_policy is required")
	}
	if !validDimensionStatus(in.ScopeLane) || !validDimensionStatus(in.AuthorityLane) ||
		!validDimensionStatus(in.ProofLane) || !validDimensionStatus(in.EvidenceLane) {
		return errors.New("lane status is invalid")
	}
	if !validCertificationVerdict(in.CertificationVerdict) {
		return errors.New("certification verdict is invalid")
	}
	return nil
}

func ValidateWaiverReceipt(in WaiverReceipt) error {
	if strings.TrimSpace(in.WaiverID) == "" || strings.TrimSpace(in.PolicyID) == "" || strings.TrimSpace(in.Justification) == "" {
		return errors.New("waiver_id, policy_id, and justification are required")
	}
	if !validDimension(in.Dimension) || !validReceiptStatus(in.Status) {
		return errors.New("waiver dimension or status is invalid")
	}
	if _, err := time.Parse(time.RFC3339, in.ExpiresAt); err != nil {
		return errors.New("waiver expires_at must be RFC3339")
	}
	return nil
}

func ValidateCompletionReceipt(in CompletionReceipt) error {
	if !validTerminalStatus(in.TerminalStatus) {
		return errors.New("terminal_status is invalid")
	}
	if err := ValidateBaseBinding(in.BaseBinding); err != nil {
		return err
	}
	if strings.TrimSpace(in.ResultBinding.BaseRevision) == "" || strings.TrimSpace(in.ResultBinding.GraphDigestSHA256) == "" {
		return errors.New("result binding is incomplete")
	}
	if in.BaseBinding.Repository.Revision != "" && in.ResultBinding.BaseRevision != "" && in.BaseBinding.Repository.Revision != in.ResultBinding.BaseRevision {
		return errors.New("result base revision must match base binding revision")
	}
	if strings.TrimSpace(in.CertificationDigestSHA256) == "" {
		return errors.New("certification_digest_sha256 is required")
	}
	if strings.TrimSpace(in.CompletionPolicy) == "" || strings.TrimSpace(in.CompletingActor) == "" {
		return errors.New("completion policy and completing actor are required")
	}
	if _, err := time.Parse(time.RFC3339, in.CompletedAt); err != nil {
		return errors.New("completed_at must be RFC3339")
	}
	return nil
}

func ValidateRevocationReceipt(in RevocationReceipt) error {
	if in.RevocationID == "" || in.RevokedTargetID == "" || in.PriorDigestSHA256 == "" || in.PolicyID == "" || in.ActorID == "" {
		return errors.New("revocation receipt is incomplete")
	}
	if _, err := time.Parse(time.RFC3339, in.RevokedAt); err != nil {
		return errors.New("revoked_at must be RFC3339")
	}
	return nil
}

func ValidateMigrationExecutionReceipt(in MigrationExecutionReceipt) error {
	if in.MigrationPlanID == "" || in.StepID == "" || in.SourceState == "" || in.TargetState == "" {
		return errors.New("migration execution receipt is incomplete")
	}
	if !validMechanismKind(in.Mechanism) || !validReceiptStatus(in.Status) {
		return errors.New("migration execution mechanism or status is invalid")
	}
	return nil
}

// governedKnowledgeCategories is the closed set of governed-knowledge categories
// whose change a result transition may record.
var governedKnowledgeCategories = []string{
	"authority", "invariants", "contracts", "failure_modes", "forbidden_fixes",
	"required_tests", "proof_obligations", "evidence_profiles", "certification_policy",
	"completion_policy",
}

// isHexSHA256 reports whether v is exactly 64 lowercase hexadecimal characters —
// the canonical SHA-256 encoding. It rejects 40-char native Git SHA-1 object ids,
// uppercase, empty, and non-hex values, so a native object id can never occupy a
// *_sha256 field.
func isHexSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// validArtifactPath requires a repository-relative or operational-store-relative
// path: never absolute, never containing a ".." traversal, so no absolute
// temporary path can participate in semantic identity.
func validArtifactPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return errors.New("artifact path is required")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || strings.Contains(p, ":\\") {
		return errors.New("artifact path must be relative, not absolute")
	}
	for _, seg := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if seg == ".." {
			return errors.New("artifact path must not contain a .. traversal")
		}
	}
	return nil
}

// GovernedKnowledgeImpactChanged derives whether a category changed. It is the
// only sanctioned way to read "changed": a difference of governed manifest
// digests, never a stored flag.
func GovernedKnowledgeImpactChanged(in GovernedKnowledgeImpact) bool {
	return strings.TrimSpace(in.BaseManifestDigestSHA256) != strings.TrimSpace(in.ResultManifestDigestSHA256)
}

// ValidateArtifactReceipt validates one operational artifact receipt: a closed
// content identity (semantic digest), a mandatory per-artifact producer, a
// serialized-bytes identity when the artifact has a path, a repository/operational
// relative path, and the exact current result binding it belongs to.
func ValidateArtifactReceipt(in ArtifactReceipt) error {
	if strings.TrimSpace(in.Kind) == "" {
		return errors.New("artifact receipt requires kind")
	}
	if !isHexSHA256(in.SemanticDigestSHA256) {
		return errors.New("artifact receipt semantic_digest_sha256 must be a 64-hex sha256")
	}
	if strings.TrimSpace(in.Path) != "" {
		if err := validArtifactPath(in.Path); err != nil {
			return err
		}
		if !isHexSHA256(in.ByteDigestSHA256) {
			return errors.New("artifact receipt with a path requires a 64-hex byte_digest_sha256")
		}
	} else if strings.TrimSpace(in.ByteDigestSHA256) != "" && !isHexSHA256(in.ByteDigestSHA256) {
		return errors.New("artifact receipt byte_digest_sha256 must be a 64-hex sha256")
	}
	if strings.TrimSpace(in.Producer.ID) == "" || strings.TrimSpace(in.Producer.Version) == "" {
		return errors.New("artifact receipt requires a producer id and version")
	}
	if !isHexSHA256(in.ResultBindingDigestSHA256) {
		return errors.New("artifact receipt result_binding_digest_sha256 must be a 64-hex sha256")
	}
	return nil
}

// validateResultBindingShape validates the embedded result binding: a base
// revision and 64-hex patch/result-tree/result-graph digests, and relative
// generated-artifact paths with 64-hex digests.
func validateResultBindingShape(in ResultBinding) error {
	if strings.TrimSpace(in.BaseRevision) == "" {
		return errors.New("result_binding requires base_revision")
	}
	for _, f := range []struct{ name, value string }{
		{"patch_digest_sha256", in.PatchDigestSHA256},
		{"result_tree_digest_sha256", in.ResultTreeDigestSHA256},
		{"graph_digest_sha256", in.GraphDigestSHA256},
	} {
		if !isHexSHA256(f.value) {
			return fmt.Errorf("result_binding %s must be a 64-hex sha256", f.name)
		}
	}
	for _, a := range in.GeneratedArtifacts {
		if err := validArtifactPath(a.Path); err != nil {
			return err
		}
		if !isHexSHA256(a.DigestSHA256) {
			return errors.New("result_binding generated artifact digest must be a 64-hex sha256")
		}
	}
	return nil
}

// ValidateResultBinding exposes the frozen result-binding shape check so a
// producer (e.g. the result pipeline) can validate a completed ResultBinding
// before it binds operational artifacts. It is the same check the result
// transition receipt applies to its embedded binding.
func ValidateResultBinding(in ResultBinding) error { return validateResultBindingShape(in) }

func validGovernedKnowledgeCategory(c string) bool { return contains(governedKnowledgeCategories, c) }
func validResultPipelineStage(s ResultPipelineStage) bool {
	return contains(ResultPipelineStages, s)
}

// ValidateResultTransitionReceipt freezes the Phase 7 result-transition contract.
// It binds the exact upstream Phase 3 truth by digest, embeds one canonical
// result binding (verified against its recomputed digest), and requires the
// operational artifacts and their derivation graph to make freshness structurally
// verifiable against the current result. It establishes no certification and no
// completion.
func ValidateResultTransitionReceipt(in ResultTransitionReceipt) error {
	if strings.TrimSpace(in.Task.ID) == "" || strings.TrimSpace(in.Task.SessionID) == "" {
		return errors.New("result transition receipt requires task id and session id")
	}
	// Exact upstream truth, bound by 64-hex digest — never reconstructed.
	for _, f := range []struct{ name, value string }{
		{"base_binding_digest_sha256", in.BaseBindingDigestSHA256},
		{"actor_binding_digest_sha256", in.ActorBindingDigestSHA256},
		{"authority_resolution_digest_sha256", in.AuthorityResolutionDigestSHA256},
		{"admission_decision_digest_sha256", in.AdmissionDecisionDigestSHA256},
		{"capability_consumption_digest_sha256", in.CapabilityConsumptionDigestSHA256},
		{"observed_change_set_digest_sha256", in.ObservedChangeSetDigestSHA256},
		{"scope_verification_digest_sha256", in.ScopeVerificationDigestSHA256},
	} {
		if !isHexSHA256(f.value) {
			return fmt.Errorf("result transition receipt %s must be a 64-hex sha256", f.name)
		}
	}
	// One canonical result representation: the embedded binding, verified against
	// its own recomputed digest.
	if err := validateResultBindingShape(in.ResultBinding); err != nil {
		return err
	}
	rbDigest, err := ResultBindingDigest(in.ResultBinding)
	if err != nil {
		return err
	}
	if in.ResultBindingDigestSHA256 != rbDigest {
		return errors.New("result_binding_digest_sha256 does not match the embedded result_binding")
	}
	if strings.TrimSpace(in.PipelinePolicyID) == "" {
		return errors.New("result transition receipt requires a pipeline policy id")
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("result transition status is invalid")
	}
	if _, err := time.Parse(time.RFC3339, in.RecordedAt); err != nil {
		return errors.New("recorded_at must be RFC3339")
	}

	// Operational artifacts: each valid, each bound to THIS result binding, indexed
	// by its receipt digest so derivations can reference it.
	opReceipts := map[string]bool{}
	for _, artifact := range in.OperationalArtifactReceipts {
		if err := ValidateArtifactReceipt(artifact); err != nil {
			return err
		}
		if artifact.ResultBindingDigestSHA256 != in.ResultBindingDigestSHA256 {
			return errors.New("operational artifact is bound to a different result binding")
		}
		d, err := ArtifactReceiptDigest(artifact)
		if err != nil {
			return err
		}
		opReceipts[d] = true
	}

	// Derivation graph: closed stages, every mandatory stage present exactly once,
	// every referenced artifact exists, inputs bind the current result, no cycle.
	outputs := map[string]bool{}
	stageSeen := map[ResultPipelineStage]bool{}
	edges := map[string][]string{}
	for _, d := range in.Derivations {
		if !validResultPipelineStage(d.Stage) {
			return fmt.Errorf("unknown derivation stage %q", d.Stage)
		}
		if stageSeen[d.Stage] {
			return fmt.Errorf("derivation stage %q appears more than once", d.Stage)
		}
		stageSeen[d.Stage] = true
		if !isHexSHA256(d.OutputArtifactReceiptDigestSHA256) {
			return errors.New("derivation output_artifact_receipt_digest_sha256 must be a 64-hex sha256")
		}
		if !opReceipts[d.OutputArtifactReceiptDigestSHA256] {
			return errors.New("derivation output references a missing artifact receipt")
		}
		if outputs[d.OutputArtifactReceiptDigestSHA256] {
			return errors.New("derivation output is produced more than once")
		}
		outputs[d.OutputArtifactReceiptDigestSHA256] = true
		for _, inp := range d.InputArtifactReceiptDigestsSHA256 {
			if !isHexSHA256(inp) {
				return errors.New("derivation input artifact digest must be a 64-hex sha256")
			}
			if !opReceipts[inp] {
				return errors.New("derivation references a missing input artifact receipt")
			}
			if inp == d.OutputArtifactReceiptDigestSHA256 {
				return errors.New("derivation cycle: output is its own input")
			}
			edges[d.OutputArtifactReceiptDigestSHA256] = append(edges[d.OutputArtifactReceiptDigestSHA256], inp)
		}
		for _, b := range d.InputBindingDigestsSHA256 {
			if b != in.ResultBindingDigestSHA256 {
				return errors.New("derivation input binding is not the current result binding")
			}
		}
	}
	if len(in.Derivations) > 0 {
		for _, st := range ResultPipelineStages {
			if !stageSeen[st] {
				return fmt.Errorf("missing mandatory derivation stage %q", st)
			}
		}
		if cyclic(edges) {
			return errors.New("derivation graph contains a cycle")
		}
	}

	// Governed-knowledge impacts: closed categories, digest-derived change.
	for _, impact := range in.GovernedKnowledgeImpacts {
		if !validGovernedKnowledgeCategory(impact.Category) {
			return fmt.Errorf("unknown governed knowledge category %q", impact.Category)
		}
		if !isHexSHA256(impact.BaseManifestDigestSHA256) || !isHexSHA256(impact.ResultManifestDigestSHA256) {
			return errors.New("governed knowledge impact manifest digests must be 64-hex sha256")
		}
	}

	// Collapse guard: the six load-bearing identities must be pairwise distinct so
	// a single reused "result digest" cannot masquerade as several facts. Unrelated
	// operational artifacts may legitimately share bytes and are not checked here.
	var manifestDigest string
	for _, d := range in.Derivations {
		if d.Stage == StageArtifactManifest {
			manifestDigest = d.OutputArtifactReceiptDigestSHA256
		}
	}
	loadBearing := []struct{ name, value string }{
		{"base_binding", in.BaseBindingDigestSHA256},
		{"observed_change", in.ObservedChangeSetDigestSHA256},
		{"patch", in.ResultBinding.PatchDigestSHA256},
		{"result_tree", in.ResultBinding.ResultTreeDigestSHA256},
		{"result_graph", in.ResultBinding.GraphDigestSHA256},
		{"artifact_manifest", manifestDigest},
	}
	seenDigest := map[string]string{}
	for _, e := range loadBearing {
		if e.value == "" {
			continue
		}
		if other, dup := seenDigest[e.value]; dup {
			return fmt.Errorf("collapsed digest: %s and %s share one identity", other, e.name)
		}
		seenDigest[e.value] = e.name
	}
	return nil
}

// ReceiptAppliesToCurrentResult reports whether a receipt bound to
// receiptResultBindingDigest is applicable to the current result identified by
// currentResultBindingDigest. Evidence, proof discharge, and certification apply
// to a task result only when their result-binding digest exactly equals the
// current result-transition's result-binding digest. A receipt bound to another
// result remains historically valid and byte-identical, but is inapplicable to
// the current result; nothing about a projection can make it current. History is
// never rewritten.
func ReceiptAppliesToCurrentResult(receiptResultBindingDigest, currentResultBindingDigest string) bool {
	a := strings.TrimSpace(receiptResultBindingDigest)
	b := strings.TrimSpace(currentResultBindingDigest)
	return a != "" && a == b
}

// cyclic reports whether the output->inputs edge map contains a directed cycle.
func cyclic(edges map[string][]string) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(n string) bool
	visit = func(n string) bool {
		color[n] = gray
		for _, m := range edges[n] {
			switch color[m] {
			case gray:
				return true
			case white:
				if visit(m) {
					return true
				}
			}
		}
		color[n] = black
		return false
	}
	for n := range edges {
		if color[n] == white {
			if visit(n) {
				return true
			}
		}
	}
	return false
}

func ValidateEvidenceReceiptAgainstProfile(profile EvidenceProfile, receipt EvidenceReceipt) error {
	if profile.ProfileID != receipt.ProfileID {
		return errors.New("evidence receipt profile_id does not match profile")
	}
	if profile.RuntimeTargetKind != "" && receipt.RuntimeTarget == nil {
		return errors.New("evidence receipt missing required runtime target")
	}
	return nil
}

func ValidateCompletionWaivers(receipt CompletionReceipt, waivers []WaiverReceipt) error {
	if len(waivers) == 0 {
		return nil
	}
	completedAt, err := time.Parse(time.RFC3339, receipt.CompletedAt)
	if err != nil {
		return err
	}
	for _, waiver := range waivers {
		if waiver.Status != ReceiptValid {
			return errors.New("waiver must be valid")
		}
		expiresAt, err := time.Parse(time.RFC3339, waiver.ExpiresAt)
		if err != nil {
			return err
		}
		if !expiresAt.After(completedAt) {
			return errors.New("waiver expired before completion")
		}
	}
	return nil
}

func validActorKind(v ActorKind) bool         { return contains(ActorKinds, v) }
func validOperationKind(v OperationKind) bool { return contains(OperationKinds, v) }
func validMechanismKind(v MechanismKind) bool { return contains(MechanismKinds, v) }
func validEvidenceKind(v EvidenceKind) bool   { return contains(EvidenceKinds, v) }
func validReceiptStatus(v ReceiptStatus) bool { return contains(ReceiptStatuses, v) }
func validCertificationVerdict(v CertificationVerdict) bool {
	return contains(CertificationVerdicts, v)
}
func validDimension(v Dimension) bool               { return contains(Dimensions, v) }
func validDimensionStatus(v DimensionStatus) bool   { return contains(DimensionStatuses, v) }
func validTaskPhase(v TaskPhase) bool               { return contains(TaskPhases, v) }
func validTerminalStatus(v TaskTerminalStatus) bool { return contains(TerminalStatuses, v) }
func validLedgerEventType(v LedgerEventType) bool   { return contains(LedgerEventTypes, v) }

func contains[T comparable](vals []T, v T) bool {
	for _, item := range vals {
		if item == v {
			return true
		}
	}
	return false
}
