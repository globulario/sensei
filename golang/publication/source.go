package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// InspectSource establishes the revision identity of a compiled source root.
//
// It NEVER infers. Every field it cannot establish is left empty and the state
// says so, because a guessed revision is worse than an absent one: an absent
// one stops a reader, a guessed one convinces them.
//
// CLEAN_EXACT is EARNED, and the bar is deliberately narrow. It requires a
// resolvable HEAD, a resolvable tree for the source root, and no uncommitted
// change under that root -- untracked files included. A publication that
// compiled an untracked YAML file did not come from the commit it names, and
// the difference is invisible in the compiled output, which is precisely why it
// has to be checked here.
// It also returns the source root RELATIVE TO THE REPOSITORY, which is the
// durable identity a knowledge contract is written against. The absolute path
// is operational and differs between machines publishing the same corpus.
func InspectSource(root string) (revision, tree string, state SourceState, resolvedRoot, relPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	abs, err := filepath.Abs(root)
	if err != nil {
		return "", "", Unknown, root, ""
	}
	resolvedRoot = abs

	top, err := git(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return "", "", Unknown, resolvedRoot, ""
	}
	head, err := git(ctx, abs, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return "", "", Unknown, resolvedRoot, ""
	}

	// The tree of the source root AS COMMITTED. Distinguishes two commits whose
	// corpus differs, and stays equal for two commits whose corpus does not --
	// which is why it is recorded alongside the revision and never instead of it.
	rel, relErr := filepath.Rel(top, abs)
	treeRef := "HEAD^{tree}"
	if relErr == nil {
		relPath = filepath.ToSlash(rel)
	}
	if relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		// For a directory, HEAD:<path> already names a tree object; appending
		// ^{tree} to it does not resolve.
		treeRef = "HEAD:" + filepath.ToSlash(rel)
	}
	tree, treeErr := git(ctx, abs, "rev-parse", treeRef)
	if treeErr != nil || tree == "" {
		// CLEAN_EXACT asserts "produced from exactly this revision", and the
		// tree is what identifies the corpus that revision holds. A publication
		// that could not resolve the tree cannot make the exact claim, so the
		// state is UNKNOWN rather than a CLEAN_EXACT missing its evidence.
		return "", "", Unknown, resolvedRoot, relPath
	}

	// Dirtiness is asked about THIS ROOT, not the whole repository. A change
	// elsewhere in the repo does not make these compiled inputs unfaithful to
	// their commit.
	// IGNORED FILES COUNT, because the compiler compiles them.
	//
	// The extractor walks the corpus with filepath.WalkDir, which knows nothing
	// about .gitignore, while `git status --untracked-files=all` OMITS ignored
	// files. Cleanliness was therefore measured over a different set than the
	// one actually compiled: an ignored YAML under the corpus produced empty
	// status output and a CLEAN_EXACT attestation for bytes the named revision
	// does not contain.
	//
	// --ignored=matching brings those files back into the answer, so the
	// question being asked is "is everything I compiled in this commit" rather
	// than "is everything git chose to tell me about in this commit".
	dirty, err := git(ctx, abs, "status", "--porcelain", "--untracked-files=all", "--ignored=matching", "--", abs)
	if err != nil {
		// The question could not be answered, so the answer is not "clean".
		return head, tree, Unknown, resolvedRoot, relPath
	}
	if strings.TrimSpace(dirty) != "" {
		// DIRTY carries the tree DELIBERATELY, and it is the tree AS COMMITTED
		// at HEAD -- not the working tree, which has no object id. It records
		// what the revision holds while the state says the compiled inputs are
		// not that. Leaving this implicit is how the two would drift.
		return head, tree, Dirty, resolvedRoot, relPath
	}
	return head, tree, CleanExact, resolvedRoot, relPath
}

// DigestBytes is the content identity of the compiled inputs.
//
// It is reported ALONGSIDE the revision, never in place of it: two different
// commits can produce identical bytes, and a receipt must still say which one
// it came from.
func DigestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Keep the caller's git configuration from changing what we measure.
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
