// SPDX-License-Identifier: Apache-2.0

// Package governedimpact derives the exact governed-knowledge change between an
// admitted base architecture and a result architecture. It is pure: it reads no
// repository, no ledger, no environment, no clock, and no active governance-pack
// pointer. Change is always a difference of governed category-manifest digests,
// never a caller-declared flag, and every change is attributed to a stable
// governed record identity.
package governedimpact

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/rdf"
)

// SchemaVersion identifies the impact report shape.
const SchemaVersion = "governedimpact.report/v1"

// Error is a typed, code-stable failure.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func newErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Stable error codes.
const (
	CodeInvalidSnapshot        = "governedimpact.invalid_snapshot"
	CodeDuplicateRecord        = "governedimpact.duplicate_record"
	CodeUnattributableRelation = "governedimpact.unattributable_relation"
	CodeInvalidReport          = "governedimpact.invalid_report"
)

// RecordIdentity is one governed record's stable id and its content digest.
type RecordIdentity struct {
	ID                   string `json:"id" yaml:"id"`
	SemanticDigestSHA256 string `json:"semantic_digest_sha256" yaml:"semantic_digest_sha256"`
}

// CategoryManifest is the sorted set of a category's governed records and the
// recomputed digest over them.
type CategoryManifest struct {
	Category       string           `json:"category" yaml:"category"`
	RecordIdentity []RecordIdentity `json:"record_identity" yaml:"record_identity"`
	DigestSHA256   string           `json:"digest_sha256" yaml:"digest_sha256"`
}

// Report is the complete governed-knowledge impact of a result transition: ten
// base manifests, ten result manifests, and ten impacts, all in canonical
// category order.
type Report struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`

	BaseGraphDigestSHA256   string `json:"base_graph_digest_sha256" yaml:"base_graph_digest_sha256"`
	ResultGraphDigestSHA256 string `json:"result_graph_digest_sha256" yaml:"result_graph_digest_sha256"`

	BaseManifests   []CategoryManifest `json:"base_manifests" yaml:"base_manifests"`
	ResultManifests []CategoryManifest `json:"result_manifests" yaml:"result_manifests"`

	Impacts []closureprotocol.GovernedKnowledgeImpact `json:"impacts" yaml:"impacts"`

	Limitations []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

// Snapshot is the exact, already-computed architecture input for one side of the
// comparison. It carries no repository path.
type Snapshot struct {
	GraphSemanticDigestSHA256 string
	Triples                   []graphsnapshot.Triple

	ProofObligationsBytes                []byte
	ProofObligationsSemanticDigestSHA256 string

	CertificationPolicyID string
	CompletionPolicyID    string
}

// categoryKind selects how a category's manifest is built.
type categoryKind int

const (
	kindGraph categoryKind = iota
	kindProofObligations
	kindCertificationPolicy
	kindCompletionPolicy
)

// category declares the closed membership of one governed-knowledge category.
type category struct {
	name string
	kind categoryKind
	// classIRIs: a subject typed as one of these is a full record whose identity
	// is all of its triples.
	classIRIs map[string]bool
	// ownedPredicates: a subject with such an edge is a relation-owner whose
	// identity (for this category) is only its owned edges — so a relation-only
	// change is attributed to the owning subject.
	ownedPredicates map[string]bool
}

func set(vals ...string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// registry is the one closed category registry, in canonical order. Membership is
// by governed class and governed predicate — never by filename.
var registry = []category{
	{name: "authority", kind: kindGraph, classIRIs: set(
		rdf.ClassAuthorityDomain, rdf.ClassAuthoritySurface, rdf.ClassActorRole, rdf.ClassAuthorityGrant,
		rdf.ClassDelegationPolicy, rdf.ClassMutationPath, rdf.ClassObservationPath, rdf.ClassAllowedWriter,
		rdf.ClassAllowedReader, rdf.ClassTrustBoundary, rdf.ClassStateObject, rdf.ClassOwnerService,
	)},
	{name: "invariants", kind: kindGraph, classIRIs: set(rdf.ClassInvariant)},
	{name: "contracts", kind: kindGraph, classIRIs: set(rdf.ClassContract)},
	{name: "failure_modes", kind: kindGraph, classIRIs: set(rdf.ClassFailureMode)},
	{name: "forbidden_fixes", kind: kindGraph, classIRIs: set(rdf.ClassForbiddenFix),
		ownedPredicates: set(rdf.PropForbids, rdf.PropForbidsCall, rdf.PropForbiddenShortcut)},
	{name: "required_tests", kind: kindGraph, classIRIs: set(rdf.ClassTest),
		ownedPredicates: set(rdf.PropRequiresTest, rdf.PropTestedBy)},
	{name: "proof_obligations", kind: kindProofObligations},
	{name: "evidence_profiles", kind: kindGraph, classIRIs: set(
		rdf.ClassEvidenceProfile, rdf.ClassRuntimeEvidence, rdf.ClassEvidenceProbe,
		rdf.ClassEvidenceFreshnessWindow, rdf.ClassEvidenceTrustLevel, rdf.ClassEvidenceOwnerPath,
	)},
	{name: "certification_policy", kind: kindCertificationPolicy},
	{name: "completion_policy", kind: kindCompletionPolicy},
}

func init() {
	// The registry order must equal the closed closureprotocol vocabulary exactly.
	want := closureprotocol.GovernedKnowledgeCategories()
	if len(want) != len(registry) {
		panic("governedimpact: registry length differs from closureprotocol vocabulary")
	}
	for i := range want {
		if registry[i].name != want[i] {
			panic("governedimpact: registry order differs from closureprotocol vocabulary at " + want[i])
		}
	}
}

// Compare derives the impact report between base and result snapshots.
func Compare(base, result Snapshot) (Report, error) {
	if err := validateSnapshot(base); err != nil {
		return Report{}, err
	}
	if err := validateSnapshot(result); err != nil {
		return Report{}, err
	}
	rep := Report{
		SchemaVersion:           SchemaVersion,
		BaseGraphDigestSHA256:   base.GraphSemanticDigestSHA256,
		ResultGraphDigestSHA256: result.GraphSemanticDigestSHA256,
	}
	baseBySubject := indexBySubject(base.Triples)
	resultBySubject := indexBySubject(result.Triples)

	for _, cat := range registry {
		bm, blimits, err := buildManifest(cat, base, baseBySubject)
		if err != nil {
			return Report{}, err
		}
		rm, rlimits, err := buildManifest(cat, result, resultBySubject)
		if err != nil {
			return Report{}, err
		}
		rep.BaseManifests = append(rep.BaseManifests, bm)
		rep.ResultManifests = append(rep.ResultManifests, rm)
		rep.Impacts = append(rep.Impacts, impactFor(cat.name, bm, rm))
		rep.Limitations = append(rep.Limitations, blimits...)
		rep.Limitations = append(rep.Limitations, rlimits...)
	}
	rep.Limitations = cleanStrings(rep.Limitations)
	if err := ValidateReport(rep); err != nil {
		return Report{}, err
	}
	return rep, nil
}

func validateSnapshot(s Snapshot) error {
	if !isHex64(s.GraphSemanticDigestSHA256) {
		return newErr(CodeInvalidSnapshot, "graph semantic digest is not a 64-hex sha256")
	}
	if len(s.ProofObligationsBytes) > 0 && !isHex64(s.ProofObligationsSemanticDigestSHA256) {
		return newErr(CodeInvalidSnapshot, "proof obligations present but semantic digest is not a 64-hex sha256")
	}
	return nil
}

type subjectTriples map[string][]graphsnapshot.Triple

func indexBySubject(triples []graphsnapshot.Triple) subjectTriples {
	by := subjectTriples{}
	for _, t := range triples {
		by[t.Subject] = append(by[t.Subject], t)
	}
	return by
}

// buildManifest builds one category manifest for a snapshot.
func buildManifest(cat category, snap Snapshot, by subjectTriples) (CategoryManifest, []string, error) {
	switch cat.kind {
	case kindGraph:
		return buildGraphManifest(cat, by)
	case kindProofObligations:
		return buildProofObligationManifest(cat, snap)
	case kindCertificationPolicy:
		return buildPolicyManifest(cat, snap.CertificationPolicyID), nil, nil
	case kindCompletionPolicy:
		return buildPolicyManifest(cat, snap.CompletionPolicyID), nil, nil
	}
	return CategoryManifest{}, nil, newErr(CodeInvalidReport, "unknown category kind for %q", cat.name)
}

func buildGraphManifest(cat category, by subjectTriples) (CategoryManifest, []string, error) {
	records := map[string]RecordIdentity{}
	for subject, triples := range by {
		full := false
		for _, t := range triples {
			if t.Predicate == rdf.PropType && cat.classIRIs[t.Object] {
				full = true
				break
			}
		}
		if full {
			records[subject] = RecordIdentity{ID: subject, SemanticDigestSHA256: recordDigest(subject, triples)}
			continue
		}
		if len(cat.ownedPredicates) == 0 {
			continue
		}
		var owned []graphsnapshot.Triple
		for _, t := range triples {
			if cat.ownedPredicates[t.Predicate] {
				owned = append(owned, t)
			}
		}
		if len(owned) == 0 {
			continue
		}
		if strings.TrimSpace(subject) == "" {
			return CategoryManifest{}, nil, newErr(CodeUnattributableRelation, "category %q has an owned relation with no owning subject", cat.name)
		}
		records[subject] = RecordIdentity{ID: subject, SemanticDigestSHA256: recordDigest(subject, owned)}
	}
	return finalizeManifest(cat.name, records), nil, nil
}

func buildProofObligationManifest(cat category, snap Snapshot) (CategoryManifest, []string, error) {
	records := map[string]RecordIdentity{}
	var limits []string
	if len(snap.ProofObligationsBytes) > 0 {
		doc, err := proofrequirements.ParseObligations(snap.ProofObligationsBytes)
		if err != nil {
			return CategoryManifest{}, nil, newErr(CodeInvalidSnapshot, "parse proof obligations: %v", err)
		}
		for _, o := range doc.ProofObligations {
			id := strings.TrimSpace(o.ID)
			if id == "" {
				return CategoryManifest{}, nil, newErr(CodeInvalidSnapshot, "proof obligation without an id")
			}
			if _, dup := records[id]; dup {
				return CategoryManifest{}, nil, newErr(CodeDuplicateRecord, "duplicate proof obligation id %q", id)
			}
			records[id] = RecordIdentity{ID: id, SemanticDigestSHA256: obligationDigest(o)}
		}
	}
	return finalizeManifest(cat.name, records), limits, nil
}

func buildPolicyManifest(cat category, policyID string) CategoryManifest {
	records := map[string]RecordIdentity{}
	if id := strings.TrimSpace(policyID); id != "" {
		// A closed, immutable, versioned policy ID is the record identity: the
		// registry treats the ID itself as the definition digest source.
		records[id] = RecordIdentity{ID: id, SemanticDigestSHA256: mustDigest(struct {
			Category string `json:"category"`
			PolicyID string `json:"policy_id"`
		}{cat.name, id})}
	}
	return finalizeManifest(cat.name, records)
}

// finalizeManifest sorts records by id, rejects duplicate ids, and recomputes the
// manifest digest over the normalized category and its record identities.
func finalizeManifest(name string, records map[string]RecordIdentity) CategoryManifest {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	m := CategoryManifest{Category: name}
	for _, id := range ids {
		m.RecordIdentity = append(m.RecordIdentity, records[id])
	}
	m.DigestSHA256 = manifestDigest(m)
	return m
}

func impactFor(name string, base, result CategoryManifest) closureprotocol.GovernedKnowledgeImpact {
	baseByID := map[string]string{}
	for _, r := range base.RecordIdentity {
		baseByID[r.ID] = r.SemanticDigestSHA256
	}
	resultByID := map[string]string{}
	for _, r := range result.RecordIdentity {
		resultByID[r.ID] = r.SemanticDigestSHA256
	}
	changed := map[string]bool{}
	for id, bd := range baseByID {
		rd, ok := resultByID[id]
		if !ok || rd != bd {
			changed[id] = true // removed or modified
		}
	}
	for id := range resultByID {
		if _, ok := baseByID[id]; !ok {
			changed[id] = true // added
		}
	}
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return closureprotocol.GovernedKnowledgeImpact{
		Category:                   name,
		BaseManifestDigestSHA256:   base.DigestSHA256,
		ResultManifestDigestSHA256: result.DigestSHA256,
		ChangedRecordIDs:           ids,
	}
}

// --- digests ---

func recordDigest(subject string, triples []graphsnapshot.Triple) string {
	type edge struct {
		P     string `json:"p"`
		O     string `json:"o"`
		IsIRI bool   `json:"iri"`
	}
	edges := make([]edge, 0, len(triples))
	for _, t := range triples {
		edges = append(edges, edge{t.Predicate, t.Object, t.ObjectIsIRI})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].P != edges[j].P {
			return edges[i].P < edges[j].P
		}
		if edges[i].O != edges[j].O {
			return edges[i].O < edges[j].O
		}
		return !edges[i].IsIRI && edges[j].IsIRI
	})
	return mustDigest(struct {
		Subject string `json:"subject"`
		Edges   []edge `json:"edges"`
	}{subject, edges})
}

func obligationDigest(o proofrequirements.Obligation) string {
	slots := make([][2]string, 0, len(o.RequiredSlots))
	for _, s := range o.RequiredSlots {
		slots = append(slots, [2]string{s.ID, s.Kind})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i][0] < slots[j][0] })
	surfaces := append([]string(nil), o.AppliesToAuthoritySurfaces...)
	sort.Strings(surfaces)
	return mustDigest(struct {
		ID           string      `json:"id"`
		Label        string      `json:"label"`
		EvidenceLane string      `json:"evidence_lane"`
		TemplateKind string      `json:"template_kind"`
		Slots        [][2]string `json:"slots"`
		Surfaces     []string    `json:"surfaces"`
		Notes        string      `json:"notes"`
	}{o.ID, o.Label, o.EvidenceLane, o.TemplateKind, slots, surfaces, o.Notes})
}

func manifestDigest(m CategoryManifest) string {
	return mustDigest(struct {
		Category string           `json:"category"`
		Records  []RecordIdentity `json:"records"`
	}{m.Category, m.RecordIdentity})
}

func mustDigest(v any) string { return closureprotocol.MustSemanticDigest(v) }

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
