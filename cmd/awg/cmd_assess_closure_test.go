// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/rdf"
)

func TestAssessClosureRequiresRequestAndClaims(t *testing.T) {
	if code := runAssessClosure(nil); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestAssessClosureRendersUncertifiableWhenArtifactsOmitted(t *testing.T) {
	fx := closureCLIFixture(t)
	opts := assessClosureOptions{Request: fx.request, Claims: fx.claims, Repo: ".", GraphDigestStatus: architecture.GraphDigestNotRequested, Format: "yaml"}
	out, report, err := buildAssessClosureOutput(opts)
	if err != nil {
		t.Fatalf("buildAssessClosureOutput: %v", err)
	}
	if report.Verdict != closure.VerdictUncertifiable {
		t.Fatalf("verdict = %s", report.Verdict)
	}
	if !bytes.Contains(out, []byte("architecture_closure_assessment:")) {
		t.Fatalf("missing closure envelope:\n%s", out)
	}
}

func TestAssessClosureRequireClosedPassesOnlyClosed(t *testing.T) {
	fx := closureCLIFixture(t)
	opts := fx.closedOptions()
	_, report, err := buildAssessClosureOutput(opts)
	if err != nil {
		t.Fatalf("buildAssessClosureOutput: %v", err)
	}
	if report.Verdict != closure.VerdictClosed {
		t.Fatalf("verdict = %s blockers=%#v", report.Verdict, report.Blockers)
	}
	if code := assessClosureStrictExit(assessClosureOptions{RequireClosed: true}, report); code != 0 {
		t.Fatalf("strict exit = %d", code)
	}
	report.Verdict = closure.VerdictConditionallyClosed
	if code := assessClosureStrictExit(assessClosureOptions{RequireClosed: true}, report); code != 1 {
		t.Fatalf("conditional strict exit = %d", code)
	}
}

func TestAssessClosureCheckFreshAndStale(t *testing.T) {
	fx := closureCLIFixture(t)
	opts := fx.closedOptions()
	out, _, err := buildAssessClosureOutput(opts)
	if err != nil {
		t.Fatalf("buildAssessClosureOutput: %v", err)
	}
	opts.Output = filepath.Join(fx.dir, "closure.yaml")
	if err := os.WriteFile(opts.Output, out, 0o644); err != nil {
		t.Fatal(err)
	}
	opts.Check = true
	if code := runAssessClosure(opts.args()); code != 0 {
		t.Fatalf("fresh check exit = %d", code)
	}
	if err := os.WriteFile(opts.Output, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runAssessClosure(opts.args()); code != 1 {
		t.Fatalf("stale check exit = %d", code)
	}
}

func TestAssessClosureRejectsActiveAwarenessOutputPath(t *testing.T) {
	fx := closureCLIFixture(t)
	opts := fx.closedOptions()
	opts.Output = filepath.Join("docs", "awareness", "architecture_closure.yaml")
	if code := runAssessClosure(opts.args()); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestAssessClosureAllowsCandidateOutputPath(t *testing.T) {
	fx := closureCLIFixture(t)
	opts := fx.closedOptions()
	opts.Output = filepath.Join(fx.dir, "docs", "awareness", "candidates", "architecture_closure.yaml")
	if code := runAssessClosure(opts.args()); code != 0 {
		t.Fatalf("exit = %d", code)
	}
}

type closureCLI struct {
	dir      string
	request  string
	claims   string
	maint    string
	plane    string
	dialogue string
	evidence string
	graph    string
	digest   string
}

func (fx closureCLI) closedOptions() assessClosureOptions {
	return assessClosureOptions{
		Request: fx.request, Claims: fx.claims, MaintenanceReport: fx.maint, PlaneAssessment: fx.plane,
		Dialogue: fx.dialogue, EvidenceState: fx.evidence, GraphNT: fx.graph, Repo: ".",
		GraphDigest: fx.digest, GraphDigestStatus: architecture.GraphDigestResolved, Format: "yaml",
	}
}

func (o assessClosureOptions) args() []string {
	args := []string{"--request", o.Request, "--claims", o.Claims, "--repo", o.Repo, "--format", o.Format, "--graph-digest-status", o.GraphDigestStatus}
	if o.MaintenanceReport != "" {
		args = append(args, "--maintenance-report", o.MaintenanceReport)
	}
	if o.PlaneAssessment != "" {
		args = append(args, "--plane-assessment", o.PlaneAssessment)
	}
	if o.Dialogue != "" {
		args = append(args, "--dialogue", o.Dialogue)
	}
	if o.EvidenceState != "" {
		args = append(args, "--evidence-state", o.EvidenceState)
	}
	if o.GraphNT != "" {
		args = append(args, "--graph-nt", o.GraphNT)
	}
	if o.GraphDigest != "" {
		args = append(args, "--graph-digest", o.GraphDigest)
	}
	if o.Output != "" {
		args = append(args, "--output", o.Output)
	}
	if o.Check {
		args = append(args, "--check")
	}
	if o.RequireClosed {
		args = append(args, "--require-closed")
	}
	return args
}

func closureCLIFixture(t *testing.T) closureCLI {
	t.Helper()
	dir := t.TempDir()
	rev, status, _ := architecture.ResolveRevision(".", true)
	if status != architecture.RevisionResolved || rev == "" {
		t.Skip("git revision unavailable")
	}
	graph := ntLine(sourceFileIRI("go.mod"), rdf.PropType, rdf.ClassSourceFile, true) +
		ntLine(sourceFileIRI("go.mod"), rdf.PropSourcePath, "go.mod", false)
	graphPath := filepath.Join(dir, "graph.nt")
	if err := os.WriteFile(graphPath, []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(graph))
	digest := hex.EncodeToString(sum[:])
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain: "example.test/repo", Revision: rev, RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: digest, GraphDigestStatus: architecture.GraphDigestResolved,
	}
	req := closure.Request{
		SchemaVersion: closure.SchemaVersion,
		Binding:       binding,
		Scope: closure.Scope{
			Domain: "repository", TaskClass: "read_go_mod", RiskClass: closure.RiskLowRisk,
			AccessMode: closure.AccessRead, DirectionRequirement: closure.DirectionNotApplicable, Files: []string{"go.mod"},
		},
	}
	reqBytes, err := closure.MarshalCanonicalRequestYAML(req)
	if err != nil {
		t.Fatal(err)
	}
	claimsBytes, err := architecture.MarshalCanonicalClaimDocumentYAML(architecture.ClaimDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: binding, Claims: []architecture.Claim{}})
	if err != nil {
		t.Fatal(err)
	}
	maintBytes, err := maintenance.MarshalCanonicalReportYAML(maintenance.Report{SchemaVersion: "1", GeneratedBy: "test", CurrentBinding: binding, ObservedBinding: binding})
	if err != nil {
		t.Fatal(err)
	}
	planeBytes, err := plane.MarshalCanonicalReportYAML(plane.Report{
		SchemaVersion: "1", GeneratedBy: "test",
		ClaimBinding:  plane.ClaimBindingReport{RepositoryDomain: binding.RepositoryDomain, Revision: binding.Revision, RevisionStatus: binding.RevisionStatus, GraphDigestSHA256: binding.GraphDigestSHA256, GraphDigestStatus: binding.GraphDigestStatus},
		GraphSnapshot: plane.GraphSnapshotReport{Path: graphPath, DigestSHA256: digest, DigestStatus: architecture.GraphDigestResolved},
	})
	if err != nil {
		t.Fatal(err)
	}
	dialogueBytes, err := architecture.MarshalCanonicalDialogueDocumentYAML(architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: binding, OpenQuestions: []architecture.OpenQuestion{}})
	if err != nil {
		t.Fatal(err)
	}
	evidenceBytes, err := maintenance.MarshalCanonicalEvidenceStateYAML(maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: binding, Evidence: []maintenance.EvidenceState{}})
	if err != nil {
		t.Fatal(err)
	}
	fx := closureCLI{dir: dir, graph: graphPath, digest: digest}
	fx.request = writeFixture(t, dir, "request.yaml", reqBytes)
	fx.claims = writeFixture(t, dir, "claims.yaml", claimsBytes)
	fx.maint = writeFixture(t, dir, "maint.yaml", maintBytes)
	fx.plane = writeFixture(t, dir, "plane.yaml", planeBytes)
	fx.dialogue = writeFixture(t, dir, "dialogue.yaml", dialogueBytes)
	fx.evidence = writeFixture(t, dir, "evidence.yaml", evidenceBytes)
	return fx
}

func writeFixture(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sourceFileIRI(path string) string {
	return strings.Trim(strings.TrimSuffix(rdf.MintIRI(rdf.ClassSourceFile, path), ">"), "<")
}

func ntLine(subject, predicate, object string, objectIRI bool) string {
	if objectIRI {
		return "<" + subject + "> <" + predicate + "> <" + object + "> .\n"
	}
	return "<" + subject + "> <" + predicate + "> \"" + object + "\" .\n"
}
