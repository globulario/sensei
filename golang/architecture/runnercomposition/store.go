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
	"strings"
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

	// PutAuxiliaryFile atomically writes name (a flat filename directly
	// inside the store's root, never a nested path) through the SAME
	// directory descriptor Put and Get use -- not a freshly re-opened path
	// -- so the write is guaranteed to land in the exact physical
	// directory a candidate was actually sealed into, even if the
	// original root path is renamed or replaced by something else while
	// this store is in use (see
	// TestFSCandidateArtifactStorePutAndGetShareStableRootIdentityAcrossRename
	// for why Put/Get already need, and get, this property).
	//
	// Unlike Put, this is replace-capable, not create-only-immutable: an
	// existing regular file at name is overwritten; an existing entry
	// that is not a regular file (a symlink, FIFO, or other special file)
	// is refused rather than followed or silently replaced. Callers
	// writing content that legitimately differs across independent calls
	// for reasons unrelated to the store's own sealed-immutability
	// contract (e.g. an admission lineage bundle, whose receipts carry
	// fresh session/receipt IDs every run even for the same candidate
	// digest) use this instead of Put.
	//
	// name must never be shaped like a sealed candidate's own filename
	// ("<64-lowercase-hex-digest>.json", exactly what Put's finalName
	// always is) -- that namespace is reserved for Put alone, and this
	// method refuses such a name rather than let its replace-capable
	// commit defeat Put's create-only, no-clobber contract.
	PutAuxiliaryFile(ctx context.Context, name string, data []byte) error

	// VerifyRootIdentity confirms that path currently names the exact
	// same directory this store's root was opened against -- i.e. that
	// nothing has renamed the original directory away and replaced it
	// with something else at the same path since construction. Every
	// write this store performs (Put, PutAuxiliaryFile) goes through the
	// stable, rename-immune root descriptor and is therefore unaffected
	// by such a replacement -- but a caller who separately reports path
	// (rather than through this store) after a long-running operation
	// must not claim that reported path still identifies where content
	// actually landed without checking. Returns a non-nil error if path
	// cannot be stat'd, or if it now names a different directory.
	VerifyRootIdentity(path string) error
}

// fsCandidateArtifactStore is a filesystem-backed CandidateArtifactStore:
// one JSON file per artifact, named "<digest>.json", directly inside root.
// The exact storage substrate is an implementation decision (the design
// doc says so explicitly) -- this is the simplest one available in this
// codebase, consistent with every other ephemeral/staging path in this
// package already being filesystem-based.
type fsCandidateArtifactStore struct {
	root *os.Root
	// dirFile is an *os.File open on "." WITHIN root -- i.e. derived from
	// root itself, not from a re-resolved path string -- kept specifically
	// so readSealedEntry can open a sealed entry via syscall.Openat
	// relative to dirFile.Fd() with O_NOFOLLOW|O_NONBLOCK. os.Root's own
	// OpenFile does not honor O_NOFOLLOW (verified directly: opening a
	// symlink through os.Root with O_NOFOLLOW set in the flag argument
	// still succeeds and follows it), so there is no way to ask os.Root
	// itself for an atomic no-follow open of the final path component --
	// but a bare real-path string is not a safe substitute either: on
	// Unix, an open directory descriptor (what both root and dirFile hold)
	// continues to reference the SAME directory even if it is renamed or
	// replaced at its original path, while re-resolving a cached path
	// string does not -- it happily opens whatever now sits at that path,
	// which could be an entirely different, replacement directory
	// (confirmed directly: rename the store's root directory away, create
	// a new empty one at the old path, and a path-string-based open reads
	// the replacement while root's own operations keep reaching the
	// original). dirFile is derived from root via root.Open(".") at
	// construction, so it is guaranteed to name the exact same directory
	// identity Put's every operation goes through -- not merely "the same
	// path at construction time."
	dirFile *os.File
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
	dirFile, err := r.Open(".")
	if err != nil {
		return nil, fmt.Errorf("NewFSCandidateArtifactStore: open root directory descriptor: %w", err)
	}
	return &fsCandidateArtifactStore{root: r, dirFile: dirFile}, nil
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

// PutAuxiliaryFile implements CandidateArtifactStore.PutAuxiliaryFile. It
// mirrors Put's own staged-write-then-commit discipline (stage under a
// random name in .tmp, commit with a single atomic operation) but through
// s.root -- the same descriptor Put and Get already use, not a fresh
// os.OpenRoot(path) -- and commits via Rename (replace-capable) rather
// than Link (create-only), since this method's whole contract is
// replace-capable, unlike Put's sealed immutability.
func (s *fsCandidateArtifactStore) PutAuxiliaryFile(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: name %q must be a flat filename", name)
	}
	// Reject any name shaped like a sealed candidate's own filename
	// (Put's finalName is always exactly "<64-lowercase-hex-digest>.json")
	// -- this method's replace-capable Rename commit would otherwise
	// defeat Put's create-only, no-clobber contract by overwriting an
	// already-sealed candidate with arbitrary auxiliary bytes, silently
	// corrupting it from Get's perspective (raw schema validation would
	// then fail, reported as ErrCandidateArtifactCorrupted). This
	// namespace is reserved for Put alone; PutAuxiliaryFile callers use a
	// name that cannot collide with it, e.g. "<digest>.lineage.json".
	if strings.HasSuffix(name, ".json") && sealedDigestPattern.MatchString(strings.TrimSuffix(name, ".json")) {
		return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: name %q is reserved for a sealed candidate artifact (Put), not an auxiliary file", name)
	}
	if info, statErr := s.root.Lstat(name); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: %q exists and is not a regular file (mode %s) -- refusing to write through it", name, info.Mode())
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: inspect %q: %w", name, statErr)
	}
	if err := s.ensureTmpDir(); err != nil {
		return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: %w", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: generate staging name: %w", err)
	}
	tmpName := ".tmp/" + name + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	if err := s.root.WriteFile(tmpName, data, 0o644); err != nil {
		return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: write staged content: %w", err), s.root.Remove(tmpName))
	}
	if err := s.root.Rename(tmpName, name); err != nil {
		return joinCleanupErr(fmt.Errorf("CandidateArtifactStore.PutAuxiliaryFile: commit (rename) failed: %w", err), s.root.Remove(tmpName))
	}
	return nil
}

// VerifyRootIdentity implements CandidateArtifactStore.VerifyRootIdentity
// by comparing os.SameFile against s.dirFile -- the same open directory
// descriptor Put/Get/PutAuxiliaryFile all address, derived from s.root at
// construction (root.Open(".")) and therefore unaffected by any later
// rename of the original path. os.Stat(path) re-resolves path fresh (it
// does NOT go through s.root), so a stat that no longer identifies the
// same underlying directory as s.dirFile proves path has been renamed
// away, replaced, or removed since construction.
func (s *fsCandidateArtifactStore) VerifyRootIdentity(path string) error {
	pathInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("CandidateArtifactStore.VerifyRootIdentity: stat %q: %w", path, err)
	}
	rootInfo, err := s.dirFile.Stat()
	if err != nil {
		return fmt.Errorf("CandidateArtifactStore.VerifyRootIdentity: stat opened root: %w", err)
	}
	if !os.SameFile(pathInfo, rootInfo) {
		return fmt.Errorf("CandidateArtifactStore.VerifyRootIdentity: %q no longer identifies the directory this store was constructed against -- it was renamed, replaced, or removed since", path)
	}
	return nil
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
// This deliberately bypasses os.Root for the open itself and instead uses
// syscall.Openat, relative to dirFile's descriptor, with raw
// O_NOFOLLOW|O_NONBLOCK flags, because os.Root's own OpenFile does not
// honor O_NOFOLLOW -- verified directly: opening a symlink through os.Root
// with O_NOFOLLOW set in the flag argument still succeeds and follows it,
// so there is no way to ask os.Root itself for an atomic no-follow open of
// the final path component. A raw open(2)-class call with these two flags
// is the POSIX primitive for exactly this, in ONE atomic syscall:
//
//   - O_NOFOLLOW makes the kernel refuse a symlink as the final path
//     component as part of the SAME syscall that opens the file -- there
//     is no separate inspect-then-open window at all, so a symlink that
//     replaces name between an earlier check and a later open (which,
//     under a two-step Lstat-then-Open design, could pass a same-inode
//     os.SameFile check even though the entry itself is, structurally, a
//     symlink -- exactly the gap an earlier design left open) can never
//     be followed, regardless of timing.
//   - O_NONBLOCK makes opening a FIFO return immediately instead of
//     blocking until a writer opens it, closing the hang this reader must
//     never expose, again with no separate check-first step that a FIFO
//     could be swapped in after.
//
// Critically, this opens relative to dirFile.Fd() -- a descriptor derived
// from root itself (root.Open(".") at construction) -- NOT a cached real
// path string. A path string re-resolves from scratch on every call: if
// root's directory is renamed away and a different directory is created
// at the original path, a path-string-based open would silently start
// reading the REPLACEMENT directory while root's own operations (which
// os.Root keeps bound to the original directory across a rename, per its
// documented behavior) keep reaching the original -- a genuine split
// between what Put publishes to and what Get reads from, confirmed
// directly by reproduction. Opening relative to dirFile's descriptor
// instead inherits the same rename-robust identity root itself has, since
// dirFile was derived from root, not independently re-resolved.
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
// specific (syscall.Openat/O_NOFOLLOW/O_NONBLOCK), consistent with this
// package's other POSIX-only assumptions (os.Root.Link's hard-link
// semantics, git subprocess invocation) and this repository's CI, which
// only runs ubuntu-latest.
//
// Returns an error satisfying os.IsNotExist if name does not exist.
func (s *fsCandidateArtifactStore) readSealedEntry(name string) ([]byte, error) {
	fd, err := syscall.Openat(int(s.dirFile.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	f := os.NewFile(uintptr(fd), name)
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
