// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/extractbudget"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type diffRepo struct {
	root string
	base string
	head string
}

// newDiffRepo builds a two-commit repository: alpha and omega at base, then a
// change to alpha plus a new gamma at head. Real commits, because the whole
// point of an incremental run is that git decides what changed.
func newDiffRepo(t *testing.T) diffRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write := func(rel, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	write("go.mod", "module example.com/difffixture\n\ngo 1.22\n")
	write("alpha/alpha.go", "package alpha\n\n// Alpha is exported.\nfunc Alpha() int { return 1 }\n")
	write("omega/omega.go", "package omega\n\n// Omega is exported.\nfunc Omega() int { return 2 }\n")
	write("doomed/doomed.go", "package doomed\n\n// Doomed is exported.\nfunc Doomed() int { return 3 }\n")
	git("init", "-q")
	git("remote", "add", "origin", "https://example.com/difffixture.git")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")

	write("alpha/alpha.go", "package alpha\n\n// Alpha is exported.\nfunc Alpha() int { return 11 }\n\n// Beta is new.\nfunc Beta() int { return 12 }\n")
	write("gamma/gamma.go", "package gamma\n\n// Gamma is exported.\nfunc Gamma() int { return 4 }\n")
	if err := os.RemoveAll(filepath.Join(root, "doomed")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "head")
	head := git("rev-parse", "HEAD")

	return diffRepo{root: root, base: base, head: head}
}

func diffOpts(r diffRepo) Options {
	o := defaultOpts()
	o.Repository.RepositoryDomain = "example.com/difffixture"
	o.Repository.Revision = r.head
	o.Diff = &DiffBinding{BaseRevision: r.base, HeadRevision: r.head}
	return o
}

// The core of checkpoint B: an incremental run describes what changed, and
// nothing else.
func TestIncrementalRunDescribesOnlyChangedFiles(t *testing.T) {
	r := newDiffRepo(t)

	whole, err := Extract(r.root, func() Options {
		o := defaultOpts()
		o.Repository.RepositoryDomain = "example.com/difffixture"
		o.Repository.Revision = r.head
		return o
	}())
	if err != nil {
		t.Fatal(err)
	}
	var wholeTouchedOmega bool
	for _, obs := range whole.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "omega/") {
			wholeTouchedOmega = true
		}
	}
	if !wholeTouchedOmega {
		t.Fatal("the whole-repository run produced nothing for omega/; the assertion below would be vacuous")
	}

	doc, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "omega/") {
			t.Fatalf("an unchanged file produced an observation in an incremental run: %s", obs.Evidence.SourceFile)
		}
	}
	var sawAlpha bool
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "alpha/") {
			sawAlpha = true
		}
	}
	if !sawAlpha {
		t.Error("the changed file produced no observations")
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("an incremental document must be a valid one: %v", err)
	}
}

// A consumer that reads only the observations must still be able to tell this
// is not a description of the repository.
func TestIncrementalReceiptSaysTheRepositoryWasNotSearched(t *testing.T) {
	r := newDiffRepo(t)
	doc, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	ds := doc.Receipt.DiffScope
	if ds == nil {
		t.Fatal("an incremental run carries no diff scope; the document is indistinguishable from a whole-repository one")
	}
	if !ds.WholeRepositoryNotSearched {
		t.Error("whole_repository_not_searched is false on an incremental run")
	}
	if ds.BaseRevision != r.base || ds.HeadRevision != r.head {
		t.Errorf("diff scope is bound to %s..%s, want %s..%s", ds.BaseRevision, ds.HeadRevision, r.base, r.head)
	}
	if len(ds.ChangedPaths) == 0 {
		t.Fatal("no changed paths recorded")
	}
	var said bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "the rest of the repository was NOT searched") {
			said = true
		}
	}
	if !said {
		t.Errorf("the document does not disclose the unsearched remainder: %+v", doc.Limitations)
	}
	if doc.Receipt.DiffScope == nil {
		t.Fatal("unreachable")
	}
	// A whole-repository run must NOT carry one, or presence would stop
	// meaning anything.
	whole, err := Extract(r.root, func() Options {
		o := defaultOpts()
		o.Repository.RepositoryDomain = "example.com/difffixture"
		o.Repository.Revision = r.head
		return o
	}())
	if err != nil {
		t.Fatal(err)
	}
	if whole.Receipt.DiffScope != nil {
		t.Error("a whole-repository run carries a diff scope")
	}
}

// A deleted file is a real change with no source position to anchor to. It
// must appear as changed-but-not-searched rather than vanish, or the document
// would silently understate what the change set contained.
func TestDeletedPathsAreChangedButNotSearched(t *testing.T) {
	r := newDiffRepo(t)
	doc, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	ds := doc.Receipt.DiffScope
	if ds == nil {
		t.Fatal("no diff scope")
	}
	var changedHasDoomed, searchedHasDoomed bool
	for _, p := range ds.ChangedPaths {
		if strings.HasPrefix(p, "doomed/") {
			changedHasDoomed = true
		}
	}
	for _, p := range ds.SearchedPaths {
		if strings.HasPrefix(p, "doomed/") {
			searchedHasDoomed = true
		}
	}
	if searchedHasDoomed {
		t.Error("a deleted file was reported as searched")
	}
	// Asserted UNCONDITIONALLY. This was previously guarded by
	// `if changedHasDoomed`, and --diff-filter=ACMRT excluded deletions
	// entirely -- so the guard was never true and the test proved nothing
	// while passing. A test that only checks a property when the property
	// already holds is not a test.
	if !changedHasDoomed {
		t.Fatalf("the deleted path is absent from changed_paths, so the change set is understated: %v", ds.ChangedPaths)
	}
	var said bool
	for _, l := range doc.Limitations {
		if strings.Contains(l.Reason, "no longer exist at head") {
			said = true
		}
	}
	if !said {
		t.Error("a deleted path was dropped without saying why")
	}
}

// A diff whose changed paths are all deleted or excluded must describe
// NOTHING. Falling back to the whole repository would produce a
// whole-repository result wearing an incremental document's receipt, while the
// receipt truthfully reported zero searched paths.
func TestIncrementalRunWithNoSearchablePathsDescribesNothing(t *testing.T) {
	r := newDiffRepo(t)
	opts := diffOpts(r)
	// Exclude every changed path, leaving the allowlist active but empty.
	opts.Budget = extractbudget.Budget{ExcludePaths: []string{"alpha", "gamma", "doomed"}}

	doc, err := Extract(r.root, opts)
	if err != nil {
		t.Fatal(err)
	}
	ds := doc.Receipt.DiffScope
	if ds == nil {
		t.Fatal("no diff scope")
	}
	if len(ds.SearchedPaths) != 0 {
		t.Fatalf("searched paths = %v, want none", ds.SearchedPaths)
	}
	for _, obs := range doc.Observations {
		t.Fatalf("an empty searchable set still produced an observation from %s", obs.Evidence.SourceFile)
	}
	if err := investigation.Validate(doc); err != nil {
		t.Errorf("a document describing nothing must still be valid: %v", err)
	}
}

// The extractors read the ambient tree, so a head that is not checked out --
// or a dirty tree -- would make the observations describe different content
// than the receipt claims, with every digest still verifying against itself.
func TestIncrementalRunRefusesAHeadThatIsNotCheckedOut(t *testing.T) {
	r := newDiffRepo(t)

	t.Run("head is not the checkout", func(t *testing.T) {
		opts := defaultOpts()
		opts.Repository.RepositoryDomain = "example.com/difffixture"
		opts.Repository.Revision = r.head
		// base..base is refused earlier; bind head to the BASE commit, which
		// exists but is not what the worktree holds.
		opts.Diff = &DiffBinding{BaseRevision: r.head, HeadRevision: r.base}
		if _, err := Extract(r.root, opts); err == nil {
			t.Fatal("a head that is not checked out was accepted")
		}
	})

	t.Run("dirty tree", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(r.root, "alpha", "alpha.go"),
			[]byte("package alpha\n\n// Alpha is exported.\nfunc Alpha() int { return 999 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Extract(r.root, diffOpts(r)); err == nil {
			t.Fatal("a dirty working tree was accepted as the bound head")
		}
	})
}

// A diff cannot be used to reach into a directory the operator excluded.
// ExactFiles narrows; it never widens.
func TestDiffCannotEscapeAnExcludeScope(t *testing.T) {
	r := newDiffRepo(t)
	opts := diffOpts(r)
	opts.Budget = extractbudget.Budget{ExcludePaths: []string{"alpha"}}
	doc, err := Extract(r.root, opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "alpha/") {
			t.Fatalf("an excluded directory produced an observation via the diff: %s", obs.Evidence.SourceFile)
		}
	}
	for _, p := range doc.Receipt.DiffScope.SearchedPaths {
		if strings.HasPrefix(p, "alpha/") {
			t.Fatalf("an excluded path was recorded as searched: %s", p)
		}
	}
}

// Repeated identical runs must be byte-equivalent. The captured_at input is
// explicit, so nothing here is allowed to vary.
func TestIncrementalRunsAreDeterministic(t *testing.T) {
	r := newDiffRepo(t)
	first, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	if first.Receipt.OutputDocumentDigestSHA256 != second.Receipt.OutputDocumentDigestSHA256 {
		t.Fatalf("two identical incremental runs produced different documents:\n%s\n%s",
			first.Receipt.OutputDocumentDigestSHA256, second.Receipt.OutputDocumentDigestSHA256)
	}
}

// The semantic snapshot must still cover the whole module. An incremental run
// that also narrowed its compiler inputs would produce observations about
// types it could not see -- and its source-snapshot identity would no longer
// identify the tree the observations were derived from.
func TestIncrementalRunPreservesWholeRepositorySnapshotIdentity(t *testing.T) {
	r := newDiffRepo(t)
	wholeOpts := defaultOpts()
	wholeOpts.Repository.RepositoryDomain = "example.com/difffixture"
	wholeOpts.Repository.Revision = r.head

	whole, err := Extract(r.root, wholeOpts)
	if err != nil {
		t.Fatal(err)
	}
	incremental, err := Extract(r.root, diffOpts(r))
	if err != nil {
		t.Fatal(err)
	}
	// The source-snapshot digest lives on every coverage entry. It is built
	// from gosemantics.SemanticInputFiles -- the whole module -- and must be
	// identical in both runs.
	snapshotOf := func(doc investigation.Document) string {
		t.Helper()
		if len(doc.Coverage) == 0 {
			t.Fatal("document has no coverage entries to read the snapshot digest from")
		}
		first := doc.Coverage[0].SourceSnapshotDigestSHA256
		if first == "" {
			t.Fatal("the source snapshot digest is empty; this assertion would be vacuous")
		}
		for _, c := range doc.Coverage {
			if c.SourceSnapshotDigestSHA256 != first {
				t.Fatalf("coverage entries disagree on the source snapshot digest: %s vs %s", first, c.SourceSnapshotDigestSHA256)
			}
		}
		return first
	}
	if a, b := snapshotOf(whole), snapshotOf(incremental); a != b {
		t.Errorf("the incremental run changed source-snapshot identity: %s vs %s", a, b)
	}
}

// An ambiguous revision makes the same command mean different things on
// different days, so it is refused rather than resolved.
func TestAmbiguousRevisionsAreRefused(t *testing.T) {
	r := newDiffRepo(t)
	for name, b := range map[string]DiffBinding{
		"branch name":     {BaseRevision: "main", HeadRevision: r.head},
		"HEAD":            {BaseRevision: r.base, HeadRevision: "HEAD"},
		"abbreviated sha": {BaseRevision: r.base[:12], HeadRevision: r.head},
		"uppercase":       {BaseRevision: strings.ToUpper(r.base), HeadRevision: r.head},
	} {
		t.Run(name, func(t *testing.T) {
			opts := defaultOpts()
			opts.Repository.RepositoryDomain = "example.com/difffixture"
			opts.Repository.Revision = r.head
			opts.Diff = &b
			if _, err := Extract(r.root, opts); err == nil {
				t.Fatal("an ambiguous revision was accepted")
			}
		})
	}
}

func TestIdenticalBaseAndHeadIsRefused(t *testing.T) {
	r := newDiffRepo(t)
	opts := defaultOpts()
	opts.Repository.RepositoryDomain = "example.com/difffixture"
	opts.Repository.Revision = r.head
	opts.Diff = &DiffBinding{BaseRevision: r.head, HeadRevision: r.head}
	_, err := Extract(r.root, opts)
	if err == nil {
		t.Fatal("a no-op diff was accepted")
	}
	if !strings.Contains(err.Error(), "no change to describe") {
		t.Errorf("the refusal does not explain itself: %v", err)
	}
}

func TestResolveChangedPathsIsSortedAndDeduplicated(t *testing.T) {
	r := newDiffRepo(t)
	got, err := resolveChangedPaths(context.Background(), r.root, DiffBinding{BaseRevision: r.base, HeadRevision: r.head})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no changed paths")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("changed paths are not strictly sorted: %v", got)
		}
	}
}
