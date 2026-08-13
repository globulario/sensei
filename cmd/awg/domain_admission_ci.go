// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// CI-attested domain/source admission.
//
// A hosted runner has no operator registry: the default location is outside any
// repository on purpose, and a fresh runner starts with an empty home. The
// tempting repair — let the action write a registry into the workspace — is the
// exact hole the registry exists to close, because a binding produced by the
// repository being published vouches for itself.
//
// A CI attestation is a DIFFERENT independent source, not a weaker registry.
// GITHUB_REPOSITORY is injected into the process environment by the runner. It
// is not read from the checkout, so the corpus cannot forge it, and it is not
// derived from the working directory, so a copied or wrong-directory checkout
// cannot launder itself through it.
//
// It is also strictly NARROWER in authority than a registry. A registry may
// bind any domain to any repository; an attestation can only ever say "this
// checkout is the repository I am running for". It therefore cannot admit a
// foreign domain at all — which is precisely the 2026-08-05 incident class.
//
// This admission path is for persistent or shared publication where --repo must
// prove which domain slice may mutate. It is deliberately not needed by the
// Marketplace action's disposable store: that action creates an empty Oxigraph
// with --no-seed, owns its whole lifetime, and may therefore use --all as the
// explicit cold-start ownership declaration. Treating those two stores as the
// same authority surface would either over-constrain the disposable one or
// weaken the persistent one.
//
// This is not a skip flag. The identity proof still runs, the repository still
// has to match, and a dirty corpus is still refused with no opt-out. What
// changes is only WHERE the expectation came from, and that provenance is
// carried out as a typed AdmissionSource rather than being flattened into the
// registry's shape.
//
// invariant: graph.publication_requires_pre_mutation_domain_source_admission

// GitHubActionsAttestation reads the runner-injected identity for the checkout
// this process is running against.
type GitHubActionsAttestation struct {
	// Running reports GITHUB_ACTIONS == "true". Anything else is refused: the
	// variable is trivially settable by a caller who is not a runner, and an
	// attestation that admits its own absence is not an attestation.
	Running bool
	// Repository is GITHUB_REPOSITORY, canonical "owner/repo".
	Repository string
	// ServerHost is the host of GITHUB_SERVER_URL. Taken from the environment
	// rather than hardcoded to github.com so GitHub Enterprise Server attests
	// to its own host instead of borrowing github.com's identity.
	ServerHost string
}

// ReadGitHubActionsAttestation collects the attestation from an environment
// lookup. env is injected so tests never depend on ambient CI variables — a
// test that passes only inside Actions proves nothing about the guard.
func ReadGitHubActionsAttestation(env func(string) string) GitHubActionsAttestation {
	return GitHubActionsAttestation{
		Running:    strings.EqualFold(strings.TrimSpace(env("GITHUB_ACTIONS")), "true"),
		Repository: strings.Trim(strings.TrimSpace(env("GITHUB_REPOSITORY")), "/"),
		ServerHost: serverHost(env("GITHUB_SERVER_URL")),
	}
}

var serverScheme = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)

// serverHost reduces GITHUB_SERVER_URL to a bare lowercase host.
func serverHost(raw string) string {
	s := strings.TrimSpace(raw)
	s = serverScheme.ReplaceAllString(s, "")
	if i := strings.IndexAny(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// AttestedDomain is the ONLY domain this attestation can speak for.
func (a GitHubActionsAttestation) AttestedDomain() string {
	if a.ServerHost == "" || a.Repository == "" {
		return ""
	}
	return strings.ToLower(a.ServerHost + "/" + a.Repository)
}

// Bind produces the expectation for domain, or refuses.
//
// Fail-closed on every unverifiable path, and — the property that makes this
// safe to accept as authority — it refuses any domain other than its own.
func (a GitHubActionsAttestation) Bind(domain string) (RegisteredDomain, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !a.Running {
		return RegisteredDomain{}, &AdmissionRefusedError{
			RequestedDomain: domain,
			Reason: "GITHUB_ACTIONS is not \"true\": a CI attestation is only admissible " +
				"from inside the CI system that issues it",
		}
	}
	if a.Repository == "" || a.ServerHost == "" {
		return RegisteredDomain{}, &AdmissionRefusedError{
			RequestedDomain: domain,
			Reason: "incomplete CI attestation: GITHUB_REPOSITORY and GITHUB_SERVER_URL " +
				"must both be present, because an identity assembled from partial " +
				"evidence is a guess",
		}
	}
	if strings.Count(a.Repository, "/") != 1 {
		return RegisteredDomain{}, &AdmissionRefusedError{
			RequestedDomain: domain,
			Reason:          fmt.Sprintf("GITHUB_REPOSITORY %q is not a canonical owner/repo", a.Repository),
		}
	}
	attested := a.AttestedDomain()
	if attested != domain {
		// The narrowness property, enforced. A registry can vouch for a third
		// party; an attestation cannot, so a workflow in repository A can never
		// publish into repository B's domain even by asking for it.
		return RegisteredDomain{}, &AdmissionRefusedError{
			RequestedDomain: domain,
			Expected:        attested,
			Actual:          domain,
			Reason: "a CI attestation can admit only the repository it runs for; " +
				"this run attests " + attested + " and cannot speak for another domain",
		}
	}
	return RegisteredDomain{
		RepositoryIdentity: strings.ToLower(a.Repository),
		// Deliberately empty, and deliberately NOT silently permissive: an
		// attestation establishes which repository owns the domain, and knows
		// nothing about which subdirectories an operator intended to publish.
		// AdmissionSourceGitHubActions is carried into the decision so the
		// unconstrained corpus root is reported as unestablished rather than
		// being read as "every root was approved".
		AllowedCorpusRoots: nil,
		// No opt-out. A dirty corpus certifies a revision whose bytes it is not
		// shipping, and CI is where that would be least visible.
		AllowDirtyWorktree: false,
	}, nil
}
