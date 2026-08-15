// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	"github.com/globulario/sensei/golang/governancepack"
	"gopkg.in/yaml.v3"
)

const (
	testPublisher = "publisher.sensei.governance"
	testKeyID     = "key.governance.1"
)

type signer struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newSigner(t *testing.T) signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return signer{pub: pub, priv: priv}
}

// trustStore publishes the signer's public key. The real one lives outside the
// repository (.sensei/governance/trusted-publishers.json, gitignored), which is
// what makes repository write access insufficient to mint authority.
func (s signer) trustStore(keyID, status string) governancepack.TrustStore {
	return governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{{
			PublisherID: testPublisher,
			Keys: []governancepack.TrustedKey{{
				KeyID:           keyID,
				Algorithm:       "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(s.pub),
				Status:          status,
			}},
		}},
	}
}

func (s signer) sign(t *testing.T, m Manifest) SignedManifest {
	t.Helper()
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return SignedManifest{
		ManifestBytes: raw,
		Signature:     ed25519.Sign(s.priv, raw),
		PublisherID:   testPublisher,
		KeyID:         testKeyID,
		Algorithm:     "ed25519",
	}
}

// The happy path: a manifest signed by a trusted governance key admits
// knowledge, and provenance is positively verified rather than assumed.
func TestSignedManifestFromTrustedPublisherAdmitsKnowledge(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	sm := s.sign(t, manifest(t, b, governedRecord("invariant.real.one")))

	admitted, prov, err := VerifySigned(sm, s.trustStore(testKeyID, "active"), testContext(b))
	if err != nil {
		t.Fatalf("verify signed: %v", err)
	}
	if prov.Verification != changebinding.ProvenanceVerified {
		t.Fatalf("provenance = %v, want ProvenanceVerified", prov.Verification)
	}
	if !admitted.IsAuthoritativelyAdmitted("invariant.real.one") {
		t.Fatal("signed governed knowledge was not authoritative")
	}
}

// Seam 1 — CONTENT INTEGRITY. Mutating any admission record after signing must
// be rejected. This is what stops a caller taking a legitimately signed manifest
// and promoting one extra identity inside it.
func TestMutatingARecordAfterSigningIsRejected(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	sm := s.sign(t, manifest(t, b,
		governedRecord("invariant.real.one"),
		Record{Identity: "candidate.invariant.authority.abort", Disposition: DispositionCandidate},
	))

	// Promote the candidate in the signed bytes, leaving the signature alone.
	sm.ManifestBytes = []byte(strings.Replace(string(sm.ManifestBytes),
		"disposition: candidate", "disposition: governed", 1))

	_, prov, err := VerifySigned(sm, s.trustStore(testKeyID, "active"), testContext(b))
	if err == nil {
		t.Fatal("a record mutated after signing was accepted")
	}
	if prov.Verification == changebinding.ProvenanceVerified {
		t.Fatal("mutated manifest reported ProvenanceVerified")
	}
}

// Seam 2 — ISSUER AUTHENTICITY. A perfectly well-formed manifest signed by a key
// the trust store does not carry must be rejected. Holding a key is not the same
// as holding a TRUSTED key.
func TestSignatureFromUntrustedKeyIsRejected(t *testing.T) {
	b := newBundle(t)
	trusted := newSigner(t)
	attacker := newSigner(t)

	sm := attacker.sign(t, manifest(t, b, governedRecord("invariant.real.one")))

	// Trust store carries the real publisher's key, not the attacker's.
	_, prov, err := VerifySigned(sm, trusted.trustStore(testKeyID, "active"), testContext(b))
	if err == nil {
		t.Fatal("a signature from an untrusted key admitted knowledge")
	}
	if prov.Verification == changebinding.ProvenanceVerified {
		t.Fatal("untrusted key reported ProvenanceVerified")
	}
}

// Seam 3 — GOVERNANCE REVOCATION. A cryptographically valid signature from a key
// that governance has revoked must be rejected. Revocation is a policy decision
// the signature itself cannot express.
func TestSignatureFromRevokedKeyIsRejected(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	sm := s.sign(t, manifest(t, b, governedRecord("invariant.real.one")))

	_, _, err := VerifySigned(sm, s.trustStore(testKeyID, "revoked"), testContext(b))
	if err == nil {
		t.Fatal("a revoked key admitted knowledge")
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("err = %v, want a revocation failure", err)
	}
}

// An unknown key id under a trusted publisher must not resolve to some other
// key that publisher happens to hold.
func TestUnknownKeyIDIsRejected(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	sm := s.sign(t, manifest(t, b, governedRecord("invariant.real.one")))

	if _, _, err := VerifySigned(sm, s.trustStore("key.governance.other", "active"), testContext(b)); err == nil {
		t.Fatal("an unknown key id admitted knowledge")
	}
}

// An unsigned manifest is not "provenance we could not check" — it is no
// provenance. Nothing is admitted.
func TestUnsignedManifestAdmitsNothing(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	sm := s.sign(t, manifest(t, b, governedRecord("invariant.real.one")))
	sm.Signature = nil

	admitted, prov, err := VerifySigned(sm, s.trustStore(testKeyID, "active"), testContext(b))
	if err == nil {
		t.Fatal("an unsigned manifest was accepted")
	}
	if prov.Verification == changebinding.ProvenanceVerified {
		t.Fatal("unsigned manifest reported ProvenanceVerified")
	}
	if len(admitted.GovernedIdentities()) != 0 {
		t.Fatal("unsigned manifest admitted identities")
	}
}

// A trust store is general-purpose: it can carry several publishers, all of them
// legitimately trusted for their own operations. Being trusted is not the same as
// being authorized to issue knowledge-admission decisions.
//
// Two active publishers, same store. The manifest is signed by the wrong one.
func TestTrustedButUnauthorizedPublisherCannotAdmitKnowledge(t *testing.T) {
	b := newBundle(t)
	governance := newSigner(t)
	vendor := newSigner(t)

	// One store, both publishers trusted and active.
	store := governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{
			{PublisherID: testPublisher, Keys: []governancepack.TrustedKey{{
				KeyID: testKeyID, Algorithm: "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(governance.pub), Status: "active",
			}}},
			{PublisherID: "publisher.some.vendor", Keys: []governancepack.TrustedKey{{
				KeyID: testKeyID, Algorithm: "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(vendor.pub), Status: "active",
			}}},
		},
	}

	sm := vendor.sign(t, manifest(t, b, governedRecord("invariant.real.one")))
	sm.PublisherID = "publisher.some.vendor" // truthful — the vendor really did sign it

	admitted, _, err := VerifySigned(sm, store, testContext(b))
	if err == nil && admitted.IsAuthoritativelyAdmitted("invariant.real.one") {
		t.Fatal("a trusted but unauthorized publisher admitted knowledge: " +
			"the trust store proves the signature, not authorization for THIS operation")
	}
}

// The admission artifacts must live OUTSIDE the graph corpus they bind.
//
// Git-revision binding was dropped (3b944865) because a committed manifest
// naming its own HEAD is stale the instant it lands. Graph-digest binding avoids
// that only while the manifest is not itself projected into the graph — if a
// later importer expansion started reading governance/, writing the manifest
// would move the digest it names and the cycle would return through a different
// door. Pin it.
func TestAdmissionArtifactsDoNotAlterTheGovernedGraphDigest(t *testing.T) {
	root := repoRoot(t)
	corpus := filepath.Join(root, "docs", "awareness")

	before := compileCorpusDigest(t, root, corpus)

	// Perturb the admission artifacts exactly as signing them would.
	manifestPath := filepath.Join(root, "governance", "knowledge-admission-baseline.yaml")
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Skipf("baseline not present: %v", err)
	}
	t.Cleanup(func() { os.WriteFile(manifestPath, original, 0o644) })
	if err := os.WriteFile(manifestPath, append(original, []byte("\n# signature added\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	sigPath := filepath.Join(root, "governance", "knowledge-admission-baseline.sig")
	if err := os.WriteFile(sigPath, []byte("bm90LWEtcmVhbC1zaWduYXR1cmU=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(sigPath) })

	if after := compileCorpusDigest(t, root, corpus); after != before {
		t.Fatalf("admission artifacts moved the governed graph digest:\n before %s\n after  %s\n"+
			"the revision-binding cycle has returned through the corpus walk", before, after)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// compileCorpusDigest digests the projected triples, which is what the graph
// digest is: seedmeta hashes canonicalized N-Triples, not source bytes.
func compileCorpusDigest(t *testing.T, root, corpus string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "./cmd/yaml2nt", "-input", corpus)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("yaml2nt unavailable: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("yaml2nt produced no triples — an empty compile digests as the " +
			"sha256 of the empty string and would prove nothing")
	}
	sum := sha256.Sum256(out)
	return hex.EncodeToString(sum[:])
}

// One governance authority, one name. An authorized publisher must not
// authenticate the envelope as itself while attributing the decision to an
// unrelated issuer.
func TestManifestAttributingToADifferentIssuerIsRejected(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)

	m := manifest(t, b, governedRecord("invariant.real.one"))
	m.ActorBinding.Issuer = "publisher.someone.else"
	sm := s.sign(t, m)

	if _, _, err := VerifySigned(sm, s.trustStore(testKeyID, "active"), testContext(b)); err == nil {
		t.Fatal("a manifest attributing the decision to an unrelated issuer was accepted")
	}
}
