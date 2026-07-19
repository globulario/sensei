// SPDX-License-Identifier: Apache-2.0

package changebinding

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseBinding strictly parses ONE completion.change_task_binding/v1 publication (YAML or
// JSON). On failure it returns the TYPED failure class so a caller distinguishes a
// malformed/absent-field/wrong-type publication from an unsupported version. On success it
// returns the binding and an empty validity.
//
// It fails loudly on: unknown fields, duplicate fields, missing required fields, wrong
// scalar/collection types, unsupported schema identifiers/versions, empty identity values,
// malformed commit/repository/task/publication/provenance/digest values, trailing
// unconsumed content, and multiple publications where one is required. It never
// reconstructs a missing field from another field or from ambient state.
func ParseBinding(data []byte) (ChangeTaskBinding, BindingValidity) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown fields anywhere in the tree
	var b ChangeTaskBinding
	if err := dec.Decode(&b); err != nil {
		return ChangeTaskBinding{}, BindingMalformed
	}
	// Trailing content / a second document is more-than-one-publication → malformed.
	var extra ChangeTaskBinding
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return ChangeTaskBinding{}, BindingMalformed
	}

	// Schema identity: an empty schema is a missing required field (malformed); a present
	// but non-canonical schema/version is an unsupported version.
	switch b.SchemaVersion {
	case "":
		return ChangeTaskBinding{}, BindingMalformed
	case SchemaVersion:
		// supported
	default:
		return b, BindingUnsupportedVersion
	}

	if v := validateShape(b); v != "" {
		return b, v
	}
	return b, ""
}

// validateShape enforces the canonical lexical contract on a parsed v1 binding: every
// required identity value present, non-empty, whitespace-free, and in canonical form.
// It REJECTS noncanonical input (e.g. shortened/upper-case SHAs, padded strings) rather
// than normalizing it. Any violation is BindingMalformed; a structurally-valid but
// unverifiable/mismatched binding is decided later by the validator, not here.
func validateShape(b ChangeTaskBinding) BindingValidity {
	required := []string{
		b.Repository.Provider, b.Repository.Identity,
		b.Change.Provider, b.Change.ID,
		b.Task.Directory, b.Task.ID, b.Task.SessionID,
		b.CompletionResultDigestSHA256,
		b.Issuer,
		b.Publication.ID,
		b.Provenance.EventSource, b.Provenance.Tool, b.Provenance.ToolVersion,
		b.DigestSHA256,
	}
	for _, s := range required {
		if !isCanonicalToken(s) {
			return BindingMalformed
		}
	}
	// Commit identities and digests must be full lower-case hex.
	if !isFullHex(b.Change.HeadSHA) || !isFullHex(b.Change.BaseSHA) {
		return BindingMalformed
	}
	if !isSHA256Hex(b.CompletionResultDigestSHA256) || !isSHA256Hex(b.DigestSHA256) {
		return BindingMalformed
	}
	return ""
}

// isCanonicalToken: non-empty and carrying no leading/trailing/embedded ASCII whitespace
// (identity tokens are exact; whitespace is never trimmed away).
func isCanonicalToken(s string) bool {
	if s == "" {
		return false
	}
	return s == strings.TrimSpace(s) && !strings.ContainsAny(s, " \t\r\n")
}

// isFullHex accepts a full-length lower-case hex commit id (40 for SHA-1, 64 for SHA-256).
// Shortened, upper-case, or non-hex values are rejected (not normalized).
func isFullHex(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return allLowerHex(s)
}

func isSHA256Hex(s string) bool {
	return len(s) == 64 && allLowerHex(s)
}

func allLowerHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
