package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	//
	// THIS IS A BROADENING, NOT AN EQUIVALENCE PROOF, and it is recorded as
	// debt for the same reason the transport ceiling was. Two sets are still
	// derived independently:
	//
	//	the extractor decides what it READS
	//	this git command decides what should COUNT AS DIRTY
	//
	// They now agree on the known disagreement. Nothing here proves they agree
	// in general, and a future extractor rule -- a new include pattern, a
	// symlinked corpus, a second input root -- could reopen the gap silently.
	//
	// The durable answer is a COMPILATION WITNESS: the extractor emits
	// {path, digest of the bytes it actually read} and that set is proven
	// against the named revision, so git stops guessing what the compiler
	// meant. Until then this narrows one oracle disagreement rather than
	// closing the family, and it must not be read as the latter.
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

// SourceWitness is what a publication actually compiled, established ONCE.
//
// THE SHARED-REFERENT LAW. Two mechanisms were describing "the compiled
// inputs" and quantifying over different things:
//
//	the compiler read EVERY --input root; the inspection read only the first,
//	so a dirty or separately versioned supplementary root could contribute
//	authenticated output under another root's CLEAN_EXACT commit;
//
//	the digest was taken from bytes compiled at one moment while HEAD, tree and
//	cleanliness were read at a later one, so a checkout that moved in between
//	produced an exact claim for a revision that did not produce those bytes.
//
// Agreement of terminology is not agreement of referent. This makes the roots
// and the moment explicit, and REFUSES the exact claim when they cannot be
// proven equivalent rather than making two independent answers happen to match.
type SourceWitness struct {
	Revision string
	Tree     string
	State    SourceState
	RelPath  string
	Roots    []string
}

// InspectCompiledSources establishes one witness over EVERY compiled root.
//
// The exact claim survives only if every root resolves to the same revision and
// the same tree and is individually clean. Roots that disagree are not a
// tie to break: the receipt has one revision, one tree and one path, so a set
// it cannot describe with them is a set it must not claim.
func InspectCompiledSources(roots []string) SourceWitness {
	if len(roots) == 0 {
		return SourceWitness{State: Unknown}
	}
	first := SourceWitness{Roots: append([]string(nil), roots...)}
	for i, root := range roots {
		rev, tree, state, _, rel := InspectSource(root)
		if i == 0 {
			first.Revision, first.Tree, first.State, first.RelPath = rev, tree, state, rel
			continue
		}
		if !state.ClaimsExactRevision() || rev != first.Revision || tree != first.Tree || rel != first.RelPath {
			// Downgrade rather than pick a winner. UNKNOWN is the honest state
			// for a set whose members this receipt cannot describe together.
			first.State = Unknown
			first.Revision, first.Tree = "", ""
			return first
		}
	}
	return first
}

// Unchanged reports whether a witness still describes the working tree.
//
// Compilation and inspection are two reads of one world. Re-establishing the
// witness afterwards and requiring it identical proves the snapshot did not
// move between them; a change is not resolved in favour of either read, because
// what was observed was two worlds.
func (w SourceWitness) Unchanged() (SourceWitness, bool) {
	now := InspectCompiledSources(w.Roots)
	return now, now.Revision == w.Revision && now.Tree == w.Tree &&
		now.State == w.State && now.RelPath == w.RelPath
}

// ConsumedFile is one file a compilation actually read.
type ConsumedFile struct {
	// Path as the compiler saw it.
	Path string
	// Digest of the exact bytes the parser consumed.
	Digest string
}

// ProveConsumedAgainstRevision is the witness the earlier state comparison was
// standing in for.
//
// ENDPOINT EQUALITY IS NOT CONTINUITY. Comparing the working tree before and
// after compilation accepts any history that starts and ends the same way: a
// file changed, compiled, and restored passes, while sliceNT came from bytes
// no revision contains. Two observations compatible with the event are not
// evidence of the event.
//
// This compares what was READ against what the revision HOLDS, per file, so the
// claim "these bytes came from this commit" is checked rather than inferred.
func ProveConsumedAgainstRevision(repoRoot, revision string, consumed []ConsumedFile) error {
	if strings.TrimSpace(revision) == "" {
		return fmt.Errorf("no revision to prove the compiled inputs against")
	}
	if len(consumed) == 0 {
		return fmt.Errorf("the compilation reported no consumed files, so nothing can be proven about it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	top, err := git(ctx, repoRoot, "rev-parse", "--show-toplevel")
	if err != nil || top == "" {
		return fmt.Errorf("the compiled inputs are not inside a resolvable git checkout")
	}
	for _, f := range consumed {
		if f.Digest == "" {
			return fmt.Errorf("%s was compiled without a recorded digest, so what was read cannot be proven", f.Path)
		}
		abs, err := filepath.Abs(f.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		rel, err := filepath.Rel(top, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%s was compiled from outside the checkout the revision describes", f.Path)
		}
		// The blob the REVISION holds, hashed the same way.
		blob, err := gitBytes(ctx, repoRoot, revision+":"+filepath.ToSlash(rel))
		if err != nil {
			return fmt.Errorf("%s was compiled but revision %s does not contain it", rel, shortRev(revision))
		}
		sum := sha256.Sum256(blob)
		if hex.EncodeToString(sum[:]) != f.Digest {
			return fmt.Errorf(
				"%s was compiled from bytes that revision %s does not hold", rel, shortRev(revision))
		}
	}
	return nil
}

func shortRev(r string) string {
	if len(r) > 12 {
		return r[:12]
	}
	return r
}

// gitBytes returns raw object bytes without the trimming git() applies.
func gitBytes(ctx context.Context, dir string, spec string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "blob", spec)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd.Output()
}
