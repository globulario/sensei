// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// ValidateCandidatePath enforces hard law 9's canonical path rules: POSIX-
// relative, "/"-separated, no leading "/", no "." or ".." segment, no
// embedded NUL or newline byte. A path failing any of these is rejected
// before it can appear in any manifest, so a traversal sequence can never
// address outside the candidate tree.
func ValidateCandidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("candidate path must not be empty")
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("candidate path %q must not have a leading '/'", path)
	}
	if strings.ContainsAny(path, "\x00\n") {
		return fmt.Errorf("candidate path %q must not contain a NUL or newline byte", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			return fmt.Errorf("candidate path %q must not contain an empty segment", path)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("candidate path %q must not contain a '.' or '..' segment", path)
		}
	}
	return nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalizeManifest validates every entry's path and mode/content
// consistency, rejects duplicate paths, and returns a NEW slice sorted by
// Path -- it never mutates the input slice. Every entry's declared
// ContentDigestSHA256 is recomputed and verified from Content/SymlinkTarget;
// a manifest carrying a stale or hand-crafted digest is rejected here,
// mirroring the "declared must equal recomputed" law applied everywhere
// else in this codebase. The unused Content/SymlinkTarget field is
// normalized to its empty (never nil) value.
func CanonicalizeManifest(entries []CandidateManifestEntry) ([]CandidateManifestEntry, error) {
	out := make([]CandidateManifestEntry, len(entries))
	seen := make(map[string]bool, len(entries))
	for i, e := range entries {
		if err := ValidateCandidatePath(e.Path); err != nil {
			return nil, err
		}
		if seen[e.Path] {
			return nil, fmt.Errorf("duplicate manifest path %q", e.Path)
		}
		seen[e.Path] = true

		switch e.Mode {
		case ModeRegular, ModeExecutable:
			if e.SymlinkTarget != "" {
				return nil, fmt.Errorf("path %q: symlink_target must be empty for mode %q", e.Path, e.Mode)
			}
			want := sha256Hex(e.Content)
			if e.ContentDigestSHA256 != want {
				return nil, fmt.Errorf("path %q: content_digest_sha256 %q does not match recomputed %q", e.Path, e.ContentDigestSHA256, want)
			}
			if e.Content == nil {
				e.Content = []byte{}
			}
		case ModeSymlink:
			if len(e.Content) != 0 {
				return nil, fmt.Errorf("path %q: content must be empty for mode symlink", e.Path)
			}
			want := sha256Hex([]byte(e.SymlinkTarget))
			if e.ContentDigestSHA256 != want {
				return nil, fmt.Errorf("path %q: content_digest_sha256 %q does not match recomputed %q", e.Path, e.ContentDigestSHA256, want)
			}
			e.Content = []byte{}
		default:
			return nil, fmt.Errorf("path %q: unknown mode %q", e.Path, e.Mode)
		}
		out[i] = e
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func modeByte(m CandidateFileMode) byte {
	switch m {
	case ModeExecutable:
		return 1
	case ModeSymlink:
		return 2
	default:
		return 0
	}
}

// writeLengthPrefixed writes an 8-byte big-endian length prefix followed by
// b's bytes. Length-prefixing every variable-length field is what makes
// ManifestDigest's encoding collision-safe: without it, entries such as
// path "ab" + digest "c..." and path "a" + digest "bc..." would serialize to
// identical byte streams. A fixed-width length prefix before every
// variable-length field removes that ambiguity entirely.
func writeLengthPrefixed(w io.Writer, b []byte) {
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(b)))
	w.Write(lenBuf[:])
	w.Write(b)
}

func writeManifestEntry(w io.Writer, e CandidateManifestEntry) {
	w.Write([]byte{modeByte(e.Mode)})
	writeLengthPrefixed(w, []byte(e.Path))
	writeLengthPrefixed(w, []byte(e.ContentDigestSHA256))
}

// ManifestDigest returns the canonical content digest of a candidate tree's
// manifest: CanonicalizeManifest validates and sorts entries, then each
// entry is serialized as
//
//	mode_byte || len(path) || path || len(content_digest) || content_digest
//
// (every variable-length field length-prefixed, per writeLengthPrefixed) in
// sorted-path order, and the concatenation is sha256'd. This is the one
// scheme both InputCandidateDigestSHA256 (over the pinned snapshot's
// manifest) and FinalCandidateContentDigestSHA256 (over the final
// candidate's manifest) are computed with -- see hard law 10.
func ManifestDigest(entries []CandidateManifestEntry) (string, error) {
	canonical, err := CanonicalizeManifest(entries)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, e := range canonical {
		writeManifestEntry(h, e)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
