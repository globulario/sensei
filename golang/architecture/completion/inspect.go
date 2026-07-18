// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bindingpkg "github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

const inspectSchemaVersion = "completion.terminal_state/v1"

// TerminalState is the closed set of honest terminal-state classifications
// reconstructed from durable owners. None collapses into a generic failure.
type TerminalState string

const (
	// TerminalNotCompleted: no completed event and no receipt residue.
	TerminalNotCompleted TerminalState = "not_completed"
	// TerminalCommitted: exactly one completed event, its valid matching receipt,
	// zero revoked facts, bindings intact, and derived projections current.
	TerminalCommitted TerminalState = "committed"
	// TerminalReceiptWithoutEvent: an orphan terminal-receipt artifact with no
	// completed event — harmless content-addressed residue, NOT completion.
	TerminalReceiptWithoutEvent TerminalState = "receipt_without_event"
	// TerminalEventWithoutValidReceipt: a completed event whose receipt artifact is
	// absent or unreferenceable — broken completion, NOT completion.
	TerminalEventWithoutValidReceipt TerminalState = "event_without_valid_receipt"
	// TerminalContradictoryHistory: more than one completed fact, or any revoked
	// fact — never repairable by choosing one.
	TerminalContradictoryHistory TerminalState = "contradictory_terminal_history"
	// TerminalWrongBinding: a valid conjunction bound to a different task/result
	// than the current world.
	TerminalWrongBinding TerminalState = "wrong_task_or_result_binding"
	// TerminalIntegrityFailure: a completed event whose receipt is present but
	// tampered, unparseable, or mismatched against the event.
	TerminalIntegrityFailure TerminalState = "integrity_failure"
	// TerminalProjectionStaleOrMissing: a valid durable conjunction whose derived
	// projections are stale or missing — recoverable by rebuild.
	TerminalProjectionStaleOrMissing TerminalState = "projection_stale_or_missing"
	// TerminalUnsupported: the ledger could not be verified for reconstruction.
	TerminalUnsupported TerminalState = "unsupported"
)

// CommittedFacts is the reconstructed evidence of a valid durable conjunction. It
// is present only when the durable event/receipt conjunction verifies (states
// committed or projection_stale_or_missing).
type CommittedFacts struct {
	ReceiptDigestSHA256                  string                        `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
	CausalIdentitySHA256                 string                        `json:"causal_identity_sha256" yaml:"causal_identity_sha256"`
	RecordedResultBinding                closureprotocol.ResultBinding `json:"recorded_result_binding" yaml:"recorded_result_binding"`
	RecordedGovernedManifestDigestSHA256 string                        `json:"recorded_governed_manifest_digest_sha256" yaml:"recorded_governed_manifest_digest_sha256"`
	CurrentGovernedManifestDigestSHA256  string                        `json:"current_governed_manifest_digest_sha256" yaml:"current_governed_manifest_digest_sha256"`
	// GovernedDriftAfterCompletion reports (distinctly from corruption) that the
	// governed world changed after completion. It never alters the historical receipt.
	GovernedDriftAfterCompletion bool   `json:"governed_drift_after_completion" yaml:"governed_drift_after_completion"`
	ProjectionState              string `json:"projection_state" yaml:"projection_state"`
	ReceiptPath                  string `json:"receipt_path" yaml:"receipt_path"`
}

// TerminalStateAssessment is the deterministic, read-only reconstruction of a
// task's terminal state. Its DigestSHA256 is a self-excluding content address, and
// no wall clock is read, so identical durable inputs yield a byte-identical output.
type TerminalStateAssessment struct {
	SchemaVersion        string                        `json:"schema_version" yaml:"schema_version"`
	State                TerminalState                 `json:"state" yaml:"state"`
	Detail               string                        `json:"detail,omitempty" yaml:"detail,omitempty"`
	Task                 closureprotocol.TaskBinding   `json:"task" yaml:"task"`
	CurrentResultBinding closureprotocol.ResultBinding `json:"current_result_binding,omitempty" yaml:"current_result_binding,omitempty"`
	CompletedCount       int                           `json:"completed_count" yaml:"completed_count"`
	RevokedCount         int                           `json:"revoked_count" yaml:"revoked_count"`
	Committed            *CommittedFacts               `json:"committed,omitempty" yaml:"committed,omitempty"`
	Bound                []string                      `json:"bound" yaml:"bound"`
	DigestSHA256         string                        `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

func inspectBound() []string {
	return []string{
		"a read-only reconstruction of terminal state from durable owners; it establishes no completion and blesses no residue",
		"a valid receipt without a completed event is residue, not completion; a completed event without a valid receipt is broken, not completion",
		"projections are derived state, never terminal authority; contradictory terminal history is never normalized",
	}
}

// InspectTerminalState reconstructs the honest terminal state of a task from durable
// owner-verified artifacts alone. It accepts only a repository/task identity — no
// caller-supplied status, receipt, event, result, cardinality, or classification —
// and reuses the 8.2b terminal-history classifier so it shares one definition of
// terminal truth. It is read-only and deterministic.
func InspectTerminalState(ctx context.Context, req Request) (TerminalStateAssessment, error) {
	_ = ctx
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	// Repository and task must name one world before any read: the governed-manifest
	// truth is computed from the root while the ledger is read from the task directory.
	if berr := validateRepositoryTaskBinding(root, taskDir); berr != nil {
		return TerminalStateAssessment{}, berr
	}

	a := TerminalStateAssessment{SchemaVersion: inspectSchemaVersion, Bound: inspectBound()}

	store := ledger.NewStore(taskDir)
	report, verr := store.Verify()
	if verr != nil || !report.Valid || report.EntryCount == 0 {
		a.State = TerminalUnsupported
		a.Detail = "task ledger did not verify"
		return stampInspect(a), nil
	}
	chain, cerr := store.VerifyChain()
	if cerr != nil || len(chain.Entries) == 0 {
		a.State = TerminalUnsupported
		a.Detail = "task ledger chain unavailable"
		return stampInspect(a), nil
	}
	a.Task = chain.Entries[len(chain.Entries)-1].Entry.Task
	currentRB, haveRB := latestResultBinding(chain)
	if haveRB {
		a.CurrentResultBinding = currentRB
	}
	governedManifest, _ := governedmutation.GovernedManifestDigest(root)

	tf := classifyTerminalFacts(chain)
	a.CompletedCount = tf.completedCount
	a.RevokedCount = tf.revokedCount

	switch {
	case tf.revokedCount > 0 || tf.completedCount > 1:
		a.State = TerminalContradictoryHistory
		a.Detail = fmt.Sprintf("completed=%d revoked=%d: terminal history is contradictory", tf.completedCount, tf.revokedCount)
	case tf.completedCount == 0:
		if hasOrphanTerminalReceipt(taskDir) {
			a.State = TerminalReceiptWithoutEvent
			a.Detail = "an orphan terminal-receipt artifact exists without a completed event (residue)"
		} else {
			a.State = TerminalNotCompleted
		}
	default:
		classifyUniqueCompleted(taskDir, tf, currentRB, haveRB, governedManifest, report.ProjectionState, &a)
	}
	return stampInspect(a), nil
}

// classifyUniqueCompleted classifies the exactly-one-completed case.
func classifyUniqueCompleted(taskDir string, tf terminalFacts, currentRB closureprotocol.ResultBinding, haveRB bool, governedManifest, projectionState string, a *TerminalStateAssessment) {
	receipt, ref, lerr := loadTerminalReceipt(taskDir, tf.completed)
	if lerr != nil {
		if ref.Path == "" || artifactMissing(taskDir, ref) {
			a.State = TerminalEventWithoutValidReceipt
		} else {
			a.State = TerminalIntegrityFailure
		}
		a.Detail = lerr.Error()
		return
	}
	if merr := completedEventMatches(tf.completed, receipt, ref); merr != nil {
		a.State = TerminalIntegrityFailure
		a.Detail = merr.Error()
		return
	}
	// A valid event/receipt pair proves which result was historically completed, but
	// without a durable CURRENT result identity we cannot prove it is still the task's
	// current result world. Missing comparison evidence is not equality: reconstruction
	// must PROVE (current result exists AND receipt result == current result), never
	// merely observe the absence of a mismatch.
	if !haveRB {
		a.State = TerminalUnsupported
		a.Detail = "no current result binding: cannot prove the completed result is the current result world"
		return
	}
	if !bindingpkg.ResultBindingEqual(receipt.Completion.ResultBinding, currentRB) {
		a.State = TerminalWrongBinding
		a.Detail = "the completed receipt binds a different result than the current result"
		return
	}
	drift := receipt.GovernedManifestDigestSHA256 != governedManifest
	a.Committed = &CommittedFacts{
		ReceiptDigestSHA256:                  receipt.ReceiptDigestSHA256,
		CausalIdentitySHA256:                 receipt.CausalIdentitySHA256,
		RecordedResultBinding:                receipt.Completion.ResultBinding,
		RecordedGovernedManifestDigestSHA256: receipt.GovernedManifestDigestSHA256,
		CurrentGovernedManifestDigestSHA256:  governedManifest,
		GovernedDriftAfterCompletion:         drift,
		ProjectionState:                      projectionState,
		ReceiptPath:                          ref.Path,
	}
	if projectionState == "current" {
		a.State = TerminalCommitted
	} else {
		a.State = TerminalProjectionStaleOrMissing
		a.Detail = "durable conjunction is valid but derived projections are " + projectionState
	}
}

// hasOrphanTerminalReceipt reports whether a terminal-receipt-shaped artifact exists
// in the content-addressed store. It is a shallow residue check (schema only), never
// a completion proof.
func hasOrphanTerminalReceipt(taskDir string) bool {
	dir := filepath.Join(taskDir, "artifacts", "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		var probe struct {
			SchemaVersion string `json:"schema_version"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.SchemaVersion == TerminalReceiptSchemaVersion {
			return true
		}
	}
	return false
}

func artifactMissing(taskDir string, ref closureprotocol.LedgerPayloadRef) bool {
	if ref.Path == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	return os.IsNotExist(err)
}

// stampInspect computes the self-excluding assessment identity.
func stampInspect(a TerminalStateAssessment) TerminalStateAssessment {
	a.DigestSHA256 = ""
	if d, err := closureprotocol.SemanticDigest(a); err == nil {
		a.DigestSHA256 = d
	}
	return a
}

// AssessmentBoundStates lists, deterministically, the closed set of terminal states
// (used by callers and tests that enumerate the vocabulary).
func AssessmentBoundStates() []TerminalState {
	out := []TerminalState{
		TerminalNotCompleted, TerminalCommitted, TerminalReceiptWithoutEvent,
		TerminalEventWithoutValidReceipt, TerminalContradictoryHistory, TerminalWrongBinding,
		TerminalIntegrityFailure, TerminalProjectionStaleOrMissing, TerminalUnsupported,
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
