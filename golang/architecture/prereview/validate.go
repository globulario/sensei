// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"errors"
	"fmt"
	"strings"
)

// passStatuses are the item statuses that assert a rule is satisfied. Such a
// status may never be claimed without evidence.
var passStatuses = map[string]bool{
	"pass": true, "satisfied": true, "discharged": true, "holds": true, "verified": true,
}

// Validate checks a finalized report against the non-negotiable laws. It never
// mutates the report. A report that passes Validate is internally consistent,
// canonically identified, and free of the known forgery shapes: a caller cannot
// establish a verdict, mark a finding satisfied without evidence, display
// certification or completion without a receipt, promote a model candidate to a
// governed fact, or bind the report to the wrong diff.
func Validate(r PreReviewReport) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if strings.TrimSpace(r.SchemaVersion) != SchemaVersion {
		add("schema_version %q is not %q", r.SchemaVersion, SchemaVersion)
	}

	validateBinding(r.Binding, add)
	validateVocabulary(r, add)
	validateCoverageGating(r, add)
	validateEvidenceBackedStatuses(r, add)
	validateEpistemicIntegrity(r, add)
	validateReceiptBackedVerdicts(r, add)
	validateResultAvailability(r, add)
	validateAttention(r, add)
	validateNarrative(r, add)
	validateSummary(r, add)
	validateIdentity(r, add)

	if len(problems) == 0 {
		return nil
	}
	errs := make([]error, len(problems))
	for i, p := range problems {
		errs[i] = errors.New(p)
	}
	return fmt.Errorf("invalid pre-review report: %w", errors.Join(errs...))
}

// Law 1/2/3: bind to an exact repository and diff; a task-backed report binds to
// the ledger head.
func validateBinding(b ReviewBinding, add func(string, ...any)) {
	if strings.TrimSpace(b.RepositoryDomain) == "" {
		add("binding.repository_domain is required")
	}
	if strings.TrimSpace(b.BaseTreeDigestSHA256) == "" {
		add("binding.base_tree_digest_sha256 is required")
	}
	if strings.TrimSpace(b.HeadTreeDigestSHA256) == "" {
		add("binding.head_tree_digest_sha256 is required")
	}
	if strings.TrimSpace(b.DiffDigestSHA256) == "" {
		add("binding.diff_digest_sha256 is required")
	}
	if strings.TrimSpace(b.TaskID) != "" && strings.TrimSpace(b.LedgerHeadDigestSHA256) == "" {
		add("task-backed report must bind to a ledger head")
	}
}

// Law: unknown vocabulary is rejected.
func validateVocabulary(r PreReviewReport, add func(string, ...any)) {
	if !ValidCoverage(r.Coverage.Level) {
		add("unknown coverage level %q", r.Coverage.Level)
	}
	if !ValidDisposition(r.Disposition) {
		add("unknown disposition %q", r.Disposition)
	}
	forEachEpistemic(r, func(where string, s EpistemicStatus) {
		if !ValidEpistemic(s) {
			add("unknown epistemic status %q at %s", s, where)
		}
	})
	forEachSeverity(r, func(where string, s Severity) {
		if !ValidSeverity(s) {
			add("unknown severity %q at %s", s, where)
		}
	})
	for _, a := range r.ReviewerAttention {
		if !ValidAttentionCategory(a.Category) {
			add("unknown attention category %q at %s", a.Category, a.ID)
		}
	}
}

// Law: certification is displayed only at proof_bound+, completion only at
// terminal, governance only at governed+.
func validateCoverageGating(r PreReviewReport, add func(string, ...any)) {
	if !r.Coverage.Level.AtLeast(CoverageGoverned) && governanceIsPopulated(r.Governance) {
		add("advisory coverage cannot display typed governance status")
	}
	if r.Proof.Certification != nil && !r.Coverage.Level.AtLeast(CoverageProofBound) {
		add("certification requires proof_bound coverage or higher")
	}
	if r.Result.Completion.HasReceipt() && !r.Coverage.Level.AtLeast(CoverageTerminal) {
		add("completion requires terminal coverage")
	}
}

// Law 6/9: a satisfied status is evidence-backed; missing evidence cannot pass.
func validateEvidenceBackedStatuses(r PreReviewReport, add func(string, ...any)) {
	checkItem := func(where, status string, evidence []string) {
		if passStatuses[strings.TrimSpace(status)] && len(evidence) == 0 {
			add("%s claims %q without evidence", where, status)
		}
	}
	forEachProtectionItem(r.Protection, func(section string, it ProtectionItem) {
		checkItem(section+":"+it.ID, it.Status, it.EvidenceRefs)
	})
	for _, o := range r.Proof.RequiredObligations {
		checkItem("proof_obligation:"+o.ID, o.Status, o.EvidenceRefs)
	}
}

// Law 5/10: findings retain epistemic status; a model candidate cannot become a
// governed fact.
func validateEpistemicIntegrity(r PreReviewReport, add func(string, ...any)) {
	forEachImpactItem(r.Impact, func(section string, it ImpactItem) {
		if it.Epistemic == EpistemicModelCandidate {
			add("impact %s:%s is a model candidate presented as a governed fact", section, it.ID)
		}
	})
	forEachProtectionItem(r.Protection, func(section string, it ProtectionItem) {
		if it.Epistemic == EpistemicModelCandidate {
			add("protection %s:%s is a model candidate presented as a governed fact", section, it.ID)
		}
	})
	for _, o := range r.Proof.RequiredObligations {
		if o.Epistemic == EpistemicModelCandidate {
			add("proof obligation %s is a model candidate presented as a governed fact", o.ID)
		}
	}
}

// Law 7/8/11: verdicts come only from receipts; caller booleans cannot forge
// them.
func validateReceiptBackedVerdicts(r PreReviewReport, add func(string, ...any)) {
	if c := r.Proof.Certification; c != nil {
		if strings.TrimSpace(c.Verdict) == "certified" && strings.TrimSpace(c.ReceiptDigestSHA256) == "" {
			add("certification claims certified without a receipt digest")
		}
	}
	if r.Disposition == DispositionCertified && !r.Proof.Certification.IsCertified() {
		add("disposition certified requires a verified certification receipt")
	}
	if r.Disposition == DispositionTerminallyClosed {
		if !r.Result.Completion.HasReceipt() {
			add("disposition terminally_closed requires a completion receipt")
		}
		if !r.Proof.Certification.IsCertified() {
			add("disposition terminally_closed requires a verified certification receipt")
		}
	}
}

// Law 15: an unavailable result architecture shows no deltas.
func validateResultAvailability(r PreReviewReport, add func(string, ...any)) {
	res := r.Result
	if res.Available {
		if strings.TrimSpace(res.ResultGraphDigestSHA256) == "" || strings.TrimSpace(res.BaseGraphDigestSHA256) == "" {
			add("available result architecture requires base and result graph digests")
		}
		return
	}
	if resultHasDeltas(res) || res.ResultGraphDigestSHA256 != "" || res.ResultTreeDigestSHA256 != "" || res.Completion.HasReceipt() {
		add("unavailable result architecture must not carry deltas, digests, or completion")
	}
}

// Laws 10/11 for attention items, plus duplicate detection.
func validateAttention(r PreReviewReport, add func(string, ...any)) {
	ids := make(map[string]struct{}, len(r.ReviewerAttention))
	keys := make(map[string]struct{}, len(r.ReviewerAttention))
	for _, a := range r.ReviewerAttention {
		if !ValidEpistemic(a.Epistemic) {
			add("attention %s has unknown epistemic status %q", a.ID, a.Epistemic)
		}
		if a.Epistemic == EpistemicModelCandidate && a.Blocking {
			add("attention %s is a model candidate but marked blocking", a.ID)
		}
		if a.Category == AttentionModelCandidate {
			if a.Epistemic != EpistemicModelCandidate {
				add("attention %s in model_candidate category must carry model_candidate epistemic", a.ID)
			}
			if a.Blocking {
				add("attention %s is a model candidate but marked blocking", a.ID)
			}
		}
		if _, dup := ids[a.ID]; dup {
			add("duplicate attention id %q", a.ID)
		}
		ids[a.ID] = struct{}{}
		if _, dup := keys[attentionDedupKey(a)]; dup {
			add("duplicate reviewer question %q", a.Question)
		}
		keys[attentionDedupKey(a)] = struct{}{}
	}
}

// Law 12: a narrative is never authoritative.
func validateNarrative(r PreReviewReport, add func(string, ...any)) {
	if r.Narrative == nil {
		return
	}
	if r.Narrative.Authoritative {
		add("narrative must not be authoritative")
	}
}

func validateSummary(r PreReviewReport, add func(string, ...any)) {
	if r.Summary.ReviewerAttentionCount != len(r.ReviewerAttention) {
		add("summary reviewer_attention_count %d != %d", r.Summary.ReviewerAttentionCount, len(r.ReviewerAttention))
	}
	if r.Summary.CurrentDisposition != string(r.Disposition) {
		add("summary current_disposition %q != disposition %q", r.Summary.CurrentDisposition, r.Disposition)
	}
}

// Laws 13/14: canonical identity is stable and self-consistent; a report bound
// to the wrong diff or a tampered body fails here.
func validateIdentity(r PreReviewReport, add func(string, ...any)) {
	wantID, err := ComputeReportID(r.Binding)
	if err != nil {
		add("compute report id: %v", err)
	} else if r.ReportID != wantID {
		add("report_id %q does not match its binding (%q)", r.ReportID, wantID)
	}
	wantDigest, err := ComputeReportDigest(r)
	if err != nil {
		add("compute report digest: %v", err)
	} else if r.ReportDigestSHA256 != wantDigest {
		add("report_digest_sha256 does not match canonical content")
	}
	if r.Disposition != DeriveDisposition(r) {
		add("stored disposition %q does not match derived disposition %q", r.Disposition, DeriveDisposition(r))
	}
}

func governanceIsPopulated(g GovernanceSummary) bool {
	return g.AuthorityStatus != "" || g.AdmissionStatus != "" || g.CapabilityStatus != "" ||
		g.ScopeStatus != "" || g.AuthorityResolutionDigestSHA256 != "" || len(g.Violations) > 0
}

func resultHasDeltas(res ResultArchitectureSummary) bool {
	return len(res.ComponentsAdded)+len(res.ComponentsRemoved)+len(res.BoundariesAdded)+
		len(res.BoundariesRemoved)+len(res.AuthorityChanges)+len(res.ContractChanges)+
		len(res.ProofRequirementChanges)+len(res.NewContradictions)+len(res.InvalidatedProofs) > 0
}
