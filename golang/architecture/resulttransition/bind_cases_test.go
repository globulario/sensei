// SPDX-License-Identifier: AGPL-3.0-only

package resulttransition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// ---- committed result mode ----

func TestBindCommittedExactResult(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	got := mustBind(t, repo, dir, ResultModeRevision, resultRev)

	if got.RepositoryResult.BaseRevision != baseRev {
		t.Fatalf("base revision = %q, want %q", got.RepositoryResult.BaseRevision, baseRev)
	}
	if got.RepositoryResult.ResultRevision != resultRev {
		t.Fatalf("result revision = %q, want %q", got.RepositoryResult.ResultRevision, resultRev)
	}
	if got.RepositoryResult.ResultTreeDigestSHA256 != canonicalTree(t, repo, resultRev) {
		t.Fatal("result tree digest is not the canonical tree of the result")
	}
	if len(got.ObservedChange.Files) != 1 || got.ObservedChange.Files[0].ChangeType != "modify" {
		t.Fatalf("observed change = %+v, want one modify", got.ObservedChange.Files)
	}
}

func TestBindCommittedWrongRevisionFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	other := commitResult(t, repo, "different\n") // a third commit, not the admitted result
	wantBindError(t, repo, dir, ResultModeRevision, other, "observed_change_mismatch")
}

func TestBindCommittedSameTreeDifferentCommit(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	// A commit with an identical tree but a different commit id (empty amend of a
	// new parentless commit-tree over the same tree).
	tree := gitRun(t, repo, "rev-parse", resultRev+"^{tree}")
	twin := gitRun(t, repo, "commit-tree", tree, "-p", baseRev, "-m", "twin")
	got := mustBind(t, repo, dir, ResultModeRevision, twin)
	if got.RepositoryResult.ResultTreeDigestSHA256 != canonicalTree(t, repo, resultRev) {
		t.Fatal("result identity should be by tree, independent of commit id")
	}
	if got.RepositoryResult.ResultRevision != gitRun(t, repo, "rev-parse", twin) {
		t.Fatal("result revision should be the bound commit")
	}
}

func TestBindResultTreeDigestIsSHA256NotOID(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	got := mustBind(t, repo, dir, ResultModeRevision, resultRev)
	if !isHex64(got.RepositoryResult.ResultTreeDigestSHA256) {
		t.Fatalf("result tree digest %q is not 64-hex sha256", got.RepositoryResult.ResultTreeDigestSHA256)
	}
	if got.RepositoryResult.GitTreeObjectID == got.RepositoryResult.ResultTreeDigestSHA256 {
		t.Fatal("native git OID must not be reused as the sha256 tree digest")
	}
	if isHex64(got.RepositoryResult.GitTreeObjectID) {
		t.Fatalf("expected a 40-char SHA-1 OID in a SHA-1 repo, got %q", got.RepositoryResult.GitTreeObjectID)
	}
}

// ---- worktree result mode ----

func TestBindWorktreeUnstagedChange(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n") // unstaged
	dir := seedChain(t, repo, baseRev, seedOpts{})
	got := mustBind(t, repo, dir, ResultModeWorktree, "")
	if got.RepositoryResult.ResultRevision != "" {
		t.Fatal("worktree result must not carry a result revision")
	}
	if len(got.ObservedChange.Files) != 1 || got.ObservedChange.Files[0].ChangeType != "modify" {
		t.Fatalf("observed = %+v, want one modify", got.ObservedChange.Files)
	}
}

func TestBindWorktreeStagedChange(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	gitRun(t, repo, "add", "-A") // staged, not committed
	dir := seedChain(t, repo, baseRev, seedOpts{})
	mustBind(t, repo, dir, ResultModeWorktree, "")
}

func TestBindWorktreeMixedStagedUnstaged(t *testing.T) {
	repo, baseRev := initRepo(t)
	writeRepoFile(t, repo, "src/a.go", "a\n")
	gitRun(t, repo, "add", "-A")         // staged new file
	mutateWorktree(t, repo, "changed\n") // unstaged modify
	dir := seedChain(t, repo, baseRev, seedOpts{})
	got := mustBind(t, repo, dir, ResultModeWorktree, "")
	if len(got.ObservedChange.Files) != 2 {
		t.Fatalf("observed = %+v, want two files", got.ObservedChange.Files)
	}
}

func TestBindWorktreeUntrackedAdmitted(t *testing.T) {
	repo, baseRev := initRepo(t)
	writeRepoFile(t, repo, "src/new.go", "new\n") // untracked, part of the admitted change
	dir := seedChain(t, repo, baseRev, seedOpts{})
	got := mustBind(t, repo, dir, ResultModeWorktree, "")
	var found bool
	for _, f := range got.ObservedChange.Files {
		if f.Path == "src/new.go" && f.ChangeType == "create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("admitted untracked file not observed: %+v", got.ObservedChange.Files)
	}
}

func TestBindWorktreeExtraUntrackedFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	writeRepoFile(t, repo, "src/sneaky.go", "extra\n") // appears AFTER admission observed
	wantBindError(t, repo, dir, ResultModeWorktree, "", "observed_change_mismatch")
}

func TestBindWorktreeExtraTrackedFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	mutateWorktree(t, repo, "changed twice\n") // tracked content diverges from observed
	wantBindError(t, repo, dir, ResultModeWorktree, "", "observed_change_mismatch")
}

func TestBindWorktreeMissingMutationFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	mutateWorktree(t, repo, "base\n") // reverted: the admitted mutation is gone
	wantBindError(t, repo, dir, ResultModeWorktree, "", "observed_change_mismatch")
}

func TestBindWorktreeEmpty(t *testing.T) {
	repo, baseRev := initRepo(t)
	dir := seedChain(t, repo, baseRev, seedOpts{}) // no mutation
	got := mustBind(t, repo, dir, ResultModeWorktree, "")
	if len(got.ObservedChange.Files) != 0 {
		t.Fatalf("expected no observed files, got %+v", got.ObservedChange.Files)
	}
	if got.RepositoryResult.ResultTreeDigestSHA256 != canonicalTree(t, repo, baseRev) {
		t.Fatal("empty worktree result tree must equal the base tree")
	}
}

// ---- safety: the real repository state is never mutated ----

func TestBindWorktreeLeavesRealIndexAndWorktreeUnchanged(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	gitRun(t, repo, "add", "-A")
	dir := seedChain(t, repo, baseRev, seedOpts{})

	indexPath := filepath.Join(repo, ".git", "index")
	indexBefore, _ := os.ReadFile(indexPath)
	fileBefore, _ := os.ReadFile(filepath.Join(repo, "src/model.go"))
	headBefore := gitRev(t, repo)

	mustBind(t, repo, dir, ResultModeWorktree, "")

	indexAfter, _ := os.ReadFile(indexPath)
	fileAfter, _ := os.ReadFile(filepath.Join(repo, "src/model.go"))
	if string(indexBefore) != string(indexAfter) {
		t.Fatal("real .git/index changed during bind")
	}
	if string(fileBefore) != string(fileAfter) {
		t.Fatal("working tree file changed during bind")
	}
	if gitRev(t, repo) != headBefore {
		t.Fatal("bind created a commit")
	}
}

func tempIndexDirs(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sensei-result-index-") {
			out[e.Name()] = true
		}
	}
	return out
}

func TestBindWorktreeCleansTempIndexOnSuccess(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	before := tempIndexDirs(t)
	mustBind(t, repo, dir, ResultModeWorktree, "")
	after := tempIndexDirs(t)
	if len(after) > len(before) {
		t.Fatalf("temp index dir leaked on success: before=%d after=%d", len(before), len(after))
	}
}

func TestBindWorktreeCleansTempIndexOnFailure(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	mutateWorktree(t, repo, "diverged\n") // force a mismatch
	before := tempIndexDirs(t)
	wantBindError(t, repo, dir, ResultModeWorktree, "", "observed_change_mismatch")
	after := tempIndexDirs(t)
	if len(after) > len(before) {
		t.Fatalf("temp index dir leaked on failure: before=%d after=%d", len(before), len(after))
	}
}

// ---- canonical identity ----

func TestBindIdentityRepeatable(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	a := mustBind(t, repo, dir, ResultModeRevision, resultRev)
	b := mustBind(t, repo, dir, ResultModeRevision, resultRev)
	if a.RepositoryResult != b.RepositoryResult {
		t.Fatalf("repeated bind differs:\n%+v\n%+v", a.RepositoryResult, b.RepositoryResult)
	}
}

func TestBindIdentityRelocatedCheckout(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	got := mustBind(t, repo, dir, ResultModeRevision, resultRev)

	// Relocate the entire checkout to a different absolute path.
	dst := t.TempDir()
	relocated := filepath.Join(dst, "checkout")
	if err := copyTree(repo, relocated); err != nil {
		t.Fatal(err)
	}
	if canonicalTree(t, relocated, resultRev) != got.RepositoryResult.ResultTreeDigestSHA256 {
		t.Fatal("canonical result identity must be independent of the absolute checkout path")
	}
}

// ---- ledger truth: fail closed on inconsistency ----

func TestBindMissingObservedChangeFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{stopBefore: closureprotocol.LedgerEventChangeObserved})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "load observed change")
}

func TestBindTamperedScopeActorFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{
		mutateScope: func(s *admission.ScopeVerification) { s.ActorBindingDigestSHA256 = strings.Repeat("0", 64) },
	})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "scope_actor_mismatch")
}

func TestBindScopeResultTreeMismatchFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{
		mutateScope: func(s *admission.ScopeVerification) { s.ObservedChangeSetDigestSHA256 = strings.Repeat("0", 64) },
	})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "scope_observed_mismatch")
}

func TestBindStaleBaseTreeFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{
		mutateScope: func(s *admission.ScopeVerification) { s.BaseTreeDigestSHA256 = strings.Repeat("0", 64) },
	})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "base_tree_mismatch")
}

func TestBindWrongTaskFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{
		changePlanTask: closureprotocol.TaskBinding{ID: "task.other", SessionID: "session.other"},
	})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "task_identity_mismatch")
}

func TestBindDuplicateDivergentObservedFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})

	// Append a second, divergent change_observed after scope_verified. The loader
	// returns the latest; scope still references the first, so it fails closed.
	validator := func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}
	store := ledger.NewStore(dir, ledger.WithPayloadValidator(validator))
	head, err := admission.TaskLedgerHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	divergent := admission.ObservedChangeSet{
		BaseTreeDigestSHA256: canonicalTree(t, repo, baseRev), ResultTreeDigestSHA256: strings.Repeat("1", 64),
		Files: []admission.ObservedFile{{Path: "src/model.go", ChangeType: "modify"}},
	}
	if _, err := admission.RecordChangeObserved(store, head, closureprotocol.TaskBinding{ID: "task.test", SessionID: "session.test"}, divergent, t0); err != nil {
		t.Fatal(err)
	}
	wantBindError(t, repo, dir, ResultModeWorktree, "", "scope_observed_mismatch")
}

func TestBindNotScopeVerifiedFails(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{
		mutateScope: func(s *admission.ScopeVerification) { s.Status = closureprotocol.ReceiptStatus("invalid") },
	})
	wantBindError(t, repo, dir, ResultModeWorktree, "", "scope_not_verified")
}

// ---- operation kinds ----

func TestBindOperationCreate(t *testing.T) {
	repo, baseRev := initRepo(t)
	writeRepoFile(t, repo, "src/created.go", "created\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "create")
	rev := gitRev(t, repo)
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	got := mustBind(t, repo, dir, ResultModeRevision, rev)
	assertKind(t, got.ObservedChange.Files, "src/created.go", "create")
}

func TestBindOperationDelete(t *testing.T) {
	repo, baseRev := initRepo(t)
	gitRun(t, repo, "rm", "-q", "src/model.go")
	gitRun(t, repo, "commit", "-q", "-m", "delete")
	rev := gitRev(t, repo)
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	got := mustBind(t, repo, dir, ResultModeRevision, rev)
	assertKind(t, got.ObservedChange.Files, "src/model.go", "delete")
}

func TestBindOperationDeleteAndCreate(t *testing.T) {
	repo, baseRev := initRepo(t)
	gitRun(t, repo, "rm", "-q", "src/model.go")
	writeRepoFile(t, repo, "src/replacement.go", "new\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "swap")
	rev := gitRev(t, repo)
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	got := mustBind(t, repo, dir, ResultModeRevision, rev)
	assertKind(t, got.ObservedChange.Files, "src/model.go", "delete")
	assertKind(t, got.ObservedChange.Files, "src/replacement.go", "create")
}

func TestBindRenameRefused(t *testing.T) {
	repo, baseRev := initRepo(t)
	gitRun(t, repo, "mv", "src/model.go", "src/renamed.go")
	gitRun(t, repo, "commit", "-q", "-m", "rename")
	rev := gitRev(t, repo)
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	wantBindError(t, repo, dir, ResultModeRevision, rev, "rename_unsupported")
}

// ---- repository edges ----

func TestBindIgnoredOutputExcluded(t *testing.T) {
	repo, baseRev := initRepo(t)
	writeRepoFile(t, repo, ".gitignore", "build/\n")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "ignore")
	baseRev = gitRev(t, repo)
	// Mutate a tracked file and drop an ignored build artifact.
	mutateWorktree(t, repo, "changed\n")
	writeRepoFile(t, repo, "build/out.bin", "junk\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	got := mustBind(t, repo, dir, ResultModeWorktree, "")
	for _, f := range got.ObservedChange.Files {
		if strings.HasPrefix(f.Path, "build/") {
			t.Fatalf("gitignored output leaked into observed change: %+v", got.ObservedChange.Files)
		}
	}
}

func TestBindExternalSymlinkRefused(t *testing.T) {
	repo, baseRev := initRepo(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(repo, "src/escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "symlink")
	rev := gitRev(t, repo)
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	wantBindError(t, repo, dir, ResultModeRevision, rev, "symlink_escape")
}

func TestBindSubmoduleGitlinkBoundDeterministically(t *testing.T) {
	repo, baseRev := initRepo(t)
	rev := commitGitlink(t, repo, baseRev, "vendor/sub", strings.Repeat("a", 40))
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: rev})
	got := mustBind(t, repo, dir, ResultModeRevision, rev)
	assertKind(t, got.ObservedChange.Files, "vendor/sub", "create")
	if !isHex64(got.RepositoryResult.ResultTreeDigestSHA256) {
		t.Fatal("gitlink result tree not bound to a canonical digest")
	}
	// Determinism: the same gitlink state re-resolves to the same identity.
	got2 := mustBind(t, repo, dir, ResultModeRevision, rev)
	if got.RepositoryResult.ResultTreeDigestSHA256 != got2.RepositoryResult.ResultTreeDigestSHA256 {
		t.Fatal("gitlink identity is not deterministic")
	}
}

// ---- request validation ----

func TestBindRejectsRevisionInWorktreeMode(t *testing.T) {
	repo, baseRev := initRepo(t)
	mutateWorktree(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{})
	wantBindError(t, repo, dir, ResultModeWorktree, "somerev", "worktree mode takes no result revision")
}

func TestBindRejectsMissingRevisionInRevisionMode(t *testing.T) {
	repo, baseRev := initRepo(t)
	resultRev := commitResult(t, repo, "changed\n")
	dir := seedChain(t, repo, baseRev, seedOpts{resultRev: resultRev})
	wantBindError(t, repo, dir, ResultModeRevision, "", "result revision is required")
}

// ---- helpers specific to cases ----

func assertKind(t *testing.T, files []admission.ObservedFile, path, kind string) {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			if f.ChangeType != kind {
				t.Fatalf("file %q change type = %q, want %q", path, f.ChangeType, kind)
			}
			return
		}
	}
	t.Fatalf("file %q not observed in %+v", path, files)
}

// copyTree copies a directory tree (including .git) to dst.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

// commitGitlink writes a result commit containing a submodule gitlink at path,
// using an isolated index so the real repository is untouched.
func commitGitlink(t *testing.T, repo, baseRev, path, commitSHA string) string {
	t.Helper()
	tmp := t.TempDir()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if _, err := runGitEnv(repo, env, "read-tree", baseRev); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitEnv(repo, env, "update-index", "--add", "--cacheinfo", "160000,"+commitSHA+","+path); err != nil {
		t.Fatal(err)
	}
	tree, err := runGitEnv(repo, env, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := runGitEnv(repo, env, "commit-tree", strings.TrimSpace(tree), "-p", baseRev, "-m", "gitlink")
	if err != nil {
		t.Fatal(err)
	}
	// Move the branch so the commit is reachable (keeps rev-parse honest).
	gitRun(t, repo, "reset", "-q", "--soft", strings.TrimSpace(commit))
	return strings.TrimSpace(commit)
}
