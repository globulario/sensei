// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

// NormalizeSession returns a copy of s with every slice field non-nil.
func NormalizeSession(s Session) Session {
	s.ProofObligationDigests = normalizeStrings(s.ProofObligationDigests)
	return s
}

// NormalizeInterpretation returns a copy of in with every slice field
// non-nil.
func NormalizeInterpretation(in Interpretation) Interpretation {
	in.ApplicableIntent = normalizeStrings(in.ApplicableIntent)
	in.BindingInvariants = normalizeStrings(in.BindingInvariants)
	in.RelevantContracts = normalizeStrings(in.RelevantContracts)
	in.AuthorityBoundaries = normalizeStrings(in.AuthorityBoundaries)
	in.KnownFailureModes = normalizeStrings(in.KnownFailureModes)
	in.ForbiddenFixes = normalizeStrings(in.ForbiddenFixes)
	in.RequiredProofObligations = normalizeStrings(in.RequiredProofObligations)
	in.Assumptions = normalizeStrings(in.Assumptions)
	in.UnresolvedQuestions = normalizeStrings(in.UnresolvedQuestions)
	in.SourceReferences = normalizeSourceReferences(in.SourceReferences)
	in.Limitations = normalizeLimitations(in.Limitations)
	return in
}

// NormalizePlan returns a copy of p with every slice field non-nil,
// including nested slices inside each step.
func NormalizePlan(p Plan) Plan {
	steps := make([]PlanStep, len(p.Steps))
	for i, step := range p.Steps {
		step.IntendedFiles = normalizeStrings(step.IntendedFiles)
		step.IntendedSymbols = normalizeStrings(step.IntendedSymbols)
		step.ExpectedEvidence = normalizeStrings(step.ExpectedEvidence)
		steps[i] = step
	}
	p.Steps = steps
	p.Assumptions = normalizeStrings(p.Assumptions)
	p.Risks = normalizeStrings(p.Risks)
	p.StopConditions = normalizeStrings(p.StopConditions)
	return p
}

// NormalizeAttempt returns a copy of a with every slice field non-nil.
func NormalizeAttempt(a Attempt) Attempt {
	a.EvidenceReferences = normalizeStrings(a.EvidenceReferences)
	return a
}

// NormalizeEvaluation returns a copy of e with every slice field non-nil,
// including nested slices inside each check.
func NormalizeEvaluation(e Evaluation) Evaluation {
	checks := make([]CheckObservation, len(e.Checks))
	for i, c := range e.Checks {
		c.EvidenceReferences = normalizeStrings(c.EvidenceReferences)
		checks[i] = c
	}
	e.Checks = checks
	e.ClassifiedFailureReasons = normalizeStrings(e.ClassifiedFailureReasons)
	e.Limitations = normalizeLimitations(e.Limitations)
	return e
}

// NormalizeReceipt returns a copy of r with every slice field non-nil.
func NormalizeReceipt(r Receipt) Receipt {
	r.Limitations = normalizeLimitations(r.Limitations)
	return r
}

func normalizeStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func normalizeLimitations(in []Limitation) []Limitation {
	if in == nil {
		return []Limitation{}
	}
	return in
}

func normalizeSourceReferences(in []SourceReference) []SourceReference {
	if in == nil {
		return []SourceReference{}
	}
	return in
}
