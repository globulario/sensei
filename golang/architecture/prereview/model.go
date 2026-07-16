// SPDX-License-Identifier: Apache-2.0

package prereview

// PreReviewReport is the versioned, canonical record produced for a proposed
// change. It is a projection over typed evidence: it may display authoritative
// verdicts sourced from receipts but never creates or upgrades them.
//
// Canonical identity (ReportID, ReportDigestSHA256) is computed from the
// semantic content only. Display metadata and the optional narrative are
// excluded, so temporary paths, render time, and PR numbers never change a
// report's identity.
type PreReviewReport struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	ReportID      string `json:"report_id" yaml:"report_id"`

	Binding    ReviewBinding             `json:"binding" yaml:"binding"`
	Coverage   CoverageSummary           `json:"coverage" yaml:"coverage"`
	Summary    ExecutiveSummary          `json:"summary" yaml:"summary"`
	Change     ChangeSummary             `json:"change" yaml:"change"`
	Impact     ArchitecturalImpact       `json:"impact" yaml:"impact"`
	Governance GovernanceSummary         `json:"governance" yaml:"governance"`
	Protection ProtectionSummary         `json:"protection" yaml:"protection"`
	History    HistoricalRiskSummary     `json:"history" yaml:"history"`
	Proof      ProofSummary              `json:"proof" yaml:"proof"`
	Result     ResultArchitectureSummary `json:"result" yaml:"result"`
	Epistemic  EpistemicSummary          `json:"epistemic" yaml:"epistemic"`

	ReviewerAttention []ReviewerAttentionItem `json:"reviewer_attention" yaml:"reviewer_attention"`
	Disposition       ReviewDisposition       `json:"disposition" yaml:"disposition"`
	Limitations       []string                `json:"limitations,omitempty" yaml:"limitations,omitempty"`

	// Narrative is an optional, non-authoritative prose summary. It is excluded
	// from canonical identity and may be absent.
	Narrative *Narrative `json:"narrative,omitempty" yaml:"narrative,omitempty"`
	// Display holds presentation-only metadata excluded from canonical identity.
	Display *DisplayMetadata `json:"display,omitempty" yaml:"display,omitempty"`

	ReportDigestSHA256 string `json:"report_digest_sha256" yaml:"report_digest_sha256"`
}

// ReviewBinding binds the report to an exact repository and diff, and optionally
// to a verified task ledger head. It is the report's semantic anchor.
type ReviewBinding struct {
	RepositoryDomain     string `json:"repository_domain" yaml:"repository_domain"`
	BaseRevision         string `json:"base_revision" yaml:"base_revision"`
	BaseTreeDigestSHA256 string `json:"base_tree_digest_sha256" yaml:"base_tree_digest_sha256"`
	HeadRevision         string `json:"head_revision" yaml:"head_revision"`
	HeadTreeDigestSHA256 string `json:"head_tree_digest_sha256" yaml:"head_tree_digest_sha256"`
	MergeBaseRevision    string `json:"merge_base_revision,omitempty" yaml:"merge_base_revision,omitempty"`
	DiffDigestSHA256     string `json:"diff_digest_sha256" yaml:"diff_digest_sha256"`

	TaskID                 string `json:"task_id,omitempty" yaml:"task_id,omitempty"`
	SessionID              string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
	LedgerHeadDigestSHA256 string `json:"ledger_head_digest_sha256,omitempty" yaml:"ledger_head_digest_sha256,omitempty"`

	BaseGraphDigestSHA256   string `json:"base_graph_digest_sha256,omitempty" yaml:"base_graph_digest_sha256,omitempty"`
	ResultGraphDigestSHA256 string `json:"result_graph_digest_sha256,omitempty" yaml:"result_graph_digest_sha256,omitempty"`

	PolicyIDs []string `json:"policy_ids,omitempty" yaml:"policy_ids,omitempty"`
}

// DisplayMetadata is presentation-only and never part of semantic identity.
type DisplayMetadata struct {
	PRNumber     int    `json:"pr_number,omitempty" yaml:"pr_number,omitempty"`
	PRURL        string `json:"pr_url,omitempty" yaml:"pr_url,omitempty"`
	BranchName   string `json:"branch_name,omitempty" yaml:"branch_name,omitempty"`
	CheckoutPath string `json:"checkout_path,omitempty" yaml:"checkout_path,omitempty"`
	RenderedAt   string `json:"rendered_at,omitempty" yaml:"rendered_at,omitempty"`
}

// CoverageSummary declares the evidence level the report was built at and names
// what was and was not available.
type CoverageSummary struct {
	Level       CoverageLevel `json:"level" yaml:"level"`
	Available   []string      `json:"available,omitempty" yaml:"available,omitempty"`
	Unavailable []string      `json:"unavailable,omitempty" yaml:"unavailable,omitempty"`
}

// ExecutiveSummary is a fixed set of derived headline fields. Its values are
// derived from the structured report, never invented from the raw diff.
type ExecutiveSummary struct {
	Purpose                string `json:"purpose,omitempty" yaml:"purpose,omitempty"`
	ArchitecturalRisk      string `json:"architectural_risk,omitempty" yaml:"architectural_risk,omitempty"`
	CurrentDisposition     string `json:"current_disposition,omitempty" yaml:"current_disposition,omitempty"`
	HighestPriorityBlocker string `json:"highest_priority_blocker,omitempty" yaml:"highest_priority_blocker,omitempty"`
	ReviewerAttentionCount int    `json:"reviewer_attention_count" yaml:"reviewer_attention_count"`
}

// RenamePair records a file rename.
type RenamePair struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

// ChangeSummary describes the observed diff.
type ChangeSummary struct {
	FilesCreated         []string     `json:"files_created,omitempty" yaml:"files_created,omitempty"`
	FilesModified        []string     `json:"files_modified,omitempty" yaml:"files_modified,omitempty"`
	FilesDeleted         []string     `json:"files_deleted,omitempty" yaml:"files_deleted,omitempty"`
	FilesRenamed         []RenamePair `json:"files_renamed,omitempty" yaml:"files_renamed,omitempty"`
	AffectedSymbols      []string     `json:"affected_symbols,omitempty" yaml:"affected_symbols,omitempty"`
	AdmittedOperations   []string     `json:"admitted_operations,omitempty" yaml:"admitted_operations,omitempty"`
	ObservedOperations   []string     `json:"observed_operations,omitempty" yaml:"observed_operations,omitempty"`
	GeneratedArtifacts   []string     `json:"generated_artifacts,omitempty" yaml:"generated_artifacts,omitempty"`
	OutOfEnvelopeChanges []string     `json:"out_of_envelope_changes,omitempty" yaml:"out_of_envelope_changes,omitempty"`
}

// ImpactItem is one architectural-impact entry with its evidence and epistemic
// standing.
type ImpactItem struct {
	ID           string          `json:"id" yaml:"id"`
	Title        string          `json:"title,omitempty" yaml:"title,omitempty"`
	Epistemic    EpistemicStatus `json:"epistemic" yaml:"epistemic"`
	EvidenceRefs []string        `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
}

// ArchitecturalImpact groups the change's architectural reach. Every item must
// carry evidence references.
type ArchitecturalImpact struct {
	RiskClass              string       `json:"risk_class,omitempty" yaml:"risk_class,omitempty"`
	AffectedComponents     []ImpactItem `json:"affected_components,omitempty" yaml:"affected_components,omitempty"`
	ChangedBoundaries      []ImpactItem `json:"changed_boundaries,omitempty" yaml:"changed_boundaries,omitempty"`
	AffectedContracts      []ImpactItem `json:"affected_contracts,omitempty" yaml:"affected_contracts,omitempty"`
	AuthorityDomains       []ImpactItem `json:"authority_domains,omitempty" yaml:"authority_domains,omitempty"`
	StateObjects           []ImpactItem `json:"state_objects,omitempty" yaml:"state_objects,omitempty"`
	UpstreamDependents     []ImpactItem `json:"upstream_dependents,omitempty" yaml:"upstream_dependents,omitempty"`
	DownstreamDependencies []ImpactItem `json:"downstream_dependencies,omitempty" yaml:"downstream_dependencies,omitempty"`
	ChangedRelationships   []ImpactItem `json:"changed_relationships,omitempty" yaml:"changed_relationships,omitempty"`
}

// GovernanceViolation is a typed scope or authority violation.
type GovernanceViolation struct {
	Code     string   `json:"code" yaml:"code"`
	Path     string   `json:"path,omitempty" yaml:"path,omitempty"`
	Detail   string   `json:"detail,omitempty" yaml:"detail,omitempty"`
	Severity Severity `json:"severity" yaml:"severity"`
}

// GovernanceSummary displays typed authority and admission status. Its fields
// are sourced from typed receipts, never from briefing prose.
type GovernanceSummary struct {
	Actor                           string                `json:"actor,omitempty" yaml:"actor,omitempty"`
	VerifiedRoles                   []string              `json:"verified_roles,omitempty" yaml:"verified_roles,omitempty"`
	AuthorityStatus                 string                `json:"authority_status,omitempty" yaml:"authority_status,omitempty"`
	AuthorityResolutionDigestSHA256 string                `json:"authority_resolution_digest_sha256,omitempty" yaml:"authority_resolution_digest_sha256,omitempty"`
	GrantIDs                        []string              `json:"grant_ids,omitempty" yaml:"grant_ids,omitempty"`
	DelegationIDs                   []string              `json:"delegation_ids,omitempty" yaml:"delegation_ids,omitempty"`
	SelectedMechanisms              []string              `json:"selected_mechanisms,omitempty" yaml:"selected_mechanisms,omitempty"`
	AdmissionStatus                 string                `json:"admission_status,omitempty" yaml:"admission_status,omitempty"`
	CapabilityStatus                string                `json:"capability_status,omitempty" yaml:"capability_status,omitempty"`
	ScopeStatus                     string                `json:"scope_status,omitempty" yaml:"scope_status,omitempty"`
	Violations                      []GovernanceViolation `json:"violations,omitempty" yaml:"violations,omitempty"`
}

// ProtectionItem is one applicable governing rule and its current status.
type ProtectionItem struct {
	ID            string          `json:"id" yaml:"id"`
	Title         string          `json:"title,omitempty" yaml:"title,omitempty"`
	Severity      Severity        `json:"severity" yaml:"severity"`
	Applicability string          `json:"applicability,omitempty" yaml:"applicability,omitempty"`
	Status        string          `json:"status" yaml:"status"`
	Epistemic     EpistemicStatus `json:"epistemic" yaml:"epistemic"`
	EvidenceRefs  []string        `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
}

// ProtectionSummary groups applicable governing rules.
type ProtectionSummary struct {
	Invariants         []ProtectionItem `json:"invariants,omitempty" yaml:"invariants,omitempty"`
	Contracts          []ProtectionItem `json:"contracts,omitempty" yaml:"contracts,omitempty"`
	FailureModes       []ProtectionItem `json:"failure_modes,omitempty" yaml:"failure_modes,omitempty"`
	ForbiddenFixes     []ProtectionItem `json:"forbidden_fixes,omitempty" yaml:"forbidden_fixes,omitempty"`
	RequiredTests      []ProtectionItem `json:"required_tests,omitempty" yaml:"required_tests,omitempty"`
	GovernedExceptions []ProtectionItem `json:"governed_exceptions,omitempty" yaml:"governed_exceptions,omitempty"`
	IntendedDirections []ProtectionItem `json:"intended_directions,omitempty" yaml:"intended_directions,omitempty"`
}

// HistoryItem is one relevant historical risk record.
type HistoryItem struct {
	ID           string   `json:"id" yaml:"id"`
	Title        string   `json:"title,omitempty" yaml:"title,omitempty"`
	Reference    string   `json:"reference,omitempty" yaml:"reference,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
}

// HistoricalRiskSummary groups relevant past failures and decisions.
type HistoricalRiskSummary struct {
	RelatedIncidents         []HistoryItem `json:"related_incidents,omitempty" yaml:"related_incidents,omitempty"`
	RevertedChanges          []HistoryItem `json:"reverted_changes,omitempty" yaml:"reverted_changes,omitempty"`
	AttemptedForbiddenFixes  []HistoryItem `json:"attempted_forbidden_fixes,omitempty" yaml:"attempted_forbidden_fixes,omitempty"`
	RecurringFailurePatterns []HistoryItem `json:"recurring_failure_patterns,omitempty" yaml:"recurring_failure_patterns,omitempty"`
	RelevantDecisions        []HistoryItem `json:"relevant_decisions,omitempty" yaml:"relevant_decisions,omitempty"`
}

// EvidenceRef is a reference to an evidence artifact by digest, never inline
// content.
type EvidenceRef struct {
	ID           string `json:"id" yaml:"id"`
	Kind         string `json:"kind,omitempty" yaml:"kind,omitempty"`
	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
}

// ProofSlot is a required architectural proof obligation and its discharge
// status. "test executed" is distinct from "test discharges this slot".
type ProofSlot struct {
	ID           string          `json:"id" yaml:"id"`
	Title        string          `json:"title,omitempty" yaml:"title,omitempty"`
	Status       string          `json:"status" yaml:"status"`
	Epistemic    EpistemicStatus `json:"epistemic" yaml:"epistemic"`
	EvidenceRefs []string        `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
}

// Waiver is a governed, expiring exception.
type Waiver struct {
	ID        string `json:"id" yaml:"id"`
	Reason    string `json:"reason,omitempty" yaml:"reason,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

// CertificationView displays an independently produced certification verdict.
// A verdict of "certified" is honored only when a certification receipt digest
// is present; there is no caller boolean that can establish it.
type CertificationView struct {
	Verdict             string `json:"verdict,omitempty" yaml:"verdict,omitempty"`
	ReceiptDigestSHA256 string `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
	VerifiedAt          string `json:"verified_at,omitempty" yaml:"verified_at,omitempty"`
}

// IsCertified reports whether the view displays a certified verdict backed by a
// receipt digest. Certification is never established without the receipt.
func (c *CertificationView) IsCertified() bool {
	return c != nil && c.Verdict == "certified" && c.ReceiptDigestSHA256 != ""
}

// ProofSummary groups proof obligations, evidence, and the certification view.
type ProofSummary struct {
	RequiredObligations []ProofSlot        `json:"required_obligations,omitempty" yaml:"required_obligations,omitempty"`
	RequiredSlots       []string           `json:"required_slots,omitempty" yaml:"required_slots,omitempty"`
	DischargedSlots     []string           `json:"discharged_slots,omitempty" yaml:"discharged_slots,omitempty"`
	UnresolvedSlots     []string           `json:"unresolved_slots,omitempty" yaml:"unresolved_slots,omitempty"`
	StaticEvidence      []EvidenceRef      `json:"static_evidence,omitempty" yaml:"static_evidence,omitempty"`
	TestReceipts        []EvidenceRef      `json:"test_receipts,omitempty" yaml:"test_receipts,omitempty"`
	RuntimeEvidence     []EvidenceRef      `json:"runtime_evidence,omitempty" yaml:"runtime_evidence,omitempty"`
	ArtifactReceipts    []EvidenceRef      `json:"artifact_receipts,omitempty" yaml:"artifact_receipts,omitempty"`
	StaleReceipts       []EvidenceRef      `json:"stale_receipts,omitempty" yaml:"stale_receipts,omitempty"`
	ConflictedReceipts  []EvidenceRef      `json:"conflicted_receipts,omitempty" yaml:"conflicted_receipts,omitempty"`
	Waivers             []Waiver           `json:"waivers,omitempty" yaml:"waivers,omitempty"`
	Certification       *CertificationView `json:"certification,omitempty" yaml:"certification,omitempty"`
}

// CompletionView displays a verified terminal completion.
type CompletionView struct {
	ReceiptDigestSHA256 string `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
	CompletedAt         string `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
}

// HasReceipt reports whether the completion is backed by a receipt digest.
func (c *CompletionView) HasReceipt() bool {
	return c != nil && c.ReceiptDigestSHA256 != ""
}

// ResultArchitectureSummary describes how the resulting architecture differs
// from the base. It is unavailable until a verified result transition exists;
// while unavailable, all its deltas are empty.
type ResultArchitectureSummary struct {
	Available               bool            `json:"available" yaml:"available"`
	BaseGraphDigestSHA256   string          `json:"base_graph_digest_sha256,omitempty" yaml:"base_graph_digest_sha256,omitempty"`
	ResultGraphDigestSHA256 string          `json:"result_graph_digest_sha256,omitempty" yaml:"result_graph_digest_sha256,omitempty"`
	ResultTreeDigestSHA256  string          `json:"result_tree_digest_sha256,omitempty" yaml:"result_tree_digest_sha256,omitempty"`
	ArtifactFreshness       string          `json:"artifact_freshness,omitempty" yaml:"artifact_freshness,omitempty"`
	ComponentsAdded         []string        `json:"components_added,omitempty" yaml:"components_added,omitempty"`
	ComponentsRemoved       []string        `json:"components_removed,omitempty" yaml:"components_removed,omitempty"`
	BoundariesAdded         []string        `json:"boundaries_added,omitempty" yaml:"boundaries_added,omitempty"`
	BoundariesRemoved       []string        `json:"boundaries_removed,omitempty" yaml:"boundaries_removed,omitempty"`
	AuthorityChanges        []string        `json:"authority_changes,omitempty" yaml:"authority_changes,omitempty"`
	ContractChanges         []string        `json:"contract_changes,omitempty" yaml:"contract_changes,omitempty"`
	ProofRequirementChanges []string        `json:"proof_requirement_changes,omitempty" yaml:"proof_requirement_changes,omitempty"`
	NewContradictions       []string        `json:"new_contradictions,omitempty" yaml:"new_contradictions,omitempty"`
	InvalidatedProofs       []string        `json:"invalidated_proofs,omitempty" yaml:"invalidated_proofs,omitempty"`
	Completion              *CompletionView `json:"completion,omitempty" yaml:"completion,omitempty"`
}

// Statement is one load-bearing statement in the epistemic summary. Its
// epistemic status is given by the group it belongs to.
type Statement struct {
	ID           string   `json:"id" yaml:"id"`
	Claim        string   `json:"claim" yaml:"claim"`
	EvidenceRefs []string `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
}

// EpistemicSummary groups load-bearing statements by epistemic status. These
// groups are never merged into a single confidence score.
type EpistemicSummary struct {
	Observed                  []Statement `json:"observed,omitempty" yaml:"observed,omitempty"`
	Governed                  []Statement `json:"governed,omitempty" yaml:"governed,omitempty"`
	DeterministicallyInferred []Statement `json:"deterministically_inferred,omitempty" yaml:"deterministically_inferred,omitempty"`
	ModelCandidates           []Statement `json:"model_candidates,omitempty" yaml:"model_candidates,omitempty"`
	Contradicted              []Statement `json:"contradicted,omitempty" yaml:"contradicted,omitempty"`
	Unknown                   []Statement `json:"unknown,omitempty" yaml:"unknown,omitempty"`
	Stale                     []Statement `json:"stale,omitempty" yaml:"stale,omitempty"`
	NotApplicable             []Statement `json:"not_applicable,omitempty" yaml:"not_applicable,omitempty"`
	Uncertifiable             []Statement `json:"uncertifiable,omitempty" yaml:"uncertifiable,omitempty"`
}

// ReviewerAttentionItem is one human decision the report surfaces. It names a
// question only a human may legitimately answer, why it matters, and how to
// resolve it. RankScore is deterministically computed.
type ReviewerAttentionItem struct {
	ID             string            `json:"id" yaml:"id"`
	Category       AttentionCategory `json:"category" yaml:"category"`
	Question       string            `json:"question" yaml:"question"`
	WhyItMatters   string            `json:"why_it_matters,omitempty" yaml:"why_it_matters,omitempty"`
	Blocking       bool              `json:"blocking" yaml:"blocking"`
	Severity       Severity          `json:"severity" yaml:"severity"`
	Epistemic      EpistemicStatus   `json:"epistemic" yaml:"epistemic"`
	EvidenceRefs   []string          `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`
	RelatedFiles   []string          `json:"related_files,omitempty" yaml:"related_files,omitempty"`
	AllowedAnswers []string          `json:"allowed_answers,omitempty" yaml:"allowed_answers,omitempty"`
	ResolutionPath string            `json:"resolution_path,omitempty" yaml:"resolution_path,omitempty"`

	// TaskRelevance and ArchitecturalReach are deterministic ranking inputs in
	// [0,3]; RankScore is their weighted combination. All are versioned by
	// AttentionPolicyVersion.
	TaskRelevance      int `json:"task_relevance" yaml:"task_relevance"`
	ArchitecturalReach int `json:"architectural_reach" yaml:"architectural_reach"`
	RankScore          int `json:"rank_score" yaml:"rank_score"`
}

// Narrative is an optional, non-authoritative prose summary generated from a
// canonical report. It may reference finding IDs but never creates findings.
type Narrative struct {
	GeneratedBy             string `json:"generated_by,omitempty" yaml:"generated_by,omitempty"`
	Model                   string `json:"model,omitempty" yaml:"model,omitempty"`
	InputReportDigestSHA256 string `json:"input_report_digest_sha256,omitempty" yaml:"input_report_digest_sha256,omitempty"`
	Text                    string `json:"text,omitempty" yaml:"text,omitempty"`
	Authoritative           bool   `json:"authoritative" yaml:"authoritative"`
}
