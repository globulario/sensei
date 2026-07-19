// SPDX-License-Identifier: AGPL-3.0-only

// Package completion (Phase 8.2a) owns the READ-ONLY terminal-completion
// readiness evidence model and evaluator. It answers exactly one question:
//
//	What authoritative evidence would terminal completion require for this task,
//	and which obligations are currently satisfied, missing, stale, contradictory,
//	or invalid?
//
// It DEFINES the conjunction; it does not turn the final key. The evaluator is a
// deterministic, read-only projection: it re-proves each obligation from durable
// artifacts and their owners (the task ledger, the Phase-6 correctness
// certification, the Phase-8.1d question-resolution certificate, the governed
// source manifest) and never trusts a caller-supplied boolean, receipt path,
// briefing, rendered summary, or prose status. It performs NO task-ledger write,
// persists NO completion receipt, appends NO completed event, and mutates nothing.
//
// Terminal-completion evaluation is kept distinct from terminal-completion
// mutation/persistence (Phase 8.2b), from Phase-6 correctness certification, and
// from Phase-8.1d question-resolution certification. A ready assessment is NOT a
// completion: it only reports that the required evidence conjunction currently
// holds.
package completion

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const (
	// ReadinessSchemaVersion identifies the read-only assessment schema.
	ReadinessSchemaVersion = "completion.readiness/v1"
	// GeneratedBy names the evaluator that produced an assessment.
	GeneratedBy = "sensei.completion.readiness/v1"
)

// ObligationID is the closed set of evidence obligations terminal completion
// requires. Each maps to one authoritative input the evaluator re-proves.
type ObligationID string

const (
	// ObligationTaskLedgerIdentity: a verified task-ledger head and exact
	// task/result identity for the world being assessed.
	ObligationTaskLedgerIdentity ObligationID = "task_ledger_identity"
	// ObligationCorrectnessCertificate: a valid Phase-6 correctness certification
	// for this exact current result.
	ObligationCorrectnessCertificate ObligationID = "correctness_certification"
	// ObligationQuestionResolution: a valid Phase-8.1d question-resolution
	// certificate for this exact current task world.
	ObligationQuestionResolution ObligationID = "question_resolution_certification"
	// ObligationClosureAndProof: the closure dimensions and proof obligations owned
	// by existing packages are represented by valid evidence — subsumed by the
	// correctness certificate's proof lane and the question-resolution loop, never
	// re-enumerated here.
	ObligationClosureAndProof ObligationID = "closure_and_proof_obligations"
	// ObligationGovernedFreshness: the governed source world has not changed since
	// the evidence was produced.
	ObligationGovernedFreshness ObligationID = "governed_world_freshness"
	// ObligationNoConflictingCompletion: no already-conflicting or superseding
	// terminal-completion fact exists.
	ObligationNoConflictingCompletion ObligationID = "no_conflicting_completion"
)

// obligationOrder is the deterministic obligation ordering.
var obligationOrder = []ObligationID{
	ObligationTaskLedgerIdentity,
	ObligationCorrectnessCertificate,
	ObligationQuestionResolution,
	ObligationClosureAndProof,
	ObligationGovernedFreshness,
	ObligationNoConflictingCompletion,
}

// EvidenceState is the closed set of per-obligation states.
type EvidenceState string

const (
	// EvidenceSatisfied: the obligation is met by valid, current, in-scope evidence.
	EvidenceSatisfied EvidenceState = "satisfied"
	// EvidenceMissing: the required evidence does not exist.
	EvidenceMissing EvidenceState = "missing"
	// EvidenceStale: the evidence exists but binds an older ledger head or governed
	// world than the current one.
	EvidenceStale EvidenceState = "stale"
	// EvidenceIntegrityFailure: the evidence exists but fails re-verification
	// (tampered, malformed, or digest mismatch).
	EvidenceIntegrityFailure EvidenceState = "integrity_failure"
	// EvidenceContradictory: two or more candidates conflict, or a conflicting
	// terminal-completion fact exists — fail closed rather than pick one.
	EvidenceContradictory EvidenceState = "contradictory"
	// EvidenceWrongBinding: the evidence binds a different task or result.
	EvidenceWrongBinding EvidenceState = "wrong_task_or_result_binding"
	// EvidenceUnsupported: the evidence is present but does not establish the
	// obligation (e.g. a non-certifying verdict), or cannot be assessed because a
	// dependency is absent.
	EvidenceUnsupported EvidenceState = "unsupported"
)

// EvidenceRef records the provenance/digest of an accepted or rejected input, so
// no state is asserted without preserving what it was decided from.
type EvidenceRef struct {
	Kind         string `json:"kind" yaml:"kind"`
	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
	Detail       string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// ObligationAssessment is the typed per-obligation result with preserved evidence.
type ObligationAssessment struct {
	Obligation ObligationID  `json:"obligation" yaml:"obligation"`
	State      EvidenceState `json:"state" yaml:"state"`
	Detail     string        `json:"detail,omitempty" yaml:"detail,omitempty"`
	Evidence   []EvidenceRef `json:"evidence,omitempty" yaml:"evidence,omitempty"`
}

// Readiness is the aggregate verdict: ready only when every obligation is satisfied.
type Readiness string

const (
	ReadinessReady    Readiness = "ready"
	ReadinessNotReady Readiness = "not_ready"
)

// ReadinessAssessment is the deterministic, read-only projection. Its DigestSHA256
// is a self-excluding content address over the exact evidence world, so identical
// durable inputs yield a byte-identical assessment and changed evidence changes the
// identity. It asserts readiness only — never terminal completion.
type ReadinessAssessment struct {
	SchemaVersion                string                        `json:"schema_version" yaml:"schema_version"`
	Task                         closureprotocol.TaskBinding   `json:"task" yaml:"task"`
	ResultBinding                closureprotocol.ResultBinding `json:"result_binding" yaml:"result_binding"`
	TaskLedgerHeadDigestSHA256   string                        `json:"task_ledger_head_digest_sha256" yaml:"task_ledger_head_digest_sha256"`
	GovernedManifestDigestSHA256 string                        `json:"governed_manifest_digest_sha256" yaml:"governed_manifest_digest_sha256"`
	Obligations                  []ObligationAssessment        `json:"obligations" yaml:"obligations"`
	Readiness                    Readiness                     `json:"readiness" yaml:"readiness"`
	// Limitations are honest gaps in what durable artifacts can prove.
	Limitations []string `json:"limitations,omitempty" yaml:"limitations,omitempty"`
	// Bound is a fixed statement of the assessment's limits, part of the content so
	// it can never be re-read as a completion.
	Bound        []string `json:"bound" yaml:"bound"`
	DigestSHA256 string   `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

func boundStatement() []string {
	return []string{
		"a read-only terminal-completion READINESS assessment; it does not perform or authorize terminal completion",
		"it persists no completion receipt and appends no completed ledger event",
		"readiness=ready means the required evidence conjunction currently holds, not that the task is completed",
		"it re-proves and conjoins Phase-6 correctness and Phase-8.1d question-resolution evidence; it establishes neither itself",
	}
}

// AssessmentDigest is the self-excluding content address of an assessment.
func AssessmentDigest(in ReadinessAssessment) (string, error) {
	in.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// ValidateAssessment enforces the schema and recomputes the digest.
func ValidateAssessment(a ReadinessAssessment) error {
	if a.SchemaVersion != ReadinessSchemaVersion {
		return fmt.Errorf("assessment schema_version = %q, want %q", a.SchemaVersion, ReadinessSchemaVersion)
	}
	if a.Task.ID == "" || a.Task.SessionID == "" {
		return fmt.Errorf("assessment task id and session id are required")
	}
	if a.Readiness != ReadinessReady && a.Readiness != ReadinessNotReady {
		return fmt.Errorf("assessment readiness = %q is off-vocabulary", a.Readiness)
	}
	seen := map[ObligationID]bool{}
	for _, o := range a.Obligations {
		if o.Obligation == "" || o.State == "" {
			return fmt.Errorf("obligation requires id and state")
		}
		seen[o.Obligation] = true
	}
	for _, id := range obligationOrder {
		if !seen[id] {
			return fmt.Errorf("assessment is missing obligation %q", id)
		}
	}
	want, err := AssessmentDigest(a)
	if err != nil {
		return err
	}
	if a.DigestSHA256 != "" && a.DigestSHA256 != want {
		return fmt.Errorf("assessment digest mismatch: stored %q recompute %q", a.DigestSHA256, want)
	}
	return nil
}
