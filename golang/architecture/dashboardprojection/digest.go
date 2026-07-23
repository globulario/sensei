// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Digest returns the canonical content digest of a projection: sha256 hex
// over CanonicalJSON of the projection with GeneratedAt cleared first, since
// generated_at is metadata, not authority (architecture-dashboard-v1.md §10)
// — two builds of identical architectural content must digest identically
// regardless of wall-clock time. This reuses the repo's existing
// canonicalization/digest convention (closureprotocol.SemanticDigest) rather
// than introducing a second one.
func Digest(p Projection) (string, error) {
	p.Identity.GeneratedAt = ""
	return closureprotocol.SemanticDigest(p)
}

// SchemaDigest returns the raw sha256 hex digest of a schema file's exact
// bytes (this repo's plain crypto/sha256 + hex convention — the same value
// `sha256sum <file>` produces), so it can be published and independently
// re-verified byte-for-byte, not just structurally.
func SchemaDigest(schemaBytes []byte) string {
	sum := sha256.Sum256(schemaBytes)
	return hex.EncodeToString(sum[:])
}
