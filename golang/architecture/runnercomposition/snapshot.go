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
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// fullHexObjectIDPattern matches a full, lowercase, hex git object ID --
// either the sha1 (40 hex digits) or sha256 (64 hex digits) length. It
// deliberately matches nothing else: no abbreviated/short SHA, no symbolic
// ref (branch, tag, "HEAD"), and no relative or tree-ish expression
// ("HEAD~3", "main^{tree}", "some-tag^{}"). ExtractSnapshot's baseRevision
// must be a structurally pinned commit identity, not an arbitrary Git
// tree-ish -- a tree-ish is inherently re-resolvable (a branch/tag can move,
// "HEAD" depends on ambient repository state, a relative expression depends
// on history at resolution time), which is incompatible with the exact,
// once-captured revision a synthesis.Session/workspacecontract.Identity
// already commits to.
var fullHexObjectIDPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

func isFullHexObjectID(s string) bool {
	return fullHexObjectIDPattern.MatchString(s)
}

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
// arguments use. baseRevision must be a full, lowercase, hex commit object
// ID (isFullHexObjectID) that names an actual commit in repositoryRoot,
// verified via `git cat-file -t` -- not a branch, tag, "HEAD", or any other
// tree-ish expression, and not a tree or blob OID. There is no fallback to
// HEAD or the live working tree anywhere in this function: baseRevision is
// used exactly as given, with no default (hard law 3).
//
// Every entry's path is validated with the existing ValidateCandidatePath
// before it is ever written, and the complete set of entries is validated
// as a whole (validateForMaterialization) for exact duplicates,
// case-insensitive/Unicode-normalization collisions, and symlink-parent
// conflicts before any of it is written -- defense in depth, since a
// well-formed git tree cannot itself contain such a conflict, but this
// function does not assume it never could. Every write goes through an
// *os.Root scoped to the destination directory.
//
// On success, ExtractSnapshot returns the snapshot directory's path, its
// canonical manifest, and that manifest's digest -- InputCandidateDigestSHA256
// per CandidateArtifact's field of the same name -- built directly from the
// same blob content that was written, never by a separate re-read of the
// filesystem after the fact. The returned cleanup function must be called
// (e.g. via defer) once the snapshot is no longer needed; its error must not
// be discarded, since whether cleanup actually succeeded is truth a caller
// may need to record (see RunnerReceipt's cleanup_succeeded field). On any
// error, nothing is left behind -- no partially-extracted directory survives
// for the caller to clean up or accidentally treat as complete.
func ExtractSnapshot(ctx context.Context, repositoryRoot, baseRevision string) (snapshotDir string, manifest []CandidateManifestEntry, inputCandidateDigestSHA256 string, cleanup func() error, err error) {
	if err := validateAbsoluteRealDirectory("repositoryRoot", repositoryRoot); err != nil {
		return "", nil, "", nil, err
	}
	if !isFullHexObjectID(baseRevision) {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: baseRevision %q must be a full, lowercase, hex commit object ID -- not a branch, tag, HEAD, abbreviated SHA, or any other tree-ish expression", baseRevision)
	}

	parent, err := os.MkdirTemp("", "runnercomposition-snapshot-")
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: create staging directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			os.RemoveAll(parent)
		}
	}()

	isolatedHome := filepath.Join(parent, "home")
	if err := os.MkdirAll(isolatedHome, 0o700); err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: create isolated HOME: %w", err)
	}
	dest := filepath.Join(parent, "snapshot")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: create destination directory: %w", err)
	}

	if err := verifyRevisionIsCommit(ctx, repositoryRoot, baseRevision, isolatedHome); err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}

	entries, err := listRevisionTree(ctx, repositoryRoot, baseRevision, isolatedHome)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}
	if err := validateForMaterialization(entries); err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
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
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}

	unsorted := make([]CandidateManifestEntry, 0, len(entries))
	for _, e := range entries {
		content, ok := blobs[e.oid]
		if !ok {
			return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %q: object %s was not fetched", e.path, e.oid)
		}
		me := CandidateManifestEntry{Path: e.path, Mode: e.mode}
		if e.mode == ModeSymlink {
			me.SymlinkTarget = string(content)
			me.ContentDigestSHA256 = sha256Hex(content)
		} else {
			me.Content = content
			me.ContentDigestSHA256 = sha256Hex(content)
		}
		unsorted = append(unsorted, me)
	}
	canonical, err := CanonicalizeManifest(unsorted)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}
	digest, err := ManifestDigest(unsorted)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: %w", err)
	}

	destRoot, err := os.OpenRoot(dest)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: open destination root: %w", err)
	}
	defer destRoot.Close()

	for _, e := range canonical {
		name := filepath.FromSlash(e.Path)
		if err := destRoot.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: mkdir for %q: %w", e.Path, err)
		}
		switch e.Mode {
		case ModeSymlink:
			// SymlinkTarget IS the symlink's blob content -- git stores a
			// symlink's blob content as its literal target, never resolved
			// or followed here.
			if err := destRoot.Symlink(e.SymlinkTarget, name); err != nil {
				return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: symlink %q: %w", e.Path, err)
			}
		case ModeExecutable:
			if err := destRoot.WriteFile(name, e.Content, 0o755); err != nil {
				return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: write %q: %w", e.Path, err)
			}
		default: // ModeRegular
			if err := destRoot.WriteFile(name, e.Content, 0o644); err != nil {
				return "", nil, "", nil, fmt.Errorf("ExtractSnapshot: write %q: %w", e.Path, err)
			}
		}
	}

	succeeded = true
	return dest, canonical, digest, func() error { return os.RemoveAll(parent) }, nil
}

// verifyRevisionIsCommit confirms baseRevision names an actual commit
// object in repositoryRoot -- not merely a syntactically well-formed hex
// string, and not a tree or blob OID passed where a commit identity is
// required. isFullHexObjectID only checks shape; this is the check that
// baseRevision names something real and of the right kind.
func verifyRevisionIsCommit(ctx context.Context, repositoryRoot, baseRevision, isolatedHome string) error {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "-t", baseRevision)
	cmd.Dir = repositoryRoot
	cmd.Env = gitIsolatedEnv(isolatedHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git cat-file -t %s: %w: %s", baseRevision, err, stderr.String())
	}
	objType := strings.TrimSpace(stdout.String())
	if objType != "commit" {
		return fmt.Errorf("baseRevision %q names a %s object, not a commit", baseRevision, objType)
	}
	return nil
}

// validateForMaterialization rejects a set of tree entries that a
// well-formed git history could never itself produce, but a maliciously or
// corruptly crafted tree object could -- defense in depth before any of it
// is written to a real filesystem, which enforces constraints git's
// abstract tree model does not:
//
//   - exact duplicate paths;
//   - a path nested under another entry's path -- since `git ls-tree -r`
//     only ever lists blobs, any existing entry path reused as a directory
//     prefix by another entry is definitionally a file or symlink being
//     asked to double as a directory, which MkdirAll would otherwise either
//     fail on unpredictably or, through an existing symlink, silently
//     redirect the write elsewhere within the destination root;
//   - paths that only differ by ASCII/Unicode case folding or by Unicode
//     normalization form (NFC), which do not round-trip as distinct entries
//     on a case-insensitive or normalizing filesystem (e.g. default macOS
//     APFS) even though they are two distinct, independently addressable
//     blobs in git.
func validateForMaterialization(entries []revisionEntry) error {
	byExact := make(map[string]bool, len(entries))
	byFold := make(map[string]string, len(entries))
	byNFC := make(map[string]string, len(entries))

	for _, e := range entries {
		if byExact[e.path] {
			return fmt.Errorf("duplicate path %q in tree", e.path)
		}
		byExact[e.path] = true

		fold := strings.ToLower(e.path)
		if prior, ok := byFold[fold]; ok {
			return fmt.Errorf("paths %q and %q collide under case-insensitive folding", prior, e.path)
		}
		byFold[fold] = e.path

		nfc := norm.NFC.String(e.path)
		if prior, ok := byNFC[nfc]; ok {
			return fmt.Errorf("paths %q and %q collide under Unicode NFC normalization", prior, e.path)
		}
		byNFC[nfc] = e.path
	}

	for _, e := range entries {
		parts := strings.Split(e.path, "/")
		for i := 1; i < len(parts); i++ {
			prefix := strings.Join(parts[:i], "/")
			if byExact[prefix] {
				return fmt.Errorf("path %q is nested under %q, which is itself a file or symlink entry -- not a directory", e.path, prefix)
			}
		}
	}
	return nil
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
//
// It derives its own cancelable context from ctx and defers cancellation,
// and every early-return error path calls abort (cancel then Wait) rather
// than Wait alone. This matters because the read loop and the stdin-writer
// goroutine run concurrently: if the read loop returns early (a malformed
// header, a missing object, a short read) while the writer goroutine is
// still blocked inside a stdin write -- which it can be, if the batch of
// object IDs is large enough that the OS pipe buffer fills before git has
// drained it -- calling Wait directly would block forever: git is not
// exiting (nothing told it to), and the writer goroutine is not unblocking
// (nothing is reading stdin faster, and nothing closed it). Cancelling the
// context first kills the process, which closes its end of the stdin pipe;
// the writer goroutine's blocked write then fails with a broken-pipe error
// instead of hanging, and Wait returns promptly once the process is dead.
func fetchBlobContents(ctx context.Context, repositoryRoot, isolatedHome string, oids []string) (map[string][]byte, error) {
	if len(oids) == 0 {
		return map[string][]byte{}, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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
	abort := func() {
		cancel()
		cmd.Wait()
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
			abort()
			return nil, fmt.Errorf("git cat-file: read header: %w", err)
		}
		header = strings.TrimSuffix(header, "\n")
		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "missing" {
			abort()
			return nil, fmt.Errorf("git cat-file: object %s missing", fields[0])
		}
		if len(fields) != 3 || fields[1] != "blob" {
			abort()
			return nil, fmt.Errorf("git cat-file: unexpected header %q", header)
		}
		oid := fields[0]
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			abort()
			return nil, fmt.Errorf("git cat-file: invalid size in header %q", header)
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(reader, data); err != nil {
			abort()
			return nil, fmt.Errorf("git cat-file: read object %s: %w", oid, err)
		}
		if _, err := reader.Discard(1); err != nil { // trailing newline after object data
			abort()
			return nil, fmt.Errorf("git cat-file: read trailing newline for %s: %w", oid, err)
		}
		results[oid] = data
	}

	if err := <-writeErr; err != nil {
		abort()
		return nil, fmt.Errorf("git cat-file: write object IDs: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file: %w: %s", err, stderr.String())
	}
	return results, nil
}

// InitializeCandidateBuffer creates a fresh, independent directory as a
// full copy of snapshotDir (hard law 8), then BINDS the result to
// snapshotManifest's identity rather than trusting the copy blindly: it
// rebuilds the freshly-copied buffer's manifest from the filesystem
// (BuildManifest) and compares that manifest's digest against
// ManifestDigest(snapshotManifest) -- the same digest ExtractSnapshot
// returns as InputCandidateDigestSHA256. A mismatch (a copyTree bug, a
// caller passing a manifest that does not actually describe snapshotDir's
// content, concurrent modification of snapshotDir between extraction and
// buffer initialization) is reported as an error rather than silently
// producing a buffer whose starting content does not match the pinned
// input identity it is supposed to start from.
//
// snapshotDir must be an absolute, real (non-symlink) directory.
// snapshotManifest is validated via ManifestDigest/CanonicalizeManifest --
// the same validation every manifest in this package goes through.
//
// On success, returns the buffer directory's path, its (freshly rebuilt,
// canonical) manifest, and that manifest's digest -- identical to
// snapshotManifest's digest, per hard law 8, which the internal comparison
// above already proved. The returned cleanup function's error must not be
// discarded, for the same reason ExtractSnapshot's is not. On error,
// nothing is left behind.
func InitializeCandidateBuffer(snapshotDir string, snapshotManifest []CandidateManifestEntry) (bufferDir string, bufferManifest []CandidateManifestEntry, bufferDigestSHA256 string, cleanup func() error, err error) {
	if err := validateAbsoluteRealDirectory("snapshotDir", snapshotDir); err != nil {
		return "", nil, "", nil, err
	}
	expectedDigest, err := ManifestDigest(snapshotManifest)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: snapshotManifest: %w", err)
	}

	parent, err := os.MkdirTemp("", "runnercomposition-buffer-")
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: create staging directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			os.RemoveAll(parent)
		}
	}()

	dest := filepath.Join(parent, "buffer")
	if err := copyTree(snapshotDir, dest); err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: %w", err)
	}

	diskEntries, err := BuildManifest(dest)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: rebuild buffer manifest: %w", err)
	}
	diskCanonical, err := CanonicalizeManifest(diskEntries)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: %w", err)
	}
	diskDigest, err := ManifestDigest(diskEntries)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: %w", err)
	}
	if diskDigest != expectedDigest {
		return "", nil, "", nil, fmt.Errorf("InitializeCandidateBuffer: freshly copied buffer's manifest digest %q does not match snapshotManifest's digest %q -- buffer is not bound to the pinned snapshot identity", diskDigest, expectedDigest)
	}

	succeeded = true
	return dest, diskCanonical, diskDigest, func() error { return os.RemoveAll(parent) }, nil
}
