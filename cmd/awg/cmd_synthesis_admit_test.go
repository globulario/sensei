// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// The fixtures below are built FROM a real git repository rather than from
// invented manifest bytes. That is the point: this command's whole job is to
// diff a sealed candidate against the tree an actual checkout holds at the
// candidate's base revision, and a fixture that mints its own "base manifest"
// would prove the diff logic while assuming away the only input that can
// realistically be wrong.

type admitConfig struct{ recordEarlierBase bool }

type admitOption func(*admitConfig)

// withRecordedBaseFromEarlierCommit makes the sealed candidate record an
// earlier revision than the tree it was actually generated against.
func withRecordedBaseFromEarlierCommit() admitOption {
	return func(c *admitConfig) { c.recordEarlierBase = true }
}

type admitFixture struct {
	repoDir      string
	storeDir     string
	lineagePath  string
	templatePath string
	baseRevision string
	baseManifest []runnercomposition.CandidateManifestEntry
	artifact     runnercomposition.CandidateArtifact
	taskDir      string
	taskBinding  synthesisRunTaskBinding
}

// newAdmitFixture creates a git repository with one commit, extracts its real
// base manifest, and seals a candidate whose final manifest is `mutate`d from
// that base. Every digest in the lineage chain is computed, never asserted, so
// a fixture that would not satisfy admissioncomposition's own validation fails
// here rather than silently testing a weaker path.
func newAdmitFixture(t *testing.T, mutate func(base []runnercomposition.CandidateManifestEntry) []runnercomposition.CandidateManifestEntry, opts ...admitOption) admitFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	var cfg admitConfig
	for _, o := range opts {
		o(&cfg)
	}

	repoDir := t.TempDir()
	// os.MkdirTemp can hand back a symlinked path (/var -> /private/var on
	// macOS); ExtractSnapshot requires a real directory.
	if resolved, err := filepath.EvalSymlinks(repoDir); err == nil {
		repoDir = resolved
	}
	writeFixtureFile(t, filepath.Join(repoDir, "a.txt"), "old\n")
	writeFixtureFile(t, filepath.Join(repoDir, "b.txt"), "unchanged\n")
	// The task session lives under .sensei/, which must stay OUT of the git
	// tree: synthesis-apply refuses a target whose worktree is not clean, and
	// an untracked task directory would make every apply fixture fail for a
	// reason that has nothing to do with what is being tested.
	writeFixtureFile(t, filepath.Join(repoDir, ".gitignore"), ".sensei/\n")
	gitFixture(t, repoDir, "init", "-q")
	gitFixture(t, repoDir, "config", "user.email", "fixture@example.invalid")
	gitFixture(t, repoDir, "config", "user.name", "fixture")
	gitFixture(t, repoDir, "add", "-A")
	gitFixture(t, repoDir, "commit", "-q", "-m", "base")
	baseRevision := strings.TrimSpace(gitFixture(t, repoDir, "rev-parse", "HEAD"))

	// A candidate that RECORDS one base revision while having been generated
	// against a different tree. git is content-addressed, so a revision can
	// never name two trees -- the realistic drift is exactly this: the
	// receipt's recorded base is not the tree the generation actually saw.
	snapshotRevision := baseRevision
	if cfg.recordEarlierBase {
		writeFixtureFile(t, filepath.Join(repoDir, "b.txt"), "moved on\n")
		gitFixture(t, repoDir, "add", "-A")
		gitFixture(t, repoDir, "commit", "-q", "-m", "second")
		snapshotRevision = strings.TrimSpace(gitFixture(t, repoDir, "rev-parse", "HEAD"))
	}

	_, baseManifest, baseDigest, cleanup, err := runnercomposition.ExtractSnapshot(ctx, repoDir, snapshotRevision)
	if err != nil {
		t.Fatalf("ExtractSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })

	final := mutate(baseManifest)
	finalDigest, err := runnercomposition.ManifestDigest(final)
	if err != nil {
		t.Fatal(err)
	}

	sessionDigest := fixtureHex(t, "session")
	attemptDigest := fixtureHex(t, "attempt")
	evaluationDigest := fixtureHex(t, "evaluation")
	proposedChangeDigest := fixtureHex(t, "change")

	artifact := runnercomposition.CandidateArtifact{
		SchemaVersion:                     runnercomposition.CandidateArtifactSchemaVersion,
		RepositoryDomain:                  "github.com/globulario/sensei",
		BaseRevision:                      baseRevision,
		WorkspaceIdentityDigestSHA256:     fixtureHex(t, "workspace"),
		SessionDigestSHA256:               sessionDigest,
		PlanDigestSHA256:                  fixtureHex(t, "plan"),
		PlanGeneration:                    1,
		AttemptNumber:                     1,
		InputCandidateDigestSHA256:        baseDigest,
		ProposedChangeDigestSHA256:        proposedChangeDigest,
		FinalCandidateContentDigestSHA256: finalDigest,
		Manifest:                          final,
	}
	artifactDigest, err := runnercomposition.CandidateArtifactDigest(artifact)
	if err != nil {
		t.Fatal(err)
	}
	artifact.CandidateArtifactDigestSHA256 = artifactDigest

	resultDigest := fixtureHex(t, "result")
	o2Digest := fixtureHex(t, "o2")
	requestDigest := fixtureHex(t, "request")
	cleanupSucceeded := true
	runner := runnercomposition.RunnerReceipt{
		SchemaVersion:                     runnercomposition.RunnerReceiptSchemaVersion,
		ReceiptID:                         "runner.receipt",
		RequestDigestSHA256:               requestDigest,
		ResultDigestSHA256:                &resultDigest,
		O2ReceiptDigestSHA256:             &o2Digest,
		InputCandidateDigestSHA256:        &baseDigest,
		ProposedChangeDigestSHA256:        &proposedChangeDigest,
		FinalCandidateContentDigestSHA256: &finalDigest,
		CandidateArtifactDigestSHA256:     &artifactDigest,
		Disposition:                       runnercomposition.DispositionVerified,
		CleanupSucceeded:                  &cleanupSucceeded,
		CompletedAt:                       "2026-08-01T22:00:00Z",
	}
	runnerDigest, err := runnercomposition.RunnerReceiptDigest(runner)
	if err != nil {
		t.Fatal(err)
	}
	runner.RunnerReceiptDigestSHA256 = runnerDigest

	o1 := synthesis.Receipt{
		SchemaVersion:               synthesis.ReceiptSchemaVersion,
		ReceiptID:                   "synthesis.receipt",
		SessionDigestSHA256:         sessionDigest,
		TerminalReason:              synthesis.ReasonCandidateReadyForAdmission,
		FinalAttemptDigestSHA256:    &attemptDigest,
		FinalEvaluationDigestSHA256: &evaluationDigest,
		Summary:                     "candidate ready for admission",
		Limitations:                 []synthesis.Limitation{},
		CompletedAt:                 "2026-08-01T22:00:00Z",
	}
	o1Digest, err := synthesis.ReceiptDigest(o1)
	if err != nil {
		t.Fatal(err)
	}
	o1.ReceiptDigestSHA256 = o1Digest

	o4 := evaluatorcomposition.EvaluationReceipt{
		SchemaVersion:                 evaluatorcomposition.EvaluationReceiptSchemaVersion,
		ReceiptID:                     "evaluation.receipt",
		SessionDigestSHA256:           sessionDigest,
		AttemptDigestSHA256:           attemptDigest,
		RunnerReceiptDigestSHA256:     runnerDigest,
		RequestDigestSHA256:           requestDigest,
		ResultDigestSHA256:            resultDigest,
		O2ReceiptDigestSHA256:         o2Digest,
		PolicyDigestSHA256:            fixtureHex(t, "policy"),
		CandidateArtifactDigestSHA256: artifactDigest,
		CandidateArtifactVerified:     true,
		EvaluatorResultBindings:       []evaluatorcomposition.EvaluatorResultBinding{},
		EvaluationDigestSHA256:        &evaluationDigest,
		O1TerminalReceiptDigestSHA256: &o1Digest,
		Disposition:                   evaluatorcomposition.DispositionEvaluated,
		CleanupSucceeded:              &cleanupSucceeded,
		CompletedAt:                   "2026-08-01T22:00:00Z",
	}
	o4Digest, err := evaluatorcomposition.EvaluationReceiptDigest(o4)
	if err != nil {
		t.Fatal(err)
	}
	o4.ReceiptDigestSHA256 = o4Digest

	storeDir := filepath.Join(t.TempDir(), "candidates")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(storeDir); err == nil {
		storeDir = resolved
	}
	store, err := runnercomposition.NewFSCandidateArtifactStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, artifact); err != nil {
		t.Fatalf("seal candidate: %v", err)
	}

	// The task binds to the repository's ACTUAL head, which is not always the
	// candidate's base revision -- withRecordedBaseFromEarlierCommit
	// deliberately moves them apart. Passing the base here would make the
	// claim document's binding disagree with the revision Prepare resolves,
	// and the fixture would fail for a reason unrelated to what it tests.
	taskDir, taskBinding := prepareFixtureTask(t, repoDir, artifact.RepositoryDomain,
		strings.TrimSpace(gitFixture(t, repoDir, "rev-parse", "HEAD")))

	lineage := synthesisRunLineage{
		SchemaVersion:                 synthesisRunLineageSchemaVersion,
		TaskBinding:                   taskBinding,
		CandidateArtifactDigestSHA256: artifactDigest,
		CandidateArtifactPath:         filepath.Join(storeDir, artifactDigest+".json"),
		SynthesisReceipt:              o1,
		RunnerReceipt:                 runner,
		EvaluationReceipt:             o4,
	}
	lineageBytes, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lineageName := artifactDigest + ".lineage.json"
	if err := store.PutAuxiliaryFile(ctx, lineageName, lineageBytes); err != nil {
		t.Fatal(err)
	}

	template := admission.Request{
		SchemaVersion: admission.SchemaVersion,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  artifact.RepositoryDomain,
			Revision:          artifact.BaseRevision,
			RevisionStatus:    "resolved",
			TreeDigestSHA256:  fixtureHex(t, "tree"),
			GraphDigestSHA256: fixtureHex(t, "graph"),
			GraphDigestStatus: "resolved",
		},
		Convergence: admission.ConvergenceBinding{
			SessionID:                 "session",
			IterationDigestSHA256:     fixtureHex(t, "iteration"),
			SemanticStateDigestSHA256: fixtureHex(t, "semantic"),
		},
		Mode:      admission.ModeModify,
		TaskClass: "implementation",
		// A real template always carries the task's DECLARED scope
		// (prepare-change writes it from the task request), and admission
		// refuses a modify request with an empty one. Naming a file the
		// candidate does not touch is what makes
		// TestSynthesisAdmitIgnoresTheTemplateScope meaningful: the composed
		// request must describe what the candidate changed, not what the task
		// said it would.
		Scope:                admission.ChangeScope{Files: []admission.FileOperation{{Path: "b.txt", Operation: admission.OperationModify}}},
		AcceptedConditionIDs: []string{},
		RequestedBy:          "fixture",
	}
	templateBytes, err := admission.MarshalCanonicalRequestYAML(template)
	if err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(t.TempDir(), "admission-request.yaml")
	if err := os.WriteFile(templatePath, templateBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	return admitFixture{
		repoDir:      repoDir,
		storeDir:     storeDir,
		lineagePath:  filepath.Join(storeDir, lineageName),
		templatePath: templatePath,
		baseRevision: baseRevision,
		baseManifest: baseManifest,
		artifact:     artifact,
		taskDir:      taskDir,
		taskBinding:  taskBinding,
	}
}

func (f admitFixture) run(extra ...string) int {
	args := append([]string{
		"--repo", f.repoDir,
		"--lineage", f.lineagePath,
		"--admission-template", f.templatePath,
	}, extra...)
	return runSynthesisAdmit(args)
}

func modifyOne(base []runnercomposition.CandidateManifestEntry) []runnercomposition.CandidateManifestEntry {
	out := cloneManifest(base)
	for i := range out {
		if out[i].Path == "a.txt" {
			out[i].Content = []byte("new\n")
			out[i].ContentDigestSHA256 = fixtureContentDigest([]byte("new\n"))
		}
	}
	return out
}

// The happy path: one modified file becomes one derived modify operation, and
// the concrete admission request lands where `sensei admit-change --request`
// can be pointed at it.
func TestSynthesisAdmitComposesTheAdmissionRequestForAModifiedFile(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)
	if code := f.run(); code != exitAdmissionRequestComposed {
		t.Fatalf("exit = %d, want %d", code, exitAdmissionRequestComposed)
	}

	requestPath := filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml")
	req, err := admission.LoadRequest(requestPath)
	if err != nil {
		t.Fatalf("the composed admission request is not loadable by the command that must consume it: %v", err)
	}
	if req.Mode != admission.ModeModify {
		t.Errorf("mode = %q, want %q", req.Mode, admission.ModeModify)
	}
	if len(req.Scope.Files) != 1 || req.Scope.Files[0].Path != "a.txt" || req.Scope.Files[0].Operation != admission.OperationModify {
		t.Fatalf("derived scope = %+v, want exactly one modify of a.txt", req.Scope.Files)
	}
	if req.Binding.Revision != f.baseRevision {
		t.Errorf("binding revision = %q, want the candidate's base %q", req.Binding.Revision, f.baseRevision)
	}

	var composed admissioncomposition.Request
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5a-request.json"), &composed)
	if !composed.AdmissionEligible {
		t.Error("composed request is not marked admission-eligible")
	}
	if composed.CandidateArtifactDigestSHA256 != f.artifact.CandidateArtifactDigestSHA256 {
		t.Error("composed request is not bound to the sealed candidate")
	}
}

// The derived scope must come from the sealed manifests, not from the
// template. A template naming a different file entirely must not survive into
// the request -- that is precisely the hand-authoring hazard this command
// exists to remove.
func TestSynthesisAdmitIgnoresTheTemplateScope(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)

	// The fixture template declares b.txt; the candidate modifies a.txt.
	if code := f.run(); code != exitAdmissionRequestComposed {
		t.Fatalf("exit = %d, want %d", code, exitAdmissionRequestComposed)
	}
	req, err := admission.LoadRequest(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Scope.Files) != 1 || req.Scope.Files[0].Path != "a.txt" {
		t.Fatalf("scope = %+v; the template's declared b.txt must not survive, and the candidate's a.txt must", req.Scope.Files)
	}
}

// An added file is outside admission's operation vocabulary. That is a
// governed refusal with a receipt, not an error and not a silent success.
func TestSynthesisAdmitRefusesAnUnsupportedOperationWithAReceipt(t *testing.T) {
	f := newAdmitFixture(t, func(base []runnercomposition.CandidateManifestEntry) []runnercomposition.CandidateManifestEntry {
		out := modifyOne(base)
		return append(out, runnercomposition.CandidateManifestEntry{
			Path:                "c.txt",
			Mode:                runnercomposition.ModeRegular,
			Content:             []byte("added\n"),
			ContentDigestSHA256: fixtureContentDigest([]byte("added\n")),
		})
	})
	if code := f.run(); code != exitUnsupportedOperationRefused {
		t.Fatalf("exit = %d, want %d", code, exitUnsupportedOperationRefused)
	}
	if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml")); !os.IsNotExist(err) {
		t.Fatal("a refused candidate must not produce an admission request to evaluate")
	}
	var receipt admissioncomposition.Receipt
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5a-receipt.json"), &receipt)
	if receipt.Disposition != admissioncomposition.DispositionUnsupportedOperationRefused {
		t.Fatalf("receipt disposition = %q", receipt.Disposition)
	}
	if !strings.Contains(receipt.Detail, "c.txt") {
		t.Errorf("the refusal does not name what it refused: %q", receipt.Detail)
	}
}

// "Refused because admission has no vocabulary for this" and "there is nothing
// here to admit" are different facts. Collapsing them would send an operator
// hunting for a defect in the second case.
func TestSynthesisAdmitDistinguishesANoOpCandidateFromARefusal(t *testing.T) {
	f := newAdmitFixture(t, cloneManifest)
	if code := f.run(); code != exitCandidateChangesNothing {
		t.Fatalf("exit = %d, want %d (a no-op candidate is not an unsupported operation)", code, exitCandidateChangesNothing)
	}
	if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5a-receipt.json")); !os.IsNotExist(err) {
		t.Error("a no-op candidate must not be recorded as an unsupported-operation refusal")
	}
	if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml")); !os.IsNotExist(err) {
		t.Error("a candidate that changes nothing must not produce an admission request")
	}
}

// Hard law 10: the base binding is refused on drift rather than silently
// re-derived against whatever the checkout happens to hold now. Without this,
// a candidate built against one tree would produce a scope describing a
// different one, and every downstream digest would still verify.
func TestSynthesisAdmitRefusesBaseRevisionDrift(t *testing.T) {
	f := newAdmitFixture(t, modifyOne, withRecordedBaseFromEarlierCommit())
	if code := f.run(); code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d -- a candidate whose recorded base is not the tree it was generated against must be refused, not re-derived against whatever the checkout holds", code, exitResolutionFailure)
	}
	if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml")); !os.IsNotExist(err) {
		t.Error("drift produced an admission request anyway")
	}
}

// The checkout simply not containing the candidate's base revision is a
// distinct, equally refusable input -- not something to work around by using
// the current tree.
func TestSynthesisAdmitRefusesAnUnrelatedCheckout(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)

	other := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(other); err == nil {
		other = resolved
	}
	writeFixtureFile(t, filepath.Join(other, "a.txt"), "old\n")
	gitFixture(t, other, "init", "-q")
	gitFixture(t, other, "add", "-A")
	gitFixture(t, other, "commit", "-q", "-m", "unrelated")

	if code := f.run("--repo", other); code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
	}
}

// The store root is the lineage bundle's own directory. A bundle whose
// recorded CandidateArtifactPath points somewhere else must not redirect the
// read -- that string is data written by a previous process, not authority.
func TestSynthesisAdmitDoesNotFollowTheRecordedCandidatePath(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)

	data, err := os.ReadFile(f.lineagePath)
	if err != nil {
		t.Fatal(err)
	}
	var lineage synthesisRunLineage
	if err := json.Unmarshal(data, &lineage); err != nil {
		t.Fatal(err)
	}
	lineage.CandidateArtifactPath = filepath.Join(t.TempDir(), "elsewhere.json")
	rewritten, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.lineagePath, rewritten, 0o644); err != nil {
		t.Fatal(err)
	}

	if code := f.run(); code != exitAdmissionRequestComposed {
		t.Fatalf("exit = %d, want %d -- the read must follow the bundle's own directory, not its recorded path", code, exitAdmissionRequestComposed)
	}
}

// A bundle that is not the shape this command's own writer produces is
// refused, including one carrying extra fields: silently dropping an unknown
// field would let a future, richer bundle be read as if the parts this version
// cannot see did not exist.
func TestSynthesisAdmitRefusesAMalformedLineageBundle(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)

	for name, body := range map[string]string{
		"wrong schema": `{"schema_version":"sensei.synthesis-run.lineage.v99","candidate_artifact_digest_sha256":"x"}`,
		"unknown field": `{"schema_version":"` + synthesisRunLineageSchemaVersion +
			`","candidate_artifact_digest_sha256":"x","future_field":1}`,
		"no candidate": `{"schema_version":"` + synthesisRunLineageSchemaVersion + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(f.lineagePath, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if code := f.run(); code != exitResolutionFailure {
				t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
			}
		})
	}
}

// Invocation errors stay distinct from resolution failures so a caller can
// tell "you called this wrong" from "the inputs did not line up".
func TestSynthesisAdmitInvocationErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"no lineage":     {"--repo", "."},
		"bad format":     {"--lineage", "x.json", "--format", "xml"},
		"stray argument": {"--lineage", "x.json", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			if code := runSynthesisAdmit(args); code != exitInvalidInvocation {
				t.Fatalf("exit = %d, want %d", code, exitInvalidInvocation)
			}
		})
	}
}

// A template bound to a different repository or revision than the candidate is
// refused rather than grafted, because the composed request would otherwise
// claim a convergence iteration that never saw this candidate's base.
func TestSynthesisAdmitRefusesATemplateBoundToAnotherBase(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)
	tpl, err := admission.LoadRequest(f.templatePath)
	if err != nil {
		t.Fatal(err)
	}
	tpl.Binding.Revision = strings.Repeat("0", 40)
	tplBytes, err := admission.MarshalCanonicalRequestYAML(tpl)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.templatePath, tplBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := f.run(); code != exitResolutionFailure {
		t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
	}
}

// --- fixture helpers -------------------------------------------------------

func cloneManifest(in []runnercomposition.CandidateManifestEntry) []runnercomposition.CandidateManifestEntry {
	out := make([]runnercomposition.CandidateManifestEntry, len(in))
	for i, e := range in {
		e.Content = append([]byte{}, e.Content...)
		out[i] = e
	}
	return out
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func readJSONFixture(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func fixtureHex(t *testing.T, seed string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func fixtureContentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// The same drift refusal guards composition, not only application: admitting a
// candidate whose task has moved would produce an admission request bound to a
// closure state that is no longer current.
func TestSynthesisAdmitRefusesTaskAndClosureDrift(t *testing.T) {
	for name, mutate := range map[string]func(*synthesisRunTaskBinding){
		"another task":        func(b *synthesisRunTaskBinding) { b.TaskID = "task.implementation.elsewhere" },
		"control state moved": func(b *synthesisRunTaskBinding) { b.TaskControlStateDigestSHA256 = strings.Repeat("a", 64) },
		"closure state moved": func(b *synthesisRunTaskBinding) { b.ClosureReportDigestSHA256 = strings.Repeat("b", 64) },
		"no binding at all":   func(b *synthesisRunTaskBinding) { *b = synthesisRunTaskBinding{} },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAdmitFixture(t, modifyOne)
			rewriteLineageTaskBinding(t, f.lineagePath, mutate)
			if code := f.run(); code != exitResolutionFailure {
				t.Fatalf("exit = %d, want %d", code, exitResolutionFailure)
			}
			if _, err := os.Stat(filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".admission-request.yaml")); !os.IsNotExist(err) {
				t.Error("a drifted candidate produced an admission request anyway")
			}
		})
	}
}

// And an unmoved task must still compose, or the check above would pass by
// refusing everything.
func TestSynthesisAdmitAcceptsAnUnmovedTask(t *testing.T) {
	f := newAdmitFixture(t, modifyOne)
	if code := f.run(); code != exitAdmissionRequestComposed {
		t.Fatalf("exit = %d, want %d -- an unmoved task must not be reported as drift", code, exitAdmissionRequestComposed)
	}
}
