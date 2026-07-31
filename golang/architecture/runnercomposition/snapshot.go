// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// revisionEntry is one blob entry from `git ls-tree -r --full-tree` at a
// pinned revision.
type revisionEntry struct {
	path string // POSIX-relative, full path from the repository root
	mode CandidateFileMode
	oid  string
}

// ExtractSnapshot extracts repositoryRoot's content at exactly baseRevision
// into a fresh directory, reading raw blob bytes directly via
// `git ls-tree` + `git cat-file --batch` -- never `git archive`, which
// applies export-subst/export-ignore attribute-driven content
// transformation when the tree at that revision declares it (confirmed
// empirically: a file marked export-subst is extracted with its
// $Format:...$ placeholders substituted, not the raw blob bytes
// `git show`/`git cat-file` return for the exact same revision -- see the
// commit message for the reproduction). This function's entire purpose is
// EXACT extraction, so it bypasses that transform entirely by never
// invoking `git archive` at all.
//
// repositoryRoot must be an absolute, real (non-symlink) directory --
// validateAbsoluteRealDirectory, the same check GitChangeDigest's root
// arguments use. baseRevision must be non-empty and must not start with
// "-" (defends against being misinterpreted as a git option rather than a
// positional revision). There is no fallback to HEAD or the live working
// tree anywhere in this function: baseRevision is used exactly as given,
// with no default (hard law 3).
//
// Every entry's path is validated with the existing ValidateCandidatePath
// before it is ever written, and every write goes through an *os.Root
// scoped to the destination directory -- defense in depth, since a
// well-formed git tree cannot itself contain a path that fails that
// validation, but this function does not assume it never could.
//
// On success, returns the snapshot directory's path and a cleanup function
// the caller must call (e.g. via defer) once the snapshot is no longer
// needed. On any error, nothing is left behind -- no partially-extracted
// directory survives for the caller to clean up or accidentally treat as
// complete.
func ExtractSnapshot(ctx context.Context, repositoryRoot, baseRevision string) (snapshotDir string, cleanup func(), err error) {
	if err := validateAbsoluteRealDirectory("repositoryRoot", repositoryRoot); err != nil {
		return "", nil, err
	}
	if baseRevision == "" {
		return "", nil, fmt.Errorf("ExtractSnapshot: baseRevision must not be empty")
	}
	if strings.HasPrefix(baseRevision, "-") {
		return "", nil, fmt.Errorf("ExtractSnapshot: baseRevision %q must not start with '-'", baseRevision)
	}

	parent, err := os.MkdirTemp("", "runnercomposition-snapshot-")
	if err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: create staging directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			os.RemoveAll(parent)
		}
	}()

	isolatedHome := filepath.Join(parent, "home")
	if err := os.MkdirAll(isolatedHome, 0o700); err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: create isolated HOME: %w", err)
	}
	dest := filepath.Join(parent, "snapshot")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: create destination directory: %w", err)
	}

	entries, err := listRevisionTree(ctx, repositoryRoot, baseRevision, isolatedHome)
	if err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}

	oidSet := make(map[string]bool, len(entries))
	oids := make([]string, 0, len(entries))
	for _, e := range entries {
		if !oidSet[e.oid] {
			oidSet[e.oid] = true
			oids = append(oids, e.oid)
		}
	}
	blobs, err := fetchBlobContents(ctx, repositoryRoot, isolatedHome, oids)
	if err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}

	destRoot, err := os.OpenRoot(dest)
	if err != nil {
		return "", nil, fmt.Errorf("ExtractSnapshot: open destination root: %w", err)
	}
	defer destRoot.Close()

	for _, e := range entries {
		content, ok := blobs[e.oid]
		if !ok {
			return "", nil, fmt.Errorf("ExtractSnapshot: %q: object %s was not fetched", e.path, e.oid)
		}
		name := filepath.FromSlash(e.path)
		if err := destRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return "", nil, fmt.Errorf("ExtractSnapshot: mkdir for %q: %w", e.path, err)
		}
		switch e.mode {
		case ModeSymlink:
			// content IS the symlink target string -- git stores a
			// symlink's blob content as its literal target, never
			// resolved or followed here.
			if err := destRoot.Symlink(string(content), name); err != nil {
				return "", nil, fmt.Errorf("ExtractSnapshot: symlink %q: %w", e.path, err)
			}
		case ModeExecutable:
			if err := destRoot.WriteFile(name, content, 0o755); err != nil {
				return "", nil, fmt.Errorf("ExtractSnapshot: write %q: %w", e.path, err)
			}
		default: // ModeRegular
			if err := destRoot.WriteFile(name, content, 0o644); err != nil {
				return "", nil, fmt.Errorf("ExtractSnapshot: write %q: %w", e.path, err)
			}
		}
	}

	succeeded = true
	return dest, func() { os.RemoveAll(parent) }, nil
}

// listRevisionTree lists every blob (file) at baseRevision via
// `git ls-tree -r -z --full-tree`, recursively, with full paths from the
// repository root. `ls-tree -r` never lists directories on their own,
// matching BuildManifest's own "directories are implicit" model exactly.
// A tree entry that is not a blob (a submodule/gitlink, or anything else)
// is rejected -- there is no representation for it in the closed
// regular/executable/symlink mode vocabulary.
func listRevisionTree(ctx context.Context, repositoryRoot, baseRevision, isolatedHome string) ([]revisionEntry, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "-z", "--full-tree", baseRevision)
	cmd.Dir = repositoryRoot
	cmd.Env = gitIsolatedEnv(isolatedHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-tree: %w: %s", err, stderr.String())
	}

	var entries []revisionEntry
	for _, raw := range bytes.Split(stdout.Bytes(), []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		tabIdx := bytes.IndexByte(raw, '\t')
		if tabIdx < 0 {
			return nil, fmt.Errorf("git ls-tree: malformed entry %q", raw)
		}
		meta := strings.Fields(string(raw[:tabIdx]))
		path := string(raw[tabIdx+1:])
		if len(meta) != 3 {
			return nil, fmt.Errorf("git ls-tree: malformed metadata %q for %q", raw[:tabIdx], path)
		}
		gitMode, objType, oid := meta[0], meta[1], meta[2]
		if objType != "blob" {
			return nil, fmt.Errorf("git ls-tree: %q has unsupported type %q (only blob entries are supported -- no submodules)", path, objType)
		}
		var mode CandidateFileMode
		switch gitMode {
		case "100644":
			mode = ModeRegular
		case "100755":
			mode = ModeExecutable
		case "120000":
			mode = ModeSymlink
		default:
			return nil, fmt.Errorf("git ls-tree: %q has unsupported mode %q", path, gitMode)
		}
		if err := ValidateCandidatePath(path); err != nil {
			return nil, fmt.Errorf("git ls-tree: %w", err)
		}
		entries = append(entries, revisionEntry{path: path, mode: mode, oid: oid})
	}
	return entries, nil
}

// fetchBlobContents fetches the raw content of every object in oids via one
// `git cat-file --batch` process, returning a map from object ID to its
// exact bytes -- never subject to any attribute-driven transformation.
func fetchBlobContents(ctx context.Context, repositoryRoot, isolatedHome string, oids []string) (map[string][]byte, error) {
	if len(oids) == 0 {
		return map[string][]byte{}, nil
	}

	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = repositoryRoot
	cmd.Env = gitIsolatedEnv(isolatedHome)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git cat-file: start: %w", err)
	}

	writeErr := make(chan error, 1)
	go func() {
		defer stdin.Close()
		for _, oid := range oids {
			if _, err := fmt.Fprintln(stdin, oid); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	results := make(map[string][]byte, len(oids))
	reader := bufio.NewReader(stdout)
	for range oids {
		header, err := reader.ReadString('\n')
		if err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: read header: %w", err)
		}
		header = strings.TrimSuffix(header, "\n")
		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "missing" {
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: object %s missing", fields[0])
		}
		if len(fields) != 3 || fields[1] != "blob" {
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: unexpected header %q", header)
		}
		oid := fields[0]
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: invalid size in header %q", header)
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: read object %s: %w", oid, err)
		}
		if _, err := reader.Discard(1); err != nil { // trailing newline after object data
			cmd.Wait()
			return nil, fmt.Errorf("git cat-file: read trailing newline for %s: %w", oid, err)
		}
		results[oid] = data
	}

	if err := <-writeErr; err != nil {
		cmd.Wait()
		return nil, fmt.Errorf("git cat-file: write object IDs: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w: %s", err, stderr.String())
	}
	return results, nil
}

// InitializeCandidateBuffer creates a fresh, independent directory as a
// full copy of snapshotDir (hard law 8). Immediately after this call,
// BuildManifest(bufferDir) is identical to BuildManifest(snapshotDir) --
// no mutation has happened yet, since nothing has touched the buffer.
//
// snapshotDir must be an absolute, real (non-symlink) directory.
//
// On success, returns the buffer directory's path and a cleanup function
// the caller must call once the buffer is no longer needed. On error,
// nothing is left behind.
func InitializeCandidateBuffer(snapshotDir string) (bufferDir string, cleanup func(), err error) {
	if err := validateAbsoluteRealDirectory("snapshotDir", snapshotDir); err != nil {
		return "", nil, err
	}

	parent, err := os.MkdirTemp("", "runnercomposition-buffer-")
	if err != nil {
		return "", nil, fmt.Errorf("InitializeCandidateBuffer: create staging directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			os.RemoveAll(parent)
		}
	}()

	dest := filepath.Join(parent, "buffer")
	if err := copyTree(snapshotDir, dest); err != nil {
		return "", nil, fmt.Errorf("InitializeCandidateBuffer: %w", err)
	}

	succeeded = true
	return dest, func() { os.RemoveAll(parent) }, nil
}
