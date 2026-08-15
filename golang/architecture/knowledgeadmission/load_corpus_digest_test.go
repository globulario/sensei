// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"gopkg.in/yaml.v3"
)

func TestLoadFromRepoBindsToAuthoredCorpusNotPublishedGraphDigest(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "governance")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "artifacts", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &bundle{root: bundleRoot}

	const id = "invariant.real.one"
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.real.one
    title: authored source
    severity: high
`)
	corpusDigest := digestFor(t, root, id)

	m := manifest(t, b, Record{
		Identity:    id,
		Disposition: DispositionGoverned,
		Receipt: adoption.Receipt{
			ValidForCorpusDigest: corpusDigest,
		},
	})
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, BaselineFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := newSigner(t)
	sig := ed25519.Sign(s.priv, raw)
	if err := os.WriteFile(filepath.Join(bundleRoot, SignatureFileName), []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := s.trustStore(testKeyID, "active")

	// Deliberately supply a nonsense v1-style published graph digest. LoadFromRepo
	// must ignore it and derive the binding from docs/awareness + the signed
	// governed identity set.
	admitted, _, err := LoadFromRepo(LoadOptions{
		RepoRoot:            root,
		GraphDigest:         "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		EvaluatedAt:         time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC),
		Index:               testIndex(),
		TrustStore:          &store,
		ExpectedPublisherID: testPublisher,
	})
	if err != nil {
		t.Fatalf("LoadFromRepo: %v", err)
	}
	if !admitted.IsAuthoritativelyAdmitted(id) {
		t.Fatal("authored governed identity was not admitted")
	}
}

func TestLoadFromRepoCleanRoomNeedsNoPublishedSeed(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "governance")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "artifacts", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &bundle{root: bundleRoot}

	const id = "invariant.clean.room"
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.clean.room
    title: clean room authority
    severity: high
`)
	corpusDigest := digestFor(t, root, id)
	m := manifest(t, b, Record{
		Identity:    id,
		Disposition: DispositionGoverned,
		Receipt: adoption.Receipt{
			ValidForCorpusDigest: corpusDigest,
		},
	})
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, BaselineFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSigner(t)
	if err := os.WriteFile(filepath.Join(bundleRoot, SignatureFileName),
		[]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, raw))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := s.trustStore(testKeyID, "active")

	seed := filepath.Join(root, "golang", "server", "embeddata", "awareness.nt")
	if _, err := os.Stat(seed); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has a prior published seed: %v", err)
	}
	load := func() {
		t.Helper()
		admitted, _, err := LoadFromRepo(LoadOptions{
			RepoRoot:            root,
			EvaluatedAt:         time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC),
			Index:               testIndex(),
			TrustStore:          &store,
			ExpectedPublisherID: testPublisher,
		})
		if err != nil {
			t.Fatalf("clean-room LoadFromRepo: %v", err)
		}
		if !admitted.IsAuthoritativelyAdmitted(id) {
			t.Fatal("clean-room load did not recover governed authority from authored source")
		}
	}

	// First load: there has never been a seed. This is the load-bearing regression
	// for the v1 bootstrap-by-lag defect.
	load()

	// A downstream publication may then appear and change arbitrarily without
	// perturbing the upstream admission binding.
	if err := os.MkdirAll(filepath.Dir(seed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed, []byte("published graph generation A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	load()
	if err := os.WriteFile(seed, []byte("published graph generation B, different digest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	load()

	// And deleting publication again must not remove the authority needed to
	// reconstruct it.
	if err := os.Remove(seed); err != nil {
		t.Fatal(err)
	}
	load()
}

func TestLoadFromRepoRejectsChangedGovernedSourceWithoutResigning(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "governance")
	if err := os.MkdirAll(filepath.Join(bundleRoot, "artifacts", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &bundle{root: bundleRoot}

	const id = "invariant.real.one"
	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.real.one
    title: before
    severity: high
`)
	corpusDigest := digestFor(t, root, id)

	m := manifest(t, b, Record{
		Identity:    id,
		Disposition: DispositionGoverned,
		Receipt: adoption.Receipt{
			ValidForCorpusDigest: corpusDigest,
		},
	})
	raw, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleRoot, BaselineFileName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s := newSigner(t)
	if err := os.WriteFile(filepath.Join(bundleRoot, SignatureFileName),
		[]byte(base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, raw))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := s.trustStore(testKeyID, "active")

	writeAdmissionCorpusFile(t, root, "invariants.yaml", `invariants:
  - id: invariant.real.one
    title: after
    severity: high
`)

	if _, _, err := LoadFromRepo(LoadOptions{
		RepoRoot:            root,
		EvaluatedAt:         time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC),
		Index:               testIndex(),
		TrustStore:          &store,
		ExpectedPublisherID: testPublisher,
	}); err == nil {
		t.Fatal("changed governed source was accepted under a stale signed corpus binding")
	}
}
