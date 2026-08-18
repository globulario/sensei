// SPDX-License-Identifier: AGPL-3.0-only

package evalmutant

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaterializeRepo writes a mutant as a real git repository with a real history:
// one commit holding the clean baseline, then one commit introducing the defect
// carrying the mutant's own CommitMessage.
//
// # Why the suite needs this
//
// Mutant carries a CommitMessage "so history-based evidence is uniform across
// the suite", but Materialize only writes files, so that message reached no
// repository and no arm could read it. Two consequences, both quiet:
//
//   - the misleading-commit-message defect was witnessed by PROXY. Its witness
//     says so outright — "the message lives outside the tree, so the witness
//     reads the change the message describes" — which means the suite was
//     measuring whether an evaluator noticed a rename, not whether it caught a
//     commit message describing work the diff does not contain. Those are
//     different findings, and only one of them is the defect class named.
//   - every arm that reads history was unrunnable over the suite. WHY
//     investigation requires an explicit commit range, so a composition arm had
//     no repository to bind to and could not be compared against the
//     deterministic baseline at all.
//
// # Determinism
//
// Reproducibility is a completion criterion of the evaluation, so nothing here
// reads a clock, an environment, or the caller's git identity. Author and
// committer dates come from the caller; identity is fixed; commit signing and
// hooks are disabled, since a developer machine configured to sign commits
// would otherwise produce a different history than CI for the same mutant.
type RepoOptions struct {
	// CommittedAt is the explicit git timestamp for BOTH commits, in a format
	// git accepts (e.g. "2026-01-01T00:00:00Z"). Required: a self-stamped
	// history would not be reproducible.
	CommittedAt string
}

// BaselineCommitSubject is the first commit's subject. Fixed rather than
// caller-supplied so the range a WHY arm binds to is identical across runs.
const BaselineCommitSubject = "baseline: the control tree"

// MaterializeRepo materializes the mutant into root as a two-commit git
// repository and returns the baseline and defect revisions.
//
// The baseline commit is the CLEAN control, so the defect commit's diff
// contains exactly the mutation and nothing else. An arm asking "what did this
// commit claim to do, and what did it actually change?" therefore reads a diff
// that is the defect itself.
func MaterializeRepo(root string, m Mutant, opts RepoOptions) (baselineRev, defectRev string, err error) {
	if strings.TrimSpace(opts.CommittedAt) == "" {
		return "", "", fmt.Errorf("evalmutant: RepoOptions.CommittedAt is required; a self-stamped history is not reproducible")
	}
	if err := Materialize(root, Baseline()); err != nil {
		return "", "", fmt.Errorf("materialize baseline: %w", err)
	}
	if err := gitRun(root, opts, "init", "-b", "main"); err != nil {
		return "", "", err
	}
	if err := gitRun(root, opts, "add", "-A"); err != nil {
		return "", "", err
	}
	if err := gitRun(root, opts, "commit", "--no-gpg-sign", "-m", BaselineCommitSubject); err != nil {
		return "", "", err
	}
	baselineRev, err = gitOutput(root, opts, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}

	// The control needs no second commit: its history is the baseline alone,
	// and inventing an empty "defect" commit for it would give the control a
	// shape no mutant has.
	if m.Defect == "" {
		return baselineRev, baselineRev, nil
	}

	if err := replaceTree(root, m); err != nil {
		return "", "", err
	}
	if err := gitRun(root, opts, "add", "-A"); err != nil {
		return "", "", err
	}
	msg := strings.TrimSpace(m.CommitMessage)
	if msg == "" {
		return "", "", fmt.Errorf("evalmutant: %s carries no commit message; the defect commit would have nothing to claim", m.Defect)
	}
	if err := gitRun(root, opts, "commit", "--no-gpg-sign", "-m", msg); err != nil {
		return "", "", err
	}
	defectRev, err = gitOutput(root, opts, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return baselineRev, defectRev, nil
}

// replaceTree makes the working tree exactly the mutant's file set, including
// DELETING files the defect removed. Writing only the changed files would leave
// a tree that is the union of baseline and mutant, so a removal defect would be
// invisible and the commit's diff would misdescribe the mutation.
func replaceTree(root string, m Mutant) error {
	tracked := map[string]bool{}
	for p := range Baseline().Files {
		tracked[p] = true
	}
	for p := range m.Files {
		tracked[p] = true
	}
	for p := range tracked {
		full := filepath.Join(root, filepath.FromSlash(p))
		body, present := m.Files[p]
		if !present {
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// gitEnv pins everything that would otherwise vary per machine.
func gitEnv(opts RepoOptions) []string {
	return []string{
		"GIT_AUTHOR_NAME=Sensei Evaluation",
		"GIT_AUTHOR_EMAIL=eval@example.invalid",
		"GIT_COMMITTER_NAME=Sensei Evaluation",
		"GIT_COMMITTER_EMAIL=eval@example.invalid",
		"GIT_AUTHOR_DATE=" + opts.CommittedAt,
		"GIT_COMMITTER_DATE=" + opts.CommittedAt,
		// A developer machine with commit.gpgsign or hooks configured would
		// otherwise produce a different history than CI for the same mutant.
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + os.TempDir(),
		"PATH=" + os.Getenv("PATH"),
	}
}

func gitRun(root string, opts RepoOptions, args ...string) error {
	_, err := gitOutput(root, opts, args...)
	return err
}

func gitOutput(root string, opts RepoOptions, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = gitEnv(opts)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("evalmutant: git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
