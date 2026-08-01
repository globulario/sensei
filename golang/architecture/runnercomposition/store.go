// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
)

// ErrCandidateArtifactNotFound is returned (wrapped) by
// CandidateArtifactStore.Get when nothing is sealed under the requested
// digest.
var ErrCandidateArtifactNotFound = errors.New("candidate artifact not found")

// ErrCandidateArtifactCorrupted is returned (wrapped) by
// CandidateArtifactStore.Get when something IS sealed under the requested
// digest, but it fails ValidateCandidateArtifact, or its own
// CandidateArtifactDigestSHA256 does not match the digest it was retrieved
// by -- a distinct failure mode from "not found," since the store answered
// with content it must refuse to hand back as if it were trustworthy.
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
	// The write is transactional: on any error, nothing new is left
	// readable under artifact's digest -- a caller that sees a Put error
	// can safely conclude the artifact was NOT sealed, never that it was
	// partially or ambiguously sealed.
	//
	// Put is idempotent for identical content: re-sealing the same
	// artifact under the same digest a second time succeeds without
	// re-writing. An attempt to seal DIFFERENT content under a digest that
	// is already sealed is rejected -- a sealed entry is immutable, never
	// silently overwritten, since two different Put calls agreeing on a
	// digest but disagreeing on content is either a caller bug or (at the
	// digest's collision-resistance limit) something this store must
	// refuse to paper over.
	Put(ctx context.Context, artifact CandidateArtifact) error

	// Get retrieves the CandidateArtifact previously sealed under
	// digestSHA256. It re-validates the artifact (ValidateCandidateArtifact)
	// before returning it, and additionally confirms the artifact's own
	// CandidateArtifactDigestSHA256 equals digestSHA256, before returning
	// it -- so an entry corrupted after being sealed (truncated, bit-rotted,
	// or otherwise no longer matching the key it is stored under) is
	// refused rather than silently handed back as if it were trustworthy.
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
}

// NewFSCandidateArtifactStore constructs a CandidateArtifactStore backed by
// root, which must already exist as an absolute, real (non-symlink)
// directory -- the same requirement this package's other filesystem
// constructors (newFSCandidateWorkspace, ExtractSnapshot's destination)
// apply, and for the same reason: this function does not create or own
// root's lifecycle, only what is written inside it.
func NewFSCandidateArtifactStore(root string) (CandidateArtifactStore, error) {
	if err := validateAbsoluteRealDirectory("root", root); err != nil {
		return nil, fmt.Errorf("NewFSCandidateArtifactStore: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("NewFSCandidateArtifactStore: open root: %w", err)
	}
	return &fsCandidateArtifactStore{root: r}, nil
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
	existing, err := s.root.ReadFile(finalName)
	switch {
	case err == nil:
		if string(existing) == string(marshaled) {
			return nil // already sealed, identical content -- idempotent no-op.
		}
		return fmt.Errorf("CandidateArtifactStore.Put: an artifact is already sealed under digest %q with different content -- a sealed entry is immutable and is never overwritten", digest)
	case os.IsNotExist(err):
		// Nothing sealed yet -- proceed to write.
	default:
		return fmt.Errorf("CandidateArtifactStore.Put: check existing entry: %w", err)
	}

	if err := s.root.MkdirAll(".tmp", 0o755); err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: create staging directory: %w", err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Errorf("CandidateArtifactStore.Put: generate staging name: %w", err)
	}
	tmpName := ".tmp/" + digest + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	if err := s.root.WriteFile(tmpName, marshaled, 0o644); err != nil {
		s.root.Remove(tmpName)
		return fmt.Errorf("CandidateArtifactStore.Put: write staged content: %w", err)
	}
	if err := s.root.Rename(tmpName, finalName); err != nil {
		s.root.Remove(tmpName)
		return fmt.Errorf("CandidateArtifactStore.Put: commit (rename) failed: %w", err)
	}
	return nil
}

func (s *fsCandidateArtifactStore) Get(ctx context.Context, digestSHA256 string) (CandidateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return CandidateArtifact{}, err
	}
	if !sealedDigestPattern.MatchString(digestSHA256) {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: digestSHA256 %q is not a well-formed sha256 hex digest", digestSHA256)
	}

	data, err := s.root.ReadFile(digestSHA256 + ".json")
	if err != nil {
		if os.IsNotExist(err) {
			return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %w", digestSHA256, ErrCandidateArtifactNotFound)
		}
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: read: %w", err)
	}

	var artifact CandidateArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: unmarshal: %v: %w", digestSHA256, err, ErrCandidateArtifactCorrupted)
	}
	if err := ValidateCandidateArtifact(artifact); err != nil {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: %s: %v: %w", digestSHA256, err, ErrCandidateArtifactCorrupted)
	}
	if artifact.CandidateArtifactDigestSHA256 != digestSHA256 {
		return CandidateArtifact{}, fmt.Errorf("CandidateArtifactStore.Get: entry stored under %q actually carries digest %q: %w", digestSHA256, artifact.CandidateArtifactDigestSHA256, ErrCandidateArtifactCorrupted)
	}
	return artifact, nil
}
