// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// dispositionOutput is the stable machine render of the disposition owner's
// result. The CLI translates operational inputs and renders the owner's typed
// result — it computes no authority, digest, phase, or projection of its own.
// correctness_certified is ALWAYS false; disposition never certifies.
type dispositionOutput struct {
	Outcome                        string   `json:"outcome" yaml:"outcome"`
	QuestionID                     string   `json:"question_id,omitempty" yaml:"question_id,omitempty"`
	ReceiptDigestSHA256            string   `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
	EntryDigestSHA256              string   `json:"entry_digest_sha256,omitempty" yaml:"entry_digest_sha256,omitempty"`
	PreviousLedgerHeadDigestSHA256 string   `json:"previous_ledger_head_digest_sha256,omitempty" yaml:"previous_ledger_head_digest_sha256,omitempty"`
	CurrentLedgerHeadDigestSHA256  string   `json:"current_ledger_head_digest_sha256,omitempty" yaml:"current_ledger_head_digest_sha256,omitempty"`
	LedgerSequence                 int      `json:"ledger_sequence,omitempty" yaml:"ledger_sequence,omitempty"`
	Contested                      bool     `json:"contested" yaml:"contested"`
	ContestedPriorDigests          []string `json:"contested_prior_digests,omitempty" yaml:"contested_prior_digests,omitempty"`
	NextAction                     string   `json:"next_action,omitempty" yaml:"next_action,omitempty"`
	ProjectionState                string   `json:"projection_state,omitempty" yaml:"projection_state,omitempty"`
	RefusalCode                    string   `json:"refusal_code,omitempty" yaml:"refusal_code,omitempty"`
	RefusalDetail                  string   `json:"refusal_detail,omitempty" yaml:"refusal_detail,omitempty"`
	PostCommitEntryDigestSHA256    string   `json:"post_commit_entry_digest_sha256,omitempty" yaml:"post_commit_entry_digest_sha256,omitempty"`
	PostCommitRecoveryAction       string   `json:"post_commit_recovery_action,omitempty" yaml:"post_commit_recovery_action,omitempty"`
	CorrectnessCertified           bool     `json:"correctness_certified" yaml:"correctness_certified"`
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// runDispositionQuestion records an authoritative disposition of one architect
// question via the questiondisposition owner. It accepts only operational inputs
// — never a caller-supplied outcome digest, phase, authority verdict, or
// certification claim. With -list it renders the disposable questions instead.
func runDispositionQuestion(args []string) int {
	var repoRoot, taskDir, identityRoot, questionID string
	var disposition, reusability, rationale, answerID, answerFile, scopeDomain, format string
	var scopeFiles, evidence stringList
	var list bool
	fs := flag.NewFlagSet("sensei disposition-question", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&identityRoot, "identity-root", "", "answering-actor identity store (default: <repo>/.sensei/identity)")
	fs.BoolVar(&list, "list", false, "list the disposable architect questions and exit")
	fs.StringVar(&questionID, "question", "", "stable id of the architect question to dispose")
	fs.StringVar(&disposition, "disposition", "", "answered|dismissed|deferred|task_local")
	fs.StringVar(&reusability, "reusability", "", "reusable_candidate|task_local (answered only)")
	fs.StringVar(&rationale, "rationale", "", "why this disposition")
	fs.StringVar(&answerID, "answer-id", "", "answer id (answered only)")
	fs.StringVar(&answerFile, "answer-file", "", "file holding the canonical answer bytes (answered only)")
	fs.StringVar(&scopeDomain, "scope-domain", "", "effective scope domain (must not broaden the question)")
	fs.Var(&scopeFiles, "scope-file", "effective scope file (repeatable)")
	fs.Var(&evidence, "evidence", "evidence ref (repeatable)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "disposition-question:", err)
		return 2
	}
	if strings.TrimSpace(identityRoot) == "" {
		identityRoot = identity.Root(repoRoot)
	}

	if list {
		return runDispositionList(dir, format)
	}

	var answerBytes []byte
	if strings.TrimSpace(answerFile) != "" {
		answerBytes, err = os.ReadFile(answerFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "disposition-question:", err)
			return 2
		}
	}

	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory:        dir,
		RepositoryRoot:       repoRoot,
		IdentityRoot:         identityRoot,
		QuestionID:           questionID,
		Disposition:          qd.Disposition(strings.TrimSpace(disposition)),
		Reusability:          qd.Reusability(strings.TrimSpace(reusability)),
		Rationale:            rationale,
		AnswerID:             answerID,
		AnswerBytes:          answerBytes,
		EffectiveScopeDomain: scopeDomain,
		EffectiveScopeFiles:  scopeFiles,
		EvidenceRefs:         evidence,
	})
	if err != nil {
		return renderDispositionError(err, format)
	}

	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: dir, Candidate: cand})
	if err != nil {
		return renderDispositionError(err, format)
	}

	out := dispositionOutput{
		Outcome:                        string(res.Outcome),
		QuestionID:                     res.QuestionID,
		ReceiptDigestSHA256:            res.ReceiptDigestSHA256,
		EntryDigestSHA256:              res.EntryDigestSHA256,
		PreviousLedgerHeadDigestSHA256: res.PreviousLedgerHeadSHA256,
		CurrentLedgerHeadDigestSHA256:  res.CurrentLedgerHeadSHA256,
		LedgerSequence:                 res.LedgerSequence,
		Contested:                      len(res.ContestedPriorDigests) > 0,
		ContestedPriorDigests:          res.ContestedPriorDigests,
		ProjectionState:                res.ProjectionState,
		CorrectnessCertified:           false,
	}
	if proj, perr := qd.ProjectQuestion(dir, res.QuestionID); perr == nil {
		out.NextAction = string(proj.NextAction)
	}

	if strings.TrimSpace(format) == "" || format == "text" {
		renderDispositionHuman(out)
	} else if err := printValue(out, format); err != nil {
		fmt.Fprintln(os.Stderr, "disposition-question:", err)
		return 1
	}
	return 0
}

func runDispositionList(taskDir, format string) int {
	questions, err := qd.OpenQuestionsForLatestTransition(taskDir)
	if err != nil {
		return renderDispositionError(err, format)
	}
	if strings.TrimSpace(format) == "" || format == "text" {
		if len(questions) == 0 {
			fmt.Println("no disposable architect questions")
			return 0
		}
		for _, q := range questions {
			req := ""
			if q.ArchitectRequired {
				req = " [architect_required]"
			}
			fmt.Printf("%s  (%s)%s\n  %s\n", q.QuestionID, q.BlocksClosureDimension, req, q.QuestionText)
		}
		return 0
	}
	if err := printValue(questions, format); err != nil {
		fmt.Fprintln(os.Stderr, "disposition-question:", err)
		return 1
	}
	return 0
}

// renderDispositionError renders a typed owner error and maps it to a stable
// exit code: 1 for a post-commit condition (the entry is durable; retry the same
// command), 3 for any pre-commit refusal or stale head (fail-closed).
func renderDispositionError(err error, format string) int {
	out := dispositionOutput{Outcome: "refused", CorrectnessCertified: false}
	exit := 3
	var pce *qd.PostCommitError
	var qe *qd.Error
	switch {
	case errors.As(err, &pce):
		out.Outcome = "post_commit_incomplete"
		out.RefusalCode = pce.Code
		out.RefusalDetail = pce.Detail
		out.PostCommitEntryDigestSHA256 = pce.EntryDigestSHA256
		out.PostCommitRecoveryAction = pce.RecoveryAction
		out.CurrentLedgerHeadDigestSHA256 = pce.LedgerHeadDigestSHA256
		out.QuestionID = pce.QuestionID
		exit = 1
	case errors.As(err, &qe):
		if qe.Code == qd.CodeStaleExpectedHead {
			out.Outcome = "stale"
		}
		out.RefusalCode = qe.Code
		out.RefusalDetail = qe.Detail
	default:
		out.RefusalCode = "disposition-question.error"
		out.RefusalDetail = err.Error()
	}
	if strings.TrimSpace(format) == "" || format == "text" {
		renderDispositionHuman(out)
	} else if perr := printValue(out, format); perr != nil {
		fmt.Fprintln(os.Stderr, "disposition-question:", perr)
	}
	return exit
}

func renderDispositionHuman(o dispositionOutput) {
	fmt.Printf("outcome:  %s\n", o.Outcome)
	if o.QuestionID != "" {
		fmt.Printf("question: %s\n", o.QuestionID)
	}
	if o.ReceiptDigestSHA256 != "" {
		fmt.Printf("receipt:  %s\n", short(o.ReceiptDigestSHA256))
	}
	if o.EntryDigestSHA256 != "" {
		fmt.Printf("entry:    %s (seq %d)\n", short(o.EntryDigestSHA256), o.LedgerSequence)
	}
	if o.Contested {
		fmt.Printf("contested: %d prior disposition(s) preserved\n", len(o.ContestedPriorDigests))
	}
	if o.NextAction != "" {
		fmt.Printf("next:     %s\n", o.NextAction)
	}
	if o.RefusalCode != "" {
		fmt.Printf("refusal:  %s — %s\n", o.RefusalCode, o.RefusalDetail)
	}
	if o.PostCommitRecoveryAction != "" {
		fmt.Printf("recovery: %s (committed entry %s)\n", o.PostCommitRecoveryAction, short(o.PostCommitEntryDigestSHA256))
	}
	fmt.Printf("correctness_certified: %v\n", o.CorrectnessCertified)
}
