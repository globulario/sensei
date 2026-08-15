// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/knowledgeadmission"
	"github.com/globulario/sensei/golang/governancepack"
	"gopkg.in/yaml.v3"
)

const (
	itPublisher = "publisher.test.governance"
	itKeyID     = "key.test.1"
	itRole      = "role.knowledge_admitter"
	itPrincipal = "human.tester"
)

// admissionFixture builds a checkout with public provenance evidence and an
// ephemeral signer. The actual admission manifest is frozen and signed only
// after the test has written its authored corpus, so the fixture exercises the
// same source-corpus binding as production.
type admissionFixture struct {
	root        string
	store       governancepack.TrustStore
	priv        ed25519.PrivateKey
	admittedIDs []string
	binding     closureprotocol.ActorBinding
}

func newAdmissionFixture(t *testing.T, admittedIDs []string) *admissionFixture {
	t.Helper()
	root := t.TempDir()
	bundle := filepath.Join(root, "governance")
	arts := filepath.Join(bundle, "artifacts", "sha256")
	if err := os.MkdirAll(arts, 0o755); err != nil {
		t.Fatal(err)
	}
	writeYAML := func(name string, v any) {
		b, err := yaml.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(arts, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	art, _ := json.Marshal(map[string]string{"principal_id": itPrincipal, "issuer": itPublisher})
	sum := sha256.Sum256(art)
	artDigest := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(arts, artDigest+".bin"), art, 0o644); err != nil {
		t.Fatal(err)
	}
	authn := closureprotocol.AuthenticationReceipt{
		ReceiptID: "authn." + itPrincipal, PrincipalID: itPrincipal, Issuer: itPublisher,
		AuthenticationArtifact: closureprotocol.LedgerPayloadRef{
			Path:      filepath.ToSlash(filepath.Join("artifacts", "sha256", artDigest+".bin")),
			MediaType: "application/octet-stream", DigestSHA256: artDigest,
		},
		AuthenticatedAt: "2026-08-14T00:00:00Z", Status: closureprotocol.ReceiptValid,
	}
	ad, err := closureprotocol.AuthenticationReceiptDigest(authn)
	if err != nil {
		t.Fatal(err)
	}
	authn.ReceiptDigestSHA256 = ad
	writeYAML(ad+".yaml", authn)

	att := closureprotocol.RoleAttestationReceipt{
		ReceiptID: "role." + itPrincipal, PrincipalID: itPrincipal,
		ActorKind: closureprotocol.ActorHuman, Issuer: itPublisher,
		RoleIDs: []string{itRole}, AuthenticationReceiptDigestSHA256: ad,
		IssuedAt: "2026-08-14T00:00:00Z", Status: closureprotocol.ReceiptValid,
	}
	rd, err := closureprotocol.RoleAttestationReceiptDigest(att)
	if err != nil {
		t.Fatal(err)
	}
	att.ReceiptDigestSHA256 = rd
	writeYAML(rd+".yaml", att)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	binding := closureprotocol.ActorBinding{
		PrincipalID: itPrincipal, ActorKind: closureprotocol.ActorHuman,
		Roles: []string{itRole}, Issuer: itPublisher,
		AuthenticationReceiptDigestSHA256: ad,
		RoleAttestationReceiptDigests:     []string{rd},
	}
	return &admissionFixture{
		root:        root,
		priv:        priv,
		admittedIDs: append([]string(nil), admittedIDs...),
		binding:     binding,
		store: governancepack.TrustStore{
			SchemaVersion: governancepack.TrustStoreSchemaV1,
			Publishers: []governancepack.TrustedPublisher{{PublisherID: itPublisher,
				Keys: []governancepack.TrustedKey{{KeyID: itKeyID, Algorithm: "ed25519",
					PublicKeyBase64: base64.StdEncoding.EncodeToString(pub), Status: "active"}}}},
		},
	}
}

func (f *admissionFixture) admitted(t *testing.T) knowledgeadmission.Admitted {
	t.Helper()
	digest, err := knowledgeadmission.AdmissionCorpusDigest(f.root, f.admittedIDs)
	if err != nil {
		t.Fatalf("derive admission corpus digest: %v", err)
	}
	records := make([]knowledgeadmission.Record, 0, len(f.admittedIDs))
	for _, id := range f.admittedIDs {
		records = append(records, knowledgeadmission.Record{
			Identity: id, Disposition: knowledgeadmission.DispositionGoverned,
			Receipt: adoption.Receipt{ValidForCorpusDigest: digest},
		})
	}
	m := knowledgeadmission.Manifest{
		SchemaVersion: knowledgeadmission.SchemaVersion,
		PolicyID:      "knowledge.admission.test.v2",
		AdmittingRole: itRole,
		ActorBinding:  f.binding,
		Records:       records,
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(f.root, "governance")
	if err := os.WriteFile(filepath.Join(bundle, knowledgeadmission.BaselineFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, knowledgeadmission.SignatureFileName),
		[]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(f.priv, raw))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, err := knowledgeadmission.LoadFromRepo(knowledgeadmission.LoadOptions{
		RepoRoot: f.root, EvaluatedAt: time.Now().UTC(),
		TrustStore: &f.store, ExpectedPublisherID: itPublisher,
		Index: authority.PolicyIndex{ActorRoles: map[string]authority.ActorRole{
			itRole: {ID: itRole, Status: "active", TrustedIssuers: []string{itPublisher},
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorHuman}}}},
	})
	if err != nil {
		t.Fatalf("load admission: %v", err)
	}
	return a
}

// writeCorpus writes a governed-schema invariants file at an arbitrary path.
func (f *admissionFixture) writeCorpus(t *testing.T, rel string, ids ...string) string {
	t.Helper()
	path := filepath.Join(f.root, "docs", "awareness", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "invariants:\n"
	for _, id := range ids {
		body += "  - id: " + id + "\n    title: probe\n    severity: high\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(f.root, "docs", "awareness")
}

// #166 acceptance test 5, at the closure boundary. Byte-identical knowledge,
// three locations. candidates/ is organisational metadata only; moving the
// authored declaration there cannot change its authority disposition.
func TestClosureAuthorityIsIndependentOfPath(t *testing.T) {
	const admittedID = "invariant.admitted.one"
	const candidateID = "invariant.candidate.one"

	for _, rel := range []string{"invariants.yaml", "candidates/invariant/rule.yaml", "deeply/nested/rule.yaml"} {
		f := newAdmissionFixture(t, []string{admittedID})
		root := f.writeCorpus(t, rel, admittedID, candidateID)
		a := f.admitted(t)
		expected, excluded, err := expectedIdentities(root, a)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := expected[admittedID]; !ok {
			t.Fatalf("%s: admitted identity was not required", rel)
		}
		if _, ok := expected[candidateID]; ok {
			t.Fatalf("%s: unadmitted identity became required by its location", rel)
		}
		if !containsStr(excluded, candidateID) {
			t.Fatalf("%s: unadmitted identity was not reported as excluded", rel)
		}
	}
}

// #166 acceptance test 1 and 2 at the closure boundary: generating candidates
// must not create a closure obligation or invalidate the existing admission
// corpus binding when the governed authored declarations did not change.
func TestGeneratedCandidatesCreateNoClosureObligation(t *testing.T) {
	const admittedID = "invariant.admitted.one"
	f := newAdmissionFixture(t, []string{admittedID})
	root := f.writeCorpus(t, "invariants.yaml", admittedID)
	a := f.admitted(t)

	before, _, err := expectedIdentities(root, a)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly what `sensei import --refresh` produced: a governed schema key
	// under candidates/. Neither authority nor the corpus binding may widen.
	f.writeCorpus(t, "candidates/invariant_candidates.yaml",
		"candidate.invariant.authority.aa", "candidate.invariant.authority.abort")
	if _, _, err := knowledgeadmission.LoadFromRepo(knowledgeadmission.LoadOptions{
		RepoRoot: f.root, EvaluatedAt: time.Now().UTC(), TrustStore: &f.store,
		ExpectedPublisherID: itPublisher,
		Index: authority.PolicyIndex{ActorRoles: map[string]authority.ActorRole{
			itRole: {ID: itRole, Status: "active", TrustedIssuers: []string{itPublisher},
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorHuman}}}},
	}); err != nil {
		t.Fatalf("candidate generation invalidated admission: %v", err)
	}
	after, _, err := expectedIdentities(root, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("required set grew from %d to %d when only candidates were generated", len(before), len(after))
	}
}

func containsStr(hay []string, needle string) bool {
	for _, v := range hay {
		if v == needle {
			return true
		}
	}
	return false
}

// generated/ is explicitly outside the authored admission corpus. A generated
// copy must therefore neither perturb the corpus digest nor confer authority.
func TestGeneratedOutputDoesNotPerturbAdmissionOrAuthority(t *testing.T) {
	const admittedID = "invariant.admitted.one"
	const unadmittedID = "invariant.generated.unadmitted"
	f := newAdmissionFixture(t, []string{admittedID})
	root := f.writeCorpus(t, "invariants.yaml", admittedID)
	a := f.admitted(t)

	before, err := knowledgeadmission.AdmissionCorpusDigest(f.root, []string{admittedID})
	if err != nil {
		t.Fatal(err)
	}
	f.writeCorpus(t, "generated/x.yaml", admittedID, unadmittedID)
	after, err := knowledgeadmission.AdmissionCorpusDigest(f.root, []string{admittedID})
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("generated output changed admission corpus digest: %s -> %s", before, after)
	}

	expected, _, err := expectedIdentities(root, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := expected[admittedID]; !ok {
		t.Fatal("authored admitted identity was not required")
	}
	if _, ok := expected[unadmittedID]; ok {
		t.Fatal("unadmitted generated identity gained authority")
	}
}
