// SPDX-License-Identifier: Apache-2.0

// Package questionpromotion defines the Phase-8.1b authoritative promotion
// artifact: the content-addressed QuestionPromotionReceipt that binds the full
// lineage of turning an accepted reusable-candidate question disposition into a
// governed record and a verified repository-graph node, plus the minimal
// provenance vocabulary needed to query that lineage.
//
// This sub-slice (8.1b-3) defines the receipt, its strict validator, its
// self-excluding digest, and the provenance triples. It performs NO source
// mutation, graph persistence, journal write, CLI call, certification, or
// completion — and structural validity alone never makes a receipt authoritative;
// authority begins only when the later promotion transaction (8.1b-4) records the
// promotion-local commit state.
package questionpromotion

import (
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/rdf"
)

// SchemaVersion is the receipt schema id.
const SchemaVersion = "questionpromotion.receipt/v1"

// GeneratedBy is the default producer id.
const GeneratedBy = "sensei.questionpromotion/v1"

// governedKindClass maps a governed-source kind to its RDF node class, so the
// governed node IRI corresponds deterministically to the record kind + id.
var governedKindClass = map[string]string{
	"invariant":     rdf.ClassInvariant,
	"failure_mode":  rdf.ClassFailureMode,
	"forbidden_fix": rdf.ClassForbiddenFix,
	"required_test": rdf.ClassTest,
	"decision":      rdf.ClassDecision,
}

// QuestionPromotionReceipt binds the complete promotion lineage. Every digest is
// recomputed by the owner; none of the trusted values is caller-supplied. The
// self-excluding ReceiptDigestSHA256 is its content address; CommittedCausalIdentitySHA256
// is reserved for the 8.1b-4 commit and is likewise excluded from the digest so
// setting it later does not change the receipt's identity.
type QuestionPromotionReceipt struct {
	SchemaVersion string                      `json:"schema_version" yaml:"schema_version"`
	Task          closureprotocol.TaskBinding `json:"task" yaml:"task"`

	// Exact result identity.
	ResultBindingDigestSHA256           string `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	ResultTransitionReceiptDigestSHA256 string `json:"result_transition_receipt_digest_sha256" yaml:"result_transition_receipt_digest_sha256"`

	// The exact disposition being promoted.
	DispositionEntryDigestSHA256           string `json:"disposition_entry_digest_sha256" yaml:"disposition_entry_digest_sha256"`
	QuestionDispositionReceiptDigestSHA256 string `json:"question_disposition_receipt_digest_sha256" yaml:"question_disposition_receipt_digest_sha256"`

	// Question + answer.
	QuestionID              string `json:"question_id" yaml:"question_id"`
	AnswerID                string `json:"answer_id" yaml:"answer_id"`
	AnswerBytesDigestSHA256 string `json:"answer_bytes_digest_sha256" yaml:"answer_bytes_digest_sha256"`

	// Disposition actor + authority (who answered).
	DispositionActorBindingDigestSHA256 string `json:"disposition_actor_binding_digest_sha256" yaml:"disposition_actor_binding_digest_sha256"`
	DispositionAuthorityGrantID         string `json:"disposition_authority_grant_id" yaml:"disposition_authority_grant_id"`
	DispositionAuthorityRoleID          string `json:"disposition_authority_role_id" yaml:"disposition_authority_role_id"`

	// Promotion actor + authority (who promoted) — a SEPARATE binding, even when
	// the same enrolled identity performed both actions.
	PromotionActorBindingDigestSHA256 string `json:"promotion_actor_binding_digest_sha256" yaml:"promotion_actor_binding_digest_sha256"`
	PromotionAuthorityGrantID         string `json:"promotion_authority_grant_id" yaml:"promotion_authority_grant_id"`
	PromotionAuthorityRoleID          string `json:"promotion_authority_role_id" yaml:"promotion_authority_role_id"`

	// Governed target.
	GovernedTargetKind   string   `json:"governed_target_kind" yaml:"governed_target_kind"`
	CanonicalRecordID    string   `json:"canonical_record_id" yaml:"canonical_record_id"`
	SourceDocument       string   `json:"source_document" yaml:"source_document"`
	TopLevelKey          string   `json:"top_level_key" yaml:"top_level_key"`
	EffectiveScopeDomain string   `json:"effective_scope_domain,omitempty" yaml:"effective_scope_domain,omitempty"`
	EffectiveScopeFiles  []string `json:"effective_scope_files,omitempty" yaml:"effective_scope_files,omitempty"`
	GovernedNodeIRI      string   `json:"governed_node_iri" yaml:"governed_node_iri"`

	// Mutation + governed manifests (pre/post worlds explicit).
	CanonicalMutationDigestSHA256    string `json:"canonical_mutation_digest_sha256" yaml:"canonical_mutation_digest_sha256"`
	PreMutationManifestDigestSHA256  string `json:"pre_mutation_manifest_digest_sha256" yaml:"pre_mutation_manifest_digest_sha256"`
	PostMutationManifestDigestSHA256 string `json:"post_mutation_manifest_digest_sha256" yaml:"post_mutation_manifest_digest_sha256"`

	// Verified repository-graph projection identities (repograph.VerifiedProjection).
	GraphBuildInputDigestSHA256    string `json:"graph_build_input_digest_sha256" yaml:"graph_build_input_digest_sha256"`
	PersistedGraphByteDigestSHA256 string `json:"persisted_graph_byte_digest_sha256" yaml:"persisted_graph_byte_digest_sha256"`
	GraphSemanticDigestSHA256      string `json:"graph_semantic_digest_sha256" yaml:"graph_semantic_digest_sha256"`
	MarkerDigestSHA256             string `json:"marker_digest_sha256" yaml:"marker_digest_sha256"`
	MarkerIRI                      string `json:"marker_iri" yaml:"marker_iri"`
	ProjectionProducerID           string `json:"projection_producer_id" yaml:"projection_producer_id"`

	// PromotionLineageID is the PRE-GRAPH stable identity of this promotion,
	// derived deterministically from the frozen prepared world (ComputeLineageID).
	// It — NOT the receipt digest — is the RDF receipt node identity, so the
	// verified graph never embeds the final receipt digest that binds that same
	// graph (no cryptographic fixed point). It doubles as the attempt/replay
	// identity. The committed causal identity is the commit/recovery identity,
	// reserved for the 8.1b-4 commit and excluded from the receipt digest.
	PromotionLineageID            string `json:"promotion_lineage_id" yaml:"promotion_lineage_id"`
	CommittedCausalIdentitySHA256 string `json:"committed_causal_identity_sha256,omitempty" yaml:"committed_causal_identity_sha256,omitempty"`

	// The combined embedded seed is a separate cross-repository obligation; the
	// receipt affirms it remains OUTSTANDING and can never claim it converged.
	CombinedSeedObligationOutstanding bool `json:"combined_seed_obligation_outstanding" yaml:"combined_seed_obligation_outstanding"`

	Producer   string `json:"producer" yaml:"producer"`
	PromotedAt string `json:"promoted_at" yaml:"promoted_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

// Digest returns the self-excluding SHA-256 content address that binds the
// verified graph identities. Both the digest field and the reserved
// committed-causal-identity field are blanked, so the receipt's identity is
// stable before and after the 8.1b-4 commit stamps it. PromotionLineageID stays
// in the digest — it is a pre-graph input, not derived from the graph.
func Digest(in QuestionPromotionReceipt) (string, error) {
	in.ReceiptDigestSHA256 = ""
	in.CommittedCausalIdentitySHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// ComputeLineageID derives the pre-graph promotion lineage identity from the
// frozen prepared world only — it excludes every value produced by the mutation,
// the graph build, the commit, and the stamp. Because it depends on no graph
// identity, the lineage-addressed RDF receipt node can be emitted into the very
// graph the receipt later binds, with no fixed point.
func ComputeLineageID(in QuestionPromotionReceipt) (string, error) {
	in.PromotionLineageID = ""
	in.ReceiptDigestSHA256 = ""
	in.CommittedCausalIdentitySHA256 = ""
	// Values produced during/after mutation, graph build, or commit are excluded.
	in.PostMutationManifestDigestSHA256 = ""
	in.GraphBuildInputDigestSHA256 = ""
	in.PersistedGraphByteDigestSHA256 = ""
	in.GraphSemanticDigestSHA256 = ""
	in.MarkerDigestSHA256 = ""
	in.MarkerIRI = ""
	in.ProjectionProducerID = ""
	in.PromotedAt = ""
	return closureprotocol.SemanticDigest(in)
}

// GovernedNodeIRIFor returns the deterministic bare governed-node IRI for a kind
// + canonical record id, or "" for an unknown kind.
func GovernedNodeIRIFor(kind, canonicalID string) string {
	class, ok := governedKindClass[strings.TrimSpace(kind)]
	if !ok {
		return ""
	}
	return bareIRI(rdf.MintIRI(class, canonicalID))
}

// Validate fails closed unless every required identifier/digest is present and
// canonical, the lineage is internally consistent, the disposition and promotion
// authorities are distinct, the governed node corresponds to the record kind/id,
// and the combined-seed obligation remains outstanding. Structural validity does
// NOT confer authority — that is the 8.1b-4 commit's role.
func Validate(in QuestionPromotionReceipt) error {
	if strings.TrimSpace(in.SchemaVersion) != SchemaVersion {
		return errors.New("question promotion: unexpected schema version")
	}
	if strings.TrimSpace(in.Task.ID) == "" || strings.TrimSpace(in.Task.SessionID) == "" {
		return errors.New("question promotion: task and session id are required")
	}
	for name, d := range map[string]string{
		"result_binding_digest":               in.ResultBindingDigestSHA256,
		"result_transition_receipt_digest":    in.ResultTransitionReceiptDigestSHA256,
		"disposition_entry_digest":            in.DispositionEntryDigestSHA256,
		"question_disposition_receipt_digest": in.QuestionDispositionReceiptDigestSHA256,
		"answer_bytes_digest":                 in.AnswerBytesDigestSHA256,
		"disposition_actor_binding_digest":    in.DispositionActorBindingDigestSHA256,
		"promotion_actor_binding_digest":      in.PromotionActorBindingDigestSHA256,
		"canonical_mutation_digest":           in.CanonicalMutationDigestSHA256,
		"pre_mutation_manifest_digest":        in.PreMutationManifestDigestSHA256,
		"post_mutation_manifest_digest":       in.PostMutationManifestDigestSHA256,
		"graph_build_input_digest":            in.GraphBuildInputDigestSHA256,
		"persisted_graph_byte_digest":         in.PersistedGraphByteDigestSHA256,
		"graph_semantic_digest":               in.GraphSemanticDigestSHA256,
		"marker_digest":                       in.MarkerDigestSHA256,
		"promotion_lineage_id":                in.PromotionLineageID,
	} {
		if !isHex64(d) {
			return errors.New("question promotion: " + name + " must be 64-hex")
		}
	}
	for name, v := range map[string]string{
		"question_id":                    in.QuestionID,
		"answer_id":                      in.AnswerID,
		"disposition_authority_grant_id": in.DispositionAuthorityGrantID,
		"disposition_authority_role_id":  in.DispositionAuthorityRoleID,
		"promotion_authority_grant_id":   in.PromotionAuthorityGrantID,
		"promotion_authority_role_id":    in.PromotionAuthorityRoleID,
		"governed_target_kind":           in.GovernedTargetKind,
		"canonical_record_id":            in.CanonicalRecordID,
		"source_document":                in.SourceDocument,
		"top_level_key":                  in.TopLevelKey,
		"governed_node_iri":              in.GovernedNodeIRI,
		"marker_iri":                     in.MarkerIRI,
		"projection_producer_id":         in.ProjectionProducerID,
		"producer":                       in.Producer,
	} {
		if strings.TrimSpace(v) == "" {
			return errors.New("question promotion: " + name + " is required")
		}
	}

	// Disposition and promotion authorities are distinct bindings — they can never
	// be collapsed, even when the same enrolled identity performed both actions.
	if in.DispositionAuthorityGrantID == in.PromotionAuthorityGrantID {
		return errors.New("question promotion: disposition and promotion authority grants must be distinct")
	}

	// The governed node IRI must correspond deterministically to the record kind+id.
	want := GovernedNodeIRIFor(in.GovernedTargetKind, in.CanonicalRecordID)
	if want == "" {
		return errors.New("question promotion: unknown governed target kind " + in.GovernedTargetKind)
	}
	if in.GovernedNodeIRI != want {
		return errors.New("question promotion: governed node IRI does not correspond to the record kind/id")
	}

	// The combined embedded seed can never be claimed converged.
	if !in.CombinedSeedObligationOutstanding {
		return errors.New("question promotion: combined-seed obligation must remain outstanding")
	}

	if in.CommittedCausalIdentitySHA256 != "" && !isHex64(in.CommittedCausalIdentitySHA256) {
		return errors.New("question promotion: committed causal identity must be 64-hex when set")
	}
	if _, err := time.Parse(time.RFC3339, in.PromotedAt); err != nil {
		return errors.New("question promotion: promoted_at must be RFC3339")
	}

	// The pre-graph lineage id must be the deterministic derivation of the frozen
	// prepared world — a stale or forged lineage id (which drives the RDF node
	// IRI) fails closed.
	wantLineage, err := ComputeLineageID(in)
	if err != nil {
		return err
	}
	if in.PromotionLineageID != wantLineage {
		return errors.New("question promotion: promotion_lineage_id does not match the frozen prepared world")
	}

	// When the content address is populated it must recompute — an arbitrary or
	// stale receipt digest can never validate structurally.
	if in.ReceiptDigestSHA256 != "" {
		wantDigest, derr := Digest(in)
		if derr != nil {
			return derr
		}
		if in.ReceiptDigestSHA256 != wantDigest {
			return errors.New("question promotion: receipt_digest_sha256 does not recompute")
		}
	}
	return nil
}

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

func bareIRI(s string) string {
	return strings.TrimSuffix(strings.TrimPrefix(s, "<"), ">")
}
