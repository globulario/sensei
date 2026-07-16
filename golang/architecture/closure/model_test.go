// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/rdf"
)

func TestClosureRequestRequiresExplicitBindingStatuses(t *testing.T) {
	req := validRequest()
	req.Binding.RevisionStatus = ""
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected missing revision status to be rejected")
	}
}

func TestClosureRequestRequiresTaskClass(t *testing.T) {
	req := validRequest()
	req.Scope.TaskClass = ""
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected missing task class to be rejected")
	}
}

func TestClosureRequestRejectsUnknownRisk(t *testing.T) {
	req := validRequest()
	req.Scope.RiskClass = "mystery"
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unknown risk to be rejected")
	}
}

func TestClosureRequestRejectsUnknownAccessMode(t *testing.T) {
	req := validRequest()
	req.Scope.AccessMode = "maybe"
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unknown access mode to be rejected")
	}
}

func TestClosureRequestRejectsUnknownDirectionRequirement(t *testing.T) {
	req := validRequest()
	req.Scope.DirectionRequirement = "sideways"
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unknown direction requirement to be rejected")
	}
}

func TestClosureRequestRejectsUnknownDimension(t *testing.T) {
	req := validRequest()
	req.Scope.AdditionalDimensions = []string{"quality"}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected unknown dimension to be rejected")
	}
}

func TestClosureRequestRejectsEscapingPath(t *testing.T) {
	req := validRequest()
	req.Scope.Files = []string{"../x.go"}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected escaping path to be rejected")
	}
}

func TestClosureRequestRequiresScopeOrDomainWide(t *testing.T) {
	req := validRequest()
	req.Scope.Files = nil
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected empty non-domain-wide scope to be rejected")
	}
}

func TestClosureRequestNormalizationIsDeterministic(t *testing.T) {
	req := validRequest()
	req.Scope.Files = []string{"b.go", "./a.go"}
	got, err := NormalizeRequest(req)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	if strings.Join(got.Scope.Files, ",") != "a.go,b.go" {
		t.Fatalf("files not deterministic: %#v", got.Scope.Files)
	}
}

func TestAdditionalDimensionsCanOnlyAdd(t *testing.T) {
	req := validRequest()
	req.Scope.RiskClass = RiskLowRisk
	req.Scope.AdditionalDimensions = []string{DimensionAuthority}
	got, err := NormalizeRequest(req)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	p, _ := PolicyForRisk(got.Scope.RiskClass)
	if !contains(p.RequiredDimensions, DimensionAuthority) && !contains(got.Scope.AdditionalDimensions, DimensionAuthority) {
		t.Fatal("additional dimension was not preserved as an added requirement")
	}
}

func TestClosureRequestRejectsDuplicateAfterNormalization(t *testing.T) {
	req := validRequest()
	req.Scope.Files = []string{"a.go", "./a.go"}
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected duplicate normalized path to be rejected")
	}
}

func TestNormalizeSinglePathCanonicalizesOnePath(t *testing.T) {
	if got := normalizeSinglePath("./docs/awareness/../awareness/invariants.yaml"); got != "docs/awareness/invariants.yaml" {
		t.Fatalf("normalizeSinglePath = %q", got)
	}
	if got := normalizeSinglePath("   "); got != "" {
		t.Fatalf("normalizeSinglePath empty = %q", got)
	}
}

func TestSafeRelPathRejectsEscapesAndAbsolutePaths(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "docs/awareness/architecture/decisions.yaml", want: true},
		{path: "./docs/awareness/architecture/decisions.yaml", want: true},
		{path: "../docs/awareness/architecture/decisions.yaml", want: false},
		{path: "/tmp/decisions.yaml", want: false},
		{path: "", want: false},
	}
	for _, tc := range cases {
		if got := safeRelPath(tc.path); got != tc.want {
			t.Fatalf("safeRelPath(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}

func TestBootstrapDigestHelpersValidateExpectedHexLengths(t *testing.T) {
	if !isSHA256(strings.Repeat("a", 64)) {
		t.Fatal("expected lowercase sha256 to validate")
	}
	if isSHA256(strings.Repeat("A", 64)) {
		t.Fatal("uppercase sha256 must not validate")
	}
	if !isHexLen(strings.Repeat("b", 40), 40) {
		t.Fatal("expected 40-char lowercase hex to validate")
	}
	if isHexLen(strings.Repeat("b", 39), 40) {
		t.Fatal("wrong-length hex must not validate")
	}
}

func TestDirectionBootstrapRequiresTaskID(t *testing.T) {
	req := validRequest()
	req.DirectionBootstrap = &DirectionBootstrapAuthorization{
		SchemaVersion:                DirectionBootstrapSchemaVersion,
		PolicyID:                     DirectionBootstrapPolicyID,
		TaskID:                       "task.bootstrap.direction",
		BaseRevision:                 strings.Repeat("a", 40),
		GraphDigestSHA256:            strings.Repeat("b", 64),
		File:                         DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.one"},
		ExpectedMutationDigestSHA256: strings.Repeat("c", 64),
		ApprovedBy:                   "architect",
		ApprovalMechanism:            DirectionBootstrapMechanismFile,
		ApprovalStatement:            "bootstrap once",
		UsagePolicy:                  DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
		ApprovalSourcePath:           "/approved/bootstrap-direction.yaml",
		ApprovalSourceDigestSHA256:   strings.Repeat("d", 64),
	}
	req.DirectionBootstrap.AuthorizationDigestSHA256 = DirectionBootstrapAuthorizationDigest(*req.DirectionBootstrap)
	if _, err := NormalizeRequest(req); err == nil {
		t.Fatal("expected task-less request to reject direction bootstrap")
	}
}

func TestDirectionBootstrapForRequestRequiresExactDecisionsTask(t *testing.T) {
	req := validRequest()
	req.TaskID = "task.bootstrap.direction"
	req.Binding = bootstrapBinding()
	req.Scope.Files = []string{"other.yaml"}
	req.Scope.DirectionRequirement = DirectionEvolve
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.DirectionBootstrap = validDirectionBootstrap()
	if _, err := DirectionBootstrapForRequest(req, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected non-decisions task to reject bootstrap")
	}
}

func TestDirectionBootstrapForRequestRejectsUnknownMechanism(t *testing.T) {
	req := validRequest()
	req.TaskID = "task.bootstrap.direction"
	req.Binding = bootstrapBinding()
	req.Scope.Domain = req.Binding.RepositoryDomain
	req.Scope.TaskClass = "activate_direction_records"
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.AccessMode = AccessReadWrite
	req.Scope.DirectionRequirement = DirectionEvolve
	req.Scope.Files = []string{DirectionBootstrapFile}
	req.DirectionBootstrap = validDirectionBootstrap()
	req.DirectionBootstrap.ApprovalMechanism = "human_review"
	req.DirectionBootstrap.AuthorizationDigestSHA256 = DirectionBootstrapAuthorizationDigest(*req.DirectionBootstrap)
	if _, err := DirectionBootstrapForRequest(req, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "approval_mechanism is unknown") {
		t.Fatalf("expected unknown mechanism rejection, got %v", err)
	}
}

func TestDirectionBootstrapForRequestRejectsExpiredAuthorization(t *testing.T) {
	req := validRequest()
	req.TaskID = "task.bootstrap.direction"
	req.Binding = bootstrapBinding()
	req.Scope.Domain = req.Binding.RepositoryDomain
	req.Scope.TaskClass = "activate_direction_records"
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.AccessMode = AccessReadWrite
	req.Scope.DirectionRequirement = DirectionEvolve
	req.Scope.Files = []string{DirectionBootstrapFile}
	req.DirectionBootstrap = validDirectionBootstrap()
	if _, err := DirectionBootstrapForRequest(req, time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "authorization expired") {
		t.Fatalf("expected expiry rejection, got %v", err)
	}
}

func TestValidateDirectionBootstrapApprovalRequiresTrustedApprovalRoot(t *testing.T) {
	trusted := filepath.Join(t.TempDir(), "trusted")
	if err := os.MkdirAll(trusted, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(DirectionBootstrapApprovalDirEnv, trusted)
	outsideDir := t.TempDir()
	sourcePath := filepath.Join(outsideDir, "bootstrap.yaml")
	data := []byte("approved")
	if err := os.WriteFile(sourcePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	auth := *validDirectionBootstrap()
	auth.ApprovalSourcePath = sourcePath
	auth.ApprovalSourceDigestSHA256 = digest(data)
	auth.AuthorizationDigestSHA256 = DirectionBootstrapAuthorizationDigest(auth)
	if err := ValidateDirectionBootstrapApproval(auth, repoRoot); err == nil || !strings.Contains(err.Error(), "trusted approval root") {
		t.Fatalf("expected trusted approval root rejection, got %v", err)
	}
}

func TestDirectionBootstrapConditionsOnlyDirection(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness", "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "awareness", "architecture", "decisions.yaml"), []byte("decisions:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.TaskID = "task.bootstrap.direction"
	req.Binding = bootstrapBinding()
	req.Scope.Domain = req.Binding.RepositoryDomain
	req.Scope.TaskClass = "activate_direction_records"
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.AccessMode = AccessReadWrite
	req.Scope.DirectionRequirement = DirectionEvolve
	req.Scope.Files = []string{DirectionBootstrapFile}
	req.DirectionBootstrap = validDirectionBootstrap()
	// This test exercises the wall-clock Evaluate path, so the authorization
	// window must be valid relative to time.Now(); the shared helper keeps its
	// fixed window for the deterministic tests that pass an explicit clock.
	req.DirectionBootstrap.IssuedAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	req.DirectionBootstrap.ExpiresAt = time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	req.DirectionBootstrap.AuthorizationDigestSHA256 = DirectionBootstrapAuthorizationDigest(*req.DirectionBootstrap)
	report, err := Evaluate(validContext(t, root, req, sourceFileGraph(DirectionBootstrapFile)))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoBlocker(t, report, "closure.direction.intended_missing")
	assertNoBlocker(t, report, "closure.direction.desired_missing")
	assertBlocker(t, report, "closure.behavior.surface_empty")
	found := map[string]bool{}
	for _, c := range report.Conditions {
		found[c.Code] = true
	}
	if !found["closure.direction.intended.bootstrap"] || !found["closure.direction.desired.bootstrap"] {
		t.Fatalf("missing bootstrap direction conditions: %+v", report.Conditions)
	}
}

func TestClosureGraphIndexUsesExplicitRDFType(t *testing.T) {
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("component", "component.a"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "component.a"), rdf.PropLabel, "A", false),
	)))
	if len(idx.Nodes) != 1 {
		t.Fatalf("expected typed node, got %d", len(idx.Nodes))
	}
}

func TestClosureGraphIndexDoesNotInferClassFromIRI(t *testing.T) {
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("component", "component.a"), rdf.PropLabel, "A", false),
	)))
	if len(idx.Nodes) != 0 {
		t.Fatalf("expected no untyped nodes, got %d", len(idx.Nodes))
	}
}

func TestClosureGraphIndexReadsAuthorityProperties(t *testing.T) {
	auth := classIRI("authority_domain", "auth.config")
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(auth, rdf.PropType, rdf.ClassAuthorityDomain, true),
		triple(auth, rdf.PropOwnerService, "service.config", false),
		triple(auth, rdf.PropOwnsState, "state.config", false),
		triple(auth, rdf.PropMayWrite, "writer", false),
		triple(auth, rdf.PropMustMutateVia, "rpc.Save", false),
		triple(auth, rdf.PropHasTruthLayer, "repository", false),
	)))
	n := idx.Nodes[auth]
	if len(n.OwnerServices) != 1 || len(n.OwnsStates) != 1 || len(n.MayWrite) != 1 || len(n.MustMutateVia) != 1 || len(n.TruthLayers) != 1 {
		t.Fatalf("authority properties missing: %#v", n)
	}
}

func TestSourceFileWithSourcePathResolves(t *testing.T) {
	iri := classIRI("source_file", "different.go")
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
		triple(iri, rdf.PropSourcePath, "x.go", false),
	)))
	if got := idx.Nodes[iri].SourcePath; got != "x.go" {
		t.Fatalf("SourcePath = %q, want x.go", got)
	}
	if got := idx.FilesByPath["x.go"]; got != iri {
		t.Fatalf("FilesByPath[x.go] = %q, want %q", got, iri)
	}
	if _, ok := idx.FilesByPath["different.go"]; ok {
		t.Fatal("canonical IRI path overrode explicit sourcePath")
	}
}

func TestCanonicalSourceFileIRIWithoutSourcePathResolves(t *testing.T) {
	iri := classIRI("source_file", "cmd/awareness-mcp/main.go")
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
	)))
	if got := idx.Nodes[iri].SourcePath; got != "cmd/awareness-mcp/main.go" {
		t.Fatalf("SourcePath = %q", got)
	}
	if got := idx.FilesByPath["cmd/awareness-mcp/main.go"]; got != iri {
		t.Fatalf("FilesByPath entry = %q, want %q", got, iri)
	}
}

func TestNonSourceFilePathShapedIRIDoesNotResolveAsFile(t *testing.T) {
	iri := classIRI("component", "cmd/awareness-mcp/main.go")
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(iri, rdf.PropType, rdf.ClassComponent, true),
	)))
	if idx.Nodes[iri].SourcePath != "" {
		t.Fatalf("non-SourceFile got SourcePath: %#v", idx.Nodes[iri])
	}
	if len(idx.FilesByPath) != 0 {
		t.Fatalf("non-SourceFile populated file index: %#v", idx.FilesByPath)
	}
}

func TestMalformedSourceFileIRIDoesNotResolve(t *testing.T) {
	iri := rdf.AwNS + "sourceFile/cmd%2fawareness-mcp%2fmain.go"
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
	)))
	if idx.Nodes[iri].SourcePath != "" {
		t.Fatalf("malformed IRI got SourcePath: %#v", idx.Nodes[iri])
	}
	if len(idx.FilesByPath) != 0 {
		t.Fatalf("malformed IRI populated file index: %#v", idx.FilesByPath)
	}
}

func TestEscapingSourceFileIRIPathIsRejected(t *testing.T) {
	iri := classIRI("source_file", "../x.go")
	idx := BuildGraphIndex(mustTriples(t, nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
	)))
	if idx.Nodes[iri].SourcePath != "" {
		t.Fatalf("escaping IRI got SourcePath: %#v", idx.Nodes[iri])
	}
	if len(idx.FilesByPath) != 0 {
		t.Fatalf("escaping IRI populated file index: %#v", idx.FilesByPath)
	}
}

func TestSourceFileMatchingRemainsExact(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "awareness-mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "awareness-mcp", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.Files = []string{"cmd/awareness-mcp/main.go"}
	report, err := Evaluate(validContext(t, root, req, sourceFileGraphCanonical("cmd/awareness-mcp/main.go.extra")))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestSourcePathBasedClosureOutputEquivalent(t *testing.T) {
	root, sourcePathGraph := closedFixture(t)
	canonicalGraph := sourceFileGraphCanonical("x.go")
	req := validRequest()
	withSourcePath, err := Evaluate(validContext(t, root, req, sourcePathGraph))
	if err != nil {
		t.Fatalf("Evaluate sourcePath: %v", err)
	}
	withoutSourcePath, err := Evaluate(validContext(t, root, req, canonicalGraph))
	if err != nil {
		t.Fatalf("Evaluate canonical: %v", err)
	}
	if withSourcePath.ScopeReceipt.Files[0] != withoutSourcePath.ScopeReceipt.Files[0] || withSourcePath.Verdict != withoutSourcePath.Verdict {
		t.Fatalf("sourcePath and canonical output differ:\nwith=%#v\nwithout=%#v", withSourcePath.ScopeReceipt, withoutSourcePath.ScopeReceipt)
	}
}

func TestClosureRequestFileRepresentedByCanonicalSourceFileIRI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cmd", "awareness-mcp", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.Files = []string{"cmd/awareness-mcp/main.go"}
	report, err := Evaluate(validContext(t, root, req, sourceFileGraphCanonical("cmd/awareness-mcp/main.go")))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !contains(report.ScopeReceipt.Files, "cmd/awareness-mcp/main.go") {
		t.Fatalf("file not represented: %#v", report.ScopeReceipt)
	}
	for _, b := range report.Blockers {
		if b.Code == "closure.structural.file_unrepresented" {
			t.Fatalf("canonical source file remained unrepresented: %#v", b)
		}
	}
}

func TestCanonicalDecisionAuthoredInRepresentsExactFile(t *testing.T) {
	testCanonicalAuthoredInRepresentsExactFile(t, "decision", rdf.ClassDecision, "accepted")
}

func TestCanonicalInvariantAuthoredInRepresentsExactFile(t *testing.T) {
	testCanonicalAuthoredInRepresentsExactFile(t, "invariant", rdf.ClassInvariant, "active")
}

func TestCanonicalFailureModeAuthoredInRepresentsExactFile(t *testing.T) {
	testCanonicalAuthoredInRepresentsExactFile(t, "failure_mode", rdf.ClassFailureMode, "active")
}

func TestCanonicalAuthorityDomainAuthoredInRepresentsExactFile(t *testing.T) {
	testCanonicalAuthoredInRepresentsExactFile(t, "authority_domain", rdf.ClassAuthorityDomain, "active")
}

func TestAuthoredInRepresentationUsesExactPath(t *testing.T) {
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/architecture/decisions.yaml.extra"},
	})))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestUnrelatedAuthoredInDoesNotRepresentFile(t *testing.T) {
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/invariants.yaml"},
	})))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestDirectoryPrefixDoesNotRepresentFile(t *testing.T) {
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/architecture"},
	})))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestAbsentAuthoredFileRemainsUnrepresented(t *testing.T) {
	root := t.TempDir()
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/architecture/decisions.yaml"},
	})))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestCandidateAuthoredInDoesNotRepresentFile(t *testing.T) {
	testIneligibleAuthoredInRepresentation(t, authoredNodeOptions{Status: "candidate"})
}

func TestContestedAuthoredInDoesNotRepresentFile(t *testing.T) {
	testIneligibleAuthoredInRepresentation(t, authoredNodeOptions{Status: "contested"})
}

func TestRejectedAuthoredInDoesNotRepresentFile(t *testing.T) {
	testIneligibleAuthoredInRepresentation(t, authoredNodeOptions{Status: "rejected"})
}

func TestTaskLocalMachineAdoptedAuthoredInDoesNotRepresentFile(t *testing.T) {
	testIneligibleAuthoredInRepresentation(t, authoredNodeOptions{
		Status:          "machine_adopted",
		PromotionStatus: "machine_adopted",
		ReviewStatus:    "not_human_reviewed",
	})
}

func TestNeuralCandidateAuthoredInDoesNotRepresentFile(t *testing.T) {
	testIneligibleAuthoredInRepresentation(t, authoredNodeOptions{
		Status:          "candidate",
		PromotionStatus: "candidate",
		SourceKind:      "neural_candidate",
	})
}

func TestAuthoredInRepresentationDoesNotMintSourceFile(t *testing.T) {
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	graph := authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/architecture/decisions.yaml"},
	})
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if graph.FilesByPath["docs/awareness/architecture/decisions.yaml"] != "" {
		t.Fatalf("graph minted SourceFile coverage: %#v", graph.FilesByPath)
	}
	for _, rep := range report.ScopeReceipt.RepresentedFiles {
		if rep.Path == "docs/awareness/architecture/decisions.yaml" && rep.RepresentationKind != "governed_authored_source" {
			t.Fatalf("representation kind = %q, want governed_authored_source", rep.RepresentationKind)
		}
	}
}

func TestAuthoredInRepresentationDoesNotChangeProductionCoverage(t *testing.T) {
	graph := authoredInGraph("decision", rdf.ClassDecision, "decision.scope", authoredNodeOptions{
		Status:     "accepted",
		AuthoredIn: []string{"docs/awareness/architecture/decisions.yaml"},
	})
	if got := len(graph.FilesByPath); got != 0 {
		t.Fatalf("FilesByPath=%d, want 0", got)
	}
}

func TestAuthorityUnrelatedDomainDoesNotBindTask(t *testing.T) {
	root, graph := closedFixture(t)
	graph = mergeGraphs(graph, authorityDomainGraph("auth.other", authorityDomainOptions{
		CoversPaths:   []string{"other.go"},
		OwnerServices: []string{"service.other"},
		MayWrite:      []string{"writer.other"},
		MustMutateVia: []string{"rpc.Other"},
		TruthLayers:   []string{"repository"},
	}))
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.owner_missing")
	assertNoAuthorityBlocker(t, report, "closure.authority.state_unmapped")
	assertNoAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
}

func TestAuthorityExactCoveredFileRemainsBound(t *testing.T) {
	root, graph := closedFixture(t)
	graph = mergeGraphs(graph, authorityDomainGraph("auth.x", authorityDomainOptions{
		CoversPaths: []string{"x.go"},
		MayWrite:    []string{"writer.x"},
		TruthLayers: []string{"repository"},
	}))
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertAuthorityBlocker(t, report, "closure.authority.owner_missing")
}

func TestAuthorityMutationClaimWithoutPathDoesNotBind(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	ctx := validContext(t, root, req, graph)
	claim := architecture.Claim{
		ID:                 "claim.write.scope",
		Statement:          architecture.ClaimStatement{Subject: "closure.LoadRequest", Predicate: "writes", Object: "err"},
		Scope:              architecture.ClaimScope{Repository: req.Binding.RepositoryDomain, Files: []string{"x.go"}},
		ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin:    architecture.OriginDerived,
		EpistemicStatus:    architecture.StatusSupported,
		Confidence:         0.6,
	}
	ctx.Claims.Claims = []architecture.Claim{claim}
	ctx.Maintenance.ClaimEvaluations = []maintenance.ClaimEvaluation{{
		ClaimID:         claim.ID,
		InputStatus:     architecture.StatusSupported,
		EvaluatedStatus: architecture.StatusSupported,
		Disposition:     "kept",
	}}
	ctx.Plane.ClaimAssessments = []plane.ClaimAssessment{{
		ClaimID:         claim.ID,
		PropositionKey:  plane.PropositionKey(claim),
		DeclaredPlane:   claim.ArchitecturalPlane,
		AssertionOrigin: claim.AssertionOrigin,
		EpistemicStatus: claim.EpistemicStatus,
		PlaneState:      plane.StateJustified,
	}}
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.state_unmapped")
}

func TestAuthorityGovernedStateWithoutDomainFailsClosed(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	graph := mergeGraphs(
		sourceFileGraph("x.go"),
		componentStateGraph("component.scope", "x.go", "state.alpha"),
	)
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertAuthorityBlocker(t, report, "closure.authority.state_unmapped")
}

func TestAuthorityGovernedStateWithOwningDomainBinds(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	graph := mergeGraphs(
		sourceFileGraph("x.go"),
		componentStateGraph("component.scope", "x.go", "state.alpha"),
		authorityDomainGraph("auth.alpha", authorityDomainOptions{
			OwnsStates: []string{"state.alpha"},
			MayWrite:   []string{"writer.alpha"},
			TruthLayers: []string{
				"repository",
			},
		}),
	)
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.state_unmapped")
	assertAuthorityBlocker(t, report, "closure.authority.owner_missing")
}

func TestAuthorityFreshBroaderDomainBeatsStaleExactDomain(t *testing.T) {
	root := authoredFileRoot(t, "pkg/x.go")
	graph := mergeGraphs(
		sourceFileGraph("pkg/x.go"),
		authorityDomainGraph("auth.stale.exact", authorityDomainOptions{
			Status:      "stale",
			CoversPaths: []string{"pkg/x.go"},
		}),
		authorityDomainGraph("auth.active.broad", authorityDomainOptions{
			CoversPaths:   []string{"pkg/"},
			OwnerServices: []string{"service.pkg"},
			MayWrite:      []string{"writer.pkg"},
			MustMutateVia: []string{"rpc.Package"},
			TruthLayers:   []string{"repository"},
		}),
	)
	req := validRequest()
	req.Scope.Files = []string{"pkg/x.go"}
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.owner_missing")
	assertNoAuthorityBlocker(t, report, "closure.authority.stale")
}

func TestAuthorityContradictoryApplicableDomainsBlock(t *testing.T) {
	root, graph := closedFixture(t)
	graph = mergeGraphs(
		graph,
		authorityDomainGraph("auth.one", authorityDomainOptions{
			CoversPaths:   []string{"x.go"},
			OwnerServices: []string{"service.one"},
			MayWrite:      []string{"writer.one"},
			MustMutateVia: []string{"rpc.One"},
			TruthLayers:   []string{"repository"},
		}),
		authorityDomainGraph("auth.two", authorityDomainOptions{
			CoversPaths:   []string{"x.go"},
			OwnerServices: []string{"service.two"},
			MayWrite:      []string{"writer.two"},
			MustMutateVia: []string{"rpc.Two"},
			TruthLayers:   []string{"repository"},
		}),
	)
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
}

func TestAuthorityUnrelatedConflictingDomainsDoNotBlock(t *testing.T) {
	root, graph := closedFixture(t)
	graph = mergeGraphs(
		graph,
		authorityDomainGraph("auth.other.one", authorityDomainOptions{
			CoversPaths:   []string{"other.go"},
			OwnerServices: []string{"service.one"},
			MayWrite:      []string{"writer.one"},
			MustMutateVia: []string{"rpc.One"},
			TruthLayers:   []string{"repository"},
		}),
		authorityDomainGraph("auth.other.two", authorityDomainOptions{
			CoversPaths:   []string{"other.go"},
			OwnerServices: []string{"service.two"},
			MayWrite:      []string{"writer.two"},
			MustMutateVia: []string{"rpc.Two"},
			TruthLayers:   []string{"repository"},
		}),
	)
	req := validRequest()
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoAuthorityBlocker(t, report, "closure.authority.applicable_records_contradict")
}

func TestFailureModeBlockerClearsWhenSourceFileIsVulnerableToFailureMode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.DirectionRequirement = DirectionPreserve

	withoutFM, err := Evaluate(validContext(t, root, req, sourceFileGraphCanonical("x.go")))
	if err != nil {
		t.Fatalf("Evaluate without failure mode: %v", err)
	}
	assertBlocker(t, withoutFM, "closure.behavior.failure_mode_missing")

	withFM, err := Evaluate(validContext(t, root, req, sourceFileVulnerableToGraph("x.go", "failure.test.scope")))
	if err != nil {
		t.Fatalf("Evaluate with failure mode: %v", err)
	}
	assertNoBlocker(t, withFM, "closure.behavior.failure_mode_missing")
	assertNoBlocker(t, withFM, "closure.behavior.surface_empty")
	assertNoBlocker(t, withFM, "closure.behavior.observed_or_enforced_missing")
}

func TestCandidateFailureModeDoesNotClearFailureSurface(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.DirectionRequirement = DirectionPreserve
	report, err := Evaluate(validContext(t, root, req, sourceFileVulnerableToFailureModeWithStatus("x.go", "failure.test.scope", "candidate")))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.behavior.failure_mode_missing")
}

func TestStructuralClaimDoesNotBecomeBehavioralPlaneBlocker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.DirectionRequirement = DirectionPreserve
	ctx := validContext(t, root, req, sourceFileGraphCanonical("x.go"))
	claim := architecture.Claim{
		ID:                     "claim.shared.path",
		Label:                  "shared path",
		Statement:              architecture.ClaimStatement{Subject: "api.resolve", Predicate: "is_shared_implementation_path_for", Object: "api.HandleContext, api.ServeHTTP"},
		Scope:                  architecture.ClaimScope{Repository: req.Binding.RepositoryDomain, Repo: req.Binding.RepositoryDomain, Files: []string{"x.go"}},
		ArchitecturalPlane:     architecture.PlaneObserved,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.shared_entrypoint_behavior_path.v1",
		PremiseFacts:           []string{"fact.reach"},
		InvalidationConditions: []string{"premise fact changes"},
		Confidence:             0.8,
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	ctx.Claims = claimDoc(t, []architecture.Claim{claim}, []architecture.Fact{testFactReceipt("fact.reach", "reachability", "entrypoint_reaches_symbol", map[string]string{"target_file": "x.go"})})
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoBlocker(t, report, "closure.behavior.plane_invalid")
	assertBlocker(t, report, "closure.behavior.failure_mode_missing")
}

func TestDirectionClaimDoesNotBecomeBehavioralPlaneBlocker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.DirectionRequirement = DirectionEvolve
	ctx := validContext(t, root, req, sourceFileGraphCanonical("x.go"))
	claim := architecture.Claim{
		ID:                     "claim.direction",
		Label:                  "desired direction",
		Statement:              architecture.ClaimStatement{Subject: "decision.x", Predicate: "defines_desired_direction_for_scope", Object: "Enforced awareness mutation support"},
		Scope:                  architecture.ClaimScope{Repository: req.Binding.RepositoryDomain, Repo: req.Binding.RepositoryDomain, Files: []string{"x.go"}},
		ArchitecturalPlane:     architecture.PlaneDesired,
		AssertionOrigin:        architecture.OriginDerived,
		EpistemicStatus:        architecture.StatusSupported,
		InferenceRule:          "rule.governed_direction_record.v1",
		PremiseFacts:           []string{"fact.direction"},
		InvalidationConditions: []string{"governed direction changes"},
		AboutNodes:             []string{"decision:decision.x"},
		Confidence:             1,
		HumanReviewRequired:    true,
		PromotionStatus:        architecture.PromotionCandidate,
	}
	ctx.Claims = claimDoc(t, []architecture.Claim{claim}, []architecture.Fact{testFactReceipt("fact.direction", "governed_direction", "defines_desired_direction_for_scope", map[string]string{"target_file": "x.go"})})
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoBlocker(t, report, "closure.behavior.plane_invalid")
}

func TestUnrepresentedFileRemainsOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(validContext(t, root, validRequest(), sourceFileGraphCanonical("other.go")))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict == VerdictClosed {
		t.Fatalf("unrepresented file closed: %#v", report)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func TestCanonicalSourceFileIRIResolutionDeterministic(t *testing.T) {
	first := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("source_file", "cmd/awareness-mcp/main.go"), rdf.PropType, rdf.ClassSourceFile, true),
	)))
	second := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("source_file", "cmd/awareness-mcp/main.go"), rdf.PropType, rdf.ClassSourceFile, true),
	)))
	if first.Nodes[classIRI("source_file", "cmd/awareness-mcp/main.go")].SourcePath != second.Nodes[classIRI("source_file", "cmd/awareness-mcp/main.go")].SourcePath {
		t.Fatalf("resolution changed: %#v vs %#v", first, second)
	}
}

func TestResolveScopeDeterministicallyExpandsRequiredTests(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	graph := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("source_file", "x.go"), rdf.PropType, rdf.ClassSourceFile, true),
		triple(classIRI("source_file", "x.go"), rdf.PropConstrainedByInvariant, classIRI("invariant", "invariant.x"), true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropType, rdf.ClassInvariant, true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropRequiresTest, classIRI("test", "closure.TestDeterministicExpansion"), true),
		triple(classIRI("test", "closure.TestDeterministicExpansion"), rdf.PropType, rdf.ClassTest, true),
	)))
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !containsNodeReceipt(report.RelevantNodes, "closure.TestDeterministicExpansion") {
		t.Fatalf("required test missing from relevant nodes: %#v", report.RelevantNodes)
	}
}

func TestResolveScopeRepeatedEvaluationIsDeterministic(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	graph := deterministicExpansionGraph()
	ctx := validContext(t, root, req, graph)
	var first []byte
	for i := 0; i < 100; i++ {
		report, err := Evaluate(ctx)
		if err != nil {
			t.Fatalf("Evaluate run %d: %v", i+1, err)
		}
		got, err := MarshalCanonicalReportYAML(report)
		if err != nil {
			t.Fatalf("MarshalCanonicalReportYAML run %d: %v", i+1, err)
		}
		if i == 0 {
			first = append([]byte(nil), got...)
			continue
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("run %d differed from run 1", i+1)
		}
	}
}

func TestResolveScopeGraphInsertionOrderDoesNotMatter(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	triplesA := nt(
		triple(classIRI("source_file", "x.go"), rdf.PropType, rdf.ClassSourceFile, true),
		triple(classIRI("source_file", "x.go"), rdf.PropDependsOn, classIRI("component", "component.x"), true),
		triple(classIRI("component", "component.x"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "component.x"), rdf.PropExposesContract, classIRI("contract", "contract.x"), true),
		triple(classIRI("contract", "contract.x"), rdf.PropType, rdf.ClassContract, true),
		triple(classIRI("contract", "contract.x"), rdf.PropConstrainedByInvariant, classIRI("invariant", "invariant.x"), true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropType, rdf.ClassInvariant, true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropRequiresTest, classIRI("test", "closure.TestGraphOrder"), true),
		triple(classIRI("test", "closure.TestGraphOrder"), rdf.PropType, rdf.ClassTest, true),
	)
	triplesB := nt(
		triple(classIRI("test", "closure.TestGraphOrder"), rdf.PropType, rdf.ClassTest, true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropRequiresTest, classIRI("test", "closure.TestGraphOrder"), true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropType, rdf.ClassInvariant, true),
		triple(classIRI("contract", "contract.x"), rdf.PropConstrainedByInvariant, classIRI("invariant", "invariant.x"), true),
		triple(classIRI("contract", "contract.x"), rdf.PropType, rdf.ClassContract, true),
		triple(classIRI("component", "component.x"), rdf.PropExposesContract, classIRI("contract", "contract.x"), true),
		triple(classIRI("component", "component.x"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("source_file", "x.go"), rdf.PropDependsOn, classIRI("component", "component.x"), true),
		triple(classIRI("source_file", "x.go"), rdf.PropType, rdf.ClassSourceFile, true),
	)
	reportA, err := Evaluate(validContext(t, root, req, BuildGraphIndex(mustTriples(t, triplesA))))
	if err != nil {
		t.Fatalf("Evaluate A: %v", err)
	}
	reportB, err := Evaluate(validContext(t, root, req, BuildGraphIndex(mustTriples(t, triplesB))))
	if err != nil {
		t.Fatalf("Evaluate B: %v", err)
	}
	aBytes, err := MarshalCanonicalReportYAML(reportA)
	if err != nil {
		t.Fatalf("Marshal A: %v", err)
	}
	bBytes, err := MarshalCanonicalReportYAML(reportB)
	if err != nil {
		t.Fatalf("Marshal B: %v", err)
	}
	if !bytes.Equal(aBytes, bBytes) {
		t.Fatal("graph insertion order changed closure output")
	}
}

func TestResolveScopeExpansionTerminatesOnCycles(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	graph := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("source_file", "x.go"), rdf.PropType, rdf.ClassSourceFile, true),
		triple(classIRI("source_file", "x.go"), rdf.PropDependsOn, classIRI("component", "A"), true),
		triple(classIRI("component", "A"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "A"), rdf.PropDependsOn, classIRI("component", "B"), true),
		triple(classIRI("component", "B"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "B"), rdf.PropDependsOn, classIRI("component", "C"), true),
		triple(classIRI("component", "C"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "C"), rdf.PropDependsOn, classIRI("component", "A"), true),
	)))
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if countNodeReceipt(report.RelevantNodes, "A") != 1 || countNodeReceipt(report.RelevantNodes, "B") != 1 || countNodeReceipt(report.RelevantNodes, "C") != 1 {
		t.Fatalf("cycle nodes not represented exactly once: %#v", report.RelevantNodes)
	}
}

func TestResolveScopeExcludesUnrelatedNodesWithoutSeedPath(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	graph := mergeGraphs(
		deterministicExpansionGraph(),
		BuildGraphIndex(mustTriples(t, nt(
			triple(classIRI("test", "closure.TestUnrelated"), rdf.PropType, rdf.ClassTest, true),
			triple(classIRI("failure_mode", "failure.unrelated"), rdf.PropType, rdf.ClassFailureMode, true),
			triple(classIRI("contract", "contract.unrelated"), rdf.PropType, rdf.ClassContract, true),
		))),
	)
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if containsNodeReceipt(report.RelevantNodes, "closure.TestUnrelated") || containsNodeReceipt(report.RelevantNodes, "failure.unrelated") || containsNodeReceipt(report.RelevantNodes, "contract.unrelated") {
		t.Fatalf("unrelated nodes entered relevant set: %#v", report.RelevantNodes)
	}
}

func TestResolveScopeSelectsTransitiveRequiredTests(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	report, err := Evaluate(validContext(t, root, req, deterministicExpansionGraph()))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !containsNodeReceipt(report.RelevantNodes, "closure.TestTransitiveRequired") {
		t.Fatalf("transitive required test missing: %#v", report.RelevantNodes)
	}
}

func TestResolveScopeDoesNotMutateGraphIndex(t *testing.T) {
	root := authoredFileRoot(t, "x.go")
	req := validRequest()
	req.Scope.Files = []string{"x.go"}
	graph := deterministicExpansionGraph()
	before := cloneGraphIndex(graph)
	if _, err := Evaluate(validContext(t, root, req, graph)); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !reflect.DeepEqual(before, graph) {
		t.Fatal("Evaluate mutated graph index")
	}
}

func TestResolveScopeAwarenessEnforcementShapeIncludesAllRelevantTestsOnFirstRun(t *testing.T) {
	root := authoredFileRoot(t, "golang/architecture/closure/model.go")
	req := validRequest()
	req.Scope.Files = []string{"golang/architecture/closure/model.go"}
	graph := BuildGraphIndex(mustTriples(t, nt(
		triple(classIRI("source_file", "golang/architecture/closure/model.go"), rdf.PropType, rdf.ClassSourceFile, true),
		triple(classIRI("source_file", "golang/architecture/closure/model.go"), rdf.PropConstrainedByInvariant, classIRI("invariant", "awareness.enforcement"), true),
		triple(classIRI("invariant", "awareness.enforcement"), rdf.PropType, rdf.ClassInvariant, true),
		triple(classIRI("invariant", "awareness.enforcement"), rdf.PropRequiresTest, classIRI("test", "cmd/awg/cmd_infer_claims_test.go:TestInferClaimsSkipsDraftGovernedDirectionalRecords"), true),
		triple(classIRI("invariant", "awareness.enforcement"), rdf.PropRequiresTest, classIRI("test", "cmd/awg/cmd_infer_claims_test.go:TestInferClaimsSynthesizesGovernedDirectionalClaimsFromGraph"), true),
		triple(classIRI("invariant", "awareness.enforcement"), rdf.PropRequiresTest, classIRI("test", "golang/architecture/closure/model_test.go:TestFailureModeBlockerClearsWhenSourceFileIsVulnerableToFailureMode"), true),
		triple(classIRI("invariant", "awareness.enforcement"), rdf.PropRequiresTest, classIRI("test", "golang/extractor/yaml_import_test.go:TestImportFixture_KnownTriplesPresent"), true),
		triple(classIRI("test", "cmd/awg/cmd_infer_claims_test.go:TestInferClaimsSkipsDraftGovernedDirectionalRecords"), rdf.PropType, rdf.ClassTest, true),
		triple(classIRI("test", "cmd/awg/cmd_infer_claims_test.go:TestInferClaimsSynthesizesGovernedDirectionalClaimsFromGraph"), rdf.PropType, rdf.ClassTest, true),
		triple(classIRI("test", "golang/architecture/closure/model_test.go:TestFailureModeBlockerClearsWhenSourceFileIsVulnerableToFailureMode"), rdf.PropType, rdf.ClassTest, true),
		triple(classIRI("test", "golang/extractor/yaml_import_test.go:TestImportFixture_KnownTriplesPresent"), rdf.PropType, rdf.ClassTest, true),
	)))
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	want := []string{
		"cmd%2Fawg%2Fcmd_infer_claims_test.go:TestInferClaimsSkipsDraftGovernedDirectionalRecords",
		"cmd%2Fawg%2Fcmd_infer_claims_test.go:TestInferClaimsSynthesizesGovernedDirectionalClaimsFromGraph",
		"golang%2Farchitecture%2Fclosure%2Fmodel_test.go:TestFailureModeBlockerClearsWhenSourceFileIsVulnerableToFailureMode",
		"golang%2Fextractor%2Fyaml_import_test.go:TestImportFixture_KnownTriplesPresent",
	}
	for _, id := range want {
		if !containsNodeReceipt(report.RelevantNodes, id) {
			t.Fatalf("expected relevant test %q missing from first run: %#v", id, report.RelevantNodes)
		}
	}
}

func TestClosureDeterministicAcrossFreshSubprocesses(t *testing.T) {
	if os.Getenv("SENSEI_CLOSURE_SUBPROCESS") == "1" {
		root := authoredFileRoot(t, "x.go")
		req := validRequest()
		req.Scope.Files = []string{"x.go"}
		report, err := Evaluate(validContext(t, root, req, deterministicExpansionGraph()))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		data, err := MarshalCanonicalReportYAML(report)
		if err != nil {
			t.Fatalf("MarshalCanonicalReportYAML: %v", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			t.Fatalf("stdout: %v", err)
		}
		return
	}
	var first []byte
	for i := 0; i < 10; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=TestClosureDeterministicAcrossFreshSubprocesses")
		cmd.Env = append(os.Environ(), "SENSEI_CLOSURE_SUBPROCESS=1")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("subprocess run %d: %v", i+1, err)
		}
		if i == 0 {
			first = out
			continue
		}
		if !bytes.Equal(first, out) {
			t.Fatalf("subprocess run %d differed from run 1", i+1)
		}
	}
}

func TestGraphSnapshotDigestMismatchIsUncertifiable(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	ctx := validContext(t, root, req, graph)
	ctx.GraphReceipt = graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: "different", Verified: true}
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictUncertifiable {
		t.Fatalf("verdict = %s", report.Verdict)
	}
}

func TestMatchingBindingsAreCertifiable(t *testing.T) {
	root, graph := closedFixture(t)
	report, err := Evaluate(validContext(t, root, validRequest(), graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictClosed {
		t.Fatalf("verdict = %s blockers=%#v", report.Verdict, report.Blockers)
	}
}

func TestEmptyMeasuredSurfaceCannotClose(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	ctx := validContext(t, root, req, GraphIndex{Nodes: map[string]Node{}, NodesByID: map[string]string{}})
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict == VerdictClosed {
		t.Fatalf("empty surface closed: %#v", report)
	}
	assertBlocker(t, report, "closure.scope.empty_measured_surface")
}

func TestMissingRepositoryFileIsStructuralOpen(t *testing.T) {
	root := t.TempDir()
	req := validRequest()
	ctx := validContext(t, root, req, sourceFileGraph("x.go"))
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_missing")
}

func TestUnknownRiskMakesOverallUncertifiable(t *testing.T) {
	req := validRequest()
	req.Scope.RiskClass = RiskUnknownImpact
	root, graph := closedFixture(t)
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictUncertifiable {
		t.Fatalf("verdict = %s", report.Verdict)
	}
}

func TestAnyRequiredUncertifiableMakesOverallUncertifiable(t *testing.T) {
	root, graph := closedFixture(t)
	ctx := validContext(t, root, validRequest(), graph)
	ctx.Maintenance = nil
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictUncertifiable {
		t.Fatalf("verdict = %s", report.Verdict)
	}
}

func TestAnyRequiredOpenMakesOverallOpen(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	req.Scope.AdditionalDimensions = []string{DimensionAuthority}
	req.Scope.AccessMode = AccessWrite
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictOpen {
		t.Fatalf("verdict = %s", report.Verdict)
	}
}

func TestPermittedConditionalMakesConditionallyClosed(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	req.Scope.AdditionalDimensions = []string{DimensionDirection}
	ctx := validContext(t, root, req, graph)
	ctx.Dialogue = &architecture.DialogueDocument{
		SchemaVersion: "1", CompiledBy: "test", Binding: binding(),
		OpenQuestions: []architecture.OpenQuestion{acceptedUnknownQuestion("q.medium", DimensionDirection, "medium")},
	}
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictConditionallyClosed {
		t.Fatalf("verdict = %s blockers=%#v conditions=%#v", report.Verdict, report.Blockers, report.Conditions)
	}
}

func TestAcceptedUnknownCannotConditionAuthority(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	req.Scope.AdditionalDimensions = []string{DimensionAuthority}
	ctx := validContext(t, root, req, graph)
	ctx.Dialogue = &architecture.DialogueDocument{
		SchemaVersion: "1", CompiledBy: "test", Binding: binding(),
		OpenQuestions: []architecture.OpenQuestion{acceptedUnknownQuestion("q.auth", DimensionAuthority, "medium")},
	}
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if report.Verdict != VerdictOpen {
		t.Fatalf("verdict = %s", report.Verdict)
	}
	assertBlocker(t, report, "closure.question.accepted_unknown_blocks")
}

func TestEvaluatorDoesNotGenerateQuestionText(t *testing.T) {
	root, graph := closedFixture(t)
	req := validRequest()
	ctx := validContext(t, root, req, graph)
	ctx.Claims.Claims = []architecture.Claim{{ID: "claim.unknown", Statement: architecture.ClaimStatement{Subject: "s", Predicate: "p", Object: "o"}, Scope: architecture.ClaimScope{Files: []string{"x.go"}}, ArchitecturalPlane: architecture.PlaneObserved, EpistemicStatus: architecture.StatusUnknown, PromotionStatus: architecture.PromotionCandidate, HumanReviewRequired: true}}
	report, err := Evaluate(ctx)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	data, err := MarshalCanonicalReportYAML(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("question_text")) || bytes.Contains(data, []byte("?")) {
		t.Fatalf("report appears to generate question text:\n%s", data)
	}
}

func TestClosureReportIsDeterministic(t *testing.T) {
	root, graph := closedFixture(t)
	report, err := Evaluate(validContext(t, root, validRequest(), graph))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	a, err := MarshalCanonicalReportYAML(report)
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalCanonicalReportYAML(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("report rendering is not deterministic")
	}
	if bytes.Contains(a, []byte("score")) {
		t.Fatal("closure report must not contain a score")
	}
}

func TestBlockerIDsIgnoreSummaryWording(t *testing.T) {
	a := Blocker{Dimension: DimensionAuthority, Code: "closure.authority.owner_missing", Summary: "one", NodeIDs: []string{"n"}, RequiredNextAction: "define_authority"}
	b := a
	b.Summary = "two"
	if blockerID(a) != blockerID(b) {
		t.Fatal("blocker ID changed with wording")
	}
}

func TestMachineAdoptedKnowledgeUsesRiskPolicy(t *testing.T) {
	node := Node{ID: "invariant.route_state", Classes: []string{"invariant"}, Status: "machine_adopted"}
	architectureRequest := validRequest()
	architectureRequest.Scope.RiskClass = RiskArchitectureSensitive
	policy, _ := PolicyForRisk(RiskArchitectureSensitive)
	builder := assessmentBuilder{ctx: Context{Request: architectureRequest}, policy: policy, scope: resolvedScope{Nodes: []Node{node}}}
	builder.applyMachineAdoptedRiskPolicy(DimensionBehavioral)
	if len(builder.conditions) != 1 || len(builder.blockers) != 0 {
		t.Fatalf("architecture-sensitive disposition conditions=%#v blockers=%#v", builder.conditions, builder.blockers)
	}
	if builder.conditions[0].RequiredNextAction != "review_machine_adopted_knowledge" {
		t.Fatalf("condition=%#v", builder.conditions[0])
	}

	lowRequest := validRequest()
	lowPolicy, _ := PolicyForRisk(RiskLowRisk)
	low := assessmentBuilder{ctx: Context{Request: lowRequest}, policy: lowPolicy, scope: resolvedScope{Nodes: []Node{node}}}
	low.applyMachineAdoptedRiskPolicy(DimensionBehavioral)
	if len(low.conditions)+len(low.blockers) != 0 {
		t.Fatalf("low-risk machine adoption was unnecessarily conditioned: %#v %#v", low.conditions, low.blockers)
	}

	securityRequest := validRequest()
	securityRequest.Scope.RiskClass = RiskSecurity
	securityPolicy, _ := PolicyForRisk(RiskSecurity)
	security := assessmentBuilder{ctx: Context{Request: securityRequest}, policy: securityPolicy, scope: resolvedScope{Nodes: []Node{node}}}
	security.applyMachineAdoptedRiskPolicy(DimensionBehavioral)
	if len(security.blockers) != 1 || security.blockers[0].Code != "closure.machine_adopted.behavioral.stronger_basis_required" {
		t.Fatalf("security disposition=%#v", security.blockers)
	}
}

func TestAwarenessMutationConditionallySupportsBehaviorPlane(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei", "tasks", "task.demo", "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness", "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "awareness", "architecture", "components.yaml"), []byte("components:\n  - id: component.demo\n    name: Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := AwarenessMutationEnforcementDocument{
		SchemaVersion:      "1",
		PolicyID:           AwarenessMutationEnforcementPolicyV1,
		TaskID:             "task.demo",
		RepositoryRevision: "rev",
		GraphDigestSHA256:  "graph",
		Plans: []AwarenessMutationEnforcementPlan{{
			SourcePath:           "docs/awareness/architecture/components.yaml",
			SourceClass:          "canonical_awareness_component_registry",
			ImporterID:           "awareness.component_yaml_import.v1",
			RequiredVerification: []string{"sensei_check", "sensei_validate", "strict_build"},
		}},
	}
	data, err := MarshalCanonicalAwarenessMutationEnforcementYAML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sensei", "tasks", "task.demo", "source", "awareness-mutation-enforcement.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	req := validRequest()
	req.Scope.RiskClass = RiskArchitectureSensitive
	req.Scope.AccessMode = AccessReadWrite
	req.Scope.DirectionRequirement = DirectionPreserve
	req.Scope.Files = []string{"docs/awareness/architecture/components.yaml"}
	req.AwarenessMutation = &AwarenessMutationBinding{
		TaskID:           "task.demo",
		Path:             ".sensei/tasks/task.demo/source/awareness-mutation-enforcement.yaml",
		PlanDigestSHA256: mustAwarenessMutationDigest(t, doc),
		PolicyID:         AwarenessMutationEnforcementPolicyV1,
	}
	graph := sourceFileGraph("docs/awareness/architecture/components.yaml")
	report, err := Evaluate(validContext(t, root, req, graph))
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range report.Blockers {
		if blocker.Code == "closure.behavior.surface_empty" || blocker.Code == "closure.behavior.observed_or_enforced_missing" {
			t.Fatalf("unexpected blocker %#v", blocker)
		}
	}
	found := false
	for _, condition := range report.Conditions {
		if condition.Code == "closure.awareness_mutation.enforcement_required" {
			found = true
			if len(condition.Files) != 1 || condition.Files[0] != "docs/awareness/architecture/components.yaml" {
				t.Fatalf("condition=%#v", condition)
			}
		}
	}
	if !found {
		t.Fatalf("conditions=%#v", report.Conditions)
	}
}

func validRequest() Request {
	return Request{
		SchemaVersion: SchemaVersion,
		Binding:       binding(),
		Scope: Scope{
			Domain: "repository", TaskClass: "modify_repository_admission", RiskClass: RiskLowRisk,
			AccessMode: AccessRead, DirectionRequirement: DirectionNotApplicable, Files: []string{"x.go"},
		},
	}
}

func mustAwarenessMutationDigest(t *testing.T, doc AwarenessMutationEnforcementDocument) string {
	t.Helper()
	digest, err := AwarenessMutationEnforcementDigest(doc)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func binding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain: "example.test/repo", Revision: "rev", RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

func bootstrapBinding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example.test/repo",
		Revision:          strings.Repeat("a", 40),
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: strings.Repeat("b", 64),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

func validDirectionBootstrap() *DirectionBootstrapAuthorization {
	auth := &DirectionBootstrapAuthorization{
		SchemaVersion:                DirectionBootstrapSchemaVersion,
		PolicyID:                     DirectionBootstrapPolicyID,
		TaskID:                       "task.bootstrap.direction",
		BaseRevision:                 strings.Repeat("a", 40),
		GraphDigestSHA256:            strings.Repeat("b", 64),
		File:                         DirectionBootstrapFile,
		GovernedRecordIDs:            []string{"decision.desired", "decision.intended"},
		ExpectedMutationDigestSHA256: strings.Repeat("c", 64),
		ApprovedBy:                   "architect",
		ApprovalMechanism:            DirectionBootstrapMechanismFile,
		ApprovalStatement:            "bootstrap exactly once",
		UsagePolicy:                  DirectionBootstrapUsageOneUse,
		IssuedAt:                     "2026-07-15T00:00:00Z",
		ExpiresAt:                    "2026-07-16T00:00:00Z",
		ApprovalSourcePath:           "/approved/bootstrap-direction.yaml",
		ApprovalSourceDigestSHA256:   strings.Repeat("d", 64),
	}
	auth.AuthorizationDigestSHA256 = DirectionBootstrapAuthorizationDigest(*auth)
	return auth
}

func validContext(t *testing.T, root string, req Request, graph GraphIndex) Context {
	t.Helper()
	planeReport := planeReportEmpty(req.Binding)
	return Context{
		Request:     req,
		Claims:      architecture.ClaimDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: req.Binding},
		Maintenance: &maintenance.Report{SchemaVersion: "1", GeneratedBy: "test", CurrentBinding: req.Binding, ObservedBinding: req.Binding},
		Plane:       &planeReport,
		Dialogue:    &architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: req.Binding, OpenQuestions: []architecture.OpenQuestion{}},
		Evidence:    &maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: req.Binding, Evidence: []maintenance.EvidenceState{}},
		Graph:       graph, GraphReceipt: graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: req.Binding.GraphDigestSHA256, Verified: true},
		RepositoryRoot: root, RepositoryRev: req.Binding.Revision, RepositoryStatus: architecture.RevisionResolved,
	}
}

func testFactReceipt(id, kind, predicate string, meta map[string]string) architecture.Fact {
	files := []string{"x.go"}
	if target := strings.TrimSpace(meta["target_file"]); target != "" {
		files = []string{target}
	}
	return architecture.Fact{
		ID:        id,
		Kind:      kind,
		Subject:   "S",
		Predicate: predicate,
		Object:    "O",
		Meta:      meta,
		Scope: architecture.Scope{
			Repository: "example.test/repo",
			Files:      files,
			Symbols:    []string{"S"},
		},
		Confidence: 0.8,
		Extractor:  "test",
		Provenance: &architecture.Provenance{
			RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			RepositoryDomain:       "example.test/repo",
			RevisionStatus:         architecture.RevisionResolved,
			Revision:               "rev",
			SourceDigestStatus:     architecture.SourceDigestUnavailable,
			SourceKind:             "test",
		},
	}
}

func claimDoc(t *testing.T, claims []architecture.Claim, facts []architecture.Fact) architecture.ClaimDocument {
	t.Helper()
	receipts := make([]architecture.ClaimFactReceipt, 0, len(facts))
	for _, f := range facts {
		prov := architecture.Provenance{}
		if f.Provenance != nil {
			prov = *f.Provenance
		}
		receipts = append(receipts, architecture.ClaimFactReceipt{Fact: f, Provenance: prov})
	}
	doc, err := architecture.NormalizeClaimDocument(architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "test",
		Binding:       binding(),
		FactReceipts:  receipts,
		Claims:        claims,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func planeReportEmpty(binding architecture.ClaimDocumentBinding) plane.Report {
	return plane.Report{
		SchemaVersion: "1", GeneratedBy: "test",
		ClaimBinding: plane.ClaimBindingReport{
			RepositoryDomain:  binding.RepositoryDomain,
			Revision:          binding.Revision,
			RevisionStatus:    binding.RevisionStatus,
			GraphDigestSHA256: binding.GraphDigestSHA256,
			GraphDigestStatus: binding.GraphDigestStatus,
		},
		GraphSnapshot: plane.GraphSnapshotReport{DigestSHA256: binding.GraphDigestSHA256, DigestStatus: binding.GraphDigestStatus},
	}
}

func closedFixture(t *testing.T) (string, GraphIndex) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, sourceFileGraph("x.go")
}

func sourceFileGraph(path string) GraphIndex {
	iri := classIRI("source_file", path)
	return BuildGraphIndex(mustTriplesForPanic(nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
		triple(iri, rdf.PropSourcePath, path, false),
	)))
}

func sourceFileGraphCanonical(path string) GraphIndex {
	iri := classIRI("source_file", path)
	return BuildGraphIndex(mustTriplesForPanic(nt(
		triple(iri, rdf.PropType, rdf.ClassSourceFile, true),
	)))
}

func sourceFileVulnerableToGraph(path, failureID string) GraphIndex {
	fileIRI := classIRI("source_file", path)
	failureIRI := classIRI("failure_mode", failureID)
	return BuildGraphIndex(mustTriplesForPanic(nt(
		triple(fileIRI, rdf.PropType, rdf.ClassSourceFile, true),
		triple(fileIRI, rdf.PropVulnerableTo, failureIRI, true),
		triple(failureIRI, rdf.PropType, rdf.ClassFailureMode, true),
		triple(failureIRI, rdf.PropLabel, "scope failure", false),
	)))
}

func sourceFileVulnerableToFailureModeWithStatus(path, failureID, status string) GraphIndex {
	fileIRI := classIRI("source_file", path)
	failureIRI := classIRI("failure_mode", failureID)
	return BuildGraphIndex(mustTriplesForPanic(nt(
		triple(fileIRI, rdf.PropType, rdf.ClassSourceFile, true),
		triple(fileIRI, rdf.PropVulnerableTo, failureIRI, true),
		triple(failureIRI, rdf.PropType, rdf.ClassFailureMode, true),
		triple(failureIRI, rdf.PropStatus, status, false),
		triple(failureIRI, rdf.PropLabel, "scope failure", false),
	)))
}

type authorityDomainOptions struct {
	Status        string
	CoversPaths   []string
	OwnsStates    []string
	OwnerServices []string
	MayWrite      []string
	MayRead       []string
	MustMutateVia []string
	MustReadVia   []string
	ObservesVia   []string
	TruthLayers   []string
}

func authorityDomainGraph(id string, opts authorityDomainOptions) GraphIndex {
	iri := classIRI("authority_domain", id)
	lines := []string{triple(iri, rdf.PropType, rdf.ClassAuthorityDomain, true)}
	if opts.Status != "" {
		lines = append(lines, triple(iri, rdf.PropStatus, opts.Status, false))
	}
	for _, path := range opts.CoversPaths {
		lines = append(lines, triple(iri, rdf.PropCoversPath, path, false))
	}
	for _, state := range opts.OwnsStates {
		lines = append(lines, triple(iri, rdf.PropOwnsState, state, false))
	}
	for _, owner := range opts.OwnerServices {
		lines = append(lines, triple(iri, rdf.PropOwnerService, owner, false))
	}
	for _, writer := range opts.MayWrite {
		lines = append(lines, triple(iri, rdf.PropMayWrite, writer, false))
	}
	for _, reader := range opts.MayRead {
		lines = append(lines, triple(iri, rdf.PropMayRead, reader, false))
	}
	for _, path := range opts.MustMutateVia {
		lines = append(lines, triple(iri, rdf.PropMustMutateVia, path, false))
	}
	for _, path := range opts.MustReadVia {
		lines = append(lines, triple(iri, rdf.PropMustReadVia, path, false))
	}
	for _, path := range opts.ObservesVia {
		lines = append(lines, triple(iri, rdf.PropObservesVia, path, false))
	}
	for _, layer := range opts.TruthLayers {
		lines = append(lines, triple(iri, rdf.PropHasTruthLayer, layer, false))
	}
	return BuildGraphIndex(mustTriplesForPanic(nt(lines...)))
}

func componentStateGraph(id, authoredFile string, writesTo ...string) GraphIndex {
	iri := classIRI("component", id)
	lines := []string{
		triple(iri, rdf.PropType, rdf.ClassComponent, true),
		triple(iri, rdf.PropAuthoredIn, authoredFile, false),
	}
	for _, state := range writesTo {
		lines = append(lines, triple(iri, rdf.PropWritesTo, state, false))
	}
	return BuildGraphIndex(mustTriplesForPanic(nt(lines...)))
}

func mergeGraphs(graphs ...GraphIndex) GraphIndex {
	merged := GraphIndex{
		Nodes:       map[string]Node{},
		NodesByID:   map[string]string{},
		FilesByPath: map[string]string{},
		SymbolsByID: map[string]string{},
	}
	for _, graph := range graphs {
		for iri, node := range graph.Nodes {
			merged.Nodes[iri] = node
		}
		for id, iri := range graph.NodesByID {
			merged.NodesByID[id] = iri
		}
		for path, iri := range graph.FilesByPath {
			merged.FilesByPath[path] = iri
		}
		for id, iri := range graph.SymbolsByID {
			merged.SymbolsByID[id] = iri
		}
	}
	return merged
}

type authoredNodeOptions struct {
	Status          string
	PromotionStatus string
	ReviewStatus    string
	SourceKind      string
	AuthoredIn      []string
}

func authoredInGraph(class, classIRIValue, id string, opts authoredNodeOptions) GraphIndex {
	iri := classIRI(class, id)
	lines := []string{triple(iri, rdf.PropType, classIRIValue, true)}
	if opts.Status != "" {
		lines = append(lines, triple(iri, rdf.PropStatus, opts.Status, false))
	}
	if opts.PromotionStatus != "" {
		lines = append(lines, triple(iri, rdf.PropPromotionStatus, opts.PromotionStatus, false))
	}
	if opts.ReviewStatus != "" {
		lines = append(lines, triple(iri, rdf.PropReviewStatus, opts.ReviewStatus, false))
	}
	if opts.SourceKind != "" {
		lines = append(lines, triple(iri, rdf.PropSourceKind, opts.SourceKind, false))
	}
	for _, path := range opts.AuthoredIn {
		lines = append(lines, triple(iri, rdf.PropAuthoredIn, path, false))
	}
	return BuildGraphIndex(mustTriplesForPanic(nt(lines...)))
}

func authoredFileRoot(t *testing.T, rel string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("kind: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func testCanonicalAuthoredInRepresentsExactFile(t *testing.T, class, classIRIValue, status string) {
	t.Helper()
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph(class, classIRIValue, class+".scope", authoredNodeOptions{
		Status:     status,
		AuthoredIn: []string{"docs/awareness/architecture/decisions.yaml"},
	})))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertNoBlocker(t, report, "closure.structural.file_unrepresented")
	if !contains(report.ScopeReceipt.Files, "docs/awareness/architecture/decisions.yaml") {
		t.Fatalf("file not represented: %#v", report.ScopeReceipt)
	}
	var got *FileRepresentationReceipt
	for i := range report.ScopeReceipt.RepresentedFiles {
		if report.ScopeReceipt.RepresentedFiles[i].Path == "docs/awareness/architecture/decisions.yaml" {
			got = &report.ScopeReceipt.RepresentedFiles[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("represented file receipt missing: %#v", report.ScopeReceipt.RepresentedFiles)
	}
	if got.RepresentationKind != "governed_authored_source" {
		t.Fatalf("representation kind = %q", got.RepresentationKind)
	}
	if !contains(got.AnchorNodeIDs, class+".scope") {
		t.Fatalf("anchor ids = %#v", got.AnchorNodeIDs)
	}
}

func deterministicExpansionGraph() GraphIndex {
	return BuildGraphIndex(mustTriplesForPanic(nt(
		triple(classIRI("source_file", "x.go"), rdf.PropType, rdf.ClassSourceFile, true),
		triple(classIRI("source_file", "x.go"), rdf.PropDependsOn, classIRI("component", "component.x"), true),
		triple(classIRI("component", "component.x"), rdf.PropType, rdf.ClassComponent, true),
		triple(classIRI("component", "component.x"), rdf.PropExposesContract, classIRI("contract", "contract.x"), true),
		triple(classIRI("contract", "contract.x"), rdf.PropType, rdf.ClassContract, true),
		triple(classIRI("contract", "contract.x"), rdf.PropConstrainedByInvariant, classIRI("invariant", "invariant.x"), true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropType, rdf.ClassInvariant, true),
		triple(classIRI("invariant", "invariant.x"), rdf.PropRequiresTest, classIRI("test", "closure.TestTransitiveRequired"), true),
		triple(classIRI("test", "closure.TestTransitiveRequired"), rdf.PropType, rdf.ClassTest, true),
	)))
}

func cloneGraphIndex(in GraphIndex) GraphIndex {
	out := GraphIndex{
		Nodes:       make(map[string]Node, len(in.Nodes)),
		NodesByID:   make(map[string]string, len(in.NodesByID)),
		FilesByPath: make(map[string]string, len(in.FilesByPath)),
		SymbolsByID: make(map[string]string, len(in.SymbolsByID)),
	}
	for k, v := range in.Nodes {
		out.Nodes[k] = v
	}
	for k, v := range in.NodesByID {
		out.NodesByID[k] = v
	}
	for k, v := range in.FilesByPath {
		out.FilesByPath[k] = v
	}
	for k, v := range in.SymbolsByID {
		out.SymbolsByID[k] = v
	}
	return out
}

func containsNodeReceipt(nodes []NodeReceipt, id string) bool {
	for _, node := range nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

func countNodeReceipt(nodes []NodeReceipt, id string) int {
	count := 0
	for _, node := range nodes {
		if node.ID == id {
			count++
		}
	}
	return count
}

func testIneligibleAuthoredInRepresentation(t *testing.T, opts authoredNodeOptions) {
	t.Helper()
	root := authoredFileRoot(t, "docs/awareness/architecture/decisions.yaml")
	req := validRequest()
	req.Scope.Files = []string{"docs/awareness/architecture/decisions.yaml"}
	opts.AuthoredIn = []string{"docs/awareness/architecture/decisions.yaml"}
	report, err := Evaluate(validContext(t, root, req, authoredInGraph("decision", rdf.ClassDecision, "decision.scope", opts)))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	assertBlocker(t, report, "closure.structural.file_unrepresented")
}

func acceptedUnknownQuestion(id, dim, priority string) architecture.OpenQuestion {
	return architecture.OpenQuestion{
		ID: id, QuestionText: "fixture", Scope: architecture.ClaimScope{Files: []string{"x.go"}},
		BlocksClosureDimension: dim, BlocksClaims: []string{}, AcceptedAnswerTypes: []string{architecture.AnswerTypeUnknownAcknowledgement},
		Priority: priority, Status: architecture.QuestionStatusAcceptedUnknown, ResolvedByAnswers: []string{"answer." + id},
	}
}

func assertBlocker(t *testing.T, report Report, code string) {
	t.Helper()
	for _, b := range report.Blockers {
		if b.Code == code {
			return
		}
	}
	t.Fatalf("missing blocker %s in %#v", code, report.Blockers)
}

func assertNoBlocker(t *testing.T, report Report, code string) {
	t.Helper()
	for _, b := range report.Blockers {
		if b.Code == code {
			t.Fatalf("unexpected blocker %s in %#v", code, report.Blockers)
		}
	}
}

func assertAuthorityBlocker(t *testing.T, report Report, code string) {
	t.Helper()
	for _, b := range report.Blockers {
		if b.Dimension == DimensionAuthority && b.Code == code {
			return
		}
	}
	t.Fatalf("missing authority blocker %s in %#v", code, report.Blockers)
}

func assertNoAuthorityBlocker(t *testing.T, report Report, code string) {
	t.Helper()
	for _, b := range report.Blockers {
		if b.Dimension == DimensionAuthority && b.Code == code {
			t.Fatalf("unexpected authority blocker %s in %#v", code, report.Blockers)
		}
	}
}

func mustTriples(t *testing.T, data string) []graphsnapshot.Triple {
	t.Helper()
	triples, err := graphsnapshot.Read(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return triples
}

func mustTriplesForPanic(data string) []graphsnapshot.Triple {
	triples, err := graphsnapshot.Read(strings.NewReader(data))
	if err != nil {
		panic(err)
	}
	return triples
}

func nt(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func triple(subject, predicate, object string, objectIRI bool) string {
	if objectIRI {
		return "<" + subject + "> <" + predicate + "> <" + object + "> ."
	}
	return "<" + subject + "> <" + predicate + "> " + strconvQuote(object) + " ."
}

func classIRI(class, id string) string {
	classIRI := map[string]string{
		"source_file":      rdf.ClassSourceFile,
		"component":        rdf.ClassComponent,
		"authority_domain": rdf.ClassAuthorityDomain,
		"decision":         rdf.ClassDecision,
		"invariant":        rdf.ClassInvariant,
		"failure_mode":     rdf.ClassFailureMode,
		"contract":         rdf.ClassContract,
		"boundary":         rdf.ClassBoundary,
		"intent":           rdf.ClassIntent,
	}[class]
	return strings.Trim(strings.TrimSuffix(rdf.MintIRI(classIRI, id), ">"), "<")
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
