// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.controlstate
// @awareness file_role=typed_source_provider
// @awareness implements=globular.awareness_graph:invariant.controlstate.server_read_handler_must_consume_canonical_projection
// @awareness implements=globular.awareness_graph:invariant.controlstate.repository_context_must_not_be_derived_from_cwd
package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	"github.com/globulario/sensei/golang/architecture/controlstate"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// ControlStateReadProvider is the typed source boundary between the control-panel read handlers
// and the underlying stores/owners. Handlers never interpret raw RDF edges or governed YAML —
// they consume controlstate inputs or owner-projected typed observations from this seam.
//
// Provider outputs are NEVER map[string]any, raw triples for the handler to interpret, raw YAML
// records, error prose used as severity, or filesystem roots selected by a request. Where no
// authoritative typed adapter exists yet, the provider supplies a typed unavailable source with
// an exact reason code — it does not infer satisfaction from graph adjacency, manufacture zero,
// or weaken unknown.
type ControlStateReadProvider interface {
	// GraphAuthority returns the typed OBSERVED graph-authority state plus the EXPECTED
	// embedded-seed identity — kept strictly separate. The observed identity (live digest) is
	// the only value ever used as graph-authority identity; the expected seed digest is
	// metadata/limitation material and never becomes an observed authority.
	GraphAuthority(ctx context.Context) (controlstate.GraphAuthorityObservation, string, error)

	// CatalogSnapshot composes the bounded typed catalog batch for one logical scope, carrying
	// the three-source ledger (catalog_enumeration primary, graph_authority required,
	// unclassified_discovery relevant). One bounded enumeration — never a full ArtifactState
	// per row.
	CatalogSnapshot(ctx context.Context, repositoryIdentity, domain string) (controlstate.CatalogSnapshot, error)

	// ControlSnapshotInput composes the full typed control-snapshot input for the EFFECTIVE
	// (already-resolved) domain scope. Sources with no authoritative typed adapter yet stay
	// honestly absent/unavailable (typed), never zero.
	ControlSnapshotInput(ctx context.Context, repositoryIdentity, effectiveDomain string) (controlstate.ControlSnapshotInput, error)

	// ArtifactSourceBundle returns the typed identity observation and source bundle for ONE
	// exact, ALREADY-VALIDATED node IRI. observed=false means the scoped lookup completed and
	// found no visible triples. errGraphAuthorityUnobserved is returned when no observed
	// authority identity exists to bind an artifact identity to.
	ArtifactSourceBundle(ctx context.Context, repositoryIdentity, effectiveDomain, nodeIRI string) (controlstate.ArtifactIdentity, controlstate.ClassResolution, controlstate.ArtifactSourceBundle, bool, error)
}

// errGraphAuthorityUnobserved is the typed provider signal that the live graph authority is
// unobserved: no artifact identity can be bound (the expected seed digest is never substituted).
var errGraphAuthorityUnobserved = fmt.Errorf("graph authority is unobserved")

// typedNodeLister is the OPTIONAL store capability behind unknown-class discovery: a bounded,
// deterministic, cursor-paged enumeration of typed nodes with their identity-relevant facts.
// A store without it yields a typed-unavailable unclassified_discovery source — never a silent
// omission.
type typedNodeLister interface {
	TypedNodeFactsPage(ctx context.Context, afterIRI string, limit int) ([]store.ImpactFact, string, error)
}

// Bounds for one catalog batch. Both caps surface as typed DEGRADED sources when plausibly hit —
// no silent truncation.
const (
	// maxCatalogClassFetch matches the store-side ClassFacts safety cap (per class).
	maxCatalogClassFetch = 300
	// maxDiscoveryNodes bounds the unknown-class discovery sweep (total distinct nodes).
	maxDiscoveryNodes = 2000
)

// storeControlStateProvider is the production provider: graph authority from the seed-freshness
// owner (seedmeta), catalog/artifact observations from the read-only store, feedback capability
// from the immutable startup-owned Phase 9.6 repository context. It holds NO write capability.
type storeControlStateProvider struct {
	s   *server
	reg controlstate.Registry
}

// controlStateProvider returns the server's provider (test-injectable via s.controlProvider).
func (s *server) controlStateProvider() ControlStateReadProvider {
	if s.controlProvider != nil {
		return s.controlProvider
	}
	return &storeControlStateProvider{s: s, reg: controlstate.DefaultRegistry()}
}

// GraphAuthority maps the seed-freshness owner's TYPED admissibility observation
// (seedmeta.AuthorityObservation from an INDEPENDENT live-marker discovery — never a handed-in
// freshness verification's Live identity, never Detail prose) to the controlstate observation.
// The frozen mapping:
//
//	current                          → observed, current, intact, live identity (required)
//	stale WITH admissible live id    → observed, stale, intact, live identity (required)
//	integrity failed, id established → observed, NOT intact, live identity
//	integrity failed, no identity    → unobserved
//	unobserved (marker absent, digest missing, empty, query failed, …) → unobserved
//
// The expected embedded-seed digest is returned SEPARATELY as expected metadata and never
// becomes an observed identity. Live and expected digests may legitimately be EQUAL when the
// independent discovery proves it (the current case).
func (p *storeControlStateProvider) GraphAuthority(ctx context.Context) (controlstate.GraphAuthorityObservation, string, error) {
	obs, _, expected := p.authorityState(ctx)
	return obs, expected, nil
}

// authorityState is the internal richer form: the mapped observation, the typed seedmeta reason
// for every non-current state, and the expected seed digest (metadata only). The observation is
// derived from an INDEPENDENT live-marker discovery over the store — a differing digest literal
// attached to the expected IRI can never masquerade as a stale-admissible live identity.
func (p *storeControlStateProvider) authorityState(ctx context.Context) (controlstate.GraphAuthorityObservation, string, string) {
	a, expected := snapshotLiveAuthority(ctx, p.s)
	switch a.State {
	case seedmeta.AuthorityCurrent:
		return controlstate.GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: a.LiveIdentity, Digest: a.LiveIdentity}, a.Reason, expected.Digest
	case seedmeta.AuthorityStaleAdmissible:
		return controlstate.GraphAuthorityObservation{Observed: true, Current: false, Integrity: true, Identity: a.LiveIdentity, Digest: a.LiveIdentity}, a.Reason, expected.Digest
	case seedmeta.AuthorityIntegrityFailed:
		if a.LiveIdentity == "" {
			// No independently established identity: nothing was observed.
			return controlstate.GraphAuthorityObservation{}, a.Reason, expected.Digest
		}
		return controlstate.GraphAuthorityObservation{Observed: true, Current: false, Integrity: false, Identity: a.LiveIdentity, Digest: a.LiveIdentity}, a.Reason, expected.Digest
	default: // AuthorityUnobserved
		return controlstate.GraphAuthorityObservation{}, a.Reason, expected.Digest
	}
}

// catalogObservation accumulates one node's typed observations across enumerations. Statuses and
// labels are COLLECTED (not first-row-wins) so backend row ordering cannot change the result.
type catalogObservation struct {
	labels   map[string]bool
	statuses map[string]bool
	classes  map[string]bool
	repos    map[string]bool
	shared   bool
	tagged   bool
}

func newCatalogObservation() *catalogObservation {
	return &catalogObservation{labels: map[string]bool{}, statuses: map[string]bool{}, classes: map[string]bool{}, repos: map[string]bool{}}
}

func (o *catalogObservation) absorb(f store.ImpactFact) {
	switch f.Predicate {
	case rdf.PropType:
		if f.ObjectIsIRI {
			o.classes[f.Object] = true
		}
	case rdf.PropLabel:
		if !f.ObjectIsIRI && f.Object != "" {
			o.labels[f.Object] = true
		}
	case rdf.PropStatus:
		if !f.ObjectIsIRI && f.Object != "" {
			o.statuses[f.Object] = true
		}
	case rdf.PropRepo:
		if f.Object != "" {
			o.tagged = true
			o.repos[f.Object] = true
		}
	case rdf.PropDomain:
		if f.Object == rdf.DomainShared {
			o.shared = true
		}
	}
}

// inScope applies the multi-tag-aware scope law (mirrors nodeInScopeFromTriples): shared →
// visible everywhere; ANY matching aw:repo tag → visible; untagged → home domain; foreign-only
// → invisible. Fact ordering cannot change the outcome (sets, not first-wins).
func (o *catalogObservation) inScope(home, scope string) bool {
	if scope == "" {
		return true
	}
	if o.shared {
		return true
	}
	if !o.tagged {
		return InScope(home, scope)
	}
	for repo := range o.repos {
		if InScope(repo, scope) {
			return true
		}
	}
	return false
}

// deterministicLabel picks ONE label deterministically: the lexically smallest candidate
// (reviewed rule — canonical preference is exact; lexical order is the tie-break). Shuffled fact
// order cannot change it.
func (o *catalogObservation) deterministicLabel() string {
	best := ""
	for l := range o.labels {
		if best == "" || l < best {
			best = l
		}
	}
	return best
}

// lifecycleSource applies the deterministic status law: zero statuses → absent; one canonical
// status (repeats deduplicated) → available; multiple DISTINCT statuses → INVALID with
// lifecycle_status_ambiguous (never first-row-wins).
func (o *catalogObservation) lifecycleSource(nodeIRI string) controlstate.LifecycleSource {
	switch len(o.statuses) {
	case 0:
		return controlstate.LifecycleSource{Observed: false}
	case 1:
		var status string
		for s := range o.statuses {
			status = s
		}
		return controlstate.LifecycleSource{
			Observed: true, Availability: controlstate.SourceAvailable,
			Owner: "governed", Schema: "governed_status", Identity: nodeIRI, Status: status,
		}
	default:
		return controlstate.LifecycleSource{
			Observed: true, Availability: controlstate.SourceInvalid,
			Owner: "governed", Schema: "governed_status", Identity: nodeIRI,
			ReasonCode: "lifecycle_status_ambiguous",
		}
	}
}

// CatalogSnapshot composes the catalog under the frozen authority law:
//
//	observed+current+integrity  → enumeration available, authority available, trusted rows;
//	observed+stale+integrity    → authority DEGRADED (graph_authority_stale), known rows kept,
//	                              projection PARTIAL via the required-source law;
//	observed+integrity failure  → authority INVALID, enumeration INVALID, no rows;
//	unobserved                  → authority UNAVAILABLE, enumeration UNAVAILABLE, no rows; the
//	                              expected seed digest appears ONLY as a limitation.
//
// Unknown-class discovery is a separate RELEVANT source: known and unknown nodes merge through
// one canonical deduplication path; a cap degrades unclassified_discovery explicitly while known
// rows stay visible.
func (p *storeControlStateProvider) CatalogSnapshot(ctx context.Context, repositoryIdentity, domain string) (controlstate.CatalogSnapshot, error) {
	if p.s.store == nil {
		return controlstate.CatalogSnapshot{}, fmt.Errorf("store is unavailable")
	}
	obs, typedReason, expected := p.authorityState(ctx)

	src := func(owner, schema, identity string, avail controlstate.SourceAvailability, impact controlstate.SourceImpact, reason string) controlstate.SourceStatus {
		st := controlstate.SourceStatus{Owner: owner, Schema: schema, Identity: identity, Availability: avail, Impact: impact, ReasonCode: reason}
		if avail == controlstate.SourceAvailable {
			st.ReasonCode = ""
		} else if st.ReasonCode == "" {
			st.ReasonCode = string(avail)
		}
		return st
	}

	// Unobserved or integrity-failed authority: no trusted rows, typed empty catalog. The
	// seedmeta typed reason (expected_marker_absent, live_marker_absent, live_store_empty,
	// verification_query_failed, live_marker_count_mismatch, live_graph_count_mismatch, …)
	// travels on the sources — never Detail prose.
	if !obs.Observed || !obs.Integrity {
		snapID := "catalog:" + domain + ":unverified"
		authorityIdentity := ""
		authAvail := controlstate.SourceUnavailable
		enumAvail := controlstate.SourceUnavailable
		reason := typedReason
		if reason == "" {
			reason = "graph_authority_unobserved"
		}
		var limits []string
		if obs.Observed && !obs.Integrity {
			authorityIdentity = obs.Identity
			authAvail = controlstate.SourceInvalid
			enumAvail = controlstate.SourceInvalid
		} else if expected != "" {
			// Expected-authority METADATA only — never an observed identity.
			limits = append(limits, "expected_authority_seed_digest:"+expected)
		}
		return controlstate.BuildCatalogSnapshot(p.reg, controlstate.CatalogScope{
			RepositoryIdentity:     repositoryIdentity,
			DomainIdentity:         domain,
			GraphAuthorityIdentity: authorityIdentity,
			SnapshotIdentity:       snapID,
			Source:                 src("controlstate.catalog", "catalog_enumeration", snapID, enumAvail, controlstate.ImpactPrimary, reason),
			AuthoritySource:        src("graph_authority", "graph_authority", authorityIdentity, authAvail, controlstate.ImpactRequired, reason),
			DiscoverySource:        src("controlstate.catalog", "unclassified_discovery", "", controlstate.SourceUnavailable, controlstate.ImpactRelevant, "not_enumerated"),
			Limitations:            limits,
		}, nil)
	}

	// Observed with integrity (current or stale): enumerate.
	merged := map[string]*catalogObservation{}
	absorbFacts := func(facts []store.ImpactFact) map[string]bool {
		distinct := map[string]bool{}
		for _, f := range facts {
			distinct[f.NodeIRI] = true
			o := merged[f.NodeIRI]
			if o == nil {
				o = newCatalogObservation()
				merged[f.NodeIRI] = o
			}
			if f.TypeIRI != "" {
				o.classes[f.TypeIRI] = true
			}
			o.absorb(f)
		}
		return distinct
	}

	// Known-class enumeration (catalog_enumeration, PRIMARY).
	enumTruncated := false
	for _, class := range p.reg.Classes {
		if class.Unclassified {
			continue // the fallback is a resolution outcome, never an RDF class to enumerate
		}
		facts, err := p.s.store.ClassFacts(ctx, class.ClassIRI, maxCatalogClassFetch)
		if err != nil {
			return controlstate.CatalogSnapshot{}, err
		}
		distinct := absorbFacts(facts)
		if len(distinct) >= maxCatalogClassFetch {
			enumTruncated = true
		}
	}

	// Unknown-class discovery (unclassified_discovery, RELEVANT): one canonical deduplication
	// path with the known rows via the same merged map.
	discoveryAvail := controlstate.SourceUnavailable
	discoveryReason := "discovery_not_supported"
	if lister, ok := p.s.store.(typedNodeLister); ok {
		discoveryAvail = controlstate.SourceAvailable
		discoveryReason = ""
		seen := 0
		after := ""
		for {
			facts, next, err := lister.TypedNodeFactsPage(ctx, after, maxCatalogClassFetch)
			if err != nil {
				return controlstate.CatalogSnapshot{}, err
			}
			distinct := absorbFacts(facts)
			seen += len(distinct)
			if next == "" {
				break // explicit completeness
			}
			if seen >= maxDiscoveryNodes {
				discoveryAvail = controlstate.SourceDegraded
				discoveryReason = "unclassified_discovery_truncated"
				break // explicit truncation — never silent omission
			}
			after = next
		}
	}

	// Scope filter (multi-tag-aware) + typed observations.
	var observations []controlstate.CatalogArtifactObservation
	iris := make([]string, 0, len(merged))
	for iri := range merged {
		iris = append(iris, iri)
	}
	sort.Strings(iris)
	for _, iri := range iris {
		o := merged[iri]
		if !o.inScope(p.s.homeDomain, domain) {
			continue
		}
		var classes []string
		for c := range o.classes {
			classes = append(classes, c)
		}
		observations = append(observations, controlstate.CatalogArtifactObservation{
			NodeIRI:         iri,
			Label:           o.deterministicLabel(),
			ObservedClasses: classes,
			Lifecycle:       o.lifecycleSource(iri),
		})
	}

	snapID := "catalog:" + domain + ":" + obs.Identity
	enumAvail := controlstate.SourceAvailable
	enumReason := ""
	if enumTruncated {
		enumAvail = controlstate.SourceDegraded
		enumReason = "class_enumeration_truncated"
	}
	authAvail := controlstate.SourceAvailable
	authReason := ""
	if !obs.Current {
		authAvail = controlstate.SourceDegraded
		authReason = "graph_authority_stale"
	}
	return controlstate.BuildCatalogSnapshot(p.reg, controlstate.CatalogScope{
		RepositoryIdentity:     repositoryIdentity,
		DomainIdentity:         domain,
		GraphAuthorityIdentity: obs.Identity,
		SnapshotIdentity:       snapID,
		Source:                 src("controlstate.catalog", "catalog_enumeration", snapID, enumAvail, controlstate.ImpactPrimary, enumReason),
		AuthoritySource:        src("graph_authority", "graph_authority", obs.Identity, authAvail, controlstate.ImpactRequired, authReason),
		DiscoverySource:        src("controlstate.catalog", "unclassified_discovery", snapID, discoveryAvail, controlstate.ImpactRelevant, discoveryReason),
	}, observations)
}

// ControlSnapshotInput composes the typed snapshot input for the EFFECTIVE scope. Sources with
// no authoritative typed adapter on the server yet stay honestly ABSENT (typed unavailable
// relevant sources / normal optional absence). The attention collection is DEGRADED with
// attention_sources_incomplete while only the graph-authority family has a canonical typed
// adapter: known items are partial data; zero is never a complete zero.
func (p *storeControlStateProvider) ControlSnapshotInput(ctx context.Context, repositoryIdentity, effectiveDomain string) (controlstate.ControlSnapshotInput, error) {
	obs, _, _ := p.authorityState(ctx)
	catalog, err := p.CatalogSnapshot(ctx, repositoryIdentity, effectiveDomain)
	if err != nil {
		return controlstate.ControlSnapshotInput{}, err
	}

	// Snapshot-level attention: only the graph-authority family has a canonical typed adapter;
	// items originate ONLY from validated controlstate construction. An unobserved authority
	// yields no item (its unavailability lives in the sources ledger + authority summary).
	var items []controlstate.AttentionItem
	if obs.Identity != "" {
		if a, ok, err := controlstate.AttentionForGraphAuthority(obs.Identity, obs.Digest, obs.Observed, obs.Current, obs.Integrity, nil); err != nil {
			return controlstate.ControlSnapshotInput{}, err
		} else if ok {
			items = append(items, a)
		}
	}

	in := controlstate.ControlSnapshotInput{
		RepositoryIdentity: repositoryIdentity,
		Domain:             effectiveDomain,
		Authority: controlstate.GraphAuthoritySummary{
			Observed: obs.Observed, Current: obs.Current, Integrity: obs.Integrity, Identity: obs.Identity,
		},
		Catalog: catalog,
		Attention: controlstate.AttentionObservation{
			Owner: "controlstate.attention", Schema: "attention",
			Identity:     "attention:" + attentionAnchor(obs.Identity),
			Availability: controlstate.SourceDegraded,
			ReasonCode:   "attention_sources_incomplete",
			Items:        items,
		},
		Feedback: p.feedbackObservation(ctx, effectiveDomain),
	}
	return in, nil
}

// attentionAnchor names the attention-collection identity anchor.
func attentionAnchor(observedIdentity string) string {
	if observedIdentity == "" {
		return "unverified"
	}
	return observedIdentity
}

// feedbackObservation exposes ONLY Phase 9.6 feedback capability/availability, bound to the
// EFFECTIVE control-panel scope: capability exists only when the immutable startup-owned
// repository context exists AND its domain exactly matches the effective scope (Phase 9.6 scope
// law + reason vocabulary — repository_context_domain_mismatch on mismatch). No promotion
// discovery, no repository-wide scan, no record collection.
func (p *storeControlStateProvider) feedbackObservation(ctx context.Context, effectiveDomain string) *controlstate.FeedbackObservation {
	if p.s.briefingRepo == nil {
		return &controlstate.FeedbackObservation{
			Owner: "briefingfeedback", Schema: "briefing.feedback_projection/v1",
			Availability: controlstate.SourceUnavailable,
			ReasonCode:   string(briefingfeedback.RepositoryContextAbsent),
			Context:      controlstate.FeedbackContext{Capable: false, Availability: "feedback_unavailable"},
		}
	}
	// The effective domain is resolved by the handler BEFORE provider acquisition; an
	// unresolved empty scope is never compared as authority.
	if effectiveDomain == "" || effectiveDomain != p.s.briefingRepo.Domain {
		return &controlstate.FeedbackObservation{
			Owner: "briefingfeedback", Schema: "briefing.feedback_projection/v1",
			Availability: controlstate.SourceUnavailable,
			ReasonCode:   string(briefingfeedback.RepositoryContextDomainMismatch),
			Context:      controlstate.FeedbackContext{Capable: false, Availability: "feedback_unavailable"},
		}
	}
	return &controlstate.FeedbackObservation{
		Owner: "briefingfeedback", Schema: "briefing.feedback_projection/v1",
		Identity:     "briefingfeedback:" + p.s.briefingRepo.Domain,
		Availability: controlstate.SourceAvailable,
		Context:      controlstate.FeedbackContext{Capable: true, Availability: "feedback_available"},
	}
}

// ArtifactSourceBundle observes ONE node under the multi-tag-aware scope law
// (nodeInScopeFromTriples: shared everywhere; any matching aw:repo tag; untagged → home;
// foreign-only invisible — triple order cannot change visibility). Statuses are COLLECTED and
// assessed deterministically (distinct conflict → invalid lifecycle_status_ambiguous). Dimension
// and contradiction sources have no authoritative typed server adapters yet and stay typed-
// unavailable. The node IRI is already RPC-validated; the store validates again in depth.
func (p *storeControlStateProvider) ArtifactSourceBundle(ctx context.Context, repositoryIdentity, effectiveDomain, nodeIRI string) (controlstate.ArtifactIdentity, controlstate.ClassResolution, controlstate.ArtifactSourceBundle, bool, error) {
	if p.s.store == nil {
		return controlstate.ArtifactIdentity{}, controlstate.ClassResolution{}, controlstate.ArtifactSourceBundle{}, false, fmt.Errorf("store is unavailable")
	}
	obs, _, _ := p.authorityState(ctx)
	// No observed authority identity → no artifact identity can be bound (the expected seed
	// digest is never substituted).
	if !obs.Observed || obs.Identity == "" {
		return controlstate.ArtifactIdentity{}, controlstate.ClassResolution{}, controlstate.ArtifactSourceBundle{}, false, errGraphAuthorityUnobserved
	}

	triples, err := p.s.store.Describe(ctx, nodeIRI)
	if err != nil {
		return controlstate.ArtifactIdentity{}, controlstate.ClassResolution{}, controlstate.ArtifactSourceBundle{}, false, fmt.Errorf("backend query failed: %w", err)
	}
	// Multi-repository scope law (shared / any-matching-repo / untagged→home / foreign-only
	// invisible). Never the single-domain resolver for a visibility decision.
	observed := len(triples) > 0
	if observed && effectiveDomain != "" && !nodeInScopeFromTriples(triples, p.s.homeDomain, effectiveDomain) {
		observed = false
		triples = nil
	}

	o := newCatalogObservation()
	for _, t := range triples {
		o.absorb(store.ImpactFact{NodeIRI: nodeIRI, Predicate: t.Predicate, Object: t.Object, ObjectIsIRI: t.ObjectIsIRI})
	}
	var classes []string
	for c := range o.classes {
		classes = append(classes, c)
	}
	sort.Strings(classes)

	id, res, err := controlstate.BuildArtifactIdentity(p.reg, nodeIRI, classes, repositoryIdentity, effectiveDomain, obs.Identity, nil)
	if err != nil {
		return controlstate.ArtifactIdentity{}, controlstate.ClassResolution{}, controlstate.ArtifactSourceBundle{}, false, err
	}
	bundle := controlstate.ArtifactSourceBundle{
		GraphAuthority: obs,
		Contradiction: controlstate.ContradictionSource{
			Availability: controlstate.SourceUnavailable, ReasonCode: "source_not_observed",
		},
		Lifecycle: o.lifecycleSource(nodeIRI),
	}
	return id, res, bundle, observed, nil
}
