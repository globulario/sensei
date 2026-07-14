// SPDX-License-Identifier: Apache-2.0

package closure

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func binding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain: "example.test/repo", Revision: "rev", RevisionStatus: architecture.RevisionResolved,
		GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

func validContext(t *testing.T, root string, req Request, graph GraphIndex) Context {
	t.Helper()
	planeReport := planeReportEmpty()
	return Context{
		Request:     req,
		Claims:      architecture.ClaimDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: binding()},
		Maintenance: &maintenance.Report{SchemaVersion: "1", GeneratedBy: "test", CurrentBinding: binding(), ObservedBinding: binding()},
		Plane:       &planeReport,
		Dialogue:    &architecture.DialogueDocument{SchemaVersion: "1", CompiledBy: "test", Binding: binding(), OpenQuestions: []architecture.OpenQuestion{}},
		Evidence:    &maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: "test", Binding: binding(), Evidence: []maintenance.EvidenceState{}},
		Graph:       graph, GraphReceipt: graphsnapshot.Receipt{Status: architecture.GraphDigestResolved, DigestSHA256: "graph", Verified: true},
		RepositoryRoot: root, RepositoryRev: "rev", RepositoryStatus: architecture.RevisionResolved,
	}
}

func planeReportEmpty() plane.Report {
	return plane.Report{
		SchemaVersion: "1", GeneratedBy: "test",
		ClaimBinding:  plane.ClaimBindingReport{RepositoryDomain: "example.test/repo", Revision: "rev", RevisionStatus: architecture.RevisionResolved, GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved},
		GraphSnapshot: plane.GraphSnapshotReport{DigestSHA256: "graph", DigestStatus: architecture.GraphDigestResolved},
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
	}[class]
	return strings.Trim(strings.TrimSuffix(rdf.MintIRI(classIRI, id), ">"), "<")
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
