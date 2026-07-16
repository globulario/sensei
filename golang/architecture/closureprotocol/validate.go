// SPDX-License-Identifier: Apache-2.0

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
	if strings.TrimSpace(in.OperationID) == "" {
		return errors.New("operation_id is required")
	}
	if !validReceiptStatus(in.Status) {
		return errors.New("status is invalid")
	}
	if !validMechanismKind(in.SelectedMechanism) {
		return errors.New("selected_mechanism is invalid")
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
