// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// prepareFixtureTask creates a real task session in root and returns its
// directory together with the binding a lineage bundle would have recorded.
//
// It goes through tasksession.Prepare rather than hand-writing the task's
// files, because the drift check compares digests the task machinery computes.
// A hand-written task would let the fixture and the code under test agree on a
// shape neither the real pipeline nor the validator would accept, and the test
// would prove only that two pieces of my own code match.
//
// The claim document is synthetic — the repository has not been through
// inference — but it is synthetic in the one way that does not matter here:
// the drift check never reads a claim. It reads the control-state and closure
// digests the task derives, and those are derived by the real machinery from
// whatever the task was prepared with.
func prepareFixtureTask(t *testing.T, root, domain, revision, sessionDigest string) (string, synthesisRunTaskBinding) {
	t.Helper()

	graphBytes := []byte("<https://example.com/subject> <https://example.com/predicate> <https://example.com/object> .\n")
	graphPath := filepath.Join(root, ".sensei", "fixture-graph.nt")
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(graphPath, graphBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(graphBytes)
	graphDigest := hex.EncodeToString(sum[:])

	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain: domain, Revision: revision, RevisionStatus: "resolved",
		GraphDigestSHA256: graphDigest, GraphDigestStatus: "resolved",
	}
	fact := architecture.Fact{
		ID: "fact.fixture.1", Kind: "topology", Subject: "a.txt",
		Predicate: "declares_symbol", Object: "Fixture",
		Scope:      architecture.Scope{Repository: domain},
		Evidence:   architecture.Evidence{SourceFile: "a.txt", LineStart: 1, LineEnd: 1},
		Confidence: 1, Extractor: "fixture_extractor",
	}
	claims := architecture.ClaimDocument{
		SchemaVersion: "architecture.claims.v1", GeneratedBy: "fixture", Binding: binding,
		FactReceipts: []architecture.ClaimFactReceipt{{Fact: fact, Provenance: architecture.Provenance{
			RepositoryDomain: domain, RepositoryDomainStatus: "resolved",
			Revision: revision, RevisionStatus: "resolved",
			SourceDigest: graphDigest, SourceDigestStatus: "resolved", SourceKind: "source_file",
		}}},
		Claims: []architecture.Claim{{
			ID: "claim.fixture.1", Label: "fixture claim",
			Statement:              architecture.ClaimStatement{Subject: "a.txt", Predicate: "declares_symbol", Object: "Fixture"},
			Scope:                  architecture.ClaimScope{Repository: domain},
			ArchitecturalPlane:     architecture.PlaneObserved,
			AssertionOrigin:        architecture.OriginDerived,
			InferenceRule:          "fixture.rule.v1",
			EpistemicStatus:        architecture.StatusSupported,
			InvalidationConditions: []string{"a.txt no longer declares Fixture"},
			PromotionStatus:        architecture.PromotionCandidate,
			HumanReviewRequired:    true,
			PremiseFacts:           []string{"fact.fixture.1"},
		}},
	}
	claimsBytes, err := yaml.Marshal(struct {
		ArchitectureClaims architecture.ClaimDocument `yaml:"architecture_claims"`
	}{claims})
	if err != nil {
		t.Fatal(err)
	}
	claimsPath := filepath.Join(root, ".sensei", "fixture-claims.yaml")
	if err := os.WriteFile(claimsPath, claimsBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := tasksession.Prepare(tasksession.PrepareOptions{
		RepoRoot: root, RepositoryDomain: domain,
		Description: "fixture task", Mode: admission.ModeModify, TaskClass: "implementation",
		RiskClass: "low_risk", DirectionRequirement: "preserve",
		GraphNT: graphPath, Claims: claimsPath,
		Files:     []tasksession.FileOperation{{Operation: "modify", Path: "a.txt"}},
		SetActive: true,
	})
	if err != nil {
		t.Fatalf("prepare fixture task: %v", err)
	}
	taskDir := res.TaskDir
	if !filepath.IsAbs(taskDir) {
		taskDir = filepath.Join(root, taskDir)
	}

	return taskDir, fixtureTaskBinding(t, root, taskDir, sessionDigest)
}

// fixtureTaskBinding reads the task's CURRENT control and closure digests the
// same way synthesis-run does, so a bundle built from it is bound to the state
// that actually exists rather than to values the test asserted.
func fixtureTaskBinding(t *testing.T, root, taskDir, sessionDigest string) synthesisRunTaskBinding {
	t.Helper()
	session, err := tasksession.LoadSession(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load fixture task session: %v", err)
	}
	control, closureReport, _, err := tasksession.ResolveControlAndClosure(root, taskDir, false)
	if err != nil {
		t.Fatalf("resolve fixture task control/closure: %v", err)
	}
	closureDigest, err := closureprotocol.SemanticDigest(closureReport)
	if err != nil {
		t.Fatal(err)
	}
	return synthesisRunTaskBinding{
		TaskID:                       session.TaskID,
		TaskControlStateDigestSHA256: taskcontrol.StateDigest(control),
		ClosureReportDigestSHA256:    closureDigest,
		// Ties the binding to the sealed candidate; a bundle whose session does
		// not match the artifact the store hands back is refused.
		SessionDigestSHA256: sessionDigest,
	}
}
