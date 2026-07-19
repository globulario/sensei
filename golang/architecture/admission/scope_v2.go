// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Phase 3 admission v2, slice 3: typed scope verification. After a capability is
// consumed and the mutation is applied, the observed change must be verified
// against exactly what was admitted — operation envelope, actor/authority
// bindings, base tree, required generated artifacts, prohibited paths, and the
// operation/risk budgets (plan §9.5). CorrectnessCertified stays false; scope
// verification proves the change matches its admission, not that it is correct.
//
// phase0 froze only the *digests* of the change set and scope verification
// (referenced by CompletionReceipt / ResultTransitionReceipt), so Phase 3 owns
// these record shapes.

// ObservedFile is one path touched by the applied mutation. When Git reports a
// rename, both endpoints are preserved diagnostically (FromPath/ToPath) so scope
// verification can refuse it honestly without dropping the source path; v1 does
// not admit or verify renames (see architectural-closure-v1.md).
type ObservedFile struct {
	Path       string
	ChangeType string
	FromPath   string
	ToPath     string
}

// ObservedChangeSet is what actually changed, as observed from the repository
// after the mutation (e.g. a git diff against the admitted base).
type ObservedChangeSet struct {
	BaseTreeDigestSHA256            string
	ResultTreeDigestSHA256          string
	ActorBindingDigestSHA256        string
	AuthorityResolutionDigestSHA256 string
	Files                           []ObservedFile
}

// ScopeExpectation is the admission-side truth the observed change is checked
// against. Digests come from the admission context; operations carry the
// admitted targets.
type ScopeExpectation struct {
	Decision                        closureprotocol.AdmissionDecision
	Operations                      []closureprotocol.ChangeOperation
	Consumption                     closureprotocol.CapabilityConsumption
	ActorBindingDigestSHA256        string
	AuthorityResolutionDigestSHA256 string
	BaseTreeDigestSHA256            string
	RequiredGeneratedArtifacts      []string
	ProhibitedPathPrefixes          []string
}

// ScopeViolation records one way the observed change left its admitted envelope.
type ScopeViolation struct {
	Code   string
	Path   string
	Detail string
}

// ScopeVerification is the typed receipt binding an observed change to its
// admission. Status is valid only when there are no violations.
type ScopeVerification struct {
	CapabilityID                    string
	DecisionDigestSHA256            string
	ActorBindingDigestSHA256        string
	AuthorityResolutionDigestSHA256 string
	BaseTreeDigestSHA256            string
	ResultTreeDigestSHA256          string
	ObservedChangeSetDigestSHA256   string
	VerifiedOperationIDs            []string
	Status                          closureprotocol.ReceiptStatus
	Violations                      []ScopeViolation
	VerifiedAt                      string
	ScopeVerificationDigestSHA256   string
}

// ObservedChangeSetDigest is the canonical digest of an observed change set. The
// change_observed ledger event and the scope verification both bind it, so the
// exact observed mutation — not merely a result-tree digest — is carried forward
// for the result-transition phase.
func ObservedChangeSetDigest(change ObservedChangeSet) (string, error) {
	return closureprotocol.SemanticDigest(change)
}

// ScopeVerificationDigest is the self-excluding digest of a scope verification.
func ScopeVerificationDigest(in ScopeVerification) (string, error) {
	copy := in
	copy.ScopeVerificationDigestSHA256 = ""
	return closureprotocol.SemanticDigest(copy)
}

// ScopeVerified reports whether the verification passed with no violations.
func ScopeVerified(v ScopeVerification) bool {
	return v.Status == closureprotocol.ReceiptValid && len(v.Violations) == 0
}

// VerifyScope checks an observed change set against its admission expectation
// and returns a typed verification receipt. Scope failures are recorded as
// violations with Status=invalid (a truthful receipt), not Go errors; an error
// is returned only for malformed input.
func VerifyScope(exp ScopeExpectation, observed ObservedChangeSet, verifiedAt string) (ScopeVerification, error) {
	if err := closureprotocol.ValidateAdmissionDecision(exp.Decision); err != nil {
		return ScopeVerification{}, err
	}
	if _, err := time.Parse(time.RFC3339, verifiedAt); err != nil {
		return ScopeVerification{}, errors.New("verified_at must be RFC3339")
	}
	decisionDigest, err := closureprotocol.SemanticDigest(exp.Decision)
	if err != nil {
		return ScopeVerification{}, err
	}
	observedDigest, err := ObservedChangeSetDigest(observed)
	if err != nil {
		return ScopeVerification{}, err
	}

	var violations []ScopeViolation
	add := func(code, path, detail string) {
		violations = append(violations, ScopeViolation{Code: code, Path: path, Detail: detail})
	}

	// The capability must have been admitted for every operation and consumed
	// exactly for this decision.
	if !AllAdmitted(exp.Decision) {
		add("scope.decision.not_admitted", "", "decision did not admit every operation")
	}
	if exp.Consumption.CapabilityID != exp.Decision.CapabilityID ||
		exp.Consumption.DecisionDigestSHA256 != decisionDigest ||
		exp.Consumption.OneUseStatus != closureprotocol.ReceiptValid {
		add("scope.capability.unbound", "", "capability consumption does not bind this decision")
	}

	// Binding integrity: the observed change must carry the same actor and
	// authority bindings the admission was computed for.
	if observed.ActorBindingDigestSHA256 != exp.ActorBindingDigestSHA256 {
		add("scope.actor.mismatch", "", "observed actor binding does not match admission")
	}
	if observed.AuthorityResolutionDigestSHA256 != exp.AuthorityResolutionDigestSHA256 {
		add("scope.authority.mismatch", "", "observed authority resolution does not match admission")
	}

	// The base the change was applied to must be the admitted base.
	if observed.BaseTreeDigestSHA256 != exp.BaseTreeDigestSHA256 {
		add("scope.base_tree.changed", "", "observed base tree differs from the admitted base")
	}

	// Index the observed change by path, flagging any path observed more than
	// once (renames are handled separately and excluded here).
	admitted := admittedOperationSet(exp.Decision)
	observedByPath := map[string]ObservedFile{}
	for _, f := range observed.Files {
		if strings.TrimSpace(string(f.ChangeType)) == string(closureprotocol.OperationRename) {
			continue
		}
		if _, dup := observedByPath[f.Path]; dup {
			add("scope.operation.duplicate_observation", f.Path, "path appears more than once in the observed change")
			continue
		}
		observedByPath[f.Path] = f
	}

	// Coverage: every admitted mutation operation must have a matching observed
	// change. An operation is only marked verified once a compatible observation
	// exists — never before comparing it to the change set. Non-mutation kinds
	// (read/observe/execute) touch no repository file and require no observation.
	allowed := map[string]bool{}
	verifiedOps := make([]string, 0, len(exp.Operations))
	for _, op := range exp.Operations {
		if !admitted[op.OperationID] {
			continue
		}
		allowed[op.Target] = true
		if op.Kind == closureprotocol.OperationRename {
			// Unreachable via admission (DecideAdmission refuses rename), but fail
			// closed defensively rather than mis-verify a rename by path alone.
			add("scope.operation.rename_mismatch", op.Target, "rename operations are unsupported in protocol v1")
			continue
		}
		if !isMutationKind(op.Kind) {
			verifiedOps = append(verifiedOps, op.OperationID)
			continue
		}
		obs, ok := observedByPath[op.Target]
		if !ok {
			add("scope.operation.not_observed", op.Target,
				"admitted "+string(op.Kind)+" operation has no matching observed change")
			continue
		}
		// A legal path with the wrong operation type (admitted modify, observed
		// delete; admitted create, observed modify; ...) must not pass on path alone.
		if !operationKindMatches(op.Kind, obs.ChangeType) {
			add("scope.operation.kind_mismatch", op.Target,
				"admitted "+string(op.Kind)+" but observed "+strings.TrimSpace(obs.ChangeType))
			continue
		}
		verifiedOps = append(verifiedOps, op.OperationID)
	}
	generated := closureprotocol.NormalizeSet(exp.RequiredGeneratedArtifacts)
	for _, g := range generated {
		allowed[g] = true
	}
	for _, f := range observed.Files {
		// v1 cannot represent or verify a rename (ChangeOperation has one Target).
		// A Git-detected rename fails closed here, naming both endpoints, rather
		// than being treated as a normal modification of the destination path.
		if strings.TrimSpace(string(f.ChangeType)) == string(closureprotocol.OperationRename) {
			from := strings.TrimSpace(f.FromPath)
			to := strings.TrimSpace(f.ToPath)
			if to == "" {
				to = f.Path
			}
			add("scope.operation.rename_unsupported", to,
				"repository rename is unsupported in protocol v1: "+from+" -> "+to)
			continue
		}
		if prefix, ok := prohibited(f.Path, exp.ProhibitedPathPrefixes); ok {
			add("scope.file.prohibited", f.Path, "path is in a prohibited class: "+prefix)
			continue
		}
		if !allowed[f.Path] {
			add("scope.file.out_of_envelope", f.Path, "file is outside the admitted operation envelope")
		}
	}

	// Generated-file rule: when a rebuild is required, every required generated
	// artifact must appear in the observed change.
	observedPaths := map[string]bool{}
	for _, f := range observed.Files {
		observedPaths[f.Path] = true
	}
	for _, g := range generated {
		if !observedPaths[g] {
			add("scope.generated.omitted", g, "required generated artifact was not rebuilt")
		}
	}

	// Budgets: operation count and coarse risk.
	if exp.Decision.OperationBudget > 0 && len(verifiedOps) > exp.Decision.OperationBudget {
		add("scope.budget.operations_exceeded", "", "admitted operation count exceeds the operation budget")
	}
	if exp.Decision.RiskBudget > 0 {
		if risk := riskWeight(exp.Operations, admitted); risk > exp.Decision.RiskBudget {
			add("scope.budget.risk_exceeded", "", "admitted operation risk exceeds the risk budget")
		}
	}

	verification := ScopeVerification{
		CapabilityID:                    exp.Decision.CapabilityID,
		DecisionDigestSHA256:            decisionDigest,
		ActorBindingDigestSHA256:        exp.ActorBindingDigestSHA256,
		AuthorityResolutionDigestSHA256: exp.AuthorityResolutionDigestSHA256,
		BaseTreeDigestSHA256:            exp.BaseTreeDigestSHA256,
		ResultTreeDigestSHA256:          observed.ResultTreeDigestSHA256,
		ObservedChangeSetDigestSHA256:   observedDigest,
		VerifiedOperationIDs:            verifiedOps,
		Violations:                      violations,
		VerifiedAt:                      verifiedAt,
	}
	if len(violations) == 0 {
		verification.Status = closureprotocol.ReceiptValid
	} else {
		verification.Status = closureprotocol.ReceiptInvalid
	}
	digest, err := ScopeVerificationDigest(verification)
	if err != nil {
		return ScopeVerification{}, err
	}
	verification.ScopeVerificationDigestSHA256 = digest
	return verification, nil
}

// isMutationKind reports whether an operation kind mutates the repository and
// therefore must appear in the observed change set. read/observe/execute do not.
func isMutationKind(k closureprotocol.OperationKind) bool {
	switch k {
	case closureprotocol.OperationCreate, closureprotocol.OperationModify, closureprotocol.OperationDelete,
		closureprotocol.OperationRename, closureprotocol.OperationMigrate, closureprotocol.OperationRebuild:
		return true
	}
	return false
}

// operationKindMatches reports whether an observed Git change type is a valid
// realization of an admitted mutation kind. A rebuild materializes as a created
// or modified generated artifact; migrate may create, modify, or delete.
func operationKindMatches(k closureprotocol.OperationKind, changeType string) bool {
	ct := strings.TrimSpace(changeType)
	switch k {
	case closureprotocol.OperationCreate:
		return ct == "create"
	case closureprotocol.OperationModify:
		return ct == "modify"
	case closureprotocol.OperationDelete:
		return ct == "delete"
	case closureprotocol.OperationRebuild:
		return ct == "create" || ct == "modify"
	case closureprotocol.OperationMigrate:
		return ct == "create" || ct == "modify" || ct == "delete"
	}
	return true
}

func prohibited(path string, prefixes []string) (string, bool) {
	clean := strings.TrimSpace(path)
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p != "" && strings.HasPrefix(clean, p) {
			return p, true
		}
	}
	return "", false
}

// riskWeight counts admitted operations that carry a high or
// architecture-sensitive risk class.
func riskWeight(ops []closureprotocol.ChangeOperation, admitted map[string]bool) int {
	weight := 0
	for _, op := range ops {
		if !admitted[op.OperationID] {
			continue
		}
		switch strings.TrimSpace(op.RiskClass) {
		case "high", "architecture_sensitive":
			weight++
		}
	}
	return weight
}
