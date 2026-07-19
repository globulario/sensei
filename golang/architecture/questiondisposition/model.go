// SPDX-License-Identifier: Apache-2.0

// Package questiondisposition is the Phase-8.1a owner of the authoritative
// task-ledger question-disposition transaction. It records what an authorized
// architect decided about one Phase-7 architect question on one exact result, as
// the frozen question_disposition_recorded ledger event. It is the sole authority
// for a task question outcome; a dialogue answer or a raw ledger payload is input
// evidence, never the authoritative outcome.
//
// Hard boundary (governed by closure.* Phase-8 invariants): this package NEVER
// promotes reusable truth, mutates governed sources, rebuilds the graph, writes
// briefing projections, certifies, completes, or sets CorrectnessCertified. Answer
// authority is verified here; promotion authority is a separate later operation.
package questiondisposition

import (
	"errors"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// SchemaVersion is the receipt schema id.
const SchemaVersion = "questiondisposition.receipt/v1"

// GeneratedBy is the default producer id.
const GeneratedBy = "sensei.questiondisposition/v1"

// Disposition is the closed disposition set. A caller may not supply a
// resolved/open boolean, governance verdict, phase, or correctness truth.
type Disposition string

const (
	DispositionAnswered  Disposition = "answered"
	DispositionDismissed Disposition = "dismissed"
	DispositionDeferred  Disposition = "deferred"
	DispositionTaskLocal Disposition = "task_local"
)

var dispositions = map[Disposition]bool{
	DispositionAnswered: true, DispositionDismissed: true,
	DispositionDeferred: true, DispositionTaskLocal: true,
}

// Reusability classifies an answered disposition's effective reach. It never
// establishes reusable truth by itself — a reusable_candidate only records that a
// separate governed-promotion owner (Slice 8.1b) must promote it.
type Reusability string

const (
	ReusabilityNone              Reusability = ""
	ReusabilityReusableCandidate Reusability = "reusable_candidate"
	ReusabilityTaskLocal         Reusability = "task_local"
)

// QuestionDispositionReceipt is the authoritative task outcome for one architect
// question on one exact result. Every reference is content-addressed; the owner
// recomputes each digest from one verified snapshot before trusting it. The
// self-excluding ReceiptDigestSHA256 is computed with that field blank.
type QuestionDispositionReceipt struct {
	SchemaVersion string                      `json:"schema_version" yaml:"schema_version"`
	Task          closureprotocol.TaskBinding `json:"task" yaml:"task"`

	// Exact result identity the disposition binds.
	ResultBindingDigestSHA256           string `json:"result_binding_digest_sha256" yaml:"result_binding_digest_sha256"`
	ResultTransitionReceiptDigestSHA256 string `json:"result_transition_receipt_digest_sha256" yaml:"result_transition_receipt_digest_sha256"`

	// The exact Phase-7 architect_questions bundle and the question within it.
	ArchitectQuestionsBundleDigestSHA256 string   `json:"architect_questions_bundle_digest_sha256" yaml:"architect_questions_bundle_digest_sha256"`
	QuestionID                           string   `json:"question_id" yaml:"question_id"`
	BlocksClosureDimension               string   `json:"blocks_closure_dimension,omitempty" yaml:"blocks_closure_dimension,omitempty"`
	BlocksClosureBlockers                []string `json:"blocks_closure_blockers,omitempty" yaml:"blocks_closure_blockers,omitempty"`
	BlocksClaims                         []string `json:"blocks_claims,omitempty" yaml:"blocks_claims,omitempty"`

	Disposition Disposition `json:"disposition" yaml:"disposition"`
	Reusability Reusability `json:"reusability,omitempty" yaml:"reusability,omitempty"`
	Rationale   string      `json:"rationale" yaml:"rationale"`

	// Answer identity + canonical bytes digest (answered only).
	AnswerID                string `json:"answer_id,omitempty" yaml:"answer_id,omitempty"`
	AnswerBytesDigestSHA256 string `json:"answer_bytes_digest_sha256,omitempty" yaml:"answer_bytes_digest_sha256,omitempty"`

	// Answering actor + the exact authority that authorized this disposition.
	AnsweringActorBindingDigestSHA256 string `json:"answering_actor_binding_digest_sha256" yaml:"answering_actor_binding_digest_sha256"`
	AuthorityGrantID                  string `json:"authority_grant_id" yaml:"authority_grant_id"`
	AuthorityRoleID                   string `json:"authority_role_id" yaml:"authority_role_id"`

	// Effective scope this disposition applies to (never broader than the task's).
	EffectiveScopeDomain string   `json:"effective_scope_domain,omitempty" yaml:"effective_scope_domain,omitempty"`
	EffectiveScopeFiles  []string `json:"effective_scope_files,omitempty" yaml:"effective_scope_files,omitempty"`

	EvidenceRefs []string `json:"evidence_refs,omitempty" yaml:"evidence_refs,omitempty"`

	Producer   string `json:"producer" yaml:"producer"`
	DisposedAt string `json:"disposed_at" yaml:"disposed_at"`

	ReceiptDigestSHA256 string `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
}

// Digest returns the self-excluding SHA-256 of the receipt (ReceiptDigestSHA256
// blank), so a receipt can carry its own identity without a circular digest.
func Digest(in QuestionDispositionReceipt) (string, error) {
	in.ReceiptDigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// Validate checks the frozen shape: closed disposition, per-disposition rules,
// required bindings present, and a canonical timestamp. It does NOT verify the
// digests against the ledger — the owner recomputes those from the verified
// snapshot (Prepare).
func Validate(in QuestionDispositionReceipt) error {
	if strings.TrimSpace(in.SchemaVersion) != SchemaVersion {
		return errors.New("question disposition: unexpected schema version")
	}
	if strings.TrimSpace(in.Task.ID) == "" || strings.TrimSpace(in.Task.SessionID) == "" {
		return errors.New("question disposition: task and session id are required")
	}
	if !isHex64(in.ResultBindingDigestSHA256) || !isHex64(in.ResultTransitionReceiptDigestSHA256) ||
		!isHex64(in.ArchitectQuestionsBundleDigestSHA256) {
		return errors.New("question disposition: result/transition/bundle digests must be 64-hex")
	}
	if strings.TrimSpace(in.QuestionID) == "" {
		return errors.New("question disposition: question id is required")
	}
	if !dispositions[in.Disposition] {
		return errors.New("question disposition: invalid disposition")
	}
	if strings.TrimSpace(in.Rationale) == "" {
		return errors.New("question disposition: rationale is required")
	}
	if !isHex64(in.AnsweringActorBindingDigestSHA256) {
		return errors.New("question disposition: answering actor binding digest must be 64-hex")
	}
	if strings.TrimSpace(in.AuthorityGrantID) == "" || strings.TrimSpace(in.AuthorityRoleID) == "" {
		return errors.New("question disposition: authority grant and role are required")
	}
	switch in.Disposition {
	case DispositionAnswered:
		if in.Reusability != ReusabilityReusableCandidate && in.Reusability != ReusabilityTaskLocal {
			return errors.New("answered disposition requires reusability reusable_candidate or task_local")
		}
		if strings.TrimSpace(in.AnswerID) == "" || !isHex64(in.AnswerBytesDigestSHA256) {
			return errors.New("answered disposition requires answer id and canonical answer bytes digest")
		}
	case DispositionTaskLocal:
		// A task_local disposition may resolve the exact task question but never
		// claims reusable truth.
		if in.Reusability == ReusabilityReusableCandidate {
			return errors.New("task_local disposition may not be reusable_candidate")
		}
	case DispositionDismissed, DispositionDeferred:
		if in.Reusability == ReusabilityReusableCandidate {
			return errors.New("dismissed/deferred disposition may not be reusable_candidate")
		}
		if in.AnswerID != "" || in.AnswerBytesDigestSHA256 != "" {
			return errors.New("dismissed/deferred disposition carries no answer")
		}
	}
	if strings.TrimSpace(in.Producer) == "" {
		return errors.New("question disposition: producer is required")
	}
	if _, err := time.Parse(time.RFC3339, in.DisposedAt); err != nil {
		return errors.New("question disposition: disposed_at must be RFC3339")
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
