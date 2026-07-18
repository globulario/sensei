// SPDX-License-Identifier: Apache-2.0

// Package questionresolution is the Phase-8.1d owner of two bounded, isolated
// concerns over a task's architect questions:
//
//  1. A deterministic, read-only QuestionResolutionSummary that projects how each
//     question was resolved — reusing the question-disposition owner and the
//     promotion verification boundary as the SOLE authorities. Building the summary
//     mutates nothing (no governed source, graph, ledger, promotion, or disposition
//     truth).
//
//  2. A bounded question-resolution certification gate. It is a CLEARLY ISOLATED
//     owner path, distinct from question disposition, governed promotion, Phase-6
//     correctness certification, and completion. It attests ONLY that every binding
//     architect question has an admissible terminal disposition and that every
//     reusable answer claimed as governed truth has a valid committed promotion. It
//     NEVER asserts overall correctness, architecture closure, merge safety, or
//     terminal completion, and it never sets CorrectnessCertified (Phase 6 remains
//     that value's sole non-writer of record) or emits the Phase-6 certified event.
//
// The certificate is content-addressed by a self-excluding digest over the exact
// durable evidence (task ledger head, per-question disposition receipts, satisfying
// promotion receipts, scopes, and the governed source manifest). Identical durable
// inputs produce a byte-identical certificate; changed evidence produces a different
// identity; a stale re-evaluation cannot succeed.
package questionresolution

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const (
	// SummarySchemaVersion identifies the read-only projection schema.
	SummarySchemaVersion = "questionresolution.summary/v1"
	// CertificateSchemaVersion identifies the bounded certificate schema.
	CertificateSchemaVersion = "questionresolution.certificate/v1"
	// GeneratedBy names the owner that produced a certificate.
	GeneratedBy = "sensei.questionresolution/v1"

	// CertificationsRelDir holds content-addressed bounded certificates, keyed by
	// the certificate's self-excluding digest. It is a repository-scoped store,
	// distinct from the promotion store and from the task ledger.
	CertificationsRelDir = ".sensei/project/question-resolution-certifications"

	// Isolated authority triple (declared in docs/awareness/*.yaml). Distinct from
	// the disposition, promotion, and correctness-certification authorities so the
	// resolver can never cross-authorize certification with another owner's grant.
	DomainCertification   = "authority.sensei_question_resolution_certification"
	GrantCertification    = "grant.sensei.question_resolution_certification"
	MechanismPathCert     = "mutation_path.question_resolution_certification"
	TargetKindCertificate = "question_resolution_certificate"

	certOperationID = "op.certify.question_resolution"
	// certRiskClass is the operation's risk; it must not exceed the grant's
	// maximum_risk_class (architecture_sensitive).
	certRiskClass = "architecture_sensitive"
)

// QuestionState is the resolved state of one architect question, kept as a typed
// closed set so status is never collapsed into prose.
type QuestionState string

const (
	// StateUnresolved: no disposition recorded — the question is still open.
	StateUnresolved QuestionState = "unresolved"
	// StateDeferred: a deferred disposition — non-terminal, awaits a later ruling.
	StateDeferred QuestionState = "deferred"
	// StateContested: two or more distinct dispositions — awaits adjudication.
	StateContested QuestionState = "contested"
	// StateAnsweredTaskLocal: answered/ruled for THIS task only; never governed
	// repository truth and never requiring a promotion.
	StateAnsweredTaskLocal QuestionState = "answered_task_local"
	// StateDismissed: dismissed with a durable rationale — terminal, no pending step.
	StateDismissed QuestionState = "dismissed"
	// StateReusableUnpromoted: a reusable-candidate answer claimed as governed truth
	// with NO valid committed promotion — an incomplete obligation (a blocker).
	StateReusableUnpromoted QuestionState = "reusable_candidate_unpromoted"
	// StateReusablePromoted: a reusable-candidate answer backed by a valid,
	// independently re-proven committed promotion binding this exact disposition.
	StateReusablePromoted QuestionState = "reusable_promoted"
)

// QuestionResolution is the per-question projection. It preserves exact identifiers
// and provenance rather than a prose status.
type QuestionResolution struct {
	QuestionID                     string        `json:"question_id" yaml:"question_id"`
	ArchitectRequired              bool          `json:"architect_required" yaml:"architect_required"`
	BlocksClosureDimension         string        `json:"blocks_closure_dimension,omitempty" yaml:"blocks_closure_dimension,omitempty"`
	ScopeDomain                    string        `json:"scope_domain,omitempty" yaml:"scope_domain,omitempty"`
	ScopeFiles                     []string      `json:"scope_files,omitempty" yaml:"scope_files,omitempty"`
	State                          QuestionState `json:"state" yaml:"state"`
	Disposition                    string        `json:"disposition,omitempty" yaml:"disposition,omitempty"`
	Reusability                    string        `json:"reusability,omitempty" yaml:"reusability,omitempty"`
	DispositionReceiptDigestSHA256 string        `json:"disposition_receipt_digest_sha256,omitempty" yaml:"disposition_receipt_digest_sha256,omitempty"`
	PromotionLineageID             string        `json:"promotion_lineage_id,omitempty" yaml:"promotion_lineage_id,omitempty"`
	PromotionReceiptDigestSHA256   string        `json:"promotion_receipt_digest_sha256,omitempty" yaml:"promotion_receipt_digest_sha256,omitempty"`
	GovernedNodeIRI                string        `json:"governed_node_iri,omitempty" yaml:"governed_node_iri,omitempty"`
}

// Summary is the deterministic, read-only projection of a task's question
// resolution. It is an observation, not authority.
type Summary struct {
	SchemaVersion              string                      `json:"schema_version" yaml:"schema_version"`
	Task                       closureprotocol.TaskBinding `json:"task" yaml:"task"`
	TaskLedgerHeadDigestSHA256 string                      `json:"task_ledger_head_digest_sha256" yaml:"task_ledger_head_digest_sha256"`
	Questions                  []QuestionResolution        `json:"questions" yaml:"questions"`
	// IntegrityFindings are typed exclusions: a discovered promotion that failed
	// independent re-verification (tampered, incomplete, superseded, or stale). Any
	// finding fails the bounded gate closed; none is silently omitted.
	IntegrityFindings []string `json:"integrity_findings,omitempty" yaml:"integrity_findings,omitempty"`
}

// QuestionEvidence is the frozen per-question evidence bound into a certificate.
type QuestionEvidence struct {
	QuestionID                     string        `json:"question_id" yaml:"question_id"`
	ArchitectRequired              bool          `json:"architect_required" yaml:"architect_required"`
	State                          QuestionState `json:"state" yaml:"state"`
	DispositionReceiptDigestSHA256 string        `json:"disposition_receipt_digest_sha256,omitempty" yaml:"disposition_receipt_digest_sha256,omitempty"`
}

// PromotionEvidence is the frozen promotion evidence that satisfied a reusable
// obligation, tied to the exact disposition it promoted.
type PromotionEvidence struct {
	PromotionLineageID             string `json:"promotion_lineage_id" yaml:"promotion_lineage_id"`
	ReceiptDigestSHA256            string `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
	DispositionReceiptDigestSHA256 string `json:"disposition_receipt_digest_sha256" yaml:"disposition_receipt_digest_sha256"`
	GovernedNodeIRI                string `json:"governed_node_iri" yaml:"governed_node_iri"`
}

// QuestionResolutionCertificate is the bounded attestation. Its DigestSHA256 is a
// self-excluding semantic digest over every other field, so it is a content address
// of the exact evidence world. It asserts ONLY the question-resolution obligation.
type QuestionResolutionCertificate struct {
	SchemaVersion                string                      `json:"schema_version" yaml:"schema_version"`
	Task                         closureprotocol.TaskBinding `json:"task" yaml:"task"`
	TaskLedgerHeadDigestSHA256   string                      `json:"task_ledger_head_digest_sha256" yaml:"task_ledger_head_digest_sha256"`
	QuestionEvidence             []QuestionEvidence          `json:"question_evidence" yaml:"question_evidence"`
	PromotionEvidence            []PromotionEvidence         `json:"promotion_evidence,omitempty" yaml:"promotion_evidence,omitempty"`
	GovernedManifestDigestSHA256 string                      `json:"governed_manifest_digest_sha256" yaml:"governed_manifest_digest_sha256"`
	AuthorityGrantID             string                      `json:"authority_grant_id" yaml:"authority_grant_id"`
	AuthorityRoleID              string                      `json:"authority_role_id" yaml:"authority_role_id"`
	Producer                     string                      `json:"producer" yaml:"producer"`
	CertifiedAt                  string                      `json:"certified_at" yaml:"certified_at"`
	// Bound is a fixed, self-describing statement of the certificate's limits. It is
	// part of the content so the attestation can never be re-read as more than it is.
	Bound        []string `json:"bound" yaml:"bound"`
	DigestSHA256 string   `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// boundStatement is the fixed limitation set embedded in every certificate.
func boundStatement() []string {
	return []string{
		"certifies only the bounded question-resolution obligation for this task/result world",
		"does not assert overall correctness, architecture closure, merge safety, or terminal completion",
		"does not set CorrectnessCertified and does not emit the Phase-6 certified event",
		"task-local answers satisfy only this task and are never repository-wide governed truth",
	}
}

// CertificateDigest is the self-excluding content address of a certificate.
func CertificateDigest(in QuestionResolutionCertificate) (string, error) {
	in.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// ValidateCertificate enforces the certificate schema and recomputes its digest.
func ValidateCertificate(c QuestionResolutionCertificate) error {
	if c.SchemaVersion != CertificateSchemaVersion {
		return fmt.Errorf("certificate schema_version = %q, want %q", c.SchemaVersion, CertificateSchemaVersion)
	}
	if c.Task.ID == "" || c.Task.SessionID == "" {
		return fmt.Errorf("certificate task id and session id are required")
	}
	if !isHex64(c.TaskLedgerHeadDigestSHA256) {
		return fmt.Errorf("certificate task_ledger_head_digest_sha256 must be 64-hex")
	}
	if !isHex64(c.GovernedManifestDigestSHA256) {
		return fmt.Errorf("certificate governed_manifest_digest_sha256 must be 64-hex")
	}
	if c.AuthorityGrantID != GrantCertification {
		return fmt.Errorf("certificate authority_grant_id = %q, want %q", c.AuthorityGrantID, GrantCertification)
	}
	if c.AuthorityRoleID == "" {
		return fmt.Errorf("certificate authority_role_id is required")
	}
	if c.Producer != GeneratedBy {
		return fmt.Errorf("certificate producer = %q, want %q", c.Producer, GeneratedBy)
	}
	if c.CertifiedAt == "" {
		return fmt.Errorf("certificate certified_at is required")
	}
	for _, q := range c.QuestionEvidence {
		if q.QuestionID == "" || q.State == "" {
			return fmt.Errorf("certificate question evidence requires id and state")
		}
	}
	for _, p := range c.PromotionEvidence {
		if p.PromotionLineageID == "" || !isHex64(p.ReceiptDigestSHA256) || !isHex64(p.DispositionReceiptDigestSHA256) {
			return fmt.Errorf("certificate promotion evidence requires lineage and 64-hex receipt/disposition digests")
		}
	}
	want, err := CertificateDigest(c)
	if err != nil {
		return err
	}
	if c.DigestSHA256 != "" && c.DigestSHA256 != want {
		return fmt.Errorf("certificate digest mismatch: stored %q recompute %q", c.DigestSHA256, want)
	}
	return nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
