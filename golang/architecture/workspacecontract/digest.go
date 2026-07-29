// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// IdentityDigest returns the canonical content digest of an identity
// receipt: sha256 hex over closureprotocol.SemanticDigest of the
// normalized value. Reuses the repository's existing canonicalization/
// digest convention rather than introducing a second one.
func IdentityDigest(id Identity) (string, error) {
	return closureprotocol.SemanticDigest(NormalizeIdentity(id))
}

// AdmissionDigest returns the canonical content digest of an admission
// record.
func AdmissionDigest(a Admission) (string, error) {
	return closureprotocol.SemanticDigest(NormalizeAdmission(a))
}

// SchemaDigest returns the raw sha256 hex digest of a schema file's exact
// bytes (this repo's plain crypto/sha256 + hex convention — the same value
// `sha256sum <file>` produces), so it can be published and independently
// re-verified byte-for-byte.
func SchemaDigest(schemaBytes []byte) string {
	sum := sha256.Sum256(schemaBytes)
	return hex.EncodeToString(sum[:])
}
