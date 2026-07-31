// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// gitChangeDigestOldName and gitChangeDigestNewName are the fixed relative
// directory names GitChangeDigest always compares under -- see its doc
// comment for why these must be fixed, not the caller's real directory
// names.
const (
	gitChangeDigestOldName = "old"
	gitChangeDigestNewName = "new"
)

// GitChangeDigest returns the canonical content digest of the change from
// oldDir to newDir: sha256 over the raw bytes of
// `git diff --no-index --no-ext-diff --binary`, the same convention
// golang/architecture/admission.CaptureChanges and resulttransition's
// private patchDigest already use in this codebase, applied here to two
// independent ephemeral directories rather than two points in one
// repository's history. --no-index is exactly git diff's mode for comparing
// two arbitrary filesystem trees -- oldDir and newDir need not be, and for
// O3's use case never are, inside any git repository at all.
//
// Neither oldDir nor newDir is ever modified, moved, or removed --
// GitChangeDigest only ever reads them, copying their content into its own
// disposable staging area.
//
// Two properties this function guarantees that a naive `git diff --no-index
// oldDir newDir` invocation does NOT:
//
//  1. Path independence. git diff --no-index embeds the exact paths passed
//     on the command line in its output headers (diff --git a/<path>
//     b/<path>, --- a/<path>, +++ b/<path>). oldDir and newDir are
//     ephemeral, randomly-named temporary directories, so passing them
//     directly would make the digest depend on unrelated temp-directory
//     naming -- the identical logical change would hash differently every
//     run, violating determinism (hard law: "the same snapshot-plus-final-
//     tree pair always produces the same ProposedChangeDigestSHA256,
//     byte-for-byte"). GitChangeDigest closes this by copying oldDir and
//     newDir's content into a fresh staging directory under the FIXED
//     relative names "old"/"new" and invoking git with that staging
//     directory as its working directory, so the diff header is always
//     exactly "diff --git a/old/... b/new/...", independent of where
//     oldDir/newDir actually live on disk. (A symlink alias was considered
//     instead of a copy -- cheaper -- but git diff --no-index treats a
//     symlink argument as a symlink FILE to compare, diffing the two link
//     targets as text rather than recursing into what they point at, which
//     would silently diff the wrong thing.)
//  2. Configuration independence. GIT_CONFIG_NOSYSTEM alone only disables
//     SYSTEM-level git configuration (/etc/gitconfig) -- it does nothing
//     about the user's global config, global .gitattributes, or ambient
//     GIT_* environment variables the calling process might have inherited,
//     any of which can change git diff's OUTPUT FORMAT (e.g. a global
//     `text` attribute override can turn a "GIT binary patch" block into a
//     plain text hunk for the identical bytes) and therefore the digest.
//     GitChangeDigest runs git with a from-scratch environment: no
//     inherited GIT_* variables, HOME/XDG_CONFIG_HOME pointed at a fresh
//     empty directory (so no global gitconfig or global attributes file is
//     ever found), GIT_CONFIG_SYSTEM and GIT_CONFIG_GLOBAL explicitly
//     pointed at /dev/null (overriding even an ambient GIT_CONFIG_GLOBAL
//     the caller's own environment might already have set), and explicit
//     -c overrides for the config keys most likely to affect diff output
//     (core.attributesFile, core.autocrlf, core.safecrlf).
//
// oldDir and newDir must both name existing directories.
func GitChangeDigest(ctx context.Context, oldDir, newDir string) (string, error) {
	if oldDir == "" || newDir == "" {
		return "", fmt.Errorf("GitChangeDigest: oldDir and newDir must both be non-empty")
	}
	// git diff --no-index exits 1 for BOTH "found differences" (success)
	// and "could not access a path" (a real error) -- the exit code alone
	// cannot distinguish them, so existence is verified here, in Go, before
	// git is ever invoked, rather than by parsing git's stderr text.
	if err := validateAbsoluteRealDirectory("oldDir", oldDir); err != nil {
		return "", err
	}
	if err := validateAbsoluteRealDirectory("newDir", newDir); err != nil {
		return "", err
	}

	staging, err := os.MkdirTemp("", "runnercomposition-changedigest-")
	if err != nil {
		return "", fmt.Errorf("GitChangeDigest: create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	oldCopy := filepath.Join(staging, gitChangeDigestOldName)
	newCopy := filepath.Join(staging, gitChangeDigestNewName)
	isolatedHome := filepath.Join(staging, "home")
	if err := os.MkdirAll(isolatedHome, 0o700); err != nil {
		return "", fmt.Errorf("GitChangeDigest: create isolated HOME: %w", err)
	}
	if err := copyTree(oldDir, oldCopy); err != nil {
		return "", fmt.Errorf("GitChangeDigest: copy oldDir: %w", err)
	}
	if err := copyTree(newDir, newCopy); err != nil {
		return "", fmt.Errorf("GitChangeDigest: copy newDir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git",
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.autocrlf=false",
		"-c", "core.safecrlf=false",
		"diff", "--no-index", "--no-ext-diff", "--binary",
		"--", gitChangeDigestOldName, gitChangeDigestNewName,
	)
	cmd.Dir = staging
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + isolatedHome,
		"XDG_CONFIG_HOME=" + isolatedHome,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// git diff --no-index exits 1 when it found differences --
			// that is success, not failure. Any other non-zero exit (2 for
			// a usage/IO error, etc.) is a real error.
			if exitErr.ExitCode() != 1 {
				return "", fmt.Errorf("git diff --no-index: %w: %s", err, stderr.String())
			}
		} else {
			return "", fmt.Errorf("git diff --no-index: %w", err)
		}
	}

	return sha256Hex(stdout.Bytes()), nil
}

// validateAbsoluteRealDirectory rejects anything GitChangeDigest cannot
// safely hand to copyTree:
//
//   - a relative path, whose identity would depend on the calling
//     process's ambient current-working-directory state rather than being
//     self-contained;
//   - a symlink root. os.Stat FOLLOWS a symlink, so using it here would let
//     a symlink root pass validation while copyTree's filepath.WalkDir --
//     which, like BuildManifest, deliberately does NOT follow symlinks,
//     including at the root -- would then copy the symlink itself into
//     staging rather than the real directory it points at. git diff
//     --no-index would go on to treat that copied symlink as a symlink
//     FILE to compare, hashing the link's TARGET STRING instead of the
//     real candidate tree's content. os.Lstat (which does not follow) is
//     used here specifically so this is caught before copyTree ever runs;
//   - a non-directory, non-symlink path (a regular file, etc).
func validateAbsoluteRealDirectory(label, path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("GitChangeDigest: %s %q must be an absolute path", label, path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("GitChangeDigest: %s: %w", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("GitChangeDigest: %s %q must not be a symlink", label, path)
	}
	if !info.IsDir() {
		return fmt.Errorf("GitChangeDigest: %s %q is not a directory", label, path)
	}
	return nil
}

// copyTree recursively copies src's content to dst. Symlinks are copied as
// symlinks (via os.Readlink/os.Symlink), never followed -- consistent with
// this package's no-follow discipline elsewhere.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		case d.Type().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		default:
			return fmt.Errorf("copyTree: %q is neither a regular file, directory, nor symlink (mode %v)", p, d.Type())
		}
	})
}
