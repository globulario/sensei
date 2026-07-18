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
// none of their validation. Discovery of promotions is non-authoritative: every
// candidate is independently re-proven before it can satisfy a reusable obligation,
// and a broken conjunction becomes a typed integrity finding rather than silent
// acceptance. Output is sorted, so identical durable inputs yield a byte-identical
// summary. Summarize mutates nothing.
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

	// Independently re-prove every discovered promotion. A verified promotion is
	// indexed by the exact disposition it promoted; a failed one becomes a typed
	// integrity finding. Discovery order does not affect the result (sorted below).
	verifiedByDisposition := map[string]questionpromotion.VerifiedPromotion{}
	var integrity []string
	lineages, derr := questionpromotion.DiscoverCommittedPromotions(root)
	if derr != nil {
		return Summary{}, fmt.Errorf("discover promotions: %w", derr)
	}
	for _, lineage := range lineages {
		vp, verr := questionpromotion.VerifyCommittedPromotion(ctx, root, lineage)
		if verr != nil {
			integrity = append(integrity, fmt.Sprintf("promotion %s excluded (integrity): %v", shortID(lineage), verr))
			continue
		}
		verifiedByDisposition[vp.Receipt.QuestionDispositionReceiptDigestSHA256] = vp
	}

	var out []QuestionResolution
	for _, q := range questions {
		qr := QuestionResolution{
			QuestionID:             q.QuestionID,
			ArchitectRequired:      q.ArchitectRequired,
			BlocksClosureDimension: q.BlocksClosureDimension,
			ScopeDomain:            q.ScopeDomain,
			ScopeFiles:             append([]string(nil), q.ScopeFiles...),
		}
		proj, perr := qd.ProjectQuestion(taskDir, q.QuestionID)
		if perr != nil || !proj.Disposed {
			qr.State = StateUnresolved
			out = append(out, qr)
			continue
		}
		if proj.Contested {
			qr.State = StateContested
			qr.DispositionReceiptDigestSHA256 = proj.Latest.ReceiptDigestSHA256
			out = append(out, qr)
			continue
		}
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
			if vp, ok := verifiedByDisposition[d.ReceiptDigestSHA256]; ok {
				qr.State = StateReusablePromoted
				qr.PromotionLineageID = vp.PromotionLineageID
				qr.PromotionReceiptDigestSHA256 = vp.Receipt.ReceiptDigestSHA256
				qr.GovernedNodeIRI = vp.GovernedNodeIRI
			} else {
				qr.State = StateReusableUnpromoted
			}
		default:
			// An answered disposition with no admissible reusability should be
			// impossible (the disposition owner's Validate enforces it); treat any
			// unclassified disposition as unresolved rather than silently passing.
			qr.State = StateUnresolved
		}
		out = append(out, qr)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].QuestionID < out[j].QuestionID })
	sort.Strings(integrity)
	return Summary{
		SchemaVersion:              SummarySchemaVersion,
		Task:                       ra.Base.Task,
		TaskLedgerHeadDigestSHA256: chain.Head.EntryDigestSHA256,
		Questions:                  out,
		IntegrityFindings:          integrity,
	}, nil
}

func shortID(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
