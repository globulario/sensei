// SPDX-License-Identifier: AGPL-3.0-only

// Package gitobject states, in one place, what a Git object id is.
//
// It exists because the same predicate was written six times across this
// repository with three different meanings, and two of them were wrong in a way
// nothing could detect from inside their own package.
package gitobject

import "strings"

// Object id widths. Git's two object formats, and nothing else.
//
// SHA-1 is the default; SHA-256 is selected with `git init --object-format=sha256`
// and is documented, supported, and produces 64-character ids from the same
// `git rev-parse HEAD` every caller here uses.
const (
	sha1Hex   = 40
	sha256Hex = 64
)

// IsObjectID reports whether v is a canonical Git object id: full-width hex in
// one of Git's two object formats.
//
// WIDTH IS THE WHOLE POINT, and getting it wrong is asymmetric. Accepting a
// shorter string admits an ABBREVIATION, which names a commit only
// probabilistically and cannot be compared for equality with one written out in
// full. Accepting only 40 rejects every id a SHA-256 repository produces --
// which is not a hypothetical: `namesRepository` already accepted both widths
// while two other validators accepted 40 alone, so the same value was valid
// evidence in one function and malformed in the next.
//
// CASE-INSENSITIVE, deliberately. Upper-case hex names the same object exactly;
// it is a difference in how the id was written down, not in which commit it
// identifies. Refusing it would reject a valid identity on presentation
// grounds. Callers that need a canonical spelling normalize after validating --
// validation and normalization are different jobs and are kept apart here.
func IsObjectID(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) != sha1Hex && len(v) != sha256Hex {
		return false
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
