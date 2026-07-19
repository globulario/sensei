// SPDX-License-Identifier: Apache-2.0

package plane

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

func PropositionKey(claim architecture.Claim) string {
	repo := claim.Scope.Repository
	if repo == "" {
		repo = claim.Scope.Repo
	}
	parts := []string{
		strings.TrimSpace(repo),
		strings.TrimSpace(claim.Scope.Domain),
		strings.TrimSpace(claim.Statement.Subject),
		strings.TrimSpace(claim.Statement.Predicate),
		strings.TrimSpace(claim.Statement.Object),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	return "proposition." + hex.EncodeToString(sum[:])[:16]
}
