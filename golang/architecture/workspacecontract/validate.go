// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import "fmt"

// ValidationError is one violation of this package's cross-field contract —
// rules JSON Schema alone cannot express, mirroring
// golang/architecture/dashboardprojection's ValidationError precedent.
type ValidationError struct {
	Rule   string
	Detail string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Rule, e.Detail)
}

// ValidateIdentity runs every producer-side check beyond what
// workspace-identity-v1.schema.json itself can express. It returns every
// violation found, not just the first.
func ValidateIdentity(id Identity) []ValidationError {
	var errs []ValidationError

	if id.SchemaVersion != IdentitySchemaVersion {
		errs = append(errs, ValidationError{
			Rule:   "unsupported_schema_version",
			Detail: fmt.Sprintf("got %q, only %q is accepted", id.SchemaVersion, IdentitySchemaVersion),
		})
	}

	if id.RepositoryDomainSource == RepositoryDomainConfigured && id.Binding.RepositoryDomain == "" {
		errs = append(errs, ValidationError{
			Rule:   "configured_source_missing_domain",
			Detail: "repository_domain_source is configured but binding.repository_domain is empty",
		})
	}
	if id.RepositoryDomainSource == RepositoryDomainUnbound && id.Binding.RepositoryDomain != "" {
		errs = append(errs, ValidationError{
			Rule:   "unbound_source_carries_domain",
			Detail: "repository_domain_source is unbound but binding.repository_domain is non-empty; an unbound receipt must never carry a guessed domain",
		})
	}

	if want := deriveCompositionState(id); id.CompositionState != want {
		errs = append(errs, ValidationError{
			Rule:   "composition_state_mismatch",
			Detail: fmt.Sprintf("composition_state=%q does not match what the binding/graph_authority facts imply (%q)", id.CompositionState, want),
		})
	}

	if id.TaskIdentity.State == TaskIdentityResolved && (id.TaskIdentity.TaskID == nil || *id.TaskIdentity.TaskID == "") {
		errs = append(errs, ValidationError{
			Rule:   "resolved_task_missing_id",
			Detail: "task_identity.state=resolved requires a non-empty task_id",
		})
	}
	if id.TaskIdentity.State != TaskIdentityResolved && id.TaskIdentity.TaskID != nil {
		errs = append(errs, ValidationError{
			Rule:   "unresolved_task_carries_id",
			Detail: fmt.Sprintf("task_identity.state=%q must carry a null task_id", id.TaskIdentity.State),
		})
	}

	return errs
}

// ValidateAdmission runs every producer-side check beyond what
// workspace-admission-v1.schema.json itself can express.
func ValidateAdmission(a Admission) []ValidationError {
	var errs []ValidationError

	if a.SchemaVersion != AdmissionSchemaVersion {
		errs = append(errs, ValidationError{
			Rule:   "unsupported_schema_version",
			Detail: fmt.Sprintf("got %q, only %q is accepted", a.SchemaVersion, AdmissionSchemaVersion),
		})
	}

	switch a.RecordKind {
	case RecordKindDecision:
		if a.Verification != nil {
			errs = append(errs, ValidationError{
				Rule:   "decision_record_carries_verification",
				Detail: "record_kind=decision requires verification: null",
			})
		}
	case RecordKindVerification:
		if a.Verification == nil {
			errs = append(errs, ValidationError{
				Rule:   "verification_record_missing_verification",
				Detail: "record_kind=verification requires a non-null verification object",
			})
		}
	default:
		errs = append(errs, ValidationError{
			Rule:   "unknown_record_kind",
			Detail: fmt.Sprintf("record_kind %q is not decision or verification", a.RecordKind),
		})
	}

	if a.AdmissionID == "" {
		errs = append(errs, ValidationError{Rule: "missing_admission_id", Detail: "admission_id is required"})
	}
	if a.PolicyID == "" {
		errs = append(errs, ValidationError{Rule: "missing_policy_id", Detail: "policy_id is required"})
	}
	if a.Binding.RepositoryDomain == "" {
		errs = append(errs, ValidationError{Rule: "missing_binding_domain", Detail: "binding.repository_domain is required"})
	}

	return errs
}

// VerificationBoundToDecision reports whether a verification record's
// identity (admission_id, decision_digest_sha256, binding) exactly matches
// the decision record it claims to verify — the structural half of
// "a verification record is never free-floating." Callers that hold both
// the original decision projection and the candidate verification
// projection should call this before trusting the verification record.
func VerificationBoundToDecision(decision, verification Admission) []ValidationError {
	var errs []ValidationError
	if verification.RecordKind != RecordKindVerification {
		errs = append(errs, ValidationError{Rule: "not_a_verification_record", Detail: "expected record_kind=verification"})
		return errs
	}
	if decision.AdmissionID != verification.AdmissionID {
		errs = append(errs, ValidationError{Rule: "admission_id_mismatch", Detail: fmt.Sprintf("decision admission_id %q != verification admission_id %q", decision.AdmissionID, verification.AdmissionID)})
	}
	if decision.DecisionDigestSHA256 != verification.DecisionDigestSHA256 {
		errs = append(errs, ValidationError{Rule: "decision_digest_mismatch", Detail: fmt.Sprintf("decision digest %q != verification-record decision digest %q", decision.DecisionDigestSHA256, verification.DecisionDigestSHA256)})
	}
	if !bindingsEqual(decision.Binding, verification.Binding) {
		errs = append(errs, ValidationError{Rule: "binding_mismatch", Detail: "decision binding does not match verification-record binding"})
	}
	return errs
}

// bindingsEqual compares two Bindings by value: Binding's Revision/
// TreeDigestSHA256/GraphDigestSHA256 are *string, so a plain == or !=
// comparison would compare pointer identity rather than the underlying
// strings.
func bindingsEqual(a, b Binding) bool {
	return a.RepositoryDomain == b.RepositoryDomain &&
		stringPtrEqual(a.Revision, b.Revision) &&
		a.RevisionStatus == b.RevisionStatus &&
		stringPtrEqual(a.TreeDigestSHA256, b.TreeDigestSHA256) &&
		stringPtrEqual(a.GraphDigestSHA256, b.GraphDigestSHA256) &&
		a.GraphDigestStatus == b.GraphDigestStatus
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
