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
	itDigest    = "1111111111111111111111111111111111111111111111111111111111111111"
)

// admissionFixture builds a checkout with a corpus and a SIGNED admission
// baseline, using an ephemeral key. Nothing here reaches the production
// governance key: the whole point is that admission is provable in tests without
// one existing.
type admissionFixture struct {
	root  string
	store governancepack.TrustStore
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

	records := make([]knowledgeadmission.Record, 0, len(admittedIDs))
	for _, id := range admittedIDs {
		records = append(records, knowledgeadmission.Record{
			Identity: id, Disposition: knowledgeadmission.DispositionGoverned,
			Receipt: adoption.Receipt{ValidForGraphDigest: itDigest},
		})
	}
	m := knowledgeadmission.Manifest{
		SchemaVersion: knowledgeadmission.SchemaVersion,
		PolicyID:      "knowledge.admission.test.v1",
		AdmittingRole: itRole,
		ActorBinding: closureprotocol.ActorBinding{
			PrincipalID: itPrincipal, ActorKind: closureprotocol.ActorHuman,
			Roles: []string{itRole}, Issuer: itPublisher,
			AuthenticationReceiptDigestSHA256: ad,
			RoleAttestationReceiptDigests:     []string{rd},
		},
		Records: records,
	}
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, knowledgeadmission.BaselineFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, raw)
	if err := os.WriteFile(filepath.Join(bundle, knowledgeadmission.SignatureFileName),
		[]byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return &admissionFixture{root: root, store: governancepack.TrustStore{
		SchemaVersion: governancepack.TrustStoreSchemaV1,
		Publishers: []governancepack.TrustedPublisher{{PublisherID: itPublisher,
			Keys: []governancepack.TrustedKey{{KeyID: itKeyID, Algorithm: "ed25519",
				PublicKeyBase64: base64.StdEncoding.EncodeToString(pub), Status: "active"}}}},
	}}
}

func (f *admissionFixture) admitted(t *testing.T) knowledgeadmission.Admitted {
	t.Helper()
	a, _, err := knowledgeadmission.LoadFromRepo(knowledgeadmission.LoadOptions{
		RepoRoot: f.root, GraphDigest: itDigest, EvaluatedAt: time.Now().UTC(),
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
// two locations. Before this change the candidates/ copy was REQUIRED by closure
// while the importer skipped it, which is what produced the failures.
func TestClosureAuthorityIsIndependentOfPath(t *testing.T) {
	const admittedID = "invariant.admitted.one"
	const candidateID = "invariant.candidate.one"
	f := newAdmissionFixture(t, []string{admittedID})
	a := f.admitted(t)

	for _, rel := range []string{"invariants.yaml", "candidates/invariant/rule.yaml", "deeply/nested/rule.yaml"} {
		root := f.writeCorpus(t, rel, admittedID, candidateID)
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
		os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
	}
}

// #166 acceptance test 1 and 2 at the closure boundary: generating candidates
// must not create a closure obligation, so an otherwise authoritative repository
// does not degrade just because discovery ran.
func TestGeneratedCandidatesCreateNoClosureObligation(t *testing.T) {
	const admittedID = "invariant.admitted.one"
	f := newAdmissionFixture(t, []string{admittedID})
	a := f.admitted(t)

	root := f.writeCorpus(t, "invariants.yaml", admittedID)
	before, _, err := expectedIdentities(root, a)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly what `sensei import --refresh` produced: a governed schema key
	// under candidates/, which took closure from 485/485 to 12695 missing.
	f.writeCorpus(t, "candidates/invariant_candidates.yaml",
		"candidate.invariant.authority.aa", "candidate.invariant.authority.abort")
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
