// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/ledger"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/questionpromotion"
)

// SummaryRequest selects the task/result world to project.
type SummaryRequest struct {
	RepositoryRoot string
	TaskDirectory  string
}

// Summarize produces the deterministic, read-only question-resolution projection.
// It reuses the question-disposition owner (enumeration + per-question projection)
// and the promotion verification boundary as the SOLE authorities; it re-implements
// none of their validation.
//
// The projection is TASK-BOUNDED. It first collects this task's reusable-candidate
// disposition receipt digests, then routes each discovered promotion by its claimed
// disposition digest — untrusted routing metadata, never authority. A promotion is
// re-proven with VerifyCommittedPromotion only when it purports to bind one of those
// current-task digests; only then can it satisfy (verified) or block (failed) this
// summary. Unrelated broken promotion debris elsewhere in the repository is excluded
// and can never veto this bounded certificate. Two verified promotions for one
// disposition are contradictory and fail closed rather than silently overwriting.
// Output is sorted, so identical durable inputs yield a byte-identical summary.
// Summarize mutates nothing.
func Summarize(ctx context.Context, req SummaryRequest) (Summary, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if root == "" || taskDir == "" {
		return Summary{}, fmt.Errorf("repository root and task directory are required")
	}

	// Bind the whole task world by its verified ledger head (transitively binds the
	// result transition, question set, and every recorded disposition by digest).
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		return Summary{}, fmt.Errorf("verify task ledger: %w", err)
	}
	// The task binding comes from the recorded authority resolution, not a caller.
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return Summary{}, fmt.Errorf("load recorded authority: %w", err)
	}

	questions, err := qd.OpenQuestionsForLatestTransition(taskDir)
	if err != nil {
		return Summary{}, fmt.Errorf("enumerate questions: %w", err)
	}

	// Pass A: project every question. Terminal, non-reusable states settle here.
	// Reusable-candidate questions are held pending the promotion decision, and
	// their exact disposition digests form the current-task relevance set.
	out := make([]QuestionResolution, 0, len(questions))
	reusableDigest := map[string]bool{} // current reusable-candidate disposition digests
	pendingReusable := map[int]string{} // index in out -> disposition digest
	for _, q := range questions {
		qr := QuestionResolution{
			QuestionID:             q.QuestionID,
			ArchitectRequired:      q.ArchitectRequired,
			BlocksClosureDimension: q.BlocksClosureDimension,
			ScopeDomain:            q.ScopeDomain,
			ScopeFiles:             append([]string(nil), q.ScopeFiles...),
		}
		proj, perr := qd.ProjectQuestion(taskDir, q.QuestionID)
		switch {
		case perr != nil || !proj.Disposed:
			qr.State = StateUnresolved
		case proj.Contested:
			qr.State = StateContested
			qr.DispositionReceiptDigestSHA256 = proj.Latest.ReceiptDigestSHA256
		default:
			d := proj.Latest
			qr.Disposition = string(d.Disposition)
			qr.Reusability = string(d.Reusability)
			qr.DispositionReceiptDigestSHA256 = d.ReceiptDigestSHA256
			switch {
			case d.Disposition == qd.DispositionDeferred:
				qr.State = StateDeferred
			case d.Disposition == qd.DispositionDismissed:
				qr.State = StateDismissed
			case d.Disposition == qd.DispositionTaskLocal,
				d.Disposition == qd.DispositionAnswered && d.Reusability == qd.ReusabilityTaskLocal:
				qr.State = StateAnsweredTaskLocal
			case d.Disposition == qd.DispositionAnswered && d.Reusability == qd.ReusabilityReusableCandidate:
				reusableDigest[d.ReceiptDigestSHA256] = true
				pendingReusable[len(out)] = d.ReceiptDigestSHA256
				// state decided in pass C
			default:
				qr.State = StateUnresolved
			}
		}
		out = append(out, qr)
	}

	// Pass B: route discovered promotions by claimed disposition (non-authoritative)
	// and re-prove only those relevant to a current reusable-candidate disposition.
	verifiedByDisp := map[string][]questionpromotion.VerifiedPromotion{}
	integrityByDisp := map[string]string{}
	lineages, derr := questionpromotion.DiscoverCommittedPromotions(root)
	if derr != nil {
		return Summary{}, fmt.Errorf("discover promotions: %w", derr)
	}
	for _, lineage := range lineages {
		claim, cerr := questionpromotion.ClaimedDispositionDigest(root, lineage)
		if cerr != nil || claim == "" || !reusableDigest[claim] {
			// Unreadable, unclaimed, or unrelated to this task — excluded. It can
			// neither satisfy nor block this bounded certificate.
			continue
		}
		vp, verr := questionpromotion.VerifyCommittedPromotion(ctx, root, lineage)
		if verr != nil {
			integrityByDisp[claim] = fmt.Sprintf("promotion %s excluded (integrity): %v", shortID(lineage), verr)
			continue
		}
		// A verified promotion may satisfy ONLY its exact current-task disposition.
		key := vp.Receipt.QuestionDispositionReceiptDigestSHA256
		if !reusableDigest[key] {
			continue
		}
		verifiedByDisp[key] = append(verifiedByDisp[key], vp)
	}

	// Pass C: settle reusable-candidate states from the task-bounded evidence.
	var findings []string
	for idx, digest := range pendingReusable {
		state, finding := classifyReusable(verifiedByDisp[digest], integrityByDisp[digest])
		out[idx].State = state
		if state == StateReusablePromoted {
			vp := verifiedByDisp[digest][0]
			out[idx].PromotionLineageID = vp.PromotionLineageID
			out[idx].PromotionReceiptDigestSHA256 = vp.Receipt.ReceiptDigestSHA256
			out[idx].GovernedNodeIRI = vp.GovernedNodeIRI
		}
		if finding != "" {
			findings = append(findings, finding)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].QuestionID < out[j].QuestionID })
	sort.Strings(findings)
	return Summary{
		SchemaVersion:              SummarySchemaVersion,
		Task:                       ra.Base.Task,
		TaskLedgerHeadDigestSHA256: chain.Head.EntryDigestSHA256,
		Questions:                  out,
		IntegrityFindings:          findings,
	}, nil
}

// classifyReusable decides a reusable-candidate question's state from the
// TASK-BOUNDED promotion evidence for its disposition digest. Two or more verified
// promotions binding one disposition are contradictory and fail closed (never
// map-order selection); a relevant verification failure is an integrity failure;
// exactly one verified promotion satisfies; none is an incomplete obligation.
func classifyReusable(verified []questionpromotion.VerifiedPromotion, relevantIntegrity string) (QuestionState, string) {
	switch {
	case len(verified) > 1:
		return StateEvidenceIntegrityFailure, "contradictory: multiple verified promotions bind the same disposition"
	case relevantIntegrity != "":
		return StateEvidenceIntegrityFailure, relevantIntegrity
	case len(verified) == 1:
		return StateReusablePromoted, ""
	default:
		return StateReusableUnpromoted, ""
	}
}

func shortID(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
