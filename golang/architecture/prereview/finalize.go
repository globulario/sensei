// SPDX-License-Identifier: Apache-2.0

package prereview

// Finalize canonicalizes an assembled report and stamps its identity. It is the
// only supported way to produce a valid report from typed inputs:
//
//  1. normalize the content so logically-equal inputs converge;
//  2. rank and de-duplicate reviewer attention deterministically;
//  3. derive the disposition from evidence, ignoring any caller-supplied value;
//  4. derive the executive summary from the structured report;
//  5. compute the canonical report ID and digest;
//  6. validate against the non-negotiable laws.
//
// Because the disposition and summary are always derived here, a caller cannot
// forge a verdict by pre-setting them: Finalize overwrites them from evidence.
func Finalize(r PreReviewReport) (PreReviewReport, error) {
	r.SchemaVersion = SchemaVersion
	Normalize(&r)
	r.ReviewerAttention = RankReviewerAttention(r.ReviewerAttention)
	r.Disposition = DeriveDisposition(r)
	r.Summary = deriveExecutiveSummary(r)

	id, err := ComputeReportID(r.Binding)
	if err != nil {
		return PreReviewReport{}, err
	}
	r.ReportID = id

	digest, err := ComputeReportDigest(r)
	if err != nil {
		return PreReviewReport{}, err
	}
	r.ReportDigestSHA256 = digest

	if err := Validate(r); err != nil {
		return PreReviewReport{}, err
	}
	return r, nil
}

// deriveExecutiveSummary fills the headline fields from the structured report.
// Author-provided Purpose is preserved; everything else is derived so no field
// is ever invented from raw text.
func deriveExecutiveSummary(r PreReviewReport) ExecutiveSummary {
	s := r.Summary
	if s.ArchitecturalRisk == "" {
		s.ArchitecturalRisk = r.Impact.RiskClass
	}
	s.CurrentDisposition = string(r.Disposition)
	s.ReviewerAttentionCount = len(r.ReviewerAttention)
	s.HighestPriorityBlocker = ""
	for _, a := range r.ReviewerAttention {
		if a.Blocking {
			s.HighestPriorityBlocker = a.Question
			break
		}
	}
	return s
}
