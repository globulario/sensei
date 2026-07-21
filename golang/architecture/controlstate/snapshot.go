// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"sort"
)

// ControlSnapshotSchema identifies architecture.control_snapshot/v1.
const ControlSnapshotSchema = "architecture.control_snapshot/v1"

const maxSnapshotAttention = 50

// KeyedCount is a deterministic (key, count) pair (used instead of maps for stable output).
type KeyedCount struct {
	Key   string `json:"key" yaml:"key"`
	Count int    `json:"count" yaml:"count"`
}

// GraphAuthoritySummary is the typed authority state (never recomputed here).
type GraphAuthoritySummary struct {
	Observed  bool   `json:"observed" yaml:"observed"`
	Current   bool   `json:"current" yaml:"current"`
	Integrity bool   `json:"integrity" yaml:"integrity"`
	Identity  string `json:"identity,omitempty" yaml:"identity,omitempty"`
}

// CoverageSummary / TaskSummary / CompletionSummary / FeedbackContext are bounded payloads.
type CoverageSummary struct {
	Sufficient     bool `json:"sufficient" yaml:"sufficient"`
	BlindSpotCount int  `json:"blind_spot_count" yaml:"blind_spot_count"`
	HighRiskBlind  int  `json:"high_risk_blind_spot_count" yaml:"high_risk_blind_spot_count"`
}

type TaskSummary struct {
	TaskID    string `json:"task_id" yaml:"task_id"`
	SessionID string `json:"session_id,omitempty" yaml:"session_id,omitempty"`
	Closure   string `json:"closure,omitempty" yaml:"closure,omitempty"`
	Admission string `json:"admission,omitempty" yaml:"admission,omitempty"`
}

type CompletionSummary struct {
	TerminalState           string `json:"terminal_state,omitempty" yaml:"terminal_state,omitempty"`
	AuthoritativeCompletion bool   `json:"authoritative_completion" yaml:"authoritative_completion"`
}

// FeedbackContext exposes ONLY feedback capability/availability — never repository-wide records.
type FeedbackContext struct {
	Capable      bool   `json:"capable" yaml:"capable"`
	Availability string `json:"availability,omitempty" yaml:"availability,omitempty"`
}

// CountObservation is a typed count owner observation. A definitive count is exposed only when the
// source is Available; zero is data only with SourceAvailable.
type CountObservation struct {
	Owner        string
	Schema       string
	Identity     string
	Digest       string
	Availability SourceAvailability
	ReasonCode   string
	Count        int
}

// AttentionObservation is the typed attention-collection source. An empty collection is zero only
// when the source is Available.
type AttentionObservation struct {
	Owner        string
	Schema       string
	Identity     string
	Availability SourceAvailability
	// ReasonCode is the typed reason for a non-available collection (e.g.
	// attention_sources_incomplete while only some attention families have canonical typed
	// adapters — known items are then PARTIAL data, and zero is never a complete zero).
	ReasonCode string
	Items      []AttentionItem
}

// CoverageObservation / TaskObservation / CompletionObservation / FeedbackObservation are typed
// envelopes for the optional summaries.
type CoverageObservation struct {
	Owner, Schema, Identity string
	Availability            SourceAvailability
	Summary                 CoverageSummary
}
type TaskObservation struct {
	Owner, Schema, Identity string
	Availability            SourceAvailability
	Summary                 TaskSummary
}
type CompletionObservation struct {
	Owner, Schema, Identity string
	Availability            SourceAvailability
	Summary                 CompletionSummary
}
type FeedbackObservation struct {
	Owner, Schema, Identity string
	Availability            SourceAvailability
	// ReasonCode is the typed reason for a non-available source (e.g. the Phase 9.6
	// repository_context_domain_mismatch when the startup-owned repository context does not
	// match the effective control-panel scope).
	ReasonCode string
	Context    FeedbackContext
}

// ControlSnapshotInput is the typed input. Every relevant source contributes a SourceStatus, so a
// missing relevant source makes the snapshot partial (never zero/available).
type ControlSnapshotInput struct {
	RepositoryIdentity string
	Domain             string
	Authority          GraphAuthoritySummary
	Catalog            CatalogSnapshot
	Attention          AttentionObservation

	OpenQuestions      *CountObservation
	Contradictions     *CountObservation
	MissingEvidence    *CountObservation
	MissingTests       *CountObservation
	MissingEnforcement *CountObservation

	Coverage   *CoverageObservation
	ActiveTask *TaskObservation
	Completion *CompletionObservation
	Feedback   *FeedbackObservation
}

// ControlSnapshot is architecture.control_snapshot/v1 — bounded summaries only.
type ControlSnapshot struct {
	ProjectionMeta   `json:",inline" yaml:",inline"`
	RegistryDigest   string                `json:"registry_digest" yaml:"registry_digest"`
	Authority        GraphAuthoritySummary `json:"graph_authority" yaml:"graph_authority"`
	CountsByClass    []KeyedCount          `json:"counts_by_class,omitempty" yaml:"counts_by_class,omitempty"`
	CoverageCounts   []KeyedCount          `json:"assessment_coverage_counts,omitempty" yaml:"assessment_coverage_counts,omitempty"`
	ClosureCounts    []KeyedCount          `json:"closure_counts,omitempty" yaml:"closure_counts,omitempty"`
	LifecycleUnknown *int                  `json:"lifecycle_unknown_count,omitempty" yaml:"lifecycle_unknown_count,omitempty"`
	AttentionCounts  []KeyedCount          `json:"attention_counts_by_severity,omitempty" yaml:"attention_counts_by_severity,omitempty"`
	TopAttention     []AttentionItem       `json:"top_attention,omitempty" yaml:"top_attention,omitempty"`
	OpenQuestions    *int                  `json:"open_question_count,omitempty" yaml:"open_question_count,omitempty"`
	Contradictions   *int                  `json:"contradiction_count,omitempty" yaml:"contradiction_count,omitempty"`
	MissingEvidence  *int                  `json:"missing_evidence_count,omitempty" yaml:"missing_evidence_count,omitempty"`
	MissingTests     *int                  `json:"missing_test_count,omitempty" yaml:"missing_test_count,omitempty"`
	MissingEnforce   *int                  `json:"missing_enforcement_count,omitempty" yaml:"missing_enforcement_count,omitempty"`
	Coverage         *CoverageSummary      `json:"coverage,omitempty" yaml:"coverage,omitempty"`
	ActiveTask       *TaskSummary          `json:"active_task,omitempty" yaml:"active_task,omitempty"`
	Completion       *CompletionSummary    `json:"completion,omitempty" yaml:"completion,omitempty"`
	FeedbackContext  *FeedbackContext      `json:"feedback_context,omitempty" yaml:"feedback_context,omitempty"`
}

// BuildControlSnapshot composes the bounded overview from typed sources. No synthetic score, no
// repository correctness, no repository-wide feedback collection.
func BuildControlSnapshot(reg Registry, in ControlSnapshotInput) (ControlSnapshot, error) {
	if err := reg.Validate(); err != nil {
		return ControlSnapshot{}, fmt.Errorf("invalid registry: %w", err)
	}
	if in.RepositoryIdentity == "" {
		return ControlSnapshot{}, fmt.Errorf("control snapshot requires a repository identity")
	}
	catalogObserved := in.Catalog.SnapshotIdentity != ""
	if catalogObserved {
		if err := ValidateCatalogScope(reg, in.Catalog); err != nil {
			return ControlSnapshot{}, fmt.Errorf("invalid catalog scope: %w", err)
		}
	}
	// Validate every typed source envelope (fail closed on a malformed observation).
	if err := validateEnvelope("attention", in.Attention.Owner, in.Attention.Schema, in.Attention.Identity, in.Attention.Availability); err != nil {
		return ControlSnapshot{}, err
	}
	// Validate every supplied attention item BEFORE dedup/truncation — a malformed item after the
	// truncation cap must still reject the snapshot (fail closed, no silent drop).
	for i, a := range in.Attention.Items {
		if err := validateAttentionItem(a); err != nil {
			return ControlSnapshot{}, fmt.Errorf("attention item %d invalid: %w", i, err)
		}
	}
	for name, c := range map[string]*CountObservation{"open_questions": in.OpenQuestions, "contradictions": in.Contradictions, "missing_evidence": in.MissingEvidence, "missing_tests": in.MissingTests, "missing_enforcement": in.MissingEnforcement} {
		if c == nil {
			continue
		}
		if err := validateEnvelope(name, c.Owner, c.Schema, c.Identity, c.Availability); err != nil {
			return ControlSnapshot{}, err
		}
		if c.Count < 0 {
			return ControlSnapshot{}, fmt.Errorf("%s count is negative", name)
		}
	}
	if in.Coverage != nil {
		if err := validateEnvelope("coverage", in.Coverage.Owner, in.Coverage.Schema, in.Coverage.Identity, in.Coverage.Availability); err != nil {
			return ControlSnapshot{}, err
		}
		if in.Coverage.Summary.BlindSpotCount < 0 || in.Coverage.Summary.HighRiskBlind < 0 {
			return ControlSnapshot{}, fmt.Errorf("coverage summary has a negative count")
		}
	}
	if in.ActiveTask != nil {
		if err := validateEnvelope("task", in.ActiveTask.Owner, in.ActiveTask.Schema, in.ActiveTask.Identity, in.ActiveTask.Availability); err != nil {
			return ControlSnapshot{}, err
		}
	}
	if in.Completion != nil {
		if err := validateEnvelope("completion", in.Completion.Owner, in.Completion.Schema, in.Completion.Identity, in.Completion.Availability); err != nil {
			return ControlSnapshot{}, err
		}
	}
	if in.Feedback != nil {
		if err := validateEnvelope("feedback", in.Feedback.Owner, in.Feedback.Schema, in.Feedback.Identity, in.Feedback.Availability); err != nil {
			return ControlSnapshot{}, err
		}
		ctxAvail := in.Feedback.Context.Availability
		if ctxAvail != "" && !phase96Availability[ctxAvail] {
			return ControlSnapshot{}, fmt.Errorf("feedback context availability %q is not a Phase 9.6 value", ctxAvail)
		}
		// The context's Phase 9.6 availability must agree with the source status: an available source
		// requires a valid context, and an unavailable/degraded/invalid source cannot claim a
		// stronger availability (e.g. feedback_available).
		if ctxAvail != "" && feedbackSourceAvailability(ctxAvail) != in.Feedback.Availability {
			return ControlSnapshot{}, fmt.Errorf("feedback context availability %q does not match the source status %q", ctxAvail, in.Feedback.Availability)
		}
		if in.Feedback.Availability == SourceAvailable && ctxAvail == "" {
			return ControlSnapshot{}, fmt.Errorf("an available feedback source requires a context availability")
		}
	}
	regDigest, err := reg.Digest()
	if err != nil {
		return ControlSnapshot{}, err
	}

	// Graph-authority coherence: Current/Integrity cannot be asserted while unobserved; an observed
	// authority carries a non-empty identity that exactly binds to the catalog authority identity.
	// A mismatched authority/catalog pair is rejected BEFORE any tallying.
	if !in.Authority.Observed && (in.Authority.Current || in.Authority.Integrity) {
		return ControlSnapshot{}, fmt.Errorf("graph authority asserts current/integrity while unobserved")
	}
	if in.Authority.Observed {
		if in.Authority.Identity == "" {
			return ControlSnapshot{}, fmt.Errorf("observed graph authority has no identity")
		}
		if catalogObserved && in.Authority.Identity != in.Catalog.GraphAuthorityIdentity {
			return ControlSnapshot{}, fmt.Errorf("snapshot authority identity %q does not match the catalog authority identity %q", in.Authority.Identity, in.Catalog.GraphAuthorityIdentity)
		}
	}

	var sources []SourceStatus
	var limitations []string
	// Primary: the artifact catalog — consume the supplied Catalog.Source directly (already
	// validated primary/identity/registry-digest by ValidateCatalogScope); never reconstruct.
	// The catalog's unclassified-discovery relevant source and typed limitations travel with it
	// (its graph-authority source is the same observation as the snapshot's own required
	// authority source below, so it is not duplicated).
	if catalogObserved {
		sources = append(sources, in.Catalog.Source, in.Catalog.DiscoverySource)
		limitations = append(limitations, in.Catalog.Limitations...)
	} else {
		sources = append(sources, srcStatus("controlstate.catalog", "catalog", "", "", SourceUnavailable, ImpactPrimary, catalogNotObservedReason))
	}
	// Only an AVAILABLE catalog is trusted to expose/tally rows.
	catalogTrusted := catalogObserved && in.Catalog.Source.Availability == SourceAvailable
	// Required: graph authority + attention.
	authAvail := SourceUnavailable
	if in.Authority.Observed {
		authAvail = SourceAvailable
		if !in.Authority.Integrity {
			authAvail = SourceInvalid
		} else if !in.Authority.Current {
			authAvail = SourceDegraded
		}
	}
	sources = append(sources, srcStatus("graph_authority", "graph_authority", in.Authority.Identity, "", authAvail, ImpactRequired, ""))
	sources = append(sources, envelopeSource("attention", in.Attention.Owner, in.Attention.Schema, in.Attention.Identity, in.Attention.Availability, ImpactRequired, in.Attention.ReasonCode))

	snap := ControlSnapshot{RegistryDigest: regDigest, Authority: in.Authority}

	if catalogTrusted {
		snap.CountsByClass = tally(in.Catalog.Artifacts, func(a ArtifactSummary) string { return a.Class })
		snap.CoverageCounts = tally(in.Catalog.Artifacts, func(a ArtifactSummary) string { return string(a.Coverage) })
		snap.ClosureCounts = tally(in.Catalog.Artifacts, func(a ArtifactSummary) string { return string(a.Closure) })
		lc := 0
		for _, a := range in.Catalog.Artifacts {
			if a.Lifecycle == LifecycleUnknown {
				lc++
			}
		}
		snap.LifecycleUnknown = &lc
	}

	// Attention: a COMPLETE zero requires an AVAILABLE collection (every integrated family
	// observed). A DEGRADED collection (attention_sources_incomplete) still exposes its
	// validated known items — as partial data the availability ledger declares, never as an
	// authoritative complete collection.
	if in.Attention.Availability == SourceAvailable || in.Attention.Availability == SourceDegraded {
		att := dedupSortAttention(in.Attention.Items)
		snap.AttentionCounts = tally(att, func(a AttentionItem) string { return string(a.Severity) })
		if len(att) > maxSnapshotAttention {
			snap.TopAttention = att[:maxSnapshotAttention]
		} else {
			snap.TopAttention = att
		}
	}

	// Relevant counts: exposed only when their source is available; always contribute a source.
	snap.OpenQuestions = countAndSource(&sources, "questiondisposition", "open_questions", in.OpenQuestions)
	snap.Contradictions = countAndSource(&sources, "extractor.contradiction", "contradictions", in.Contradictions)
	snap.MissingEvidence = countAndSource(&sources, "closure.evidence", "missing_evidence", in.MissingEvidence)
	snap.MissingTests = countAndSource(&sources, "closure.verification", "missing_tests", in.MissingTests)
	snap.MissingEnforce = countAndSource(&sources, "closure.enforcement", "missing_enforcement", in.MissingEnforcement)

	if in.Coverage != nil {
		sources = append(sources, envelopeSource("coverage", in.Coverage.Owner, in.Coverage.Schema, in.Coverage.Identity, in.Coverage.Availability, ImpactRelevant, ""))
		if in.Coverage.Availability == SourceAvailable {
			c := in.Coverage.Summary
			snap.Coverage = &c
		}
	} else {
		sources = append(sources, srcStatus("coverage", "coverage", "", "", SourceUnavailable, ImpactRelevant, "source_not_observed"))
	}
	// Optional summaries: a SUPPLIED but degraded/unavailable/invalid source is preserved in the
	// ledger (never trusted for a payload); absence (nil) is normal and contributes nothing.
	if in.ActiveTask != nil {
		sources = append(sources, envelopeSource("task", in.ActiveTask.Owner, in.ActiveTask.Schema, in.ActiveTask.Identity, in.ActiveTask.Availability, ImpactOptional, ""))
		if in.ActiveTask.Availability == SourceAvailable {
			s := in.ActiveTask.Summary
			snap.ActiveTask = &s
		}
	}
	if in.Completion != nil {
		sources = append(sources, envelopeSource("completion", in.Completion.Owner, in.Completion.Schema, in.Completion.Identity, in.Completion.Availability, ImpactOptional, ""))
		if in.Completion.Availability == SourceAvailable {
			s := in.Completion.Summary
			snap.Completion = &s
		}
	}
	if in.Feedback != nil {
		// Always retain the supplied source status; expose the context ONLY when the source is
		// available (a degraded/unavailable/invalid source withholds its payload).
		sources = append(sources, envelopeSource("briefingfeedback", in.Feedback.Owner, in.Feedback.Schema, in.Feedback.Identity, in.Feedback.Availability, ImpactOptional, in.Feedback.ReasonCode))
		if in.Feedback.Availability == SourceAvailable {
			c := in.Feedback.Context
			snap.FeedbackContext = &c
		}
	}

	avail := aggregateAvailability(sources)
	snap.ProjectionMeta = newMeta(ControlSnapshotSchema, in.RepositoryIdentity, in.Domain, avail, sources, limitations)
	dig, err := computeSnapshotDigest(snap)
	if err != nil {
		return ControlSnapshot{}, err
	}
	snap.DigestSHA256 = dig
	if err := ValidateControlSnapshot(snap); err != nil {
		return ControlSnapshot{}, err
	}
	return snap, nil
}

// countAndSource exposes the count only when the observation is available, and always contributes
// a relevant SourceStatus (a missing observation → unavailable relevant → partial snapshot).
func countAndSource(sources *[]SourceStatus, owner, schema string, obs *CountObservation) *int {
	if obs == nil {
		*sources = append(*sources, srcStatus(owner, schema, "", "", SourceUnavailable, ImpactRelevant, "source_not_observed"))
		return nil
	}
	*sources = append(*sources, srcStatus(nonEmpty(obs.Owner, owner), nonEmpty(obs.Schema, schema), obs.Identity, obs.Digest, obs.Availability, ImpactRelevant, obs.ReasonCode))
	if obs.Availability == SourceAvailable {
		n := obs.Count
		return &n
	}
	return nil
}

// validateEnvelope fails closed on a malformed typed source envelope: an off-vocabulary
// availability, or an observed (available/degraded) envelope missing owner/schema/identity.
func validateEnvelope(name, owner, schema, identity string, avail SourceAvailability) error {
	if avail != "" && !validSourceAvailability(avail) {
		return fmt.Errorf("%s envelope availability %q off-vocabulary", name, avail)
	}
	if avail == SourceAvailable || avail == SourceDegraded {
		if owner == "" || schema == "" {
			return fmt.Errorf("observed %s envelope missing owner/schema", name)
		}
		if identity == "" {
			return fmt.Errorf("observed %s envelope missing identity", name)
		}
	}
	return nil
}

// envelopeSource builds a normalized SourceStatus (central srcStatus normalization: a non-available
// source carries a typed reason; an available source carries none).
func envelopeSource(defOwner, owner, schema, identity string, avail SourceAvailability, impact SourceImpact, reason string) SourceStatus {
	a := avail
	if a == "" {
		a = SourceUnavailable
	}
	return srcStatus(nonEmpty(owner, defOwner), nonEmpty(schema, defOwner), identity, "", a, impact, reason)
}

func tally[T any](items []T, key func(T) string) []KeyedCount {
	m := map[string]int{}
	for _, it := range items {
		m[key(it)]++
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]KeyedCount, 0, len(keys))
	for _, k := range keys {
		out = append(out, KeyedCount{Key: k, Count: m[k]})
	}
	return out
}

func computeSnapshotDigest(s ControlSnapshot) (string, error) {
	s.DigestSHA256 = ""
	return digestOf(s)
}

// ValidateControlSnapshot strictly validates the snapshot.
func ValidateControlSnapshot(s ControlSnapshot) error {
	if err := validateMeta(s.ProjectionMeta, ControlSnapshotSchema); err != nil {
		return err
	}
	if s.RegistryDigest == "" {
		return fmt.Errorf("control snapshot missing registry digest")
	}
	if len(s.TopAttention) > maxSnapshotAttention {
		return fmt.Errorf("control snapshot exceeds the attention cap")
	}
	for _, a := range s.TopAttention {
		if err := validateAttentionItem(a); err != nil {
			return err
		}
	}
	want, err := computeSnapshotDigest(s)
	if err != nil {
		return err
	}
	if s.DigestSHA256 != want {
		return fmt.Errorf("control snapshot digest does not match its content")
	}
	return nil
}
