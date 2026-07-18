// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
)

// Request selects the task/result world to assess. It carries NO caller booleans,
// receipt paths, or status: the evaluator re-proves everything from durable owners.
type Request struct {
	RepositoryRoot string
	TaskDirectory  string
}

// AssessReadiness produces the deterministic, read-only terminal-completion
// readiness assessment for a task. It re-proves each obligation from durable
// artifacts and their owners and conjoins them. It mutates nothing: no task-ledger
// write, no completion receipt, no completed event. A ready assessment reports only
// that the required evidence conjunction currently holds — it is NOT a completion.
func AssessReadiness(ctx context.Context, req Request) (ReadinessAssessment, error) {
	_ = ctx
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if root == "" || taskDir == "" {
		return ReadinessAssessment{}, fmt.Errorf("repository root and task directory are required")
	}

	chain, task, head, rb, haveRB, err := loadTaskWorld(taskDir)
	if err != nil {
		return ReadinessAssessment{}, err
	}
	ev := &evidence{task: task, resultBinding: rb, haveResultBinding: haveRB, headDigest: head}
	// Governed freshness is recomputed live; the certificate cannot supply it.
	if manifest, merr := governedmutation.GovernedManifestDigest(root); merr == nil {
		ev.governedManifest = manifest
	}
	loadCorrectnessEvidence(taskDir, chain, rb, haveRB, ev)
	loadQuestionResolutionEvidence(root, ev)
	detectConflictingCompletion(chain, ev)

	a := assess(ev)
	digest, derr := AssessmentDigest(a)
	if derr != nil {
		return ReadinessAssessment{}, derr
	}
	a.DigestSHA256 = digest
	if verr := ValidateAssessment(a); verr != nil {
		return ReadinessAssessment{}, verr
	}
	return a, nil
}

// assess is the pure conjunction. Every branch is a function of the loaded evidence
// only — no clock, no filesystem, no caller input — so identical evidence yields a
// byte-identical assessment. It is ready only when every obligation is satisfied.
func assess(ev *evidence) ReadinessAssessment {
	byID := map[ObligationID]ObligationAssessment{}

	if ev.haveResultBinding {
		byID[ObligationTaskLedgerIdentity] = ob(ObligationTaskLedgerIdentity, EvidenceSatisfied, "",
			[]EvidenceRef{
				{Kind: "task_ledger_head", DigestSHA256: ev.headDigest},
				{Kind: "result_tree", DigestSHA256: ev.resultBinding.ResultTreeDigestSHA256},
			})
	} else {
		byID[ObligationTaskLedgerIdentity] = ob(ObligationTaskLedgerIdentity, EvidenceMissing, "no result transition recorded for this task", nil)
	}

	correctness := assessCorrectness(ev)
	byID[ObligationCorrectnessCertificate] = correctness

	qr := assessQuestionResolution(ev)
	byID[ObligationQuestionResolution] = qr

	byID[ObligationClosureAndProof] = assessClosureAndProof(ev, correctness.State, qr.State)
	byID[ObligationGovernedFreshness] = assessGovernedFreshness(ev, qr.State)

	if ev.conflictingCompletion {
		byID[ObligationNoConflictingCompletion] = ob(ObligationNoConflictingCompletion, EvidenceContradictory, ev.conflictDetail, nil)
	} else {
		byID[ObligationNoConflictingCompletion] = ob(ObligationNoConflictingCompletion, EvidenceSatisfied, "", nil)
	}

	list := make([]ObligationAssessment, 0, len(obligationOrder))
	ready := true
	for _, id := range obligationOrder {
		o := byID[id]
		list = append(list, o)
		if o.State != EvidenceSatisfied {
			ready = false
		}
	}
	readiness := ReadinessNotReady
	if ready {
		readiness = ReadinessReady
	}

	return ReadinessAssessment{
		SchemaVersion:                ReadinessSchemaVersion,
		Task:                         ev.task,
		ResultBinding:                ev.resultBinding,
		TaskLedgerHeadDigestSHA256:   ev.headDigest,
		GovernedManifestDigestSHA256: ev.governedManifest,
		Obligations:                  list,
		Readiness:                    readiness,
		Limitations:                  readinessLimitations(),
		Bound:                        boundStatement(),
	}
}

// readinessLimitations is the fixed limitation set embedded in every readiness
// assessment. It is factored out so a historical readiness reconstruction (8.2d)
// can reproduce the exact frozen assessment and recompute its digest.
func readinessLimitations() []string {
	return []string{
		"the Phase-6 correctness certificate records no governed-manifest digest; governed-world freshness is anchored on the Phase-8.1d certificate, which does bind it",
	}
}

// assessCorrectness classifies the CURRENT-result correctness evidence. Historical
// certificates (for older/different results) are excluded upstream and never reach
// here, so an older certification can neither satisfy nor contradict the current
// result. Fail-closed precedence: a tampered current candidate, then a verified
// receipt that binds another result, then multiple distinct valid current
// certificates, then the single valid one, then historical-only (stale), then none.
func assessCorrectness(ev *evidence) ObligationAssessment {
	id := ObligationCorrectnessCertificate
	switch {
	case ev.correctnessTampered:
		return ob(id, EvidenceIntegrityFailure, ev.correctnessTamperedErr, refEvidence("certification_receipt", ev.correctnessDigest))
	case ev.correctnessWrongResult:
		return ob(id, EvidenceWrongBinding, "a current-routed certificate's verified receipt binds another result", nil)
	case ev.correctnessCurrentValid > 1:
		return ob(id, EvidenceContradictory, "multiple distinct valid certifications for the current result", nil)
	case ev.correctnessCurrentValid == 1:
		v := ev.correctness.CertificationVerdict
		if v != closureprotocol.Certified && v != closureprotocol.CertifiedWithConditions {
			return ob(id, EvidenceUnsupported, "certification verdict is "+string(v), refEvidence("certification_receipt", ev.correctnessDigest))
		}
		return ob(id, EvidenceSatisfied, "verdict "+string(v), refEvidence("certification_receipt", ev.correctnessDigest))
	case ev.correctnessHistorical > 0:
		return ob(id, EvidenceStale, "only older results are certified; the current result has no certification", nil)
	default:
		return ob(id, EvidenceMissing, "no Phase-6 correctness certification recorded for the current result", nil)
	}
}

func assessQuestionResolution(ev *evidence) ObligationAssessment {
	id := ObligationQuestionResolution
	switch {
	case ev.qrRelevantCount == 0:
		return ob(id, EvidenceMissing, "no question-resolution certificate for this task", nil)
	case ev.qrCurrentCount > 1:
		return ob(id, EvidenceContradictory, "multiple question-resolution certificates bind this task at the current head", nil)
	case ev.qrCurrentCount == 0:
		return ob(id, EvidenceStale, "question-resolution certificate binds an older ledger head", nil)
	case !ev.qrValid:
		return ob(id, EvidenceIntegrityFailure, ev.qrErr, nil)
	}
	return ob(id, EvidenceSatisfied, "", refEvidence("question_resolution_certificate", ev.qrDigest))
}

func assessClosureAndProof(ev *evidence, correctnessState, qrState EvidenceState) ObligationAssessment {
	id := ObligationClosureAndProof
	if correctnessState != EvidenceSatisfied || qrState != EvidenceSatisfied {
		return ob(id, EvidenceUnsupported, "depends on a satisfied correctness proof lane and a closed question-resolution loop", nil)
	}
	proof := ev.correctness.ProofLane
	if proof != closureprotocol.DimensionPass && proof != closureprotocol.DimensionPassWithException {
		return ob(id, EvidenceUnsupported, "correctness proof lane is "+string(proof), nil)
	}
	return ob(id, EvidenceSatisfied, "proof lane "+string(proof)+"; question loop closed",
		[]EvidenceRef{
			{Kind: "correctness_proof_lane", Detail: string(proof)},
			{Kind: "question_resolution_certificate", DigestSHA256: ev.qrDigest},
		})
}

func assessGovernedFreshness(ev *evidence, qrState EvidenceState) ObligationAssessment {
	id := ObligationGovernedFreshness
	if qrState != EvidenceSatisfied || ev.qr == nil {
		return ob(id, EvidenceUnsupported, "no valid current question-resolution certificate to anchor governed freshness", nil)
	}
	if ev.qr.GovernedManifestDigestSHA256 != ev.governedManifest {
		return ob(id, EvidenceStale, "governed source changed after the question-resolution certificate",
			[]EvidenceRef{{Kind: "governed_manifest", DigestSHA256: ev.governedManifest}})
	}
	return ob(id, EvidenceSatisfied, "", []EvidenceRef{{Kind: "governed_manifest", DigestSHA256: ev.governedManifest}})
}

func ob(id ObligationID, state EvidenceState, detail string, ev []EvidenceRef) ObligationAssessment {
	return ObligationAssessment{Obligation: id, State: state, Detail: detail, Evidence: ev}
}

func refEvidence(kind, digest string) []EvidenceRef {
	if digest == "" {
		return nil
	}
	return []EvidenceRef{{Kind: kind, DigestSHA256: digest}}
}
