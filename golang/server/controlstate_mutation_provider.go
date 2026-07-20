// SPDX-License-Identifier: AGPL-3.0-only

package main

// Phase 9.5 Checkpoint 5 — the guarded architect-answer mutation write path.
//
// This is the FIRST mutation surface in the control panel. It is a thin, guarded
// DELEGATION to the existing owners (questiondisposition / questionpromotion); it
// assigns NO authority of its own. The load-bearing discipline:
//
//   - Client-supplied repository/domain/task/session/actor fields are CLAIMS to
//     VERIFY against server-resolved authority — never authority to trust. The
//     server resolves the real repository root, active task, and enrolled actor
//     from its immutable startup-owned context and REFUSES on any mismatch.
//   - Prepare performs NO mutation. Record commits exactly one disposition. A
//     domain refusal (unconfigured, mismatch, unauthorized, ineligible, stale,
//     contested) writes NOTHING and is surfaced as a typed refusal with the
//     UNCHANGED ledger identity — never as a mutation that appears to succeed.
//   - No hidden lifecycle chaining: recording never promotes; promoting never
//     completes or certifies.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/propose"
)

// mutationRefusal is a typed, pre-write refusal. It guarantees nothing was
// written; ledgerHead is the UNCHANGED head at refusal time.
type mutationRefusal struct {
	Owner      string
	Code       string
	Detail     string
	LedgerHead string
}

// mutationBindings are the SERVER-RESOLVED bindings (authority), echoed into the
// audit envelope. The actor/task/domain here are what the server proved, not
// what the client claimed.
type mutationBindings struct {
	OperationKind string
	Actor         string
	Repository    string
	Domain        string
	TaskID        string
	SessionID     string
	QuestionID    string
	LedgerHead    string // head observed at resolution (unchanged on refusal)
}

type mutationContext struct {
	repoRoot     string
	taskDir      string
	identityRoot string
	actor        string
	domain       string
	taskID       string
	sessionID    string
	ledgerHead   string
}

// loadActivePointer resolves the repository's active task. It is a package var
// (not a client input) so the active task is ALWAYS server-resolved; tests
// override it to point at an ephemeral fixture. Production uses the real pointer.
var loadActivePointer = tasksession.LoadActivePointer

// writePathConfigured reports whether the guarded mutation surface has its
// startup-owned repository context. Absence is a stable typed refusal, never an
// internal server failure.
func (s *server) writePathConfigured() bool { return s != nil && s.briefingRepo != nil }

// resolveMutationContext resolves authority server-side and VERIFIES every client
// claim against it. Any mismatch is a typed refusal (nothing is touched).
func (s *server) resolveMutationContext(repositoryIdentity, domain, taskID, sessionID, actor string) (mutationContext, *mutationRefusal) {
	if !s.writePathConfigured() {
		return mutationContext{}, &mutationRefusal{Owner: "server.control", Code: "repository_context_unavailable",
			Detail: "the guarded mutation surface is not configured on this server"}
	}
	repoRoot := s.briefingRepo.Root

	// Domain is the startup-owned repository domain; a differing claim is refused.
	if d := strings.TrimSpace(domain); d != "" && d != s.briefingRepo.Domain {
		return mutationContext{}, &mutationRefusal{Owner: "server.control", Code: "domain_mismatch",
			Detail: "requested domain does not match the served repository"}
	}

	ptr, err := loadActivePointer(repoRoot)
	if err != nil {
		return mutationContext{}, &mutationRefusal{Owner: "tasksession", Code: "no_active_task",
			Detail: "no active task is bound on the server"}
	}
	if t := strings.TrimSpace(taskID); t != "" && t != ptr.TaskID {
		return mutationContext{}, &mutationRefusal{Owner: "tasksession", Code: "task_mismatch",
			Detail: "requested task is not the active task", LedgerHead: ptr.LedgerHeadDigestSHA256}
	}
	if d := strings.TrimSpace(domain); d != "" && ptr.RepositoryDomain != "" && d != ptr.RepositoryDomain {
		return mutationContext{}, &mutationRefusal{Owner: "tasksession", Code: "domain_mismatch",
			Detail: "requested domain does not match the active task", LedgerHead: ptr.LedgerHeadDigestSHA256}
	}
	taskDir, err := filepath.Abs(filepath.Join(repoRoot, filepath.Dir(filepath.FromSlash(ptr.SessionPath))))
	if err != nil {
		return mutationContext{}, &mutationRefusal{Owner: "tasksession", Code: "task_dir_unresolved", Detail: "could not resolve the active task directory"}
	}

	identityRoot := identity.Root(repoRoot)
	manifest, ok, err := identity.LoadManifest(identityRoot)
	if err != nil {
		return mutationContext{}, &mutationRefusal{Owner: "identity", Code: "identity_load_failed",
			Detail: "the enrolled identity could not be loaded", LedgerHead: ptr.LedgerHeadDigestSHA256}
	}
	if !ok {
		return mutationContext{}, &mutationRefusal{Owner: "identity", Code: "actor_not_enrolled",
			Detail: "no actor is enrolled for this repository", LedgerHead: ptr.LedgerHeadDigestSHA256}
	}
	resolvedActor := manifest.ActorBinding().PrincipalID
	if a := strings.TrimSpace(actor); a != "" && a != resolvedActor {
		return mutationContext{}, &mutationRefusal{Owner: "identity", Code: "actor_mismatch",
			Detail: "the claimed actor is not the enrolled actor", LedgerHead: ptr.LedgerHeadDigestSHA256}
	}

	return mutationContext{
		repoRoot: repoRoot, taskDir: taskDir, identityRoot: identityRoot,
		actor: resolvedActor, domain: ptr.RepositoryDomain, taskID: ptr.TaskID,
		sessionID: strings.TrimSpace(sessionID), ledgerHead: ptr.LedgerHeadDigestSHA256,
	}, nil
}

func (c mutationContext) bindings(kind, questionID string) mutationBindings {
	return mutationBindings{OperationKind: kind, Actor: c.actor, Repository: c.repoRoot,
		Domain: c.domain, TaskID: c.taskID, SessionID: c.sessionID, QuestionID: questionID, LedgerHead: c.ledgerHead}
}

// dispositionInput is the owner-native disposition request (proto already
// converted). answerBytes is opaque and hashed by the owner.
type dispositionInput struct {
	repositoryIdentity, domain, taskID, sessionID, questionID, actor string
	disposition                                                      qd.Disposition
	reusability                                                      qd.Reusability
	rationale, answerID                                              string
	answerBytes                                                      []byte
	scopeDomain                                                      string
	scopeFiles, evidence                                             []string
}

// prepareDisposition builds the pure candidate. It NEVER writes. It returns the
// resolved context so the commit path can reuse the exact task directory.
func (s *server) prepareDisposition(in dispositionInput) (*qd.DispositionCandidate, mutationContext, mutationBindings, *mutationRefusal) {
	ctx, ref := s.resolveMutationContext(in.repositoryIdentity, in.domain, in.taskID, in.sessionID, in.actor)
	b := ctx.bindings("disposition", in.questionID)
	if ref != nil {
		return nil, ctx, b, ref
	}
	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: ctx.taskDir, RepositoryRoot: ctx.repoRoot, IdentityRoot: ctx.identityRoot,
		QuestionID: in.questionID, Disposition: in.disposition, Reusability: in.reusability,
		Rationale: in.rationale, AnswerID: in.answerID, AnswerBytes: in.answerBytes,
		EffectiveScopeDomain: in.scopeDomain, EffectiveScopeFiles: in.scopeFiles, EvidenceRefs: in.evidence,
	})
	if err != nil {
		return nil, ctx, b, refusalFromDispositionErr(err, ctx.ledgerHead)
	}
	return &cand, ctx, b, nil
}

// recordDisposition prepares then commits EXACTLY ONE disposition. The client's
// expected ledger head is a precondition: if the live head moved since the
// client prepared, the commit is refused and nothing is written. Record never
// silently re-prepares against a moved head.
func (s *server) recordDisposition(in dispositionInput, expectedHead string) (*qd.RecordResult, string, mutationBindings, *mutationRefusal) {
	cand, ctx, b, ref := s.prepareDisposition(in)
	if ref != nil {
		return nil, "", b, ref
	}
	if eh := strings.TrimSpace(expectedHead); eh != "" && eh != cand.ExpectedLedgerHeadDigestSHA256 {
		return nil, "", b, &mutationRefusal{Owner: "questiondisposition", Code: "stale_expected_head",
			Detail: "the ledger head moved since prepare; re-prepare and retry", LedgerHead: cand.ExpectedLedgerHeadDigestSHA256}
	}
	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: ctx.taskDir, Candidate: *cand})
	if err != nil {
		return nil, "", b, refusalFromDispositionErr(err, cand.ExpectedLedgerHeadDigestSHA256)
	}
	return &res, cand.ExpectedLedgerHeadDigestSHA256, b, nil
}

// promotionInput is the owner-native promotion request.
type promotionInput struct {
	repositoryIdentity, domain, taskID, actor, dispositionReceiptDigest string
	proposal                                                            propose.Request
	scopeDomain                                                         string
	scopeFiles                                                          []string
	expectedManifestDigest                                              string
}

// promoteAnswer promotes an already-accepted disposition. It CONSUMES an
// independently-authored governed proposal; it never manufactures one from the
// answer. A refusal (ineligible/authority/scope/stale) writes nothing.
func (s *server) promoteAnswer(in promotionInput) (*qp.PromoteResult, mutationBindings, *mutationRefusal) {
	ctx, ref := s.resolveMutationContext(in.repositoryIdentity, in.domain, in.taskID, "", in.actor)
	b := ctx.bindings("promotion", "")
	if ref != nil {
		return nil, b, ref
	}
	res, err := qp.Promote(context.Background(), qp.PromoteRequest{
		RepositoryRoot: ctx.repoRoot, TaskDirectory: ctx.taskDir, RepositoryDomain: ctx.domain,
		IdentityRoot: ctx.identityRoot, QuestionDispositionReceiptDigestSHA256: in.dispositionReceiptDigest,
		Proposal: in.proposal, EffectiveScopeDomain: in.scopeDomain, EffectiveScopeFiles: in.scopeFiles,
	})
	if err != nil {
		return nil, b, &mutationRefusal{Owner: "questionpromotion", Code: "promotion_failed", Detail: sanitizeMutationDetail(err.Error()), LedgerHead: ctx.ledgerHead}
	}
	return &res, b, nil
}

// refusalFromDispositionErr maps the owner's typed error to a refusal, verbatim
// code, sanitized detail. Any error is a refusal (nothing was written pre-commit).
func refusalFromDispositionErr(err error, ledgerHead string) *mutationRefusal {
	var qerr *qd.Error
	if errors.As(err, &qerr) {
		return &mutationRefusal{Owner: "questiondisposition", Code: qerr.Code, Detail: sanitizeMutationDetail(qerr.Detail), LedgerHead: ledgerHead}
	}
	var pce *qd.PostCommitError
	if errors.As(err, &pce) {
		return &mutationRefusal{Owner: "questiondisposition", Code: pce.Code, Detail: "post-commit validation; retry the same candidate", LedgerHead: ledgerHead}
	}
	return &mutationRefusal{Owner: "questiondisposition", Code: "disposition_failed", Detail: sanitizeMutationDetail(err.Error()), LedgerHead: ledgerHead}
}

// sanitizeMutationDetail strips filesystem paths and never echoes raw answer
// text; details are typed reason prose only.
func sanitizeMutationDetail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
