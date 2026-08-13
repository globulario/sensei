// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A CI attestation is admissible as an INDEPENDENT source of the domain→
// repository binding, never as a relaxation of the proof.
//
// The registry closes the 2026-08-05 wrong-workspace hole by asserting the
// expectation from outside the corpus. A hosted runner has no registry, and the
// obvious repair — synthesize one into the workspace — reopens the hole exactly,
// because the repository being published would be vouching for itself.
//
// GITHUB_REPOSITORY is different in kind: injected by the runner, unreadable
// from the checkout, independent of the working directory. These tests pin the
// three properties that make it acceptable — it cannot self-enable, it cannot
// speak for another domain, and it removes no existing refusal.
//
// invariant: graph.publication_requires_pre_mutation_domain_source_admission

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func actionsEnv(repo string) map[string]string {
	return map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": repo,
		"GITHUB_SERVER_URL": "https://github.com",
	}
}

// THE property. A registry may bind any domain to any repository; an
// attestation may bind only its own. Without this, a workflow in any repository
// could publish into any other repository's domain merely by asking, which is a
// strictly worse hole than the one this whole layer exists to close.
func TestAttestationCannotSpeakForAnotherDomain(t *testing.T) {
	a := ReadGitHubActionsAttestation(envFrom(actionsEnv("globulario/sensei")))

	_, err := a.Bind("github.com/globulario/services")
	if err == nil {
		t.Fatal("a run for globulario/sensei was allowed to admit the services domain — " +
			"an attestation that can vouch for a third party is a registry without an operator")
	}
	msg := err.Error()
	for _, want := range []string{
		"PUBLICATION_REFUSED",
		"github.com/globulario/sensei",
		"mutation_started:    false",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must state %q; got:\n%s", want, msg)
		}
	}
}

// The attestation must not be able to assert its own presence. GITHUB_ACTIONS
// is settable by anyone; treating it as self-certifying would turn the flag into
// the production opt-out the invariant forbids.
func TestAttestationRefusesOutsideCI(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"not running": {"GITHUB_REPOSITORY": "globulario/sensei", "GITHUB_SERVER_URL": "https://github.com"},
		"no repo":     {"GITHUB_ACTIONS": "true", "GITHUB_SERVER_URL": "https://github.com"},
		"no server":   {"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "globulario/sensei"},
		"not owner/repo": {
			"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "sensei", "GITHUB_SERVER_URL": "https://github.com",
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := ReadGitHubActionsAttestation(envFrom(env))
			if _, err := a.Bind("github.com/globulario/sensei"); err == nil {
				t.Fatal("incomplete attestation was admitted; unverifiable must never read as verified")
			}
		})
	}
}

// Enterprise Server attests to its own host rather than borrowing github.com's.
func TestAttestationUsesItsOwnServerHost(t *testing.T) {
	env := actionsEnv("acme/widgets")
	env["GITHUB_SERVER_URL"] = "https://ghe.corp.example.com"
	a := ReadGitHubActionsAttestation(envFrom(env))

	if got := a.AttestedDomain(); got != "ghe.corp.example.com/acme/widgets" {
		t.Fatalf("attested domain = %q, want the enterprise host", got)
	}
	if _, err := a.Bind("github.com/acme/widgets"); err == nil {
		t.Fatal("an enterprise run was allowed to publish into the github.com domain")
	}
}

// The end-to-end path: no registry on the runner, attestation offered, and the
// corpus genuinely belongs to the attested repository.
func TestCIAdmissionAdmitsItsOwnRepositoryWithoutARegistry(t *testing.T) {
	repo := gitRepo(t, "https://github.com/globulario/sensei.git")
	a := ReadGitHubActionsAttestation(envFrom(actionsEnv("globulario/sensei")))

	decision, err := AdmitPublicationFromSource(
		"github.com/globulario/sensei",
		[]string{filepath.Join(repo, "docs", "awareness")},
		filepath.Join(t.TempDir(), "absent.yaml"),
		&a,
	)
	if err != nil {
		t.Fatalf("a runner publishing its own repository was refused: %v", err)
	}
	if decision.Source != AdmissionSourceGitHubActions {
		t.Errorf("source = %q, want %q — provenance must survive into the receipt",
			decision.Source, AdmissionSourceGitHubActions)
	}
	// Absence made legible. An attestation cannot know the operator's intended
	// roots, and reporting that as "unestablished" is what stops an empty list
	// from being read later as "every root was approved".
	if decision.CorpusRoots != "unestablished" {
		t.Errorf("corpus roots = %q, want \"unestablished\"", decision.CorpusRoots)
	}
}

// Even holding a valid attestation for the right domain, the corpus must still
// belong to it. This is the 2026-08-05 refusal, re-proved on the CI path.
func TestCIAdmissionStillRefusesTheWrongWorkspace(t *testing.T) {
	other := gitRepo(t, "https://github.com/globulario/services.git")
	a := ReadGitHubActionsAttestation(envFrom(actionsEnv("globulario/sensei")))

	_, err := AdmitPublicationFromSource(
		"github.com/globulario/sensei",
		[]string{filepath.Join(other, "docs", "awareness")},
		filepath.Join(t.TempDir(), "absent.yaml"),
		&a,
	)
	if err == nil {
		t.Fatal("the services corpus was published into the sensei domain under a valid " +
			"attestation — CI admission must bind the domain, not excuse the corpus")
	}
	if !strings.Contains(err.Error(), "globulario/services") {
		t.Errorf("refusal must name the actual repository; got:\n%s", err)
	}
}

// No opt-out reaches the dirty check. CI is exactly where certifying a revision
// whose bytes are not being shipped would be least visible.
func TestCIAdmissionStillRefusesADirtyCorpus(t *testing.T) {
	repo := gitRepo(t, "https://github.com/globulario/sensei.git")
	writeFile(t, filepath.Join(repo, "docs", "awareness", "invariants.yaml"), "invariants: [ {id: uncommitted} ]\n")
	a := ReadGitHubActionsAttestation(envFrom(actionsEnv("globulario/sensei")))

	_, err := AdmitPublicationFromSource(
		"github.com/globulario/sensei",
		[]string{filepath.Join(repo, "docs", "awareness")},
		filepath.Join(t.TempDir(), "absent.yaml"),
		&a,
	)
	if err == nil {
		t.Fatal("a dirty corpus was admitted on the CI path")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("refusal must name the dirty corpus; got:\n%s", err)
	}
}

// An operator-authored registry outranks a machine-issued attestation: a
// registered domain keeps answering to its operator even inside CI.
func TestRegistryOutranksAttestation(t *testing.T) {
	sensei := gitRepo(t, "https://github.com/globulario/sensei.git")
	a := ReadGitHubActionsAttestation(envFrom(actionsEnv("globulario/sensei")))

	// The registry says "globular" belongs to globulario/services. A sensei
	// checkout must still be refused, attestation in hand.
	_, err := AdmitPublicationFromSource(
		"globular",
		[]string{filepath.Join(sensei, "docs", "awareness")},
		writeRegistry(t, servicesRegistry),
		&a,
	)
	if err == nil {
		t.Fatal("an attestation overrode a registered domain — the operator's binding must win")
	}
	if !strings.Contains(err.Error(), "globulario/services") {
		t.Errorf("refusal must come from the registry expectation; got:\n%s", err)
	}
}

// Without an attestation the behaviour is exactly what it was.
func TestNoAttestationKeepsTheOriginalRefusal(t *testing.T) {
	repo := gitRepo(t, "https://github.com/globulario/sensei.git")

	_, err := AdmitPublicationFromSource(
		"github.com/globulario/sensei",
		[]string{filepath.Join(repo, "docs", "awareness")},
		filepath.Join(t.TempDir(), "absent.yaml"),
		nil,
	)
	if err == nil {
		t.Fatal("a missing registry was admitted with no attestation offered")
	}
	if !strings.Contains(err.Error(), "no readable domain registry") {
		t.Errorf("refusal must be the original registry refusal; got:\n%s", err)
	}
}
