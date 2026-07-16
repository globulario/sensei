// SPDX-License-Identifier: AGPL-3.0-only

package authority

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func Resolve(index PolicyIndex, req ResolveRequest) (closureprotocol.AuthorityResolution, error) {
	if strings.TrimSpace(req.ActorBindingDigestSHA256) == "" {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("actor binding digest is required")
	}
	if strings.TrimSpace(req.BaseBindingDigestSHA256) == "" {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("base binding digest is required")
	}
	if strings.TrimSpace(req.ClosureAssessmentDigestSHA256) == "" {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("closure assessment digest is required")
	}
	if strings.TrimSpace(req.PolicyID) == "" {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("policy id is required")
	}
	if strings.TrimSpace(req.AuthorityPolicyGraphDigestSHA256) == "" {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("authority policy graph digest is required")
	}
	evaluatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(req.EvaluatedAt))
	if err != nil {
		return closureprotocol.AuthorityResolution{}, fmt.Errorf("evaluated_at must be RFC3339: %w", err)
	}

	appByOp := map[string]AuthorityApplicability{}
	for _, item := range req.Applicability {
		if strings.TrimSpace(item.OperationID) == "" {
			return closureprotocol.AuthorityResolution{}, fmt.Errorf("authority applicability operation_id is required")
		}
		appByOp[item.OperationID] = AuthorityApplicability{
			OperationID:                 strings.TrimSpace(item.OperationID),
			TargetFile:                  strings.TrimSpace(item.TargetFile),
			TargetSymbol:                strings.TrimSpace(item.TargetSymbol),
			AuthorityDomainIDs:          cleanSet(item.AuthorityDomainIDs),
			RequiredRuntimeMechanismIDs: cleanSet(item.RequiredRuntimeMechanismIDs),
			RelationPaths:               normalizeRelationPaths(item.RelationPaths),
		}
	}

	ops := append([]closureprotocol.ChangeOperation(nil), req.Operations...)
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].OperationID < ops[j].OperationID })

	res := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:          strings.TrimSpace(req.ActorBindingDigestSHA256),
		AuthenticationReceiptDigestSHA256: strings.TrimSpace(req.Actor.AuthenticationReceiptDigestSHA256),
		BaseBindingDigestSHA256:           strings.TrimSpace(req.BaseBindingDigestSHA256),
		ClosureAssessmentDigestSHA256:     strings.TrimSpace(req.ClosureAssessmentDigestSHA256),
		OperationSetDigestSHA256:          operationSetDigest(ops),
		AuthorityPolicyGraphDigestSHA256:  strings.TrimSpace(req.AuthorityPolicyGraphDigestSHA256),
		PolicyID:                          strings.TrimSpace(req.PolicyID),
		EvaluatedAt:                       evaluatedAt.UTC().Format(time.RFC3339),
		Status:                            closureprotocol.ReceiptValid,
	}

	for _, op := range ops {
		result, err := resolveOperation(index, req.Actor, op, appByOp[op.OperationID], evaluatedAt)
		if err != nil {
			return closureprotocol.AuthorityResolution{}, err
		}
		res.OperationResults = append(res.OperationResults, result)
		res.Status = combineStatus(res.Status, result.Status)
		res.Limitations = append(res.Limitations, result.Limitations...)
	}

	res.Limitations = cleanSet(res.Limitations)
	digest, err := closureprotocol.AuthorityResolutionDigest(res)
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	res.AuthorityResolutionDigestSHA256 = digest
	return res, closureprotocol.ValidateAuthorityResolution(res)
}

func resolveOperation(index PolicyIndex, actor VerifiedActor, op closureprotocol.ChangeOperation, applicability AuthorityApplicability, evaluatedAt time.Time) (closureprotocol.AuthorityResolutionOperation, error) {
	result := closureprotocol.AuthorityResolutionOperation{
		OperationID:                 op.OperationID,
		Status:                      actor.Status,
		SelectedMechanism:           op.SelectedMechanism,
		AuthorityDomainIDs:          cleanSet(applicability.AuthorityDomainIDs),
		RequiredRuntimeMechanismIDs: cleanSet(applicability.RequiredRuntimeMechanismIDs),
	}

	if actor.Status != closureprotocol.ReceiptValid {
		result.Limitations = append(result.Limitations, "actor_not_verified")
		result.Limitations = append(result.Limitations, actor.Limitations...)
		if result.Status == "" {
			result.Status = closureprotocol.ReceiptUnknown
		}
		result.Limitations = cleanSet(result.Limitations)
		return result, nil
	}

	if strings.TrimSpace(applicability.OperationID) == "" {
		result.Status = closureprotocol.ReceiptUnknown
		result.Limitations = []string{"authority_applicability_missing"}
		return result, nil
	}
	if applicability.OperationID != op.OperationID {
		return result, fmt.Errorf("authority applicability operation %s does not match change operation %s", applicability.OperationID, op.OperationID)
	}
	if len(result.AuthorityDomainIDs) == 0 {
		result.Status = closureprotocol.ReceiptUnknown
		result.Limitations = []string{"authority_domain_unmapped"}
		return result, nil
	}

	var selected []selectedDomainGrant
	for _, domainID := range result.AuthorityDomainIDs {
		domain, ok := index.AuthorityDomains[domainID]
		if !ok {
			return result, fmt.Errorf("authority domain %s is unknown", domainID)
		}
		if domain.Status != "active" {
			result.Status = closureprotocol.ReceiptInvalid
			result.Limitations = append(result.Limitations, "authority_domain_inactive:"+domainID)
			continue
		}
		if len(domain.MayWriteRoleIDs) == 0 && len(domain.LegacyMayWrite) > 0 {
			result.Status = closureprotocol.ReceiptInvalid
			result.Limitations = append(result.Limitations, "legacy_writer_strings_only:"+domainID)
			continue
		}
		if len(domain.MustMutateViaIDs) == 0 && len(domain.LegacyMustMutateVia) > 0 {
			result.Status = closureprotocol.ReceiptInvalid
			result.Limitations = append(result.Limitations, "legacy_mutation_paths_only:"+domainID)
			continue
		}
		result.RequiredRuntimeMechanismIDs = cleanSet(append(result.RequiredRuntimeMechanismIDs, domain.MustMutateViaIDs...))

		match, ok := selectGrantForDomain(index, actor, op, domainID, evaluatedAt)
		if !ok {
			result.Status = combineStatus(result.Status, closureprotocol.ReceiptInvalid)
			result.Limitations = append(result.Limitations, "grant_cover_missing:"+domainID)
			continue
		}
		selected = append(selected, match)
	}

	for _, item := range selected {
		result.GrantIDs = append(result.GrantIDs, item.Grant.ID)
		result.LegalMechanisms = append(result.LegalMechanisms, item.Grant.RequiredMechanismIDs...)
		result.DelegationChain = append(result.DelegationChain, item.DelegationChain...)
	}
	result.GrantIDs = cleanSet(result.GrantIDs)
	result.LegalMechanisms = cleanSet(result.LegalMechanisms)
	result.DelegationChain = cleanOrdered(result.DelegationChain)
	result.Limitations = cleanSet(result.Limitations)

	if len(result.Limitations) == 0 {
		result.Status = closureprotocol.ReceiptValid
	}
	if result.Status == "" {
		result.Status = closureprotocol.ReceiptUnknown
	}
	return result, nil
}

type selectedDomainGrant struct {
	Grant           AuthorityGrant
	DelegationChain []string
	Specificity     int
}

func selectGrantForDomain(index PolicyIndex, actor VerifiedActor, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) (selectedDomainGrant, bool) {
	matches := matchingGrants(index, actor, op, domainID, evaluatedAt)
	if len(matches) == 0 {
		return selectedDomainGrant{}, false
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Specificity != matches[j].Specificity {
			return matches[i].Specificity > matches[j].Specificity
		}
		if matches[i].Grant.ID != matches[j].Grant.ID {
			return matches[i].Grant.ID < matches[j].Grant.ID
		}
		return strings.Join(matches[i].DelegationChain, "\x00") < strings.Join(matches[j].DelegationChain, "\x00")
	})
	return matches[0], true
}

func matchingGrants(index PolicyIndex, actor VerifiedActor, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) []selectedDomainGrant {
	var out []selectedDomainGrant
	for _, grant := range index.AuthorityGrants {
		if grant.Status != "active" {
			continue
		}
		if !grantAllowsOperation(index, grant, actor, op, domainID, evaluatedAt) {
			continue
		}
		out = append(out, selectedDomainGrant{Grant: grant, Specificity: grantSpecificity(grant)})
	}
	for _, receipt := range actor.DelegationReceipts {
		grant, ok := index.AuthorityGrants[receipt.ParentGrantID]
		if !ok || grant.Status != "active" {
			continue
		}
		if !grantAllowsDelegatedOperation(index, grant, op, domainID, evaluatedAt) {
			continue
		}
		if !delegationAllowsOperation(index, grant, receipt, op, domainID, evaluatedAt) {
			continue
		}
		out = append(out, selectedDomainGrant{
			Grant:           grant,
			DelegationChain: []string{receipt.DelegationID},
			Specificity:     grantSpecificity(grant) + 1,
		})
	}
	return out
}

func grantAllowsOperation(index PolicyIndex, grant AuthorityGrant, actor VerifiedActor, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) bool {
	if !intersectsString(grant.ActorRoleIDs, actor.VerifiedRoleIDs) {
		return false
	}
	return grantAllowsDelegatedOperation(index, grant, op, domainID, evaluatedAt)
}

func grantAllowsDelegatedOperation(index PolicyIndex, grant AuthorityGrant, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) bool {
	if !containsString(grant.AuthorityDomainIDs, domainID) {
		return false
	}
	if !containsOperation(grant.Actions, op.Kind) {
		return false
	}
	if !containsString(grant.TargetKinds, op.TargetKind) {
		return false
	}
	if riskRank(op.RiskClass) > riskRank(grant.MaximumRiskClass) {
		return false
	}
	if !grantMechanismMatches(index, grant.RequiredMechanismIDs, op.SelectedMechanism) {
		return false
	}
	if grant.ValidFrom != "" {
		validFrom, err := time.Parse(time.RFC3339, grant.ValidFrom)
		if err != nil || evaluatedAt.Before(validFrom) {
			return false
		}
	}
	if grant.ValidUntil != "" {
		validUntil, err := time.Parse(time.RFC3339, grant.ValidUntil)
		if err != nil || !evaluatedAt.Before(validUntil) {
			return false
		}
	}
	return true
}

// delegationAllowsOperation gates authority resolution on the same monotonicity
// verdict the certification engine re-runs independently. It is a thin wrapper
// over CheckDelegationForOperation so the resolver and the certifier never drift
// apart on what a legitimate one-level delegation is.
func delegationAllowsOperation(index PolicyIndex, grant AuthorityGrant, receipt closureprotocol.DelegationReceipt, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) bool {
	return CheckDelegationForOperation(index, grant, receipt, op, domainID, evaluatedAt) == DelegationOK
}

func grantMechanismMatches(index PolicyIndex, ids []string, selected closureprotocol.MechanismKind) bool {
	for _, id := range cleanSet(ids) {
		path, ok := index.MutationPaths[id]
		if ok && path.Status == "active" && path.MechanismKind == selected {
			return true
		}
	}
	return false
}

func delegationMechanismMatches(kinds []closureprotocol.MechanismKind, selected closureprotocol.MechanismKind) bool {
	for _, kind := range kinds {
		if kind == selected {
			return true
		}
	}
	return false
}

func containsOperation(in []closureprotocol.OperationKind, want closureprotocol.OperationKind) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func subsetOperations(have, allowed []closureprotocol.OperationKind) bool {
	for _, item := range have {
		if !containsOperation(allowed, item) {
			return false
		}
	}
	return true
}

func subsetTargetKinds(have, allowed []string) bool {
	return subsetStrings(have, allowed)
}

func subsetStrings(have, allowed []string) bool {
	for _, item := range cleanSet(have) {
		if !containsString(allowed, item) {
			return false
		}
	}
	return true
}

func subsetMechanismKinds(index PolicyIndex, kinds []closureprotocol.MechanismKind, allowedIDs []string) bool {
	allowed := map[closureprotocol.MechanismKind]bool{}
	for _, id := range cleanSet(allowedIDs) {
		if path, ok := index.MutationPaths[id]; ok && path.Status == "active" {
			allowed[path.MechanismKind] = true
		}
	}
	for _, kind := range kinds {
		if !allowed[kind] {
			return false
		}
	}
	return true
}

func subsetMechanismIDs(index PolicyIndex, kinds []closureprotocol.MechanismKind, allowedIDs []string) bool {
	allowed := map[closureprotocol.MechanismKind]bool{}
	for _, id := range cleanSet(allowedIDs) {
		if path, ok := index.MutationPaths[id]; ok && path.Status == "active" {
			allowed[path.MechanismKind] = true
		}
	}
	for _, kind := range kinds {
		if !allowed[kind] {
			return false
		}
	}
	return true
}

func intersectsString(a, b []string) bool {
	for _, item := range cleanSet(a) {
		if containsString(b, item) {
			return true
		}
	}
	return false
}

func operationSetDigest(ops []closureprotocol.ChangeOperation) string {
	return closureprotocol.MustSemanticDigest(struct {
		Operations []closureprotocol.ChangeOperation `json:"operations"`
	}{Operations: ops})
}

func combineStatus(current, next closureprotocol.ReceiptStatus) closureprotocol.ReceiptStatus {
	if receiptStatusRank(next) > receiptStatusRank(current) {
		return next
	}
	return current
}

func receiptStatusRank(status closureprotocol.ReceiptStatus) int {
	switch status {
	case closureprotocol.ReceiptValid:
		return 0
	case closureprotocol.ReceiptUnknown:
		return 1
	case closureprotocol.ReceiptStale, closureprotocol.ReceiptSuperseded, closureprotocol.ReceiptRevoked:
		return 2
	case closureprotocol.ReceiptInvalid:
		return 3
	case closureprotocol.ReceiptConflicted:
		return 4
	default:
		return 1
	}
}

func grantSpecificity(grant AuthorityGrant) int {
	score := 0
	if len(grant.AuthorityDomainIDs) == 1 {
		score += 8
	}
	if len(grant.Actions) == 1 {
		score += 4
	}
	if len(grant.TargetKinds) == 1 {
		score += 2
	}
	if len(grant.RequiredMechanismIDs) == 1 {
		score++
	}
	return score
}

func cleanOrdered(in []string) []string {
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func normalizeRelationPaths(in [][]string) [][]string {
	out := make([][]string, 0, len(in))
	for _, path := range in {
		out = append(out, cleanOrdered(path))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})
	return out
}

func riskRank(v string) int {
	switch strings.TrimSpace(v) {
	case "", "low_risk":
		return 0
	case "architecture_sensitive":
		return 1
	case "convergence_risk":
		return 2
	case "security_risk":
		return 3
	case "data_loss_risk":
		return 4
	default:
		return 5
	}
}
