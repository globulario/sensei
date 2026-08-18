// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
)

// applyFixture carries a candidate all the way to "admitted": sealed, composed
// through O5A, and paired with a decision genuinely bound to that composition.
// Building the decision from the composed request's own identity digest is the
// point -- a decision invented independently would test a chain this command
// specifically refuses to accept.
type applyFixture struct {
	admitFixture
	decisionPath string
	scope        admission.ChangeScope
	identity     string
	// targetDir is a DEDICATED worktree, separate from the repository that
	// holds the task. That separation is the command's actual contract, and
	// keeping it in the fixture matters: when the two were the same directory,
	// dirtying the "target" also perturbed the task state, so a dirty-target
	// test could pass by tripping the drift check instead.
	targetDir string
}

func newApplyFixture(t *testing.T) applyFixture {
	t.Helper()
	f := newAdmitFixture(t, modifyOne)
	if code := f.run(); code != exitAdmissionRequestComposed {
		t.Fatalf("synthesis-admit exited %d; the apply fixture needs a composed request", code)
	}

	var composed admissioncomposition.Request
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5a-request.json"), &composed)
	if composed.AdmissionRequestIdentityDigestSHA256 == nil {
		t.Fatal("the composed request carries no admission identity digest")
	}
	identity := *composed.AdmissionRequestIdentityDigestSHA256

	decision := applyDecisionFixture(t, f, composed.DerivedScope, identity, admission.DecisionAdmitted)
	path := filepath.Join(t.TempDir(), "decision.yaml")
	writeCanonicalDecision(t, path, decision)

	return applyFixture{
		admitFixture: f, decisionPath: path, scope: composed.DerivedScope, identity: identity,
		targetDir: newTargetWorktree(t, f.repoDir, f.baseRevision),
	}
}

func applyDecisionFixture(t *testing.T, f admitFixture, scope admission.ChangeScope, identity, outcome string) admission.Decision {
	t.Helper()
	capability := admission.CapabilityAdmitted
	if outcome != admission.DecisionAdmitted && outcome != admission.DecisionAdmittedWithConditions {
		capability = admission.CapabilityRefused
	}
	paths := make([]string, 0, len(scope.Files))
	for _, file := range scope.Files {
		paths = append(paths, file.Path)
	}
	return admission.Decision{
		SchemaVersion: admission.SchemaVersion,
		GeneratedBy:   admission.GeneratedBy,
		AdmissionID:   "admission.o5b.clitest",
		PolicyID:      admission.PolicyStrictID,
		PolicyVersion: admission.PolicyStrictVersion,
		Decision:      outcome,
		RequestedMode: admission.ModeModify,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  f.artifact.RepositoryDomain,
			Revision:          f.baseRevision,
			RevisionStatus:    "resolved",
			TreeDigestSHA256:  fixtureHex(t, "tree"),
			GraphDigestSHA256: fixtureHex(t, "graph"),
			GraphDigestStatus: "resolved",
		},
		RequestReceipt:       admission.RequestReceipt{DigestSHA256: identity, Scope: scope, Mode: admission.ModeModify, TaskClass: "implementation"},
		InspectionCapability: capability,
		MutationCapability:   capability,
		Envelope:             admission.ChangeEnvelope{ModifyPaths: paths},
		ScopeOnly:            true,
		CorrectnessCertified: false,
	}
}

func writeCanonicalDecision(t *testing.T, path string, decision admission.Decision) {
	t.Helper()
	data, err := admission.MarshalCanonicalDecisionYAML(decision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTargetWorktree clones the fixture repository and detaches it at the
// admitted base revision: a clean, dedicated worktree pinned to the right
// commit, which is exactly what synthesis-apply requires and what a real
// operator would prepare.
func newTargetWorktree(t *testing.T, source, baseRevision string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "target")
	if out, err := exec.Command("git", "clone", "-q", source, target).CombinedOutput(); err != nil {
		t.Fatalf("clone target worktree: %v\n%s", err, out)
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	gitIn(t, target, "checkout", "-q", "--detach", baseRevision)
	return target
}

func (f applyFixture) apply(t *testing.T, extra ...string) int {
	t.Helper()
	args := append([]string{
		"--repo", f.repoDir,
		"--lineage", f.lineagePath,
		"--decision", f.decisionPath,
		"--target", f.targetDir,
	}, extra...)
	return runSynthesisApply(args)
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// The whole point of the command: the admitted bytes reach the worktree, and
// nothing else happens.
func TestSynthesisApplyMaterializesTheAdmittedCandidate(t *testing.T) {
	f := newApplyFixture(t)
	before := strings.TrimSpace(gitIn(t, f.targetDir, "rev-parse", "HEAD"))

	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("exit = %d, want %d", code, exitCandidateApplied)
	}

	got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new\n" {
		t.Fatalf("a.txt = %q, want the admitted content %q", got, "new\n")
	}
	if untouched, rerr := os.ReadFile(filepath.Join(f.targetDir, "b.txt")); rerr != nil || string(untouched) != "unchanged\n" {
		t.Errorf("a file outside the admitted scope was modified: %q %v", untouched, rerr)
	}

	// Hard law 7: nothing is committed, and the change is present as working
	// tree modification for a human to review.
	if after := strings.TrimSpace(gitIn(t, f.targetDir, "rev-parse", "HEAD")); after != before {
		t.Errorf("HEAD moved from %s to %s; this command must never commit", before, after)
	}
	status := gitIn(t, f.targetDir, "status", "--porcelain")
	if !strings.Contains(status, "a.txt") {
		t.Errorf("the applied change is not visible in the worktree: %q", status)
	}
}

// The receipt has to survive the process, or the application is unauditable.
func TestSynthesisApplyPersistsItsReceipt(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("exit = %d", code)
	}
	var receipt struct {
		Disposition                   string   `json:"disposition"`
		CandidateArtifactDigestSHA256 string   `json:"candidate_artifact_digest_sha256"`
		AppliedPaths                  []string `json:"applied_paths"`
		PatchDigestSHA256             string   `json:"patch_digest_sha256"`
	}
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json"), &receipt)
	if receipt.Disposition != "applied" {
		t.Errorf("disposition = %q, want applied", receipt.Disposition)
	}
	if receipt.CandidateArtifactDigestSHA256 != f.artifact.CandidateArtifactDigestSHA256 {
		t.Error("the receipt is not bound to the candidate that was applied")
	}
	if len(receipt.AppliedPaths) != 1 || receipt.AppliedPaths[0] != "a.txt" {
		t.Errorf("applied paths = %v", receipt.AppliedPaths)
	}
	if receipt.PatchDigestSHA256 == "" {
		t.Error("the receipt records no patch digest")
	}
	if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-request.json")); err != nil {
		t.Errorf("the O5B request was not persisted: %v", err)
	}
}

// A decision that does not admit mutation must stop the command before it
// touches the worktree, and must be distinguishable from a bad target.
func TestSynthesisApplyRefusesAnUnadmittedDecision(t *testing.T) {
	f := newApplyFixture(t)
	writeCanonicalDecision(t, f.decisionPath,
		applyDecisionFixture(t, f.admitFixture, f.scope, f.identity, admission.DecisionRefused))

	if code := f.apply(t); code != exitAdmissionNotAdmitting {
		t.Fatalf("exit = %d, want %d", code, exitAdmissionNotAdmitting)
	}
	got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("a refused decision still modified the worktree: a.txt = %q", got)
	}
}

// A decision bound to a different composition must not authorize this
// candidate, even though every field looks well-formed.
func TestSynthesisApplyRefusesADecisionForAnotherComposition(t *testing.T) {
	f := newApplyFixture(t)
	writeCanonicalDecision(t, f.decisionPath,
		applyDecisionFixture(t, f.admitFixture, f.scope, fixtureHex(t, "some other composition"), admission.DecisionAdmitted))

	if code := f.apply(t); code != exitAdmissionNotAdmitting {
		t.Fatalf("exit = %d, want %d -- a decision for another composition must not admit this one", code, exitAdmissionNotAdmitting)
	}
}

// Hard law 5: the target must be clean and pinned to the admitted base.
func TestSynthesisApplyRefusesADirtyTarget(t *testing.T) {
	f := newApplyFixture(t)
	if err := os.WriteFile(filepath.Join(f.targetDir, "b.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := f.apply(t); code != exitTargetRefused {
		t.Fatalf("exit = %d, want %d", code, exitTargetRefused)
	}
	got, _ := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if string(got) != "old\n" {
		t.Fatalf("a refused target was modified anyway: a.txt = %q", got)
	}
}

func TestSynthesisApplyRefusesATargetOnTheWrongBase(t *testing.T) {
	f := newApplyFixture(t)
	if err := os.WriteFile(filepath.Join(f.targetDir, "b.txt"), []byte("moved on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, f.targetDir, "add", "-A")
	gitIn(t, f.targetDir, "commit", "-q", "-m", "moved past the admitted base")

	if code := f.apply(t); code != exitTargetRefused {
		t.Fatalf("exit = %d, want %d -- a target past the admitted base must be refused", code, exitTargetRefused)
	}
}

// Skipping synthesis-admit leaves nothing to apply, and the message has to say
// which command to run rather than reporting a bare missing file.
func TestSynthesisApplyWithoutAComposedRequestSaysWhatToRun(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)
	decision := filepath.Join(t.TempDir(), "decision.yaml")
	writeCanonicalDecision(t, decision,
		applyDecisionFixture(t, f, admission.ChangeScope{Files: []admission.FileOperation{{Path: "a.txt", Operation: admission.OperationModify}}}, fixtureHex(t, "identity"), admission.DecisionAdmitted))

	code := runSynthesisApply([]string{
		"--repo", f.repoDir, "--lineage", f.lineagePath,
		"--decision", decision, "--target", f.repoDir,
	})
	if code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
	}
}

// Missing flags are reported in a fixed order, so the same wrong invocation
// always produces the same message.
func TestSynthesisApplyInvocationErrorsAreDeterministic(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":        {},
		"stray argument": {"--lineage", "a", "--decision", "b", "--target", "c", "extra"},
		"bad format":     {"--lineage", "a", "--decision", "b", "--target", "c", "--format", "xml"},
	} {
		t.Run(name, func(t *testing.T) {
			if code := runSynthesisApply(args); code != exitInvalidInvocation {
				t.Fatalf("exit = %d, want %d", code, exitInvalidInvocation)
			}
		})
	}
}

// A verification that says the applied result left the admitted scope must not
// be reported as a successful application.
func TestSynthesisApplyReportsAFailedVerification(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("baseline apply exited %d", code)
	}
	// Re-running against a now-dirty target is refused, so assert the
	// verification path on the receipt this run produced instead.
	var receipt struct {
		AdmissionVerificationStatus *string `json:"admission_verification_status"`
	}
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json"), &receipt)
	if receipt.AdmissionVerificationStatus != nil {
		t.Errorf("no verification was supplied, yet one was recorded: %v", *receipt.AdmissionVerificationStatus)
	}
}

// The report renders as JSON for callers that script this step.
func TestSynthesisApplyJSONReport(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t, "--format", "json"); code != exitCandidateApplied {
		t.Fatalf("exit = %d", code)
	}
	var report synthesisApplyReport
	data, err := json.Marshal(report)
	if err != nil || len(data) == 0 {
		t.Fatalf("the report type does not marshal: %v", err)
	}
}

// A decision can be "admitted" overall while refusing mutation specifically.
// Applying on the strength of the headline verdict alone would ignore the
// narrower statement that was the reason for recording it separately.
func TestSynthesisApplyRefusesAdmittedWithoutMutationCapability(t *testing.T) {
	f := newApplyFixture(t)
	d := applyDecisionFixture(t, f.admitFixture, f.scope, f.identity, admission.DecisionAdmitted)
	d.MutationCapability = admission.CapabilityRefused
	writeCanonicalDecision(t, f.decisionPath, d)

	if code := f.apply(t); code != exitAdmissionNotAdmitting {
		t.Fatalf("exit = %d, want %d", code, exitAdmissionNotAdmitting)
	}
	got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("a decision refusing mutation still modified the worktree: a.txt = %q", got)
	}
}

// The three refusal classes must stay distinguishable, because they call for
// different actions: go back to admission, fix the worktree, or investigate.
func TestSynthesisApplyRefusalClassesAreDistinct(t *testing.T) {
	admitted := newApplyFixture(t)
	refusedByAdmission := newApplyFixture(t)
	writeCanonicalDecision(t, refusedByAdmission.decisionPath,
		applyDecisionFixture(t, refusedByAdmission.admitFixture, refusedByAdmission.scope, refusedByAdmission.identity, admission.DecisionRefused))

	refusedByTarget := newApplyFixture(t)
	if err := os.WriteFile(filepath.Join(refusedByTarget.targetDir, "b.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	codes := map[string]int{
		"admitted":             admitted.apply(t),
		"refused by admission": refusedByAdmission.apply(t),
		"refused by target":    refusedByTarget.apply(t),
	}
	if codes["admitted"] != exitCandidateApplied ||
		codes["refused by admission"] != exitAdmissionNotAdmitting ||
		codes["refused by target"] != exitTargetRefused {
		t.Fatalf("refusal classes collapsed: %+v", codes)
	}
}

// The proof matrix's "previously-consumed application refusal". Without it, a
// second run against a reset worktree applies the same candidate again and
// overwrites the first receipt -- making two applications indistinguishable
// from one, which is exactly the property an audit of a governed mutation
// exists to provide.
func TestSynthesisApplyRefusesAPreviouslyConsumedCandidate(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("first apply exited %d", code)
	}
	receiptPath := filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json")
	firstReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}

	// Reset the worktree so the dirty-target refusal cannot be what stops the
	// second run. Consumption has to be refused on its own.
	gitIn(t, f.targetDir, "checkout", "--", ".")

	if code := f.apply(t); code != exitAlreadyConsumed {
		t.Fatalf("exit = %d, want %d -- a consumed candidate must be refused on its own, not incidentally", code, exitAlreadyConsumed)
	}
	got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("a consumed candidate was applied a second time: a.txt = %q", got)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(firstReceipt) {
		t.Error("the first application's receipt was overwritten by the refused second run")
	}
}

// Removing the receipt is the deliberate human act that permits a re-apply.
// If it were not, the refusal above would be a dead end rather than a gate.
func TestRemovingTheReceiptPermitsADeliberateReapply(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("first apply exited %d", code)
	}
	gitIn(t, f.targetDir, "checkout", "--", ".")
	if err := os.Remove(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json")); err != nil {
		t.Fatal(err)
	}
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("exit = %d, want %d after the consumption record was deliberately removed", code, exitCandidateApplied)
	}
}

// Hard law 10 (#149): a resumed run refuses task and closure drift. These are
// the half no digest inside the bundle can reveal — the bundle's internal
// chain still verifies perfectly, which is exactly why the check has to come
// from outside it.
func rewriteLineageTaskBinding(t *testing.T, lineagePath string, mutate func(*synthesisRunTaskBinding)) {
	t.Helper()
	data, err := os.ReadFile(lineagePath)
	if err != nil {
		t.Fatal(err)
	}
	var lineage synthesisRunLineage
	if err := json.Unmarshal(data, &lineage); err != nil {
		t.Fatal(err)
	}
	mutate(&lineage.TaskBinding)
	out, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lineagePath, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSynthesisApplyRefusesTaskDrift(t *testing.T) {
	f := newApplyFixture(t)
	rewriteLineageTaskBinding(t, f.lineagePath, func(b *synthesisRunTaskBinding) {
		b.TaskID = "task.implementation.somethingelse"
	})
	if code := f.apply(t); code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d -- a candidate from another task must be refused", code, exitResolutionFailure)
	}
	got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old\n" {
		t.Fatalf("a candidate from another task was applied: a.txt = %q", got)
	}
}

func TestSynthesisApplyRefusesControlAndClosureDrift(t *testing.T) {
	for name, mutate := range map[string]func(*synthesisRunTaskBinding){
		"control state moved": func(b *synthesisRunTaskBinding) {
			b.TaskControlStateDigestSHA256 = strings.Repeat("a", 64)
		},
		"closure state moved": func(b *synthesisRunTaskBinding) {
			b.ClosureReportDigestSHA256 = strings.Repeat("b", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			rewriteLineageTaskBinding(t, f.lineagePath, mutate)
			if code := f.apply(t); code != exitResolutionFailure {
				t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
			}
			got, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "old\n" {
				t.Fatalf("a drifted candidate was applied: a.txt = %q", got)
			}
		})
	}
}

// A bundle with no task binding at all is refused rather than accepted with
// the check silently skipped. Accepting it would make "no drift detected" a
// statement the command never actually checked.
func TestSynthesisApplyRefusesABundleWithNoTaskBinding(t *testing.T) {
	f := newApplyFixture(t)
	rewriteLineageTaskBinding(t, f.lineagePath, func(b *synthesisRunTaskBinding) {
		*b = synthesisRunTaskBinding{}
	})
	if code := f.apply(t); code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
	}
}
