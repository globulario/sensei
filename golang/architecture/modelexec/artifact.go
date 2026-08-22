// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// ArtifactSchemaVersion is the closed contract a provider must answer in.
const ArtifactSchemaVersion = "sensei.model_artifact.v1"

// Item kinds a model may propose. All of them are DERIVED lanes that still
// require the existing governed review path. There is deliberately no kind that
// means "canonical fact".
const (
	ItemKindCandidateClaim = "candidate_claim"
	ItemKindQuestion       = "question"
	ItemKindChallenge      = "challenge"
	ItemKindLimitation     = "limitation"
)

func validItemKind(kind string) bool {
	switch kind {
	case ItemKindCandidateClaim, ItemKindQuestion, ItemKindChallenge, ItemKindLimitation:
		return true
	default:
		return false
	}
}

// ValidateArtifact decides whether an artifact may be ACCEPTED. It says nothing
// about whether the model's answer is true — only that Sensei can attribute and
// bound what came back.
//
// It returns a typed investigation.ModelReason* on failure so a caller never
// infers the cause by matching prose.
func ValidateArtifact(a Artifact, r Request) (reason string, ok bool) {
	if a.SchemaVersion != ArtifactSchemaVersion {
		return investigation.ModelReasonArtifactMalformed, false
	}
	if len(a.Items) == 0 {
		// An artifact with nothing in it cannot be an accepted result; an
		// empty answer is reported as such, not accepted as one.
		return investigation.ModelReasonArtifactUnhashable, false
	}
	if strings.TrimSpace(a.NondeterminismDeclaration) == "" {
		return investigation.ModelReasonArtifactMalformed, false
	}

	supplied := suppliedIDs(r)
	shown := suppliedFilePaths(r)
	for _, item := range a.Items {
		if !validItemKind(item.Kind) || strings.TrimSpace(item.Text) == "" {
			return investigation.ModelReasonArtifactMalformed, false
		}
		// A bid for authority is refused, not quietly dropped. Silently
		// stripping it would let a model keep asking with no record that it did.
		if item.ClaimsCanonical || item.ClaimsPromotion || item.ClaimsAdmission {
			return investigation.ModelReasonArtifactAuthority, false
		}
		// Grounding: every citation must be material the request actually
		// supplied. This is decidable precisely because the request is the only
		// thing the model was given.
		for _, cited := range item.CitedEvidenceIDs {
			if !supplied[cited] {
				return investigation.ModelReasonArtifactUngrounded, false
			}
		}
		// Scope: the item must stay inside the bound repository and, when it
		// names files, inside the material it was shown. Checking only the
		// repository domain would leave file attribution unbounded — a model
		// could pin a claim to any path in the repo, including one it never
		// saw, and still be accepted.
		if item.RepositoryDomain != "" && item.RepositoryDomain != r.RepositoryDomain {
			return investigation.ModelReasonArtifactOutOfScope, false
		}
		for _, p := range item.FilePaths {
			clean := filepath.ToSlash(strings.TrimSpace(p))
			// An absolute or escaping path is out of scope by construction,
			// before any set membership question.
			if clean == "" || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
				return investigation.ModelReasonArtifactOutOfScope, false
			}
			if !shown[clean] {
				return investigation.ModelReasonArtifactOutOfScope, false
			}
		}
		// A claim of the kind that carries weight must cite something. An
		// uncited candidate is an assertion, and this lane does not carry
		// assertions.
		if item.Kind == ItemKindCandidateClaim && len(item.CitedEvidenceIDs) == 0 {
			return investigation.ModelReasonArtifactUngrounded, false
		}
	}
	return "", true
}

// ArtifactDigest content-addresses the NORMALIZED artifact, so two providers
// returning the same answer in a different order produce the same identity.
func ArtifactDigest(a Artifact) (string, error) {
	norm := Artifact{
		SchemaVersion:             a.SchemaVersion,
		NondeterminismDeclaration: a.NondeterminismDeclaration,
	}
	norm.Items = append(norm.Items, a.Items...)
	for i := range norm.Items {
		norm.Items[i].CitedEvidenceIDs = sortedCopy(norm.Items[i].CitedEvidenceIDs)
		norm.Items[i].FilePaths = sortedCopy(norm.Items[i].FilePaths)
	}
	sort.SliceStable(norm.Items, func(i, j int) bool {
		if norm.Items[i].Kind != norm.Items[j].Kind {
			return norm.Items[i].Kind < norm.Items[j].Kind
		}
		return norm.Items[i].Text < norm.Items[j].Text
	})
	data, err := json.Marshal(norm)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
