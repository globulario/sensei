// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resulttransition"

	"gopkg.in/yaml.v3"
)

// Phase 3 admission v2 CLI. These commands are thin protocol diagnostics: they
// parse, locate and verify the task ledger, load governed records, call typed
// package APIs, append the frozen ledger event, rebuild projections, and render.
// No admission, authority, capability, or scope business rule lives here. They
// require expected-head protection and never fabricate certification or
// completion. Slice 4c composes them; here each is explicit.
//
// The capability is consumed BEFORE the mutation, never retroactively during
// verification. The operator sequence is:
//
//	sensei advance-task      -> ready_for_mutation
//	sensei consume-admission -> spends the single-use capability for this
//	                            exact operation set
//	<agent applies the mutation in the worktree>
//	sensei verify-admission  -> records the observed change, verifies scope
//
// consume-admission is a distinct, explicit step. verify-admission does NOT
// consume the capability: it requires an existing admission_consumed receipt and
// fails closed (exit 3) when none is present.

const admissionV2Window = 24 * time.Hour

func admissionV2Validator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

func readTypedFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, out)
}

// verifyExpectedHead fails closed unless the ledger head matches the caller's
// expected head, so no command operates on a forked or stale chain.
func verifyExpectedHead(taskDir, expectedHead string) (string, error) {
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(expectedHead) == "" {
		return "", fmt.Errorf("--expected-head is required (current head %s)", head)
	}
	if head != strings.TrimSpace(expectedHead) {
		return "", fmt.Errorf("stale expected head: got %s, ledger head is %s", expectedHead, head)
	}
	return head, nil
}

func rebuildAndReport(taskDir string) error {
	if _, err := ledger.RebuildProjections(taskDir, admissionV2Validator); err != nil {
		return err
	}
	report, err := ledger.VerifyTaskLedger(taskDir)
	if err != nil {
		return err
	}
	if !report.Valid {
		return fmt.Errorf("task ledger failed verification after append")
	}
	return nil
}

func nowUTC() time.Time { return time.Now().UTC() }

// ---- authority-resolve ----------------------------------------------------

func runAuthorityResolve(args []string) int {
	var repoRoot, taskDir, actorPath, changePlanPath, applicabilityPath, expectedHead, evaluatedAt, format string
	fs := flag.NewFlagSet("sensei authority-resolve", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory")
	fs.StringVar(&actorPath, "actor-binding", "", "typed actor binding file")
	fs.StringVar(&changePlanPath, "change-plan", "", "typed change plan file")
	fs.StringVar(&applicabilityPath, "applicability", "", "typed authority applicability file")
	fs.StringVar(&expectedHead, "expected-head", "", "expected current ledger head digest")
	fs.StringVar(&evaluatedAt, "evaluated-at", "", "RFC3339 evaluation time (default now)")
	fs.StringVar(&format, "format", "text", "output format")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve:", err)
		return 2
	}
	head, err := verifyExpectedHead(dir, expectedHead)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve:", err)
		return 1
	}
	if actorPath == "" || changePlanPath == "" {
		fmt.Fprintln(os.Stderr, "authority-resolve: --actor-binding and --change-plan are required")
		return 2
	}

	base, err := admission.LoadTaskBaseBinding(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve:", err)
		return 1
	}
	var actor closureprotocol.ActorBinding
	if err := readTypedFile(actorPath, &actor); err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: actor binding:", err)
		return 1
	}
	var changePlan closureprotocol.ChangePlan
	if err := readTypedFile(changePlanPath, &changePlan); err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: change plan:", err)
		return 1
	}
	var applicability []authority.AuthorityApplicability
	if applicabilityPath != "" {
		if err := readTypedFile(applicabilityPath, &applicability); err != nil {
			fmt.Fprintln(os.Stderr, "authority-resolve: applicability:", err)
			return 1
		}
	}
	when := strings.TrimSpace(evaluatedAt)
	if when == "" {
		when = nowUTC().Format(time.RFC3339)
	}
	evalTime, err := time.Parse(time.RFC3339, when)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: --evaluated-at must be RFC3339")
		return 2
	}

	index, err := authority.LoadPolicyIndex(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: load policy:", err)
		return 1
	}
	resolver := authority.NewLocalBundleResolver(repoRoot)
	verified, err := authority.VerifyActorBinding(actor, resolver, index, evalTime)
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: actor verification failed:", err)
		return 3
	}
	resolution, err := admission.ResolveAuthority(index, admission.ResolveAuthorityInput{
		Actor:                            actor,
		VerifiedActor:                    verified,
		Base:                             base,
		ChangePlan:                       changePlan,
		Applicability:                    applicability,
		PolicyID:                         strings.TrimSpace(base.Policies.Admission),
		AuthorityPolicyGraphDigestSHA256: closureprotocol.MustSemanticDigest(index),
		EvaluatedAt:                      when,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve:", err)
		return 1
	}
	task := base.Task
	if _, err := admission.RecordAuthorityResolved(newAdmissionStore(dir), head, task, resolution, actor, changePlan, base, verified.DelegationReceipts, nowUTC()); err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve: record:", err)
		return 1
	}
	if err := rebuildAndReport(dir); err != nil {
		fmt.Fprintln(os.Stderr, "authority-resolve:", err)
		return 1
	}
	if err := printValue(resolution, format); err != nil {
		return 1
	}
	return 0
}

func newAdmissionStore(dir string) *ledger.Store {
	return ledger.NewStore(dir, ledger.WithPayloadValidator(admissionV2Validator))
}

// hasTaskDirFlag reports whether --task-dir was supplied, selecting the typed
// v2 path. A strict v2 task must not fall back to legacy path-only admission.
func hasTaskDirFlag(args []string) bool {
	for i, a := range args {
		if a == "--task-dir" || a == "-task-dir" {
			return true
		}
		if strings.HasPrefix(a, "--task-dir=") || strings.HasPrefix(a, "-task-dir=") {
			return true
		}
		_ = i
	}
	return false
}

// dispatchAdmitChange routes to typed admission v2 when a task ledger is named,
// otherwise to the legacy bundle-based command.
func dispatchAdmitChange(args []string) int {
	if hasTaskDirFlag(args) {
		return runAdmitChangeV2Args(args)
	}
	return runAdmitChange(args)
}

func dispatchVerifyAdmission(args []string) int {
	if hasTaskDirFlag(args) {
		return runVerifyAdmissionV2Args(args)
	}
	return runVerifyAdmission(args)
}

func runAdmitChangeV2Args(args []string) int {
	var repoRoot, taskDir, expectedHead, format string
	fs := flag.NewFlagSet("sensei admit-change", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory")
	fs.StringVar(&expectedHead, "expected-head", "", "expected current ledger head digest")
	fs.StringVar(&format, "format", "text", "output format")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "admit-change:", err)
		return 2
	}
	return runAdmitChangeV2(dir, expectedHead, format)
}

func runVerifyAdmissionV2Args(args []string) int {
	var repoRoot, taskDir, expectedHead, resultRevision, format string
	fs := flag.NewFlagSet("sensei verify-admission", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory")
	fs.StringVar(&expectedHead, "expected-head", "", "expected current ledger head digest")
	fs.StringVar(&resultRevision, "result-revision", "", "result revision to observe; default observes the working tree (staged + unstaged) against the admitted base")
	fs.StringVar(&format, "format", "text", "output format")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission:", err)
		return 2
	}
	return runVerifyAdmissionV2(dir, repoRoot, expectedHead, resultRevision, format)
}

// ---- admit-change (v2) ----------------------------------------------------

func runAdmitChangeV2(dir, expectedHead, format string) int {
	head, err := verifyExpectedHead(dir, expectedHead)
	if err != nil {
		fmt.Fprintln(os.Stderr, "admit-change:", err)
		return 1
	}
	rec, err := admission.LoadRecordedAuthority(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "admit-change: no current authority resolution:", err)
		return 3
	}
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    rec.Actor,
		BaseBinding:                     rec.Base,
		ChangePlan:                      rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        strings.TrimSpace(rec.Base.Policies.Admission),
	}
	policy := admission.AdmissionV2Policy{
		PolicyID:           strings.TrimSpace(rec.Base.Policies.Admission),
		CompletionPolicyID: strings.TrimSpace(rec.Base.Policies.Completion),
		ValidityWindow:     admissionV2Window,
	}
	decision, err := admission.DecideAdmission(req, rec.Resolution, policy, nowUTC().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintln(os.Stderr, "admit-change:", err)
		return 3
	}
	if _, err := admission.RecordAdmissionDecided(newAdmissionStore(dir), head, decision, rec.Base.Task, nowUTC()); err != nil {
		fmt.Fprintln(os.Stderr, "admit-change: record:", err)
		return 1
	}
	if err := rebuildAndReport(dir); err != nil {
		fmt.Fprintln(os.Stderr, "admit-change:", err)
		return 1
	}
	if err := printValue(decision, format); err != nil {
		return 1
	}
	if !admission.AllAdmitted(decision) {
		return 3
	}
	return 0
}

// ---- consume-admission ----------------------------------------------------

func runConsumeAdmission(args []string) int {
	var repoRoot, taskDir, expectedHead, format string
	fs := flag.NewFlagSet("sensei consume-admission", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory")
	fs.StringVar(&expectedHead, "expected-head", "", "expected current ledger head digest")
	fs.StringVar(&format, "format", "text", "output format")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission:", err)
		return 2
	}
	head, err := verifyExpectedHead(dir, expectedHead)
	if err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission:", err)
		return 1
	}
	decision, err := admission.LoadRecordedDecision(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission: no admission decision:", err)
		return 3
	}
	rec, err := admission.LoadRecordedAuthority(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission:", err)
		return 1
	}
	// Single-use is durable: the append-only ledger, not a process-local
	// registry, is the source of truth. If this task already recorded an
	// admission_consumed event, a second consume is a replay and is refused.
	if prior, err := admission.LoadRecordedConsumption(dir); err == nil {
		fmt.Fprintln(os.Stderr, "consume-admission: capability already consumed:", prior.CapabilityID)
		return 3
	}
	ops := make([]string, 0, len(decision.OperationVerdicts))
	for _, v := range decision.OperationVerdicts {
		if v.Verdict == admission.AdmissionVerdictAdmitted {
			ops = append(ops, v.OperationID)
		}
	}
	consumption, err := admission.ConsumeCapability(decision, rec.Base.Task, rec.Actor, ops, nowUTC().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission:", err)
		return 3
	}
	if _, err := admission.RecordAdmissionConsumed(newAdmissionStore(dir), head, consumption, nowUTC()); err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission: record:", err)
		return 1
	}
	if err := rebuildAndReport(dir); err != nil {
		fmt.Fprintln(os.Stderr, "consume-admission:", err)
		return 1
	}
	if err := printValue(consumption, format); err != nil {
		return 1
	}
	return 0
}

// ---- verify-admission (v2) ------------------------------------------------

func runVerifyAdmissionV2(dir, repoRoot, expectedHead, resultRevision, format string) int {
	head, err := verifyExpectedHead(dir, expectedHead)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission:", err)
		return 1
	}
	decision, err := admission.LoadRecordedDecision(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission: no admission decision:", err)
		return 3
	}
	consumption, err := admission.LoadRecordedConsumption(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission: capability not consumed:", err)
		return 3
	}
	rec, err := admission.LoadRecordedAuthority(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission:", err)
		return 1
	}
	actorDigest := closureprotocol.MustSemanticDigest(rec.Actor)
	baseRev := strings.TrimSpace(rec.Base.Repository.Revision)
	observed, err := observeChange(repoRoot, baseRev, resultRevision, actorDigest, rec.Resolution.AuthorityResolutionDigestSHA256)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission: observe change:", err)
		return 1
	}

	// Record the exact observed change before verifying scope, so the ledger
	// sequence is admission_consumed -> change_observed -> scope_verified. On a
	// re-run after a crash, reconcile with any change_observed already recorded:
	// an identical observation resumes from it; a different observation is stale
	// or divergent and fails closed rather than silently replacing the first.
	observedDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission: digest observed change:", err)
		return 1
	}
	scopeHead := head
	if prior, err := admission.LoadRecordedObservedChange(dir); err == nil {
		priorDigest, derr := admission.ObservedChangeSetDigest(prior)
		if derr != nil {
			fmt.Fprintln(os.Stderr, "verify-admission:", derr)
			return 1
		}
		if priorDigest != observedDigest {
			fmt.Fprintln(os.Stderr, "verify-admission: observed change conflicts with the recorded change_observed (result stale or divergent); not replacing it")
			return 3
		}
		observed = prior // identical: resume from the recorded observation
	} else {
		res, rerr := admission.RecordChangeObserved(newAdmissionStore(dir), head, rec.Base.Task, observed, nowUTC())
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "verify-admission: record change_observed:", rerr)
			return 1
		}
		scopeHead = res.Head.EntryDigestSHA256
	}

	exp := admission.ScopeExpectation{
		Decision:                        decision,
		Operations:                      rec.ChangePlan.Operations,
		Consumption:                     consumption,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
		BaseTreeDigestSHA256:            rec.Base.Repository.TreeDigestSHA256,
		RequiredGeneratedArtifacts:      decision.RequiredResultRebuilds,
	}
	verification, err := admission.VerifyScope(exp, observed, nowUTC().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission:", err)
		return 1
	}
	if _, err := admission.RecordScopeVerified(newAdmissionStore(dir), scopeHead, rec.Base.Task, verification, nowUTC()); err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission: record:", err)
		return 1
	}
	if err := rebuildAndReport(dir); err != nil {
		fmt.Fprintln(os.Stderr, "verify-admission:", err)
		return 1
	}
	out := struct {
		ScopeOnly            bool                        `json:"scope_only" yaml:"scope_only"`
		CorrectnessCertified bool                        `json:"correctness_certified" yaml:"correctness_certified"`
		Verification         admission.ScopeVerification `json:"scope_verification" yaml:"scope_verification"`
	}{ScopeOnly: true, CorrectnessCertified: false, Verification: verification}
	if err := printValue(out, format); err != nil {
		return 1
	}
	if !admission.ScopeVerified(verification) {
		return 3
	}
	return 0
}

// observeChange computes the actual change from repository state via git,
// delegating to the resulttransition package. That package owns the single
// canonical committed/worktree observation — isolated-index worktree
// materialization, canonical tree identity, and name-status change derivation —
// so the admission command path and the Phase 7 result binding observe exactly
// the same change and cannot drift. The actor and authority digests are taken
// from the admission context; they are not caller-authoritative scope input.
func observeChange(repoRoot, baseRev, resultRev, actorDigest, authorityDigest string) (admission.ObservedChangeSet, error) {
	return resulttransition.ObserveChange(repoRoot, baseRev, resultRev, actorDigest, authorityDigest)
}
