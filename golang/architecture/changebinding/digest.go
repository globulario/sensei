// SPDX-License-Identifier: AGPL-3.0-only

package changebinding

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// CanonicalBytes is the ONE deterministic canonical representation used exclusively for
// digest computation: canonical JSON (recursively key-sorted, formatting/order/whitespace
// independent, via closureprotocol.CanonicalJSON) over the typed binding with the
// self-excluding DigestSHA256 field cleared. It is computed over the typed VALUE, never
// over raw YAML/JSON input, so source formatting, comments, indentation, and map
// iteration order cannot affect it, and it is byte-identical across platforms.
//
// It includes every identity- and authority-bearing field (schema/version, repository,
// change, base, head, task, completion-result digest, issuer, publication, provenance)
// and excludes ONLY the binding-digest field itself. Identity-bearing strings are never
// trimmed, case-folded, shortened, path-cleaned, or otherwise transformed here — a
// difference in any of them yields different canonical bytes.
func CanonicalBytes(b ChangeTaskBinding) ([]byte, error) {
	b.DigestSHA256 = ""
	return closureprotocol.CanonicalJSON(b)
}

// BindingDigest is the SHA-256 of CanonicalBytes — the self-excluding binding digest.
func BindingDigest(b ChangeTaskBinding) (string, error) {
	b.DigestSHA256 = ""
	return closureprotocol.SemanticDigest(b)
}
