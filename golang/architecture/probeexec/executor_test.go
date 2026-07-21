// SPDX-License-Identifier: AGPL-3.0-only

package probeexec

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/probe"
)

func probeFixture(t *testing.T) Context {
	t.Helper()
	root := t.TempDir()
	data := []byte("package sample\n")
	if err := os.WriteFile(filepath.Join(root, "sample.go"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	binding := architecture.ClaimDocumentBinding{RepositoryDomain: "github.com/example/repo", Revision: "abc", RevisionStatus: architecture.RevisionResolved, GraphDigestSHA256: "graph", GraphDigestStatus: architecture.GraphDigestResolved}
	fact := architecture.Fact{ID: "fact.one", Kind: "source", Subject: "sample", Predicate: "defines", Object: "sample", Scope: architecture.Scope{Repository: binding.RepositoryDomain, Files: []string{"sample.go"}}, Evidence: architecture.Evidence{SourceFile: "sample.go"}, Extractor: "test"}
	prov := architecture.Provenance{RepositoryDomain: binding.RepositoryDomain, RepositoryDomainStatus: architecture.RepositoryDomainResolved, Revision: binding.Revision, RevisionStatus: architecture.RevisionResolved, SourceDigest: hex.EncodeToString(sum[:]), SourceDigestStatus: architecture.SourceDigestResolved, SourceKind: "source_file"}
	p := probe.EvidenceProbe{ID: "probe.one", QuestionID: "question.one", Status: probe.StatusProposed, ProbeKind: probe.KindSourceReceiptVerification, SafetyClass: probe.SafetyStaticRead, ApprovalGate: probe.GateNone, AutomaticExecutionAllowed: true, EvidenceRole: probe.RoleDiagnostic, Steps: []probe.ProbeStep{{Kind: probe.StepVerifySourceDigest, Target: "sample.go", SourceRef: "evidence:fact.one", Description: "verify"}}}
	return Context{
		RepositoryRoot: root, Binding: binding, Probes: probe.ProbeDocument{Binding: binding, Probes: []probe.EvidenceProbe{p}}, ProbeDocumentDigest: strings.Repeat("0", 64),
		Claims:        architecture.ClaimDocument{Binding: binding, FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: prov}}},
		EvidenceState: maintenance.EvidenceStateDocument{SchemaVersion: "1", Binding: binding, Evidence: []maintenance.EvidenceState{}}, ObservedAt: "2026-07-14T18:30:00Z", Budget: DefaultBudget(),
	}
}

func TestProbeResultPreservesExactReceipts(t *testing.T) {
	ctx := probeFixture(t)
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results.Results) != 1 || len(result.Results.Results[0].Artifacts) != 1 {
		t.Fatalf("result=%+v", result.Results.Results)
	}
	if result.Results.Results[0].ResultStatus != probe.ResultCompleted || result.Results.Results[0].Artifacts[0].Path != "sample.go" {
		t.Fatalf("result=%+v", result.Results.Results[0])
	}
}

func TestStaticProbeCannotExecuteShell(t *testing.T) {
	ctx := probeFixture(t)
	ctx.Probes.Probes[0].Steps[0].Command = "cat sample.go"
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].ReasonCode != ReasonCommandRejected || result.Metrics.ProbesExecuted != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestStaticProbeCannotUseNetwork(t *testing.T) {
	ctx := probeFixture(t)
	ctx.Probes.Probes[0].ProbeKind = probe.KindOwnerPathRuntimeObservation
	ctx.Probes.Probes[0].SafetyClass = probe.SafetyRuntimeRead
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].ReasonCode != ReasonKindUnsupported {
		t.Fatalf("decision=%+v", result.Decisions[0])
	}
}

func TestStaticProbeCannotReadOutsideRepository(t *testing.T) {
	ctx := probeFixture(t)
	ctx.Probes.Probes[0].Steps[0].Target = "../secret"
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Results.Results[0].ResultStatus != probe.ResultRejected {
		t.Fatalf("result=%+v", result.Results.Results[0])
	}
}

func TestStaticProbeCannotReadSecrets(t *testing.T) {
	ctx := probeFixture(t)
	if err := os.WriteFile(filepath.Join(ctx.RepositoryRoot, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx.Probes.Probes[0].Steps[0].Kind = probe.StepInspectSource
	ctx.Probes.Probes[0].Steps[0].Target = ".env"
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Results.Results[0].ResultStatus != probe.ResultRejected || !strings.Contains(strings.Join(result.Results.Results[0].Notes, " "), ReasonSecretRejected) {
		t.Fatalf("result=%+v", result.Results.Results[0])
	}
}

func TestStaticProbeKindRegistryIsClosed(t *testing.T) {
	ctx := probeFixture(t)
	ctx.Probes.Probes[0].ProbeKind = "custom"
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].ReasonCode != ReasonKindUnsupported {
		t.Fatalf("decision=%+v", result.Decisions[0])
	}
}

func TestInconclusiveProbePreservesUnknown(t *testing.T) {
	ctx := probeFixture(t)
	ctx.Claims.FactReceipts[0].Provenance.SourceDigest = strings.Repeat("0", 64)
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Results.Results[0].ResultStatus != probe.ResultInconclusive {
		t.Fatalf("result=%+v", result.Results.Results[0])
	}
}

func TestProbeBudgetIsEnforced(t *testing.T) {
	ctx := probeFixture(t)
	second := ctx.Probes.Probes[0]
	second.ID = "probe.two"
	ctx.Probes.Probes = append(ctx.Probes.Probes, second)
	ctx.Budget.MaxProbes = 1
	result, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Metrics.BudgetExhausted || result.Metrics.ProbesExecuted != 1 {
		t.Fatalf("metrics=%+v", result.Metrics)
	}
}

func TestProbeReplayDoesNotExecuteTwice(t *testing.T) {
	ctx := probeFixture(t)
	first, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ctx.ExistingResults = &first.Results
	ctx.EvidenceState = first.EvidenceState
	second, err := ExecuteBatch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Metrics.ProbesExecuted != 0 || second.Metrics.ReplayPrevented != 1 {
		t.Fatalf("metrics=%+v", second.Metrics)
	}
}
