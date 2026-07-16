// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"fmt"
	"strings"
)

// RenderOptions controls the human Markdown rendering.
type RenderOptions struct {
	// MaxReviewerItems caps how many reviewer-attention items are shown. Zero
	// means DefaultMaxReviewerItems. The complete set always remains in JSON.
	MaxReviewerItems int
}

var dispositionLabels = map[ReviewDisposition]string{
	DispositionReadyForHumanReview:       "Ready for human review",
	DispositionMechanicalRepairRequired:  "Mechanical repair required",
	DispositionGovernanceRequired:        "Governance required",
	DispositionArchitectDecisionRequired: "Architect decision required",
	DispositionEvidenceRequired:          "Evidence required",
	DispositionScopeViolation:            "Scope violation",
	DispositionCannotVerify:              "Cannot verify",
	DispositionCertified:                 "Certified",
	DispositionTerminallyClosed:          "Terminally closed",
}

var coverageLabels = map[CoverageLevel]string{
	CoverageAdvisory:   "Advisory",
	CoverageGoverned:   "Governed",
	CoverageProofBound: "Proof-bound",
	CoverageTerminal:   "Terminal",
}

// RenderMarkdown renders the human-facing report. High-value reviewer questions
// appear near the top; exhaustive provenance is pushed into collapsible detail
// sections. Rendering is deterministic: identical reports render byte-identical.
func RenderMarkdown(r PreReviewReport, opts RenderOptions) ([]byte, error) {
	max := opts.MaxReviewerItems
	if max <= 0 {
		max = DefaultMaxReviewerItems
	}
	var b strings.Builder

	b.WriteString("# Sensei Architectural Pre-Review\n\n")
	fmt.Fprintf(&b, "> **Disposition:** %s  \n", labelOr(dispositionLabels[r.Disposition], string(r.Disposition)))
	fmt.Fprintf(&b, "> **Coverage:** %s  \n", labelOr(coverageLabels[r.Coverage.Level], string(r.Coverage.Level)))
	fmt.Fprintf(&b, "> **Risk:** %s  \n", orNone(r.Summary.ArchitecturalRisk))
	fmt.Fprintf(&b, "> **Human decisions:** %d\n\n", r.Summary.ReviewerAttentionCount)

	// Executive summary.
	b.WriteString("## Executive summary\n\n")
	writeBullet(&b, "Purpose", r.Summary.Purpose)
	writeBullet(&b, "Architectural risk", r.Summary.ArchitecturalRisk)
	writeBullet(&b, "Highest-priority blocker", r.Summary.HighestPriorityBlocker)
	b.WriteString("\n")

	// Reviewer attention — the flagship section, before any detail.
	b.WriteString("## Reviewer attention\n\n")
	if len(r.ReviewerAttention) == 0 {
		b.WriteString("_No human decisions are currently required._\n\n")
	} else {
		shown := r.ReviewerAttention
		if len(shown) > max {
			shown = shown[:max]
		}
		for i, a := range shown {
			flag := ""
			if a.Blocking {
				flag = " _(blocking)_"
			}
			fmt.Fprintf(&b, "%d. **%s**%s — %s\n", i+1, orNone(a.Question), flag, string(a.Severity))
			if a.WhyItMatters != "" {
				fmt.Fprintf(&b, "   %s\n", a.WhyItMatters)
			}
			if len(a.RelatedFiles) > 0 {
				fmt.Fprintf(&b, "   - Related files: %s\n", strings.Join(a.RelatedFiles, ", "))
			}
			if len(a.AllowedAnswers) > 0 {
				fmt.Fprintf(&b, "   - Allowed answers: %s\n", strings.Join(a.AllowedAnswers, ", "))
			}
			if a.ResolutionPath != "" {
				fmt.Fprintf(&b, "   - Resolution: %s\n", a.ResolutionPath)
			}
		}
		if len(r.ReviewerAttention) > len(shown) {
			fmt.Fprintf(&b, "\n_%d more reviewer question(s) in machine-readable output._\n", len(r.ReviewerAttention)-len(shown))
		}
		b.WriteString("\n")
	}

	// Architectural impact.
	b.WriteString("## Architectural impact\n\n")
	writeBullet(&b, "Risk class", r.Impact.RiskClass)
	writeImpactLine(&b, "Affected components", r.Impact.AffectedComponents)
	writeImpactLine(&b, "Changed boundaries", r.Impact.ChangedBoundaries)
	writeImpactLine(&b, "Affected contracts", r.Impact.AffectedContracts)
	writeImpactLine(&b, "Authority domains", r.Impact.AuthorityDomains)
	b.WriteString("\n")

	// Governance and scope.
	b.WriteString("## Governance and scope\n\n")
	if !r.Coverage.Level.AtLeast(CoverageGoverned) {
		b.WriteString("_Typed governance is unavailable at advisory coverage._\n\n")
	} else {
		writeBullet(&b, "Actor", r.Governance.Actor)
		writeBullet(&b, "Authority", r.Governance.AuthorityStatus)
		writeBullet(&b, "Admission", r.Governance.AdmissionStatus)
		writeBullet(&b, "Capability", r.Governance.CapabilityStatus)
		writeBullet(&b, "Scope", r.Governance.ScopeStatus)
		if len(r.Governance.Violations) > 0 {
			b.WriteString("- Violations:\n")
			for _, v := range r.Governance.Violations {
				fmt.Fprintf(&b, "  - `%s` %s %s\n", v.Code, v.Path, v.Detail)
			}
		}
		b.WriteString("\n")
	}

	// Proof and evidence.
	b.WriteString("## Proof and evidence\n\n")
	writeStringsBullet(&b, "Required slots", r.Proof.RequiredSlots)
	writeStringsBullet(&b, "Discharged slots", r.Proof.DischargedSlots)
	writeStringsBullet(&b, "Unresolved slots", r.Proof.UnresolvedSlots)
	if r.Proof.Certification != nil {
		fmt.Fprintf(&b, "- Certification: %s\n", certificationLine(r.Proof.Certification))
	} else {
		b.WriteString("- Certification: not available at this coverage\n")
	}
	b.WriteString("\n")

	// Result architecture.
	b.WriteString("## Result architecture\n\n")
	if !r.Result.Available {
		fmt.Fprintf(&b, "_Unavailable at %s coverage; result transition receipts are not yet present._\n\n",
			labelOr(coverageLabels[r.Coverage.Level], string(r.Coverage.Level)))
	} else {
		writeStringsBullet(&b, "Components added", r.Result.ComponentsAdded)
		writeStringsBullet(&b, "Components removed", r.Result.ComponentsRemoved)
		writeStringsBullet(&b, "Invalidated proofs", r.Result.InvalidatedProofs)
		b.WriteString("\n")
	}

	// Collapsible provenance.
	writeProtectionDetails(&b, r.Protection, r.History)
	writeBindingDetails(&b, r.Binding)

	return []byte(b.String()), nil
}

func writeProtectionDetails(b *strings.Builder, p ProtectionSummary, h HistoricalRiskSummary) {
	b.WriteString("<details>\n<summary>Applicable invariants and failure history</summary>\n\n")
	writeProtectionSection(b, "Invariants", p.Invariants)
	writeProtectionSection(b, "Contracts", p.Contracts)
	writeProtectionSection(b, "Failure modes", p.FailureModes)
	writeProtectionSection(b, "Forbidden fixes", p.ForbiddenFixes)
	writeProtectionSection(b, "Required tests", p.RequiredTests)
	if n := len(h.RelatedIncidents) + len(h.RevertedChanges) + len(h.AttemptedForbiddenFixes) +
		len(h.RecurringFailurePatterns) + len(h.RelevantDecisions); n > 0 {
		b.WriteString("\n**Historical risk**\n\n")
		writeHistorySection(b, "Related incidents", h.RelatedIncidents)
		writeHistorySection(b, "Reverted changes", h.RevertedChanges)
		writeHistorySection(b, "Attempted forbidden fixes", h.AttemptedForbiddenFixes)
		writeHistorySection(b, "Recurring failure patterns", h.RecurringFailurePatterns)
		writeHistorySection(b, "Relevant decisions", h.RelevantDecisions)
	}
	b.WriteString("\n</details>\n\n")
}

func writeProtectionSection(b *strings.Builder, title string, items []ProtectionItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "**%s**\n\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- `%s` %s — status: %s, severity: %s, epistemic: %s\n",
			it.ID, it.Title, it.Status, it.Severity, it.Epistemic)
	}
	b.WriteString("\n")
}

func writeHistorySection(b *strings.Builder, title string, items []HistoryItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "- %s:\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "  - `%s` %s %s\n", it.ID, it.Title, it.Reference)
	}
}

func writeBindingDetails(b *strings.Builder, bind ReviewBinding) {
	b.WriteString("<details>\n<summary>Bindings and receipt digests</summary>\n\n")
	fmt.Fprintf(b, "- Repository: %s\n", orNone(bind.RepositoryDomain))
	fmt.Fprintf(b, "- Base: %s (%s)\n", orNone(bind.BaseRevision), shortDigest(bind.BaseTreeDigestSHA256))
	fmt.Fprintf(b, "- Head: %s (%s)\n", orNone(bind.HeadRevision), shortDigest(bind.HeadTreeDigestSHA256))
	fmt.Fprintf(b, "- Diff digest: %s\n", shortDigest(bind.DiffDigestSHA256))
	if bind.TaskID != "" {
		fmt.Fprintf(b, "- Task: %s @ ledger %s\n", bind.TaskID, shortDigest(bind.LedgerHeadDigestSHA256))
	}
	if len(bind.PolicyIDs) > 0 {
		fmt.Fprintf(b, "- Policies: %s\n", strings.Join(bind.PolicyIDs, ", "))
	}
	b.WriteString("\n</details>\n")
}

func writeBullet(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "- %s: %s\n", label, orNone(value))
}

func writeStringsBullet(b *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(b, "- %s: none\n", label)
		return
	}
	fmt.Fprintf(b, "- %s: %s\n", label, strings.Join(values, ", "))
}

func writeImpactLine(b *strings.Builder, label string, items []ImpactItem) {
	if len(items) == 0 {
		fmt.Fprintf(b, "- %s: none\n", label)
		return
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	fmt.Fprintf(b, "- %s: %s\n", label, strings.Join(ids, ", "))
}

func certificationLine(c *CertificationView) string {
	if c.IsCertified() {
		return fmt.Sprintf("certified (receipt %s)", shortDigest(c.ReceiptDigestSHA256))
	}
	if strings.TrimSpace(c.Verdict) != "" {
		return c.Verdict
	}
	return "not certified"
}

func labelOr(label, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func shortDigest(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return "—"
	}
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
