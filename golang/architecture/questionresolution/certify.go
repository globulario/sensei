// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Outcome is the closed set of bounded-gate results.
type Outcome string

const (
	// OutcomeSatisfied: the bounded obligation holds and a certificate was produced.
	OutcomeSatisfied Outcome = "satisfied"
	// OutcomeReplay: an identical certificate already existed; nothing was written.
	OutcomeReplay Outcome = "exact_replay"
	// OutcomeUnresolvedQuestion: a binding question lacks an admissible terminal
	// disposition (undisposed or deferred).
	OutcomeUnresolvedQuestion Outcome = "blocked_unresolved_question"
	// OutcomeContestedQuestion: a binding question has contested dispositions.
	OutcomeContestedQuestion Outcome = "blocked_contested_question"
	// OutcomeIncompletePromotion: a reusable-candidate answer has no valid committed
	// promotion.
	OutcomeIncompletePromotion Outcome = "blocked_incomplete_promotion"
	// OutcomeIntegrityFailure: a discovered promotion failed re-verification
	// (tampered, incomplete, stale, or superseded).
	OutcomeIntegrityFailure Outcome = "blocked_integrity_failure"
	// OutcomeStaleExpectedHead: the caller's expected ledger head no longer matches.
	OutcomeStaleExpectedHead Outcome = "stale_expected_head"
	// OutcomeAuthorityRefusal: the certification actor/authority did not resolve.
	OutcomeAuthorityRefusal Outcome = "authority_refusal"
	// OutcomeLedgerInvalid: the task ledger did not verify.
	OutcomeLedgerInvalid Outcome = "ledger_invalid"
	// OutcomeInputInvalid: the request was malformed.
	OutcomeInputInvalid Outcome = "input_invalid"
)

// CertifyRequest drives the bounded gate over one task/result world.
type CertifyRequest struct {
	RepositoryRoot string
	TaskDirectory  string
	// IdentityRoot holds the enrolled certification actor identity.
	IdentityRoot string
	// ExpectedLedgerHeadDigestSHA256, when set, is a freshness guard: a mismatch
	// with the current verified head refuses the gate (no stale certification).
	ExpectedLedgerHeadDigestSHA256 string
}

// CertifyResult carries the verdict, the read-only summary it was computed from,
// and — only on satisfaction — the produced certificate and its repo-relative path.
type CertifyResult struct {
	Outcome         Outcome
	Detail          string
	Summary         Summary
	Certificate     *QuestionResolutionCertificate
	CertificatePath string
}

func refuse(o Outcome, format string, a ...any) (CertifyResult, error) {
	return CertifyResult{Outcome: o, Detail: fmt.Sprintf(format, a...)}, nil
}

// Certify is the bounded question-resolution certification gate. It fails closed on
// any missing, ambiguous, stale, contested, superseded, out-of-scope, tampered, or
// incomplete evidence and produces a content-addressed certificate ONLY when every
// binding architect question has an admissible terminal disposition and every
// reusable answer claimed as governed truth has a valid committed promotion. It
// writes nothing on refusal and is idempotent on an unchanged world.
func Certify(ctx context.Context, req CertifyRequest) (CertifyResult, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	idRoot := strings.TrimSpace(req.IdentityRoot)
	if root == "" || taskDir == "" || idRoot == "" {
		return refuse(OutcomeInputInvalid, "repository root, task dir, and identity root are required")
	}

	// A composable lock gives a consistent snapshot: no promotion can mutate the
	// governed world between reading dispositions/promotions and binding the
	// certificate. It nests beneath no other lock here and mutates no governed source.
	release, err := governedmutation.AcquireLock(ctx, root, "question_resolution_certification", time.Now().UTC())
	if err != nil {
		return refuse(OutcomeAuthorityRefusal, "acquire lock: %v", err)
	}
	defer release()

	// Verify the task ledger and enforce freshness before any authority work.
	store := ledger.NewStore(taskDir)
	report, err := store.Verify()
	if err != nil || !report.Valid || report.EntryCount == 0 {
		return refuse(OutcomeLedgerInvalid, "task ledger did not verify")
	}
	if exp := strings.TrimSpace(req.ExpectedLedgerHeadDigestSHA256); exp != "" && exp != report.HeadDigestSHA256 {
		return refuse(OutcomeStaleExpectedHead, "expected head %s, current %s", shortID(exp), shortID(report.HeadDigestSHA256))
	}
	chain, err := store.VerifyChain()
	if err != nil || len(chain.Entries) == 0 {
		return refuse(OutcomeLedgerInvalid, "task ledger chain unavailable")
	}
	certifiedAt := chain.Entries[len(chain.Entries)-1].Entry.ProducedAt

	// Verify the separate certification actor and the exact certify operation.
	index, err := authority.LoadPolicyIndex(root)
	if err != nil {
		return refuse(OutcomeAuthorityRefusal, "load policy index: %v", err)
	}
	id, enrolled, err := identity.LoadManifest(idRoot)
	if err != nil || !enrolled {
		return refuse(OutcomeAuthorityRefusal, "certification actor is not enrolled")
	}
	binding := id.ActorBinding()
	evaluatedAt := time.Now().UTC()
	verified, verr := authority.VerifyActorBinding(binding, identity.Resolver(idRoot), index, evaluatedAt)
	if verr != nil || verified.Status != closureprotocol.ReceiptValid {
		return refuse(OutcomeAuthorityRefusal, "certification actor not verified")
	}
	grantID, roleID, aerr := resolveCertificationAuthority(index, binding, verified, evaluatedAt, taskDir)
	if aerr != nil {
		return refuse(OutcomeAuthorityRefusal, "%v", aerr)
	}

	// Compute the read-only summary and the bounded verdict.
	summary, err := Summarize(ctx, SummaryRequest{RepositoryRoot: root, TaskDirectory: taskDir})
	if err != nil {
		return refuse(OutcomeLedgerInvalid, "summarize: %v", err)
	}
	if summary.TaskLedgerHeadDigestSHA256 != report.HeadDigestSHA256 {
		// The world moved between verification and projection — refuse rather than
		// certify a head we did not freshly verify.
		return refuse(OutcomeStaleExpectedHead, "ledger head advanced during evaluation")
	}
	outcome, detail := evaluate(summary)
	if outcome != OutcomeSatisfied {
		return CertifyResult{Outcome: outcome, Detail: detail, Summary: summary}, nil
	}

	manifest, err := governedmutation.GovernedManifestDigest(root)
	if err != nil {
		return refuse(OutcomeLedgerInvalid, "governed manifest: %v", err)
	}

	cert := QuestionResolutionCertificate{
		SchemaVersion:                CertificateSchemaVersion,
		Task:                         summary.Task,
		TaskLedgerHeadDigestSHA256:   summary.TaskLedgerHeadDigestSHA256,
		QuestionEvidence:             questionEvidence(summary),
		PromotionEvidence:            promotionEvidence(summary),
		GovernedManifestDigestSHA256: manifest,
		AuthorityGrantID:             grantID,
		AuthorityRoleID:              roleID,
		Producer:                     GeneratedBy,
		CertifiedAt:                  certifiedAt,
		Bound:                        boundStatement(),
	}
	digest, err := CertificateDigest(cert)
	if err != nil {
		return refuse(OutcomeLedgerInvalid, "certificate digest: %v", err)
	}
	cert.DigestSHA256 = digest
	if err := ValidateCertificate(cert); err != nil {
		return refuse(OutcomeLedgerInvalid, "certificate invalid: %v", err)
	}
	relPath, replay, err := persistCertificate(root, cert)
	if err != nil {
		return refuse(OutcomeLedgerInvalid, "persist certificate: %v", err)
	}
	res := CertifyResult{Outcome: OutcomeSatisfied, Summary: summary, Certificate: &cert, CertificatePath: relPath}
	if replay {
		res.Outcome = OutcomeReplay
	}
	return res, nil
}

// evaluate is the deterministic bounded verdict. Precedence is strictly
// fail-closed: any promotion integrity failure blocks first, then any binding
// question that is not terminally resolved, then any reusable answer without a
// committed promotion.
func evaluate(s Summary) (Outcome, string) {
	if len(s.IntegrityFindings) > 0 {
		return OutcomeIntegrityFailure, s.IntegrityFindings[0]
	}
	for _, q := range s.Questions {
		if !q.ArchitectRequired {
			continue
		}
		switch q.State {
		case StateUnresolved, StateDeferred:
			return OutcomeUnresolvedQuestion, q.QuestionID
		case StateContested:
			return OutcomeContestedQuestion, q.QuestionID
		}
	}
	// Every reusable-candidate answer — binding or not — is a claim of governed truth
	// and must be backed by a valid committed promotion.
	for _, q := range s.Questions {
		if q.State == StateReusableUnpromoted {
			return OutcomeIncompletePromotion, q.QuestionID
		}
	}
	return OutcomeSatisfied, ""
}

func questionEvidence(s Summary) []QuestionEvidence {
	out := make([]QuestionEvidence, 0, len(s.Questions))
	for _, q := range s.Questions {
		out = append(out, QuestionEvidence{
			QuestionID:                     q.QuestionID,
			ArchitectRequired:              q.ArchitectRequired,
			State:                          q.State,
			DispositionReceiptDigestSHA256: q.DispositionReceiptDigestSHA256,
		})
	}
	return out
}

func promotionEvidence(s Summary) []PromotionEvidence {
	var out []PromotionEvidence
	for _, q := range s.Questions {
		if q.State != StateReusablePromoted {
			continue
		}
		out = append(out, PromotionEvidence{
			PromotionLineageID:             q.PromotionLineageID,
			ReceiptDigestSHA256:            q.PromotionReceiptDigestSHA256,
			DispositionReceiptDigestSHA256: q.DispositionReceiptDigestSHA256,
			GovernedNodeIRI:                q.GovernedNodeIRI,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PromotionLineageID < out[j].PromotionLineageID })
	return out
}
