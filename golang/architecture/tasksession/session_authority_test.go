// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/rdf"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// authorityRepo builds a repo carrying the real authority policy sources plus a
// graph whose authority domain covers gin.go, so typed authority can resolve.
func authorityRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "gin.go", "package gin\n")
	writeFile(t, root, "gin_test.go", "package gin\n")

	src := filepath.Join(moduleRoot(t), "docs", "awareness")
	dst := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
		"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read policy %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(t, root, "init")
	git(t, root, "config", "user.email", "sensei@example.test")
	git(t, root, "config", "user.name", "Sensei Test")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	authIRI := strings.Trim(strings.TrimSuffix(rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.sensei_repository_mutation"), ">"), "<")
	graph := strings.Join([]string{
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropType, rdf.ClassSourceFile, true),
		triple("https://globular.io/awareness#sourceFile/gin.go", rdf.PropSourcePath, "gin.go", false),
		triple(authIRI, rdf.PropType, rdf.ClassAuthorityDomain, true),
		triple(authIRI, rdf.PropStatus, "active", false),
		triple(authIRI, rdf.PropCoversPath, "gin.go", false),
		triple(authIRI, rdf.PropMayWrite, "role.repository_repair_agent", false),
		triple(authIRI, rdf.PropMustMutateVia, "mutation_path.repository_edit", false),
		triple(authIRI, rdf.PropHasTruthLayer, "repository", false),
		"",
	}, "\n")
	graphPath := filepath.Join(root, "graph.nt")
	if err := os.WriteFile(graphPath, []byte(graph), 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	writeTestProjectClaims(t, root, graphPath)
	return root, graphPath
}

func prepareEdit(t *testing.T, repo, graph string) PrepareResult {
	t.Helper()
	res, err := Prepare(PrepareOptions{
		RepoRoot:             repo,
		RepositoryDomain:     "github.com/example/project",
		Description:          "Resolve typed authority for a bounded repository edit.",
		Mode:                 admission.ModeModify,
		TaskClass:            "typed_authority_resolution",
		RiskClass:            closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve,
		Files:                []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT:              graph,
		SetActive:            true,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return res
}

func headEventLimitations(t *testing.T, taskDir string) []string {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	last := chain.Entries[len(chain.Entries)-1]
	data, err := os.ReadFile(last.PayloadPath)
	if err != nil {
		t.Fatalf("read head payload: %v", err)
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		t.Fatalf("parse head payload: %v", err)
	}
	return payload.Limitations
}

func hasLimitation(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestPrepareResolvesTypedAuthorityWhenEnrolled(t *testing.T) {
	repo, graph := authorityRepo(t)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(repo), Now: time.Now().UTC()}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	res := prepareEdit(t, repo, graph)
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))

	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		t.Fatalf("authority_resolved not recorded: %v", err)
	}
	// A valid overall status means the covers-path-matched applicability mapped
	// the operation to a domain the grant authorizes — an empty/hard-coded
	// applicability would have left it unmapped and invalid.
	if rec.Resolution.Status != closureprotocol.ReceiptValid {
		t.Fatalf("resolution status = %q, want valid; limitations %v", rec.Resolution.Status, rec.Resolution.Limitations)
	}
	if len(rec.Resolution.OperationResults) == 0 {
		t.Fatal("resolution has no operation results")
	}
	op := rec.Resolution.OperationResults[0]
	if !hasLimitation(op.AuthorityDomainIDs, "authority.sensei_repository_mutation") {
		t.Fatalf("operation authority domains = %v, want authority.sensei_repository_mutation", op.AuthorityDomainIDs)
	}
	if len(rec.ChangePlan.Operations) != 1 || rec.ChangePlan.Operations[0].Target != "gin.go" {
		t.Fatalf("unexpected change plan: %+v", rec.ChangePlan.Operations)
	}

	// The head event drops the now-false limitations; the capability is not yet
	// consumed and correctness is never certified here.
	lim := headEventLimitations(t, taskDir)
	if hasLimitation(lim, "typed_actor_authority_not_yet_resolved") || hasLimitation(lim, "legacy_scope_admission") {
		t.Fatalf("resolved head still carries legacy limitations: %v", lim)
	}
	if !hasLimitation(lim, "single_use_capability_not_available") || !hasLimitation(lim, "correctness_not_certified") {
		t.Fatalf("resolved head missing expected residual limitations: %v", lim)
	}
}

func TestPrepareRecordsLimitationWhenNotEnrolled(t *testing.T) {
	repo, graph := authorityRepo(t) // policy present, but no agent enrolled
	res := prepareEdit(t, repo, graph)
	taskDir := filepath.Join(repo, filepath.FromSlash(res.TaskDir))

	if _, err := admission.LoadRecordedAuthority(taskDir); err == nil {
		t.Fatal("expected no authority_resolved event without an enrolled identity")
	}
	lim := headEventLimitations(t, taskDir)
	if !hasLimitation(lim, "agent_identity_not_enrolled") {
		t.Fatalf("expected agent_identity_not_enrolled limitation, got %v", lim)
	}
	if !hasLimitation(lim, "typed_actor_authority_not_yet_resolved") || !hasLimitation(lim, "legacy_scope_admission") {
		t.Fatalf("un-enrolled task should keep legacy limitations, got %v", lim)
	}
}
