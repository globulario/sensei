// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	"github.com/globulario/sensei/golang/governancepack"
	"gopkg.in/yaml.v3"
)

// SignedManifest is an admission manifest inside an authenticated envelope.
//
// ManifestBytes are the exact bytes the signature covers. The manifest is parsed
// from those bytes and from nothing else, so every field the admission decision
// rests on — the actor binding, the admitting role, the dispositions and the
// admitted identities themselves — is covered by the signature. Accepting a
// pre-parsed Manifest alongside a signature would let a caller sign one document
// and act on another.
type SignedManifest struct {
	ManifestBytes []byte `yaml:"-" json:"-"`
	Signature     []byte `yaml:"-" json:"-"`
	PublisherID   string `yaml:"publisher_id" json:"publisher_id"`
	KeyID         string `yaml:"key_id" json:"key_id"`
	Algorithm     string `yaml:"algorithm" json:"algorithm"`
}

// Provenance reports what was established about an admission manifest's origin.
//
// It reuses changebinding's three-valued semantics deliberately: the zero value
// is invalid, "structurally fine but unproven" is a distinct state from "proven",
// and only positive verification is authoritative. Collapsing unverifiable into
// verified is how a system ends up trusting an issuer string.
type Provenance struct {
	Verification changebinding.ProvenanceVerification
	PublisherID  string
	KeyID        string
	TrustWarning string
}

// VerifySigned is the full admission chain, and the only entry point that
// establishes governed provenance.
//
//	out-of-band trusted-publishers.json
//	  -> publisher is the AUTHORIZED knowledge-admission publisher [scope]
//	  -> publisher/key lookup + key-state policy (revoked, future, expired)
//	  -> Ed25519 over the exact manifest bytes        [issuer authenticity]
//	  -> manifest parsed from those same bytes
//	  -> receipt digests bind the referenced authn/role evidence [integrity]
//	  -> VerifyActorBinding + admitting role
//	  -> revision and graph-digest binding                       [freshness]
//	  -> IsAuthoritativelyAdmitted(identity)
//
// The layers reinforce each other. The signature stops a caller inventing the
// issuer, the actor binding, the dispositions or the admitted identities and
// recomputing every digest to match. The receipt digests stop a referenced
// receipt being swapped while its digest is retained. Neither alone is enough:
// a signature over a document that names its own trust anchor proves nothing,
// and digests under an unauthenticated document only prove it is internally
// consistent.
//
// Fails closed. Any error means nothing is admitted.
func VerifySigned(sm SignedManifest, store governancepack.TrustStore, ctx Context) (Admitted, Provenance, error) {
	prov := Provenance{
		Verification: changebinding.ProvenanceInvalid,
		PublisherID:  strings.TrimSpace(sm.PublisherID),
		KeyID:        strings.TrimSpace(sm.KeyID),
	}

	// Authorization scope, before any signature work. The publisher that signed
	// must be the publisher trusted configuration authorizes for knowledge
	// admission — not merely some publisher the trust store carries.
	expected := strings.TrimSpace(ctx.ExpectedPublisherID)
	if expected == "" {
		return Admitted{}, prov, fmt.Errorf("admission context names no expected publisher")
	}
	if !strings.EqualFold(expected, strings.TrimSpace(sm.PublisherID)) {
		return Admitted{}, prov, fmt.Errorf(
			"admission manifest was signed by publisher %q, which is not the authorized knowledge-admission publisher %q",
			strings.TrimSpace(sm.PublisherID), expected)
	}

	algorithm := strings.TrimSpace(sm.Algorithm)
	if algorithm == "" {
		algorithm = "ed25519"
	}
	key, warning, err := governancepack.VerifyPublisherSignature(
		sm.ManifestBytes, sm.Signature, sm.PublisherID, sm.KeyID, algorithm, store, ctx.EvaluatedAt,
	)
	if err != nil {
		// Structurally valid but unproven is not the same as malformed, and
		// neither is authoritative.
		prov.Verification = changebinding.ProvenanceUnverifiable
		return Admitted{}, prov, fmt.Errorf("admission manifest provenance: %w", err)
	}
	prov.Verification = changebinding.ProvenanceVerified
	prov.KeyID = strings.TrimSpace(key.KeyID)
	prov.TrustWarning = warning

	// Parsed from the signed bytes, never from a caller-supplied struct.
	var m Manifest
	if err := yaml.Unmarshal(sm.ManifestBytes, &m); err != nil {
		return Admitted{}, prov, fmt.Errorf("admission manifest: %w", err)
	}

	admitted, err := verify(m, ctx)
	if err != nil {
		return Admitted{}, prov, err
	}
	return admitted, prov, nil
}
