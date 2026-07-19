// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const (
	// TerminalReceiptSchemaVersion identifies the terminal-completion receipt schema.
	TerminalReceiptSchemaVersion = "completion.terminal_receipt/v1"
	// CompletionPolicyID is the completion policy the receipt records.
	CompletionPolicyID = "completion.architectural_closure.v1"

	// Isolated completion authority triple (declared in docs/awareness/*.yaml).
	DomainTerminalCompletion = "authority.sensei_terminal_completion"
	GrantTerminalCompletion  = "grant.sensei.terminal_completion"
	MechanismPathCompletion  = "mutation_path.terminal_completion"
	TargetKindTaskCompletion = "task_completion"

	completionOperationID = "op.complete.task"
	completionRiskClass   = "architecture_sensitive"
	completionArtifactKey = "completion_receipt"
)

// TerminalCompletionReceipt is the durable evidence the sole completion mutation
// records. It embeds the frozen protocol completion fact and wraps it with the exact
// readiness conjunction that justified the transition. Its ReceiptDigestSHA256 is a
// self-excluding content address; its CausalIdentitySHA256 is a deterministic
// replay identity derived from the durable evidence and EXCLUDING the ledger head,
// so it is stable across the pre- and post-completion ledger states.
type TerminalCompletionReceipt struct {
	SchemaVersion                             string                            `json:"schema_version" yaml:"schema_version"`
	Completion                                closureprotocol.CompletionReceipt `json:"completion" yaml:"completion"`
	PreCompletionLedgerHeadDigestSHA256       string                            `json:"pre_completion_ledger_head_digest_sha256" yaml:"pre_completion_ledger_head_digest_sha256"`
	ReadinessAssessmentDigestSHA256           string                            `json:"readiness_assessment_digest_sha256" yaml:"readiness_assessment_digest_sha256"`
	Obligations                               []ObligationAssessment            `json:"obligations" yaml:"obligations"`
	CorrectnessReceiptDigestSHA256            string                            `json:"correctness_receipt_digest_sha256" yaml:"correctness_receipt_digest_sha256"`
	QuestionResolutionCertificateDigestSHA256 string                            `json:"question_resolution_certificate_digest_sha256" yaml:"question_resolution_certificate_digest_sha256"`
	GovernedManifestDigestSHA256              string                            `json:"governed_manifest_digest_sha256" yaml:"governed_manifest_digest_sha256"`
	AuthorityGrantID                          string                            `json:"authority_grant_id" yaml:"authority_grant_id"`
	AuthorityRoleID                           string                            `json:"authority_role_id" yaml:"authority_role_id"`
	CompletionActorBindingDigestSHA256        string                            `json:"completion_actor_binding_digest_sha256" yaml:"completion_actor_binding_digest_sha256"`
	OperationID                               string                            `json:"operation_id" yaml:"operation_id"`
	CausalIdentitySHA256                      string                            `json:"causal_identity_sha256" yaml:"causal_identity_sha256"`
	Bound                                     []string                          `json:"bound" yaml:"bound"`
	ReceiptDigestSHA256                       string                            `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
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

func completionBoundStatement() []string {
	return []string{
		"this receipt establishes terminal completion of THIS task only, not repository-wide perfection",
		"completion is authoritative only as the verified conjunction of this durable receipt and the matching completed ledger event",
		"it establishes neither correctness (Phase 6) nor question resolution (Phase 8.1d); it re-proves and binds their evidence",
		"it mutates no correctness, disposition, promotion, question-resolution, or governed-source truth",
	}
}

// causalIdentity is the deterministic replay identity. It EXCLUDES the ledger head
// (which changes when the completed event is appended) so an exact retry on the same
// evidence recomputes the same identity before and after commit.
func causalIdentity(task closureprotocol.TaskBinding, rb closureprotocol.ResultBinding, correctnessDigest, qrDigest, governedManifest, grantID, roleID string) string {
	h := sha256.New()
	for _, part := range []string{
		"completion.terminal/v1",
		task.ID, task.SessionID, task.IterationDigestSHA256,
		rb.BaseRevision, rb.PatchDigestSHA256, rb.ResultTreeDigestSHA256, rb.GraphDigestSHA256,
		correctnessDigest, qrDigest, governedManifest,
		grantID, roleID,
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TerminalReceiptDigest is the self-excluding content address of the receipt.
func TerminalReceiptDigest(in TerminalCompletionReceipt) (string, error) {
	in.ReceiptDigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// ValidateTerminalReceipt enforces the schema and recomputes the digest. It also
// validates the embedded protocol completion fact.
func ValidateTerminalReceipt(in TerminalCompletionReceipt) error {
	if in.SchemaVersion != TerminalReceiptSchemaVersion {
		return fmt.Errorf("terminal receipt schema_version = %q, want %q", in.SchemaVersion, TerminalReceiptSchemaVersion)
	}
	if err := closureprotocol.ValidateCompletionReceipt(in.Completion); err != nil {
		return fmt.Errorf("embedded completion receipt invalid: %w", err)
	}
	for _, d := range []struct {
		name, val string
	}{
		{"pre_completion_ledger_head", in.PreCompletionLedgerHeadDigestSHA256},
		{"readiness_assessment", in.ReadinessAssessmentDigestSHA256},
		{"correctness_receipt", in.CorrectnessReceiptDigestSHA256},
		{"question_resolution_certificate", in.QuestionResolutionCertificateDigestSHA256},
		{"governed_manifest", in.GovernedManifestDigestSHA256},
		{"causal_identity", in.CausalIdentitySHA256},
	} {
		if !isHex64(d.val) {
			return fmt.Errorf("terminal receipt %s digest must be 64-hex", d.name)
		}
	}
	if in.AuthorityGrantID != GrantTerminalCompletion {
		return fmt.Errorf("terminal receipt authority_grant_id = %q, want %q", in.AuthorityGrantID, GrantTerminalCompletion)
	}
	if in.AuthorityRoleID == "" || in.OperationID == "" {
		return fmt.Errorf("terminal receipt authority role and operation id are required")
	}
	if len(in.Obligations) == 0 {
		return fmt.Errorf("terminal receipt must bind the readiness obligations")
	}
	want, err := TerminalReceiptDigest(in)
	if err != nil {
		return err
	}
	if in.ReceiptDigestSHA256 != "" && in.ReceiptDigestSHA256 != want {
		return fmt.Errorf("terminal receipt digest mismatch: stored %q recompute %q", in.ReceiptDigestSHA256, want)
	}
	return nil
}
