// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DiffBinding asks for an incremental HOW extraction: describe what changed
// between two exact revisions, rather than the whole repository.
//
// Both revisions are required and both must be full, lowercase, 40-hex commit
// object IDs. A branch name, a tag, HEAD, or an abbreviated SHA would each
// make the same request mean different things on different days, and an
// incremental result whose base cannot be reproduced is not evidence about a
// change -- it is evidence about whatever the name pointed at when someone
// happened to run it.
type DiffBinding struct {
	BaseRevision string
	HeadRevision string
}

// verifyHeadIsCheckedOut refuses a diff whose head is not what the extractor
// will actually read.
//
// The extractors read the ambient working tree. If --diff-head names a
// historical commit, or the checkout has moved on, or the tree carries
// uncommitted edits, the observations and the source-snapshot digest describe
// content that is not the head the receipt claims. Every digest would still
// verify against itself while the document described a different tree.
//
// Refusing is the honest option available here: extracting from a materialized
// head snapshot instead would be a larger change, and silently describing the
// wrong tree is not an option at all.
func verifyHeadIsCheckedOut(ctx context.Context, root string, binding DiffBinding) error {
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("howextract: resolve the checkout's HEAD: %w", err)
	}
	if head != binding.HeadRevision {
		return fmt.Errorf("howextract: --diff-head %s is not what this checkout holds (HEAD is %s); the extractors read the working tree, so the observations would describe different content than the diff claims",
			binding.HeadRevision, head)
	}
	status, err := gitOutput(ctx, root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("howextract: read worktree status: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("howextract: the working tree has uncommitted changes, so it is not %s; an incremental extraction must describe the exact head it is bound to",
			binding.HeadRevision)
	}
	return nil
}

// gitOutput runs one read-only git command with an isolated HOME.
func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	isolated, err := os.MkdirTemp("", "howextract-git-home-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(isolated)
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", abs}, args...)...)
	cmd.Env = append(os.Environ(),
		"HOME="+isolated,
		"GIT_CONFIG_GLOBAL="+filepath.Join(isolated, "gitconfig"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(isolated, "gitconfig"),
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DiffScopeReceipt is what the document records about an incremental run: the
// exact pair it was bound to, the files it therefore described, and -- the
// part that keeps a partial document from reading as a whole one -- an
// explicit statement that the rest of the repository was not searched.
type DiffScopeReceipt struct {
	BaseRevision string   `json:"base_revision" yaml:"base_revision"`
	HeadRevision string   `json:"head_revision" yaml:"head_revision"`
	ChangedPaths []string `json:"changed_paths" yaml:"changed_paths"`
	// SearchedPaths is ChangedPaths after the budget's own scopes have had
	// their say, and after paths that no longer exist at head are dropped. It
	// is what the observations in this document can actually be about.
	SearchedPaths []string `json:"searched_paths" yaml:"searched_paths"`
	// WholeRepositoryNotSearched is always true for a diff-scoped run. It is
	// stated rather than inferred, because a consumer that only reads the
	// observations has no other way to know this document is not a description
	// of the repository.
	WholeRepositoryNotSearched bool `json:"whole_repository_not_searched" yaml:"whole_repository_not_searched"`
}

var fullHexRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

// resolveChangedPaths returns the repo-relative source paths that differ
// between base and head, sorted, restricted to files that exist at head.
//
// Deleted files are excluded deliberately: HOW observations are anchored to
// source positions, and there is no position to anchor to in a file that is
// gone. Their absence is not silent -- a deleted path appears in ChangedPaths
// and not in SearchedPaths, and the difference between the two lists is
// exactly the set of changes this document could not describe.
func resolveChangedPaths(ctx context.Context, root string, binding DiffBinding) (all []string, err error) {
	for name, rev := range map[string]string{"base": binding.BaseRevision, "head": binding.HeadRevision} {
		if !fullHexRevision.MatchString(rev) {
			return nil, fmt.Errorf("howextract: diff %s revision %q must be a full, lowercase, 40-hex commit object id -- not a branch, tag, HEAD, or abbreviated sha, because an incremental result whose base cannot be reproduced is not evidence about a change", name, rev)
		}
	}
	if binding.BaseRevision == binding.HeadRevision {
		return nil, fmt.Errorf("howextract: diff base and head are the same revision %s; there is no change to describe", binding.BaseRevision)
	}

	out, err := gitDiffNames(ctx, root, binding.BaseRevision, binding.HeadRevision)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range strings.Split(out, "\x00") {
		// Deliberately NOT trimmed: a pathname may legitimately begin or end
		// with whitespace, and -z already delimits unambiguously.
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		all = append(all, filepath.ToSlash(p))
	}
	sort.Strings(all)
	return all, nil
}

// existingAtHead filters to the paths still present in the working tree, which
// is what the extractor will actually read.
func existingAtHead(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err == nil && info.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

// gitDiffNames runs the one git command this file needs, with an isolated
// HOME and config so a developer's global git configuration (diff drivers,
// rename limits, external tools) cannot change what a governed document
// reports as changed.
func gitDiffNames(ctx context.Context, root, base, head string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	isolated, err := os.MkdirTemp("", "howextract-diff-home-")
	if err != nil {
		return "", fmt.Errorf("howextract: create isolated HOME: %w", err)
	}
	defer os.RemoveAll(isolated)

	// -z: NUL-terminated, so a pathname containing non-ASCII, quotes, newlines,
	// or leading/trailing whitespace arrives verbatim. Plain --name-only quotes
	// and escapes such names, and splitting on newlines then trimming mangles
	// them further -- producing a synthetic path that fails os.Stat and matches
	// no extractor path, silently dropping a genuinely changed source file.
	//
	// No --diff-filter: deletions must be REPORTED. Excluding them (the
	// previous ACMRT) contradicted the receipt's own contract, which says
	// deleted paths appear in changed_paths and not in searched_paths. It also
	// made the test guarding that behaviour vacuous, since the guard only ran
	// when a deletion appeared.
	cmd := exec.CommandContext(ctx, "git", "-C", abs,
		"diff", "--name-only", "-z", "--no-renames", base, head, "--")
	cmd.Env = append(os.Environ(),
		"HOME="+isolated,
		"GIT_CONFIG_GLOBAL="+filepath.Join(isolated, "gitconfig"),
		"GIT_CONFIG_SYSTEM="+filepath.Join(isolated, "gitconfig"),
		"GIT_TERMINAL_PROMPT=0",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("howextract: git diff %s..%s in %s: %w: %s", base, head, abs, err, strings.TrimSpace(stderr.String()))
	}
	return string(stdout), nil
}
