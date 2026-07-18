// SPDX-License-Identifier: Apache-2.0

package questiondisposition

import "github.com/globulario/sensei/golang/architecture"

// OpenQuestionRef identifies a disposable architect question on the latest
// recorded result transition.
type OpenQuestionRef struct {
	QuestionID             string   `json:"question_id" yaml:"question_id"`
	QuestionText           string   `json:"question_text" yaml:"question_text"`
	BlocksClosureDimension string   `json:"blocks_closure_dimension,omitempty" yaml:"blocks_closure_dimension,omitempty"`
	ArchitectRequired      bool     `json:"architect_required" yaml:"architect_required"`
	ScopeDomain            string   `json:"scope_domain,omitempty" yaml:"scope_domain,omitempty"`
	ScopeFiles             []string `json:"scope_files,omitempty" yaml:"scope_files,omitempty"`
}

// OpenQuestionsForLatestTransition lists the architect questions carried by the
// latest result_transition_recorded event, each with its stable id. It reads
// from one verified snapshot and never writes.
func OpenQuestionsForLatestTransition(taskDir string) ([]OpenQuestionRef, error) {
	store := newStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return nil, qdErr(CodeChainVerifyFailed, "%v", err)
	}
	_, payload, ok := latestResultTransition(chain)
	if !ok {
		return nil, qdErr(CodeNoResultTransition, "no result_transition_recorded event on the ledger")
	}
	bundle, _, err := loadArchitectQuestions(taskDir, payload)
	if err != nil {
		return nil, err
	}
	var out []OpenQuestionRef
	for _, q := range bundle.Dialogue.OpenQuestions {
		out = append(out, OpenQuestionRef{
			QuestionID:             architecture.StableOpenQuestionID(q),
			QuestionText:           q.QuestionText,
			BlocksClosureDimension: q.BlocksClosureDimension,
			ArchitectRequired:      q.ArchitectRequired,
			ScopeDomain:            q.Scope.Domain,
			ScopeFiles:             append([]string(nil), q.Scope.Files...),
		})
	}
	return out, nil
}

// NextAction is the single load-bearing next step for a disposed (or undisposed)
// architect question. It is advisory routing only — this owner never promotes,
// rebuilds, certifies, or completes; the named later owners do.
type NextAction string

const (
	// NextProvideOrRepairAnswer: the question is not yet authoritatively disposed.
	NextProvideOrRepairAnswer NextAction = "provide_or_repair_answer"
	// NextPromoteReusable: an answered reusable_candidate awaits the governed
	// promotion owner (Slice 8.1b). This owner does NOT promote.
	NextPromoteReusable NextAction = "promote_reusable"
	// NextReevaluateTaskLocal: an answered task-local outcome is confined to this
	// task/result; re-evaluate within the task, never a global promotion.
	NextReevaluateTaskLocal NextAction = "reevaluate_task_local"
	// NextAwaitArchitect: a deferred question awaits a later authorized disposition.
	NextAwaitArchitect NextAction = "await_architect"
	// NextAwaitAdjudication: the question is contested (conflicting immutable
	// dispositions); it awaits authorized architect adjudication.
	NextAwaitAdjudication NextAction = "await_architect_adjudication"
	// NextNone: a dismissed question is a durable explanation with no pending step.
	NextNone NextAction = "none"
)

// NextActionFor computes the single next step from a disposition receipt and
// whether the question is contested. Contested always dominates.
func NextActionFor(rc QuestionDispositionReceipt, contested bool) NextAction {
	if contested {
		return NextAwaitAdjudication
	}
	switch rc.Disposition {
	case DispositionAnswered:
		if rc.Reusability == ReusabilityReusableCandidate {
			return NextPromoteReusable
		}
		return NextReevaluateTaskLocal
	case DispositionTaskLocal:
		return NextReevaluateTaskLocal
	case DispositionDeferred:
		return NextAwaitArchitect
	case DispositionDismissed:
		return NextNone
	default:
		return NextNone
	}
}

// QuestionProjection is the derived disposition state of one question, folded
// from the verified ledger.
type QuestionProjection struct {
	QuestionID       string
	Disposed         bool
	Latest           QuestionDispositionReceipt
	Contested        bool
	ContestedDigests []string
	NextAction       NextAction
}

// ProjectQuestion folds the disposition state of a single question from one
// verified ledger snapshot. Conflicting immutable dispositions mark it contested;
// the latest (highest-sequence) disposition drives the projection otherwise.
func ProjectQuestion(taskDir, questionID string) (QuestionProjection, error) {
	list, err := ListRecordedDispositions(taskDir)
	if err != nil {
		return QuestionProjection{}, err
	}
	proj := QuestionProjection{QuestionID: questionID}
	digests := map[string]bool{}
	for _, rd := range list {
		if rd.Receipt.QuestionID != questionID {
			continue
		}
		proj.Disposed = true
		proj.Latest = rd.Receipt // list is sequence-ordered; last wins
		if !digests[rd.Receipt.ReceiptDigestSHA256] {
			digests[rd.Receipt.ReceiptDigestSHA256] = true
			proj.ContestedDigests = append(proj.ContestedDigests, rd.Receipt.ReceiptDigestSHA256)
		}
	}
	if !proj.Disposed {
		proj.NextAction = NextProvideOrRepairAnswer
		proj.ContestedDigests = nil
		return proj, nil
	}
	proj.Contested = len(proj.ContestedDigests) > 1
	proj.NextAction = NextActionFor(proj.Latest, proj.Contested)
	return proj, nil
}
