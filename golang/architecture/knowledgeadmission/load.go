// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/statedir"
)

// ErrNoAdmission reports that no admission decision could be established.
//
// This is a TYPED absence, never an empty success. A caller that treats "no
// admission" as "nothing is required" turns every downstream proof vacuous:
// closure would report itself proven precisely because it had nothing to check.
// Absence must propagate as unprovable.
var ErrNoAdmission = errors.New("no verified knowledge admission")

const (
	BaselineFileName  = "knowledge-admission-baseline.yaml"
	SignatureFileName = "knowledge-admission-baseline.sig"
)

// TrustStorePath is where the PUBLIC trust store is provisioned.
//
// Deliberately outside the repository. A repo-owned trust store is
// self-defeating: a repository writer could swap in their own public key, sign
// their own baseline, and be discovered as trusted. Distribution is not
// authority — the installed distribution, user-global config or CI provisions
// this, and only the public half ever lands here.
func TrustStorePath(repoRoot string) string {
	return statedir.Path(repoRoot, "governance", "trusted-publishers.json")
}

// AuthorizedPublisherPath names the publisher authorized to admit knowledge.
//
// Also outside the repository, and for the same reason: being present in the
// trust store proves a signature is genuine, not that the signer may decide THIS
// operation. Reading the authorized publisher out of the signed manifest would
// let the manifest authorize itself.
func AuthorizedPublisherPath(repoRoot string) string {
	return statedir.Path(repoRoot, "governance", "authorized-knowledge-publisher")
}

// LoadOptions supplies what verification cannot read from the repository.
type LoadOptions struct {
	RepoRoot    string
	EvaluatedAt time.Time
	Index       authority.PolicyIndex

	// Deprecated: v1 callers supplied the published graph digest here. v2 ignores
	// it intentionally. Admission derives its own canonical authored-corpus digest
	// from RepoRoot and the governed identities inside the signed manifest.
	GraphDigest string

	// Overrides for callers that provision trust out of band (tests, CI).
	TrustStore          *governancepack.TrustStore
	ExpectedPublisherID string
}

// LoadFromRepo establishes the admission decision for a checkout, or fails.
//
// Returns ErrNoAdmission (wrapped) when any part of the trust chain is absent:
// no baseline, no signature, no provisioned trust store, no authorized
// publisher. Callers must surface that as unprovable rather than as an empty
// required set.
func LoadFromRepo(opts LoadOptions) (Admitted, Provenance, error) {
	root := strings.TrimSpace(opts.RepoRoot)
	if root == "" {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: no repository root", ErrNoAdmission)
	}
	bundle := BundleRoot(root)

	manifestBytes, err := os.ReadFile(filepath.Join(bundle, BaselineFileName))
	if err != nil {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: baseline: %v", ErrNoAdmission, err)
	}
	rawSig, err := os.ReadFile(filepath.Join(bundle, SignatureFileName))
	if err != nil {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: signature: %v", ErrNoAdmission, err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(rawSig)))
	if err != nil {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: decode signature: %v", ErrNoAdmission, err)
	}

	// Derive freshness from the authored source, never from awareness.nt or a
	// live graph marker. Freezing calls this SAME function, so there is one corpus
	// definition rather than a freezer interpretation and a verifier interpretation.
	// The manifest determines WHICH stable identities claim governing authority;
	// the checkout determines WHAT those identities say. The outer signature is
	// still what authenticates that claim — this precomputation grants no authority.
	corpusDigest, err := AdmissionCorpusDigestForManifest(root, manifestBytes)
	if err != nil {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: corpus digest: %v", ErrNoAdmission, err)
	}

	store := governancepack.TrustStore{}
	if opts.TrustStore != nil {
		store = *opts.TrustStore
	} else {
		raw, err := os.ReadFile(TrustStorePath(root))
		if err != nil {
			return Admitted{}, Provenance{}, fmt.Errorf("%w: trust store: %v", ErrNoAdmission, err)
		}
		if err := json.Unmarshal(raw, &store); err != nil {
			return Admitted{}, Provenance{}, fmt.Errorf("%w: trust store: %v", ErrNoAdmission, err)
		}
	}

	publisher := strings.TrimSpace(opts.ExpectedPublisherID)
	if publisher == "" {
		raw, err := os.ReadFile(AuthorizedPublisherPath(root))
		if err != nil {
			return Admitted{}, Provenance{}, fmt.Errorf("%w: authorized publisher: %v", ErrNoAdmission, err)
		}
		publisher = strings.TrimSpace(string(raw))
	}
	if publisher == "" {
		return Admitted{}, Provenance{}, fmt.Errorf("%w: authorized publisher is empty", ErrNoAdmission)
	}

	// The key id comes from the manifest envelope; the trust store decides
	// whether that key is trusted, and ExpectedPublisherID decides whether the
	// publisher may admit knowledge.
	var envelope struct {
		Signature struct {
			KeyID     string `yaml:"key_id"`
			Algorithm string `yaml:"algorithm"`
		} `yaml:"signature"`
	}
	_ = yamlUnmarshal(manifestBytes, &envelope)

	sm := SignedManifest{
		ManifestBytes: manifestBytes,
		Signature:     sig,
		PublisherID:   publisher,
		KeyID:         envelope.Signature.KeyID,
		Algorithm:     envelope.Signature.Algorithm,
	}
	if strings.TrimSpace(sm.KeyID) == "" {
		sm.KeyID = firstKeyID(store, publisher)
	}

	return VerifySigned(sm, store, Context{
		CorpusDigest:        corpusDigest,
		ExpectedPublisherID: publisher,
		EvaluatedAt:         opts.EvaluatedAt,
		Index:               opts.Index,
		Resolver:            authority.NewLocalBundleResolver(bundle),
	})
}

func governedIdentityClaims(m Manifest) []string {
	out := make([]string, 0, len(m.Records))
	for _, r := range m.Records {
		if Disposition(strings.ToLower(strings.TrimSpace(string(r.Disposition)))) != DispositionGoverned {
			continue
		}
		if id := strings.TrimSpace(r.Identity); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func firstKeyID(store governancepack.TrustStore, publisherID string) string {
	for _, p := range store.Publishers {
		if strings.TrimSpace(p.PublisherID) != publisherID {
			continue
		}
		for _, k := range p.Keys {
			return strings.TrimSpace(k.KeyID)
		}
	}
	return ""
}
