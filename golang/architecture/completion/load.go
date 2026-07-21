// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/certification"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/questionresolution"
)

// evidence is the fully-loaded, re-proven durable evidence the pure assessor
// consumes. Every field is derived from an owner or a durable artifact; none comes
// from a caller.
type evidence struct {
	task              closureprotocol.TaskBinding
	resultBinding     closureprotocol.ResultBinding
	haveResultBinding bool
	headDigest        string
	governedManifest  string

	// Phase-6 correctness certification, bounded to the CURRENT result. A certified
	// event is routed by its claimed result binding; only current-result candidates
	// are re-verified, and a verified receipt satisfies only when its own binding
	// equals the current result.
	correctness             *closureprotocol.CertificationReceipt // the single valid current-result receipt
	correctnessDigest       string
	correctnessCurrentValid int  // distinct valid receipts binding the current result
	correctnessTampered     bool // a current-result candidate failed byte/parse/receipt verification
	correctnessTamperedErr  string
	correctnessWrongResult  bool // a current-routed candidate verified but its receipt binds another result
	correctnessHistorical   int  // certified events bound to a different/older result (historical, excluded)

	// Phase-8.1d question-resolution certification.
	qr              *questionresolution.QuestionResolutionCertificate
	qrDigest        string
	qrValid         bool
	qrErr           string
	qrRelevantCount int // certificates purporting to bind THIS task
	qrCurrentCount  int // relevant certificates bound to the CURRENT head

	// Conflicting terminal-completion fact.
	conflictingCompletion bool
	conflictDetail        string
}

// loadTaskWorld verifies the task ledger and reads the current task identity, head,
// and result binding read-only. It never repairs or rebuilds.
func loadTaskWorld(ctx context.Context, taskDir string) (ledger.VerifiedChain, closureprotocol.TaskBinding, string, closureprotocol.ResultBinding, bool, error) {
	store := ledger.NewStore(taskDir)
	report, err := store.VerifyCtx(ctx)
	if err != nil || !report.Valid || report.EntryCount == 0 {
		return ledger.VerifiedChain{}, closureprotocol.TaskBinding{}, "", closureprotocol.ResultBinding{}, false, fmt.Errorf("task ledger did not verify")
	}
	chain, err := store.VerifyChainCtx(ctx)
	if err != nil || len(chain.Entries) == 0 {
		return ledger.VerifiedChain{}, closureprotocol.TaskBinding{}, "", closureprotocol.ResultBinding{}, false, fmt.Errorf("task ledger chain unavailable")
	}
	ra, err := admission.LoadRecordedAuthorityCtx(ctx, taskDir)
	if err != nil {
		return ledger.VerifiedChain{}, closureprotocol.TaskBinding{}, "", closureprotocol.ResultBinding{}, false, fmt.Errorf("load recorded authority: %w", err)
	}
	rb, ok := latestResultBinding(chain)
	return chain, ra.Base.Task, chain.Head.EntryDigestSHA256, rb, ok, nil
}

// latestResultBinding returns the result binding of the highest-sequence verified
// result_transition_recorded event.
func latestResultBinding(chain ledger.VerifiedChain) (closureprotocol.ResultBinding, bool) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventResultTransitionRecorded {
			continue
		}
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			return closureprotocol.ResultBinding{}, false
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil || payload.ResultBinding == nil {
			return closureprotocol.ResultBinding{}, false
		}
		return *payload.ResultBinding, true
	}
	return closureprotocol.ResultBinding{}, false
}

// loadCorrectnessEvidence re-proves the recorded Phase-6 correctness certification
// bounded to the CURRENT result. Each verified certified event is routed by its
// claimed (untrusted) result binding: events bound to a different/older result are
// historical and excluded from the current-result decision. Current-result
// candidates are byte-checked and independently re-verified with the certification
// owner; a verified receipt counts only when its OWN result binding equals the
// current result. It never re-runs certification lanes. Historical certificates —
// broken or valid — cannot poison the current result.
func loadCorrectnessEvidence(taskDir string, chain ledger.VerifiedChain, currentRB closureprotocol.ResultBinding, haveRB bool, ev *evidence) {
	if !haveRB {
		return // without a current result we cannot route correctness → missing
	}
	valid := map[string]bool{}
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventCertified {
			continue
		}
		data, err := ledger.ReadVerifiedPayload(ve)
		if err != nil {
			continue
		}
		payload, err := ledger.ParseTaskEventPayload(data)
		if err != nil || payload.ResultBinding == nil {
			continue
		}
		// Route by the event's claimed result binding (untrusted routing metadata).
		if !binding.ResultBindingEqual(*payload.ResultBinding, currentRB) {
			ev.correctnessHistorical++
			continue
		}
		// A current-result candidate: byte-check + independently re-verify.
		ref, ok := payload.Artifacts["certification_receipt"]
		if !ok {
			ev.correctnessTampered = true
			ev.correctnessTamperedErr = "current certified event has no certification_receipt artifact"
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
		if rerr != nil {
			ev.correctnessTampered = true
			ev.correctnessTamperedErr = "certification receipt artifact unreadable: " + rerr.Error()
			continue
		}
		if sha256Hex(raw) != ref.DigestSHA256 {
			ev.correctnessTampered = true
			ev.correctnessTamperedErr = "certification receipt artifact digest mismatch"
			continue
		}
		var rc closureprotocol.CertificationReceipt
		if jerr := json.Unmarshal(raw, &rc); jerr != nil {
			ev.correctnessTampered = true
			ev.correctnessTamperedErr = "certification receipt unparseable: " + jerr.Error()
			continue
		}
		if verr := certification.VerifyReceipt(rc); verr != nil {
			ev.correctnessTampered = true
			ev.correctnessTamperedErr = "certification receipt failed re-verification: " + verr.Error()
			continue
		}
		// A verified receipt may satisfy only when its OWN binding equals the current
		// result — a receipt binding another result fails closed.
		if !binding.ResultBindingEqual(rc.ResultBinding, currentRB) {
			ev.correctnessWrongResult = true
			continue
		}
		valid[rc.DigestSHA256] = true
		if ev.correctness == nil {
			e := rc
			ev.correctness = &e
			ev.correctnessDigest = rc.DigestSHA256
		}
	}
	ev.correctnessCurrentValid = len(valid)
}

// loadQuestionResolutionEvidence re-proves the Phase-8.1d question-resolution
// certificate for the CURRENT task world. Discovery is non-authoritative; a
// certificate is routed to this task only by its untrusted claimed task binding,
// and a routed candidate must pass full validation (LoadCertificate) to count.
func loadQuestionResolutionEvidence(root string, ev *evidence) {
	digests, err := questionresolution.DiscoverCertificates(root)
	if err != nil {
		ev.qrErr = "certificate discovery unavailable: " + err.Error()
		return
	}
	var currentDigests []string
	relevant := 0
	for _, digest := range digests {
		claim, cerr := questionresolution.ReadCertificateClaim(root, digest)
		if cerr != nil || claim.Task.ID != ev.task.ID || claim.Task.SessionID != ev.task.SessionID {
			// Unreadable or bound to a different task — unrelated; excluded.
			continue
		}
		relevant++
		if claim.TaskLedgerHeadDigestSHA256 != ev.headDigest {
			continue // relevant to this task but bound to an older head → stale
		}
		currentDigests = append(currentDigests, digest)
	}
	ev.qrRelevantCount = relevant
	ev.qrCurrentCount = len(currentDigests)
	if len(currentDigests) != 1 {
		return // 0 → missing/stale; >1 → contradictory (decided by the assessor)
	}
	cert, verr := questionresolution.LoadCertificate(root, currentDigests[0])
	if verr != nil {
		ev.qrErr = "question-resolution certificate failed validation: " + verr.Error()
		return
	}
	ev.qr = &cert
	ev.qrDigest = cert.DigestSHA256
	ev.qrValid = true
}

// detectConflictingCompletion scans for an already-recorded terminal-completion or
// revocation fact on the task ledger.
func detectConflictingCompletion(chain ledger.VerifiedChain, ev *evidence) {
	for _, ve := range chain.Entries {
		switch ve.Entry.EventType {
		case closureprotocol.LedgerEventCompleted:
			ev.conflictingCompletion = true
			ev.conflictDetail = "a terminal-completion (completed) event already exists"
			return
		case closureprotocol.LedgerEventRevoked:
			ev.conflictingCompletion = true
			ev.conflictDetail = "a revocation (revoked) event already exists"
			return
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
