// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/admission"
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

	// Phase-6 correctness certification.
	correctness           *closureprotocol.CertificationReceipt
	correctnessDigest     string
	correctnessValid      bool
	correctnessErr        string
	correctnessCount      int  // distinct certified receipts on the ledger
	correctnessSuperseded bool // a later result transition advanced the world after certification

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
func loadTaskWorld(taskDir string) (ledger.VerifiedChain, closureprotocol.TaskBinding, string, closureprotocol.ResultBinding, bool, error) {
	store := ledger.NewStore(taskDir)
	report, err := store.Verify()
	if err != nil || !report.Valid || report.EntryCount == 0 {
		return ledger.VerifiedChain{}, closureprotocol.TaskBinding{}, "", closureprotocol.ResultBinding{}, false, fmt.Errorf("task ledger did not verify")
	}
	chain, err := store.VerifyChain()
	if err != nil || len(chain.Entries) == 0 {
		return ledger.VerifiedChain{}, closureprotocol.TaskBinding{}, "", closureprotocol.ResultBinding{}, false, fmt.Errorf("task ledger chain unavailable")
	}
	ra, err := admission.LoadRecordedAuthority(taskDir)
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
// from the task ledger. It counts distinct certified receipts (for contradiction),
// reads the most recent one's artifact, checks its byte integrity, and re-verifies
// it with the certification owner. It never re-runs certification lanes.
func loadCorrectnessEvidence(taskDir string, chain ledger.VerifiedChain, ev *evidence) {
	distinct := map[string]bool{}
	var current *ledger.VerifiedEntry
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		ve := chain.Entries[i]
		if ve.Entry.EventType != closureprotocol.LedgerEventCertified {
			continue
		}
		if current == nil {
			e := ve
			current = &e
		}
		distinct[ve.Entry.Payload.DigestSHA256] = true
	}
	ev.correctnessCount = len(distinct)
	if current == nil {
		return
	}
	// A result transition recorded after the certified event means the world
	// advanced and the certificate certified an older result — stale.
	for _, ve := range chain.Entries {
		if ve.Entry.EventType == closureprotocol.LedgerEventResultTransitionRecorded &&
			ve.Entry.Sequence > current.Entry.Sequence {
			ev.correctnessSuperseded = true
			break
		}
	}
	data, err := ledger.ReadVerifiedPayload(*current)
	if err != nil {
		ev.correctnessErr = "certified event payload unreadable: " + err.Error()
		return
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		ev.correctnessErr = "certified event payload malformed: " + err.Error()
		return
	}
	ref, ok := payload.Artifacts["certification_receipt"]
	if !ok {
		ev.correctnessErr = "certified event has no certification_receipt artifact"
		return
	}
	raw, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		ev.correctnessErr = "certification receipt artifact unreadable: " + err.Error()
		return
	}
	if sha256Hex(raw) != ref.DigestSHA256 {
		ev.correctnessErr = "certification receipt artifact digest mismatch"
		return
	}
	var rc closureprotocol.CertificationReceipt
	if err := json.Unmarshal(raw, &rc); err != nil {
		ev.correctnessErr = "certification receipt unparseable: " + err.Error()
		return
	}
	if err := certification.VerifyReceipt(rc); err != nil {
		ev.correctnessErr = "certification receipt failed re-verification: " + err.Error()
		return
	}
	ev.correctness = &rc
	ev.correctnessDigest = rc.DigestSHA256
	ev.correctnessValid = true
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
