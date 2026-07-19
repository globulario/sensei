// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

const cliTarget = "src/model.go"
const dummyTree = "0000000000000000000000000000000000000000000000000000000000000000"

func cliIndex() authority.PolicyIndex {
	return authority.PolicyIndex{
		ActorRoles: map[string]authority.ActorRole{
			"role.repository_repair_agent": {ID: "role.repository_repair_agent", Status: "active", AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent}, TrustedIssuers: []string{"sensei.local"}},
		},
		MutationPaths: map[string]authority.MutationPath{
			"mutation_path.repository_edit": {ID: "mutation_path.repository_edit", Status: "active", MechanismKind: closureprotocol.MechanismRepositoryEdit, TargetKinds: []string{"source_file"}},
		},
		AuthorityDomains: map[string]authority.AuthorityDomain{
			"authority.sensei_closure": {ID: "authority.sensei_closure", Status: "active", MayWriteRoleIDs: []string{"role.repository_repair_agent"}, MustMutateViaIDs: []string{"mutation_path.repository_edit"}},
		},
		AuthorityGrants: map[string]authority.AuthorityGrant{
			"grant.edit": {ID: "grant.edit", Status: "active", ActorRoleIDs: []string{"role.repository_repair_agent"}, AuthorityDomainIDs: []string{"authority.sensei_closure"}, Actions: []closureprotocol.OperationKind{closureprotocol.OperationModify}, TargetKinds: []string{"source_file"}, RequiredMechanismIDs: []string{"mutation_path.repository_edit"}, MaximumRiskClass: "architecture_sensitive", ValidFrom: "2026-07-15T00:00:00Z"},
		},
	}
}

func cliActor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{PrincipalID: "actor.cli", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
}

func cliVerified() authority.VerifiedActor {
	return authority.VerifiedActor{PrincipalID: "actor.cli", ActorKind: closureprotocol.ActorAgent, Issuer: "sensei.local", Status: closureprotocol.ReceiptValid, VerifiedRoleIDs: []string{"role.repository_repair_agent"}}
}

func cliChangePlan() closureprotocol.ChangePlan {
	return closureprotocol.ChangePlan{PlanID: "plan.cli", Operations: []closureprotocol.ChangeOperation{{OperationID: "op.cli", Kind: closureprotocol.OperationModify, TargetKind: "source_file", Target: cliTarget, SelectedMechanism: closureprotocol.MechanismRepositoryEdit, RiskClass: "architecture_sensitive"}}}
}

func cliBase(rev, treeDigest string) closureprotocol.BaseBinding {
	return closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: "github.com/globulario/sensei", Revision: rev, RevisionStatus: "resolved", TreeDigestSHA256: treeDigest},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: "g", DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.cli", SessionID: "session.cli"},
		Policies: closureprotocol.PolicyBinding{
			Admission:        "admission.strict.v2",
			Certification:    "certification.architectural_closure.v1",
			Completion:       "completion.architectural_closure.v1",
			Revocation:       "revocation.architectural_closure.v1",
			Ledger:           "ledger.task.v1",
			Canonicalization: "canon.v1",
		},
	}
}

func cliValidator(et closureprotocol.LedgerEventType, mt string, data []byte) error {
	return ledger.ValidateTaskEventPayload(et, data)
}

// seedLedger builds a task ledger with task_prepared (base) + authority_resolved,
// returning the task dir and the head after authority_resolved.
func seedLedger(t *testing.T, rev, treeDigest string, withAuthority bool) string {
	t.Helper()
	dir := t.TempDir()
	store := ledger.NewStore(dir, ledger.WithPayloadValidator(cliValidator))
	base := cliBase(rev, treeDigest)
	genesis, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          ledger.TaskEventPayload{SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared, TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("genesis: %v", err)
	}
	if !withAuthority {
		return dir
	}
	resolution, err := admission.ResolveAuthority(cliIndex(), admission.ResolveAuthorityInput{
		Actor: cliActor(), VerifiedActor: cliVerified(), Base: base, ChangePlan: cliChangePlan(),
		Applicability:                    []authority.AuthorityApplicability{{OperationID: "op.cli", TargetFile: cliTarget, AuthorityDomainIDs: []string{"authority.sensei_closure"}}},
		PolicyID:                         "admission.strict.v2",
		ClosureAssessmentDigestSHA256:    "closure000",
		AuthorityPolicyGraphDigestSHA256: "policygraph000",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := admission.RecordAuthorityResolved(store, genesis.Entry.EntryDigestSHA256, base.Task, resolution, cliActor(), cliChangePlan(), base, nil, time.Unix(0, 0).UTC()); err != nil {
		t.Fatalf("record authority: %v", err)
	}
	return dir
}

func head(t *testing.T, dir string) string {
	t.Helper()
	h, err := admission.TaskLedgerHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAdmitChangeV2Admits(t *testing.T) {
	dir := seedLedger(t, "rev123", dummyTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatalf("admit-change exit = %d, want 0", code)
	}
	if _, err := admission.LoadRecordedDecision(dir); err != nil {
		t.Fatalf("admission_decided not recorded: %v", err)
	}
}

func TestAdmitChangeV2MissingAuthorityRefuses(t *testing.T) {
	dir := seedLedger(t, "rev123", dummyTree, false) // no authority_resolved
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 3 {
		t.Fatalf("admit-change exit = %d, want 3 (missing authority)", code)
	}
}

func TestAdmitChangeV2StaleHeadRefuses(t *testing.T) {
	dir := seedLedger(t, "rev123", dummyTree, true)
	if code := runAdmitChangeV2(dir, "staledigest", "yaml"); code != 1 {
		t.Fatalf("admit-change exit = %d, want 1 (stale head)", code)
	}
}

func TestConsumeAdmissionOnceThenReplay(t *testing.T) {
	dir := seedLedger(t, "rev123", dummyTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatal("admit failed")
	}
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 0 {
		t.Fatalf("first consume exit = %d, want 0", code)
	}
	if _, err := admission.LoadRecordedConsumption(dir); err != nil {
		t.Fatalf("admission_consumed not recorded: %v", err)
	}
	// Replay: the capability id is already consumed.
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 3 {
		t.Fatalf("replay consume exit = %d, want 3", code)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitRev(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out[:len(out)-1])
}

// verifyRepo builds a git repo whose base commit contains cliTarget, applies an
// in-envelope change (optionally an extra file), and returns
// (repoRoot, baseRev, baseTree, resultRev).
func verifyRepo(t *testing.T, extraFile bool) (string, string, string, string) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	baseRev := gitRev(t, repo)
	baseTree := canonicalTreeOf(t, repo, baseRev)
	if err := os.WriteFile(filepath.Join(repo, cliTarget), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if extraFile {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "result")
	return repo, baseRev, baseTree, gitRev(t, repo)
}

// canonicalTreeOf returns the Sensei canonical SHA-256 tree digest (not the
// native Git object id) for a revision, matching what a Phase 1 base binding
// records and what verify-admission observes. Seeding the base binding with the
// native object id would spuriously trip scope.base_tree.changed.
func canonicalTreeOf(t *testing.T, repo, rev string) string {
	t.Helper()
	id, err := binding.ResolveTreeIdentity(context.Background(), repo, rev)
	if err != nil {
		t.Fatal(err)
	}
	return id.DigestSHA256
}

func TestVerifyAdmissionV2ExactMutationVerifies(t *testing.T) {
	repo, baseRev, baseTree, resultRev := verifyRepo(t, false)
	dir := seedLedger(t, baseRev, baseTree, true)
	if code := runAdmitChangeV2(dir, head(t, dir), "yaml"); code != 0 {
		t.Fatal("admit failed")
	}
	if code := runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"}); code != 0 {
		t.Fatal("consume failed")
	}
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 0 {
		t.Fatalf("verify-admission exit = %d, want 0 (scope verified)", code)
	}
	// Closure invariant: scope verification never certifies correctness — that
	// remains Phase 6's sole authority. The task ledger must not carry a
	// certification or completion event after admission-v2 scope verification.
	assertNoEvents(t, dir, closureprotocol.LedgerEventCertified, closureprotocol.LedgerEventCompleted)
}

func assertNoEvents(t *testing.T, dir string, forbidden ...closureprotocol.LedgerEventType) {
	t.Helper()
	chain, err := ledger.NewStore(dir, ledger.WithPayloadValidator(cliValidator)).VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	banned := map[closureprotocol.LedgerEventType]bool{}
	for _, f := range forbidden {
		banned[f] = true
	}
	for _, ve := range chain.Entries {
		if banned[ve.Entry.EventType] {
			t.Fatalf("admission v2 emitted forbidden event %q", ve.Entry.EventType)
		}
	}
}

func TestVerifyAdmissionV2ExtraFileRefuses(t *testing.T) {
	repo, baseRev, baseTree, resultRev := verifyRepo(t, true)
	dir := seedLedger(t, baseRev, baseTree, true)
	runAdmitChangeV2(dir, head(t, dir), "yaml")
	runConsumeAdmission([]string{"--task-dir", dir, "--expected-head", head(t, dir), "--format", "yaml"})
	if code := runVerifyAdmissionV2(dir, repo, head(t, dir), resultRev, "yaml"); code != 3 {
		t.Fatalf("verify-admission exit = %d, want 3 (extra file scope violation)", code)
	}
}

func TestAuthorityResolveRequiresExpectedHead(t *testing.T) {
	dir := seedLedger(t, "rev123", dummyTree, false)
	if code := runAuthorityResolve([]string{"--task-dir", dir, "--actor-binding", "x", "--change-plan", "y"}); code != 1 {
		t.Fatalf("authority-resolve without --expected-head exit = %d, want 1", code)
	}
}
