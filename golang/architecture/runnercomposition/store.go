// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// ErrCandidateArtifactNotFound is returned (wrapped) by
// CandidateArtifactStore.Get when nothing is sealed under the requested
// digest.
var ErrCandidateArtifactNotFound = errors.New("candidate artifact not found")

// ErrCandidateArtifactCorrupted is returned (wrapped) by
// CandidateArtifactStore.Get when something IS sealed under the requested
// digest, but it fails raw-schema validation, strict decoding,
// ValidateCandidateArtifact, its own CandidateArtifactDigestSHA256 does not
// match the digest it was retrieved by, or the stored entry is not a plain
// regular file -- a distinct failure mode from "not found," since the
// store found SOMETHING under that key but must refuse to hand it back as
// if it were trustworthy.
var ErrCandidateArtifactCorrupted = errors.New("candidate artifact corrupted")

// sealedDigestPattern matches exactly the shape sha256Hex ever produces: 64
// lowercase hex digits. Used as a defense-in-depth guard immediately before
// a digest string is used to construct a filesystem path -- even though
// ValidateCandidateArtifact (called before every Put) and the schema's own
// sha256Hex pattern already constrain CandidateArtifactDigestSHA256 to this
// shape, this package's established discipline is to never let a single
// validation layer be the only thing standing between an untrusted-shaped
// string and a filesystem path.
var sealedDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CandidateArtifactStore is the sole typed owner of CandidateArtifact
// persistence -- hard law 14. The ephemeral capture surface (snapshot,
// candidate buffer) that produces a CandidateArtifact's content is
// destroyed after every run; Put seals that content into this store before
// that happens, and O4, O5, retry, and resume address a candidate only by
// CandidateArtifactDigestSHA256 against this store -- never against a
// temporary directory or any other filesystem location.
type CandidateArtifactStore interface {
	// Put seals artifact into the store, keyed by its own
	// CandidateArtifactDigestSHA256. It calls ValidateCandidateArtifact
	// before writing anything, so an internally inconsistent artifact is
	// rejected before it can be persisted (hard law 14a) -- Put alone
	// carries the "verified" promise this store's whole contract rests on.
	//
	// Publication is atomic no-clobber sealing, not replace-capable: the
	// content is staged, then committed via a hard link that ONLY
	// succeeds if no entry already exists under the digest (the OS makes
	// this check-and-create a single atomic operation -- there is no
	// window between "does it exist" and "create it" for a second
	// publisher, in this process or another, to win a race and overwrite
	// an already-sealed entry). If the digest is already occupied, the
	// existing entry is read back (via a no-follow, regular-file-only
	// reader) and strictly compared to what this call would have sealed;
	// identical content is an idempotent no-op, and different content is
	// refused -- a sealed entry is immutable, never silently overwritten.
	//
	// Put's error return is an unambiguous, unconditional promise: nil
	// means the artifact is now durably, verifiably sealed and retrievable
	// by Get; non-nil means it is NOT sealed. This holds even if removing
	// the now-redundant internal staging link fails after a successful
	// seal -- that is best-effort tidying, never part of the seal itself,
	// and its failure is never surfaced as a Put error, since doing so
	// would itself be the broken promise: telling a caller "not sealed"
	// about content that unambiguously is. A staging-cleanup failure on a
	// genuine failure path (the content was never sealed by this call) IS
	// surfaced, joined with the real failure, never silently discarded.
	Put(ctx context.Context, artifact CandidateArtifact) error

	// Get retrieves the CandidateArtifact previously sealed under
	// digestSHA256. It reads the stored entry through the same no-follow,
	// regular-file-only reader Put's conflict check uses -- a single
	// atomic open(2) with O_NOFOLLOW|O_NONBLOCK, not a separate
	// inspect-then-open sequence, so there is no window in which the
	// entry could be replaced between a check and a read -- validates the
	// RAW bytes against CandidateArtifact's closed schema before any typed
	// decoding (so an injected unknown field cannot be silently dropped by
	// json.Unmarshal before additionalProperties:false ever sees it),
	// decodes strictly (rejecting unknown fields and requiring exactly one
	// complete JSON document) as a second, redundant layer, then runs
	// ValidateCandidateArtifact for semantic/digest verification, then
	// confirms the artifact's own CandidateArtifactDigestSHA256 equals
	// digestSHA256 -- so an entry corrupted after being sealed (truncated,
	// bit-rotted, replaced by a symlink or FIFO, tampered with an extra
	// field, or no longer matching the key it is stored under) is refused
	// rather than silently handed back as if it were trustworthy.
	//
	// Returns an error satisfying errors.Is(err, ErrCandidateArtifactNotFound)
	// if nothing is sealed under digestSHA256, or
	// errors.Is(err, ErrCandidateArtifactCorrupted) if something is sealed
	// there but fails verification.
	Get(ctx context.Context, digestSHA256 string) (CandidateArtifact, error)
}

// fsCandidateArtifactStore is a filesystem-backed CandidateArtifactStore:
// one JSON file per artifact, named "<digest>.json", directly inside root.
// The exact storage substrate is an implementation decision (the design
// doc says so explicitly) -- this is the simplest one available in this
// codebase, consistent with every other ephemeral/staging path in this
// package already being filesystem-based.
type fsCandidateArtifactStore struct {
	root *os.Root
	// realRoot is root's validated absolute real path, kept alongside the
	// *os.Root handle specifically so readSealedEntry can open a sealed
	// entry via a raw, real-path syscall.Open with O_NOFOLLOW|O_NONBLOCK --
	// os.Root's own OpenFile does not honor O_NOFOLLOW (verified directly:
	// opening a symlink through os.Root with O_NOFOLLOW set in the flag
	// argument still succeeds and follows it), so there is no way to ask
	// os.Root itself for an atomic no-follow open of the final path
	// component. See readSealedEntry's doc comment for why bypassing
	// os.Root's containment is safe specifically for that function's use.
	realRoot string
}

// NewFSCandidateArtifactStore constructs a CandidateArtifactStore backed by
// root, which must already exist as an absolute, real (non-symlink)
// directory -- the same requirement this package's other filesystem
// constructors (newFSCandidateWorkspace, ExtractSnapshot's destination)
// apply, and for the same reason: this function does not create or own
// root's lifecycle, only what is written inside it.
//
// Multiple independent Go values returned by this function -- including
// from separate process instances pointed at the same root -- may safely
// address the same store concurrently; Put's atomicity does not depend on
// in-process locking.
func NewFSCandidateArtifactStore(root string) (CandidateArtifactStore, error) {
	if err := validateAbsoluteRealDirectory("root", root); err != nil {
		return nil, fmt.Errorf("NewFSCandidateArtifactStore: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("NewFSCandidateArtifactStore: open root: %w", err)
	}
	return &fsCandidateArtifactStore{root: r, realRoot: root}, nil
}

func (s *fsCandidateArtifactStore) Put(ctx context.Context, artifact CandidateArtifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateCandidateArtifact(artifact); err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: %w", err)
	}
	digest := artifact.CandidateArtifactDigestSHA256
	if !sealedDigestPattern.MatchString(digest) {
		return fmt.Errorf("CandidateArtifactStore.Put: candidate_artifact_digest_sha256 %q is not a well-formed sha256 hex digest", digest)
	}
	marshaled, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: marshal: %w", err)
	}
	finalName := digest + ".json"

	if err := s.ensureTmpDir(); err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: %w", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: generate staging name: %w", err)
	}
	tmpName := ".tmp/" + digest + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	if err := s.root.WriteFile(tmpName, marshaled, 0o644); err != nil {
		return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.Put: write staged content: %w", err), s.root.Remove(tmpName))
	}

	// Commit is a single atomic "create final only if absent" operation --
	// link(2) either creates finalName pointing at the staged content's
	// inode, or fails with EEXIST, with no window in between for a second
	// publisher to observe "absent" and also proceed to create it: exactly
	// one Link call across any number of concurrent racers can ever
	// succeed for a given finalName.
	linkErr := s.root.Link(tmpName, finalName)
	if linkErr == nil {
		// Sealed. From here Put's contract is unconditional: nil means
		// sealed. Removing the now-redundant staging link is best-effort
		// tidying, not part of the seal -- even if it fails, the leftover
		// link inside .tmp is inert: nothing ever treats a ".tmp/*" name
		// as a sealed entry, only a literal "<digest>.json" name directly
		// in root is (readSealedEntry, and everything that calls it, only
		// ever looks there). Returning an error in this branch would
		// itself be the broken promise: it would tell a caller "not
		// sealed" about content that unambiguously is.
		s.root.Remove(tmpName)
		return nil
	}
	removeErr := s.root.Remove(tmpName) // always attempt cleanup of our own staging file.

	if !os.IsExist(linkErr) {
		return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.Put: commit (link) failed: %w", linkErr), removeErr)
	}

	// Something is already sealed under this digest. Read it back through
	// the no-follow, regular-file-only reader -- an existing entry that is
	// not a plain regular file (a symlink, FIFO, or other special file)
	// cannot be safely compared, and is refused as an unverifiable,
	// immutable conflicting entry rather than tolerated.
	existing, readErr := s.readSealedEntry(finalName)
	if readErr != nil {
		return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.Put: digest %q is already occupied by an entry that could not be safely verified: %w", digest, readErr), removeErr)
	}
	if string(existing) == string(marshaled) {
		// Already sealed (by this call or an earlier one) with identical
		// content: an idempotent success, unconditionally, regardless of
		// whether removing THIS call's own now-useless staging link
		// succeeded -- see the comment on the Link-success branch above
		// for why that cannot change this outcome.
		return nil
	}
	return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.Put: an artifact is already sealed under digest %q with different content -- a sealed entry is immutable and is never overwritten", digest), removeErr)
}

func (s *fsCandidateArtifactStore) Get(ctx context.Context, digestSHA256 string) (CandidateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return CandidateArtifact{}, err
	}
	if !sealedDigestPattern.MatchString(digestSHA256) {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: digestSHA256 %q is not a well-formed sha256 hex digest", digestSHA256)
	}

	data, err := s.readSealedEntry(digestSHA256 + ".json")
	if err != nil {
		if os.IsNotExist(err) {
			return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %w", digestSHA256, ErrCandidateArtifactNotFound)
		}
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %v: %w", digestSHA256, err, ErrCandidateArtifactCorrupted)
	}

	artifact, err := decodeCandidateArtifactStrict(data)
	if err != nil {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %v: %w", digestSHA256, err, ErrCandidateArtifactCorrupted)
	}
	if err := ValidateCandidateArtifact(artifact); err != nil {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %v: %w", digestSHA256, err, ErrCandidateArtifactCorrupted)
	}
	if artifact.CandidateArtifactDigestSHA256 != digestSHA256 {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: entry stored under %q actually carries digest %q: %w", digestSHA256, artifact.CandidateArtifactDigestSHA256, ErrCandidateArtifactCorrupted)
	}
	return artifact, nil
}

// decodeCandidateArtifactStrict validates raw against CandidateArtifact's
// closed schema BEFORE any typed decoding -- so additionalProperties:false
// sees exactly the bytes that were actually stored, not whatever survives
// json.Unmarshal's default behavior of silently discarding unrecognized
// keys before ValidateCandidateArtifact would ever re-marshal and check
// the now-clean struct. It then decodes strictly (rejecting unknown
// fields, a second and redundant layer) into exactly one complete JSON
// document -- trailing content after the first value is rejected too.
func decodeCandidateArtifactStrict(raw []byte) (CandidateArtifact, error) {
	if err := ValidateCandidateArtifactSchema(raw); err != nil {
		return CandidateArtifact{}, fmt.Errorf("raw schema: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var artifact CandidateArtifact
	if err := dec.Decode(&artifact); err != nil {
		return CandidateArtifact{}, fmt.Errorf("strict decode: %w", err)
	}
	if dec.More() {
		return CandidateArtifact{}, fmt.Errorf("trailing content after one JSON document")
	}
	return artifact, nil
}

// ensureTmpDir creates the internal staging directory if it does not
// already exist, and refuses to proceed if ".tmp" exists as anything other
// than a real directory. os.Root follows an internal symlink, so MkdirAll
// alone would silently accept ".tmp" being a symlink to some other
// directory within root, staging every Put's content through a followed
// alias instead of the store's own dedicated area.
func (s *fsCandidateArtifactStore) ensureTmpDir() error {
	if err := s.root.MkdirAll(".tmp", 0o755); err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	info, err := s.root.Lstat(".tmp")
	if err != nil {
		return fmt.Errorf("inspect staging directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf(".tmp is not a real directory (mode %v) -- refusing to stage through a followed alias", info.Mode())
	}
	return nil
}

// readSealedEntry reads name (a single flat filename directly inside root
// -- both of readSealedEntry's callers only ever pass a digest-derived
// "<digest>.json" name, never a nested path) as sealed artifact bytes.
//
// This deliberately bypasses os.Root for the open itself and opens the
// real absolute path directly with raw syscall.O_NOFOLLOW|syscall.O_NONBLOCK
// flags, because os.Root's own OpenFile does not honor O_NOFOLLOW --
// verified directly: opening a symlink through os.Root with O_NOFOLLOW set
// in the flag argument still succeeds and follows it, so there is no way
// to ask os.Root itself for an atomic no-follow open of the final path
// component. A raw open(2) with these two flags is the POSIX primitive for
// exactly this, in ONE atomic syscall:
//
//   - O_NOFOLLOW makes the kernel refuse a symlink as the final path
//     component as part of the SAME syscall that opens the file -- there
//     is no separate inspect-then-open window at all, so a symlink that
//     replaces name between an earlier check and a later open (which,
//     under a two-step Lstat-then-Open design, could pass a same-inode
//     os.SameFile check even though the entry itself is, structurally, a
//     symlink -- exactly the gap the previous design left open) can never
//     be followed, regardless of timing.
//   - O_NONBLOCK makes opening a FIFO return immediately instead of
//     blocking until a writer opens it, closing the hang this reader must
//     never expose, again with no separate check-first step that a FIFO
//     could be swapped in after.
//
// After the open, an fstat on the resulting descriptor confirms it is a
// plain regular file: O_NOFOLLOW alone only excludes symlinks, not
// directories, FIFOs (opened non-blocking, but still not something this
// function may return), sockets, or devices.
//
// Bypassing os.Root's containment here is safe specifically because name
// is never attacker-influenced path segments -- it is always exactly a
// sealedDigestPattern-validated, flat "<digest>.json" filename directly
// inside root, never nested, never containing "..". This is Linux/POSIX-
// specific (syscall.O_NOFOLLOW/O_NONBLOCK), consistent with this
// package's other POSIX-only assumptions (os.Root.Link's hard-link
// semantics, git subprocess invocation) and this repository's CI, which
// only runs ubuntu-latest.
//
// Returns an error satisfying os.IsNotExist if name does not exist.
func (s *fsCandidateArtifactStore) readSealedEntry(name string) ([]byte, error) {
	path := filepath.Join(s.realRoot, name)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file (mode %v)", name, info.Mode())
	}
	return io.ReadAll(f)
}

// joinCleanupErr never discards a cleanup failure: cleanupErr, if non-nil,
// is always represented in the returned error, either standing alone (when
// primary is nil -- the main operation succeeded but cleanup did not) or
// joined alongside primary (errors.Join, so both remain independently
// inspectable via errors.Is/As).
func joinCleanupErr(primary, cleanupErr error) error {
	if cleanupErr == nil {
		return primary
	}
	wrapped := fmt.Errorf("cleanup: remove staged content: %w", cleanupErr)
	if primary == nil {
		return wrapped
	}
	return errors.Join(primary, wrapped)
}
