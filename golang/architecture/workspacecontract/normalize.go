// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

// NormalizeIdentity returns a copy of id with every slice field non-nil
// (so JSON marshaling always emits [] rather than null for "no items",
// matching the schema's array-typed, always-required fields) and nested
// slices normalized the same way. Normalization is deterministic: two
// logically identical Identity values normalize to byte-identical JSON.
func NormalizeIdentity(id Identity) Identity {
	id.Limitations = normalizeLimitations(id.Limitations)
	return id
}

// NormalizeAdmission returns a copy of a with every slice field non-nil,
// including nested slices inside RequestReceipt.Scope, Envelope, and (when
// present) Verification.
func NormalizeAdmission(a Admission) Admission {
	a.RequestReceipt.Scope = normalizeChangeScope(a.RequestReceipt.Scope)
	a.Envelope = normalizeEnvelope(a.Envelope)
	a.Reasons = normalizeReasons(a.Reasons)
	a.Limitations = normalizeLimitations(a.Limitations)
	if a.Verification != nil {
		v := normalizeVerification(*a.Verification)
		a.Verification = &v
	}
	return a
}

func normalizeLimitations(in []Limitation) []Limitation {
	if in == nil {
		return []Limitation{}
	}
	return in
}

func normalizeReasons(in []Reason) []Reason {
	if in == nil {
		return []Reason{}
	}
	return in
}

func normalizeStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func normalizeFileOperations(in []FileOperation) []FileOperation {
	if in == nil {
		return []FileOperation{}
	}
	return in
}

func normalizeChangeScope(in ChangeScope) ChangeScope {
	in.Files = normalizeFileOperations(in.Files)
	in.Symbols = normalizeStrings(in.Symbols)
	in.Components = normalizeStrings(in.Components)
	in.ClaimIDs = normalizeStrings(in.ClaimIDs)
	in.PropositionKeys = normalizeStrings(in.PropositionKeys)
	return in
}

func normalizeEnvelope(in Envelope) Envelope {
	in.ReadPaths = normalizeStrings(in.ReadPaths)
	in.ModifyPaths = normalizeStrings(in.ModifyPaths)
	in.Symbols = normalizeStrings(in.Symbols)
	in.Components = normalizeStrings(in.Components)
	in.ClaimIDs = normalizeStrings(in.ClaimIDs)
	in.PropositionKeys = normalizeStrings(in.PropositionKeys)
	in.UnsupportedOperations = normalizeStrings(in.UnsupportedOperations)
	return in
}

func normalizeChangeReceipts(in []ChangeReceipt) []ChangeReceipt {
	if in == nil {
		return []ChangeReceipt{}
	}
	return in
}

func normalizeViolations(in []Violation) []Violation {
	if in == nil {
		return []Violation{}
	}
	return in
}

func normalizeVerification(in Verification) Verification {
	in.Changes = normalizeChangeReceipts(in.Changes)
	in.Violations = normalizeViolations(in.Violations)
	in.PendingConditionIDs = normalizeStrings(in.PendingConditionIDs)
	in.PendingTestIDs = normalizeStrings(in.PendingTestIDs)
	in.PendingProofObligationIDs = normalizeStrings(in.PendingProofObligationIDs)
	in.PendingRuntimeEvidenceIDs = normalizeStrings(in.PendingRuntimeEvidenceIDs)
	in.Reasons = normalizeReasons(in.Reasons)
	in.Limitations = normalizeLimitations(in.Limitations)
	return in
}
