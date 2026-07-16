// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prereview"
)

// gitDiffSource resolves a proposed change from a local git repository into a
// digest-bound diff. It is deterministic: tree digests are git's own tree object
// hashes and the diff digest is a content hash of the textual diff.
type gitDiffSource struct{}

func (gitDiffSource) ResolveReviewDiff(ctx context.Context, req prereview.DiffRequest) (prereview.BoundDiff, error) {
	root := strings.TrimSpace(req.RepoRoot)
	if root == "" {
		root = "."
	}
	base := strings.TrimSpace(req.Base)
	if base == "" {
		base = "origin/main"
	}
	head := strings.TrimSpace(req.Head)
	if head == "" {
		head = "HEAD"
	}

	if _, err := gitOut(ctx, root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("not a git repository at %q: %w", root, err)
	}
	baseRev, err := gitOut(ctx, root, "rev-parse", base)
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("resolve base %q: %w", base, err)
	}
	headRev, err := gitOut(ctx, root, "rev-parse", head)
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("resolve head %q: %w", head, err)
	}
	baseTree, err := gitOut(ctx, root, "rev-parse", base+"^{tree}")
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("resolve base tree: %w", err)
	}
	headTree, err := gitOut(ctx, root, "rev-parse", head+"^{tree}")
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("resolve head tree: %w", err)
	}
	mergeBase, _ := gitOut(ctx, root, "merge-base", base, head) // best-effort

	diffBytes, err := gitRaw(ctx, root, "diff", "--no-color", "-M", baseRev, headRev)
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("compute diff: %w", err)
	}
	nameStatus, err := gitOut(ctx, root, "diff", "--name-status", "-M", baseRev, headRev)
	if err != nil {
		return prereview.BoundDiff{}, fmt.Errorf("compute name-status: %w", err)
	}
	created, modified, deleted, renamed := parseNameStatus(nameStatus)

	return prereview.BoundDiff{
		RepositoryDomain:     gitRemoteDomain(root),
		BaseRevision:         baseRev,
		BaseTreeDigestSHA256: baseTree,
		HeadRevision:         headRev,
		HeadTreeDigestSHA256: headTree,
		MergeBaseRevision:    strings.TrimSpace(mergeBase),
		DiffDigestSHA256:     sha256Hex(diffBytes),
		FilesCreated:         created,
		FilesModified:        modified,
		FilesDeleted:         deleted,
		FilesRenamed:         renamed,
	}, nil
}

// parseNameStatus parses `git diff --name-status -M` output into typed file
// change lists.
func parseNameStatus(out string) (created, modified, deleted []string, renamed []prereview.RenamePair) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		status := fields[0]
		switch {
		case strings.HasPrefix(status, "A") && len(fields) >= 2:
			created = append(created, fields[1])
		case strings.HasPrefix(status, "M") && len(fields) >= 2:
			modified = append(modified, fields[1])
		case strings.HasPrefix(status, "D") && len(fields) >= 2:
			deleted = append(deleted, fields[1])
		case strings.HasPrefix(status, "R") && len(fields) >= 3:
			renamed = append(renamed, prereview.RenamePair{From: fields[1], To: fields[2]})
		case strings.HasPrefix(status, "C") && len(fields) >= 3:
			created = append(created, fields[2])
		case len(fields) >= 2:
			modified = append(modified, fields[1])
		}
	}
	return created, modified, deleted, renamed
}

func gitOut(ctx context.Context, root string, args ...string) (string, error) {
	out, err := gitRaw(ctx, root, args...)
	return strings.TrimSpace(string(out)), err
}

func gitRaw(ctx context.Context, root string, args ...string) ([]byte, error) {
	full := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
