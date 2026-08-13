// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Pre-mutation domain/source identity admission.
//
// Closure proves that a published slice is internally coherent with the input
// it was given. It CANNOT prove that the input belonged to the requested
// domain: once the certified roots are derived from the corpus the build read,
// a wrong-workspace publication is perfectly self-consistent and reports
// closure PROVEN. Verified live on 2026-08-05 — the third repetition of the
// same wrong-directory build passed closure while publishing sensei's corpus
// into the services domain.
//
// So the domain must be resolved through a registry that is independent of
// both the current working directory and the corpus about to be certified:
//
//	requested domain
//	→ independently registered source repository
//	→ exact source root and revision
//	→ pre-mutation identity admission   ← this file
//	→ publication
//	→ post-publication domain closure
//	→ authority
//
// Neither proof substitutes for the other. Admission answers "is this the
// correct source repository for this domain?"; closure answers "did the
// published slice faithfully represent that admitted source?".
//
// invariant: graph.publication_requires_pre_mutation_domain_source_admission

// AdmissionSource names the independent authority that bound this domain to a
// repository.
//
// It is a recorded fact, not a mode. Both sources must prove the same thing;
// they differ only in who asserted the expectation, and that difference has to
// survive into the receipt. Flattening a CI attestation into the registry's
// shape would make a machine-issued binding indistinguishable from an
// operator-authored one at every later point that reads the decision.
type AdmissionSource string

const (
	// AdmissionSourceRegistry is the operator-authored registry, held outside
	// any published repository.
	AdmissionSourceRegistry AdmissionSource = "registry"
	// AdmissionSourceGitHubActions is a runner-injected attestation. See
	// domain_admission_ci.go for why it is an independent source rather than a
	// relaxation of the registry.
	AdmissionSourceGitHubActions AdmissionSource = "github-actions"
)

// AdmissionDecision is what admitted a publication, kept for the receipt.
type AdmissionDecision struct {
	// Source is the authority that stated the expectation.
	Source AdmissionSource
	// CorpusRoots is TYPED, not silently empty. The registry declares allowed
	// roots; an attestation cannot know them. Reporting "unestablished" keeps
	// that absence legible instead of letting an empty list read as "every root
	// approved" — the two are the same value and very different facts.
	CorpusRoots string
}

func decide(rd RegisteredDomain, src AdmissionSource) AdmissionDecision {
	switch {
	case len(rd.AllowedCorpusRoots) > 0:
		return AdmissionDecision{Source: src, CorpusRoots: "enforced"}
	case src == AdmissionSourceGitHubActions:
		return AdmissionDecision{Source: src, CorpusRoots: "unestablished"}
	default:
		return AdmissionDecision{Source: src, CorpusRoots: "unconstrained_by_operator"}
	}
}

// DomainRegistry binds a domain name to the repository allowed to publish it.
type DomainRegistry struct {
	Domains map[string]RegisteredDomain `yaml:"domains"`
}

// RegisteredDomain is the operator-controlled expectation for one domain.
type RegisteredDomain struct {
	// RepositoryIdentity is the canonical "owner/repo", derived from the git
	// remote rather than the directory name. A copied checkout or a worktree
	// therefore still validates — it has the correct repository identity — while
	// a same-named directory from another project does not.
	RepositoryIdentity string `yaml:"repository_identity"`
	// AllowedCorpusRoots are repo-relative corpus directories this domain may
	// publish from.
	AllowedCorpusRoots []string `yaml:"allowed_corpus_roots"`
	// AllowDirtyWorktree permits publishing uncommitted content. Default false:
	// a dirty tree must never silently certify only the git commit while
	// publishing something else.
	AllowDirtyWorktree bool `yaml:"allow_dirty_worktree"`
}

// SourceIdentity is what a corpus directory actually resolves to.
type SourceIdentity struct {
	RepositoryIdentity string
	RepoRoot           string
	Revision           string
	Dirty              bool
	CorpusRelPath      string
}

// AdmissionRefusedError reports a refusal in a form that states plainly that
// nothing was mutated.
type AdmissionRefusedError struct {
	RequestedDomain string
	Expected        string
	Actual          string
	Reason          string
}

func (e *AdmissionRefusedError) Error() string {
	return fmt.Sprintf(
		"PUBLICATION_REFUSED\n  requested_domain:    %s\n  expected_repository: %s\n  actual_repository:   %s\n  reason:              %s\n  mutation_started:    false",
		e.RequestedDomain, orNone(e.Expected), orNone(e.Actual), e.Reason)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}

// LoadDomainRegistry reads the registry. A registry inside the repository being
// published would be as untrustworthy as the corpus itself, so the default
// location is outside any repo.
func LoadDomainRegistry(path string) (*DomainRegistry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r DomainRegistry
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("domain registry %s: %w", path, err)
	}
	return &r, nil
}

// DefaultDomainRegistryPath is outside any published repository on purpose.
func DefaultDomainRegistryPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".sensei", "domains.yaml")
	}
	return ""
}

// ResolveSourceIdentity determines what repository a corpus directory belongs
// to, using git rather than the directory name.
func ResolveSourceIdentity(corpusDir string) (SourceIdentity, error) {
	abs, err := filepath.Abs(corpusDir)
	if err != nil {
		return SourceIdentity{}, err
	}
	top, err := gitOut(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return SourceIdentity{}, fmt.Errorf("%s is not inside a git repository: %w", abs, err)
	}
	id := SourceIdentity{RepoRoot: top}
	if rel, rerr := filepath.Rel(top, abs); rerr == nil {
		id.CorpusRelPath = filepath.ToSlash(rel)
	}
	id.Revision, _ = gitOut(abs, "rev-parse", "HEAD")
	// Dirty is scoped to the CORPUS being published, not the whole repository.
	//
	// What matters is whether the bytes about to be certified are committed. A
	// whole-repo check also fails on unrelated edits and — worse — on the build's
	// own artifacts: `sensei build` writes .sensei/graph-authority.json into the
	// repo, so a repo-wide check would leave the tree permanently dirty and
	// refuse every subsequent publication.
	if status, serr := gitOut(abs, "status", "--porcelain", "--", abs); serr == nil {
		id.Dirty = strings.TrimSpace(status) != ""
	}
	remote, rerr := gitOut(abs, "config", "--get", "remote.origin.url")
	if rerr != nil || strings.TrimSpace(remote) == "" {
		return id, fmt.Errorf("no origin remote for %s: repository identity cannot be established from a directory name alone", top)
	}
	id.RepositoryIdentity = NormalizeRepoIdentity(remote)
	return id, nil
}

// NormalizeRepoIdentity reduces a git remote URL to canonical "owner/repo".
func NormalizeRepoIdentity(remote string) string {
	s := strings.TrimSpace(remote)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if at := strings.Index(s, "@"); at >= 0 {
			s = s[at+1:]
		}
	} else if at := strings.Index(s, "@"); at >= 0 {
		s = s[at+1:] // scp form: git@host:owner/repo
	}
	s = strings.ReplaceAll(s, ":", "/")
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) >= 2 {
		return strings.ToLower(parts[len(parts)-2] + "/" + parts[len(parts)-1])
	}
	return strings.ToLower(s)
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// AdmitPublication decides whether a publication may proceed. It must be called
// BEFORE any store mutation.
//
// Fail-closed throughout: an unreadable registry, an unregistered domain, an
// unresolvable source identity, a mismatched repository, a corpus root outside
// the allowed list, or an unexpectedly dirty tree all refuse. "I could not
// verify" is never treated as "verified".
func AdmitPublication(domain string, inputDirs []string, registryPath string) error {
	_, err := AdmitPublicationFromSource(domain, inputDirs, registryPath, nil)
	return err
}

// AdmitPublicationFromSource is AdmitPublication with the binding authority made
// explicit. It returns the source that admitted the publication so the caller
// can record it.
//
// The registry is tried first and always wins: an operator-authored expectation
// outranks a machine-issued one, and a repository that IS registered must keep
// answering to the registry even when it happens to build inside CI. The
// attestation is consulted only where the registry has nothing to say — a
// missing registry or an unregistered domain — and only when the caller passed
// one. Absent both, this refuses exactly as before.
//
// Note what this is not: there is no path here that admits a publication
// without proving repository identity. Selecting a source chooses who states
// the expectation; it never removes the obligation to meet it.
func AdmitPublicationFromSource(domain string, inputDirs []string, registryPath string, attestation *GitHubActionsAttestation) (AdmissionDecision, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return AdmissionDecision{}, nil // not a domain-scoped publication
	}
	if len(inputDirs) == 0 {
		return AdmissionDecision{}, &AdmissionRefusedError{RequestedDomain: domain, Reason: "no corpus root resolved"}
	}

	source := AdmissionSourceRegistry
	var rd RegisteredDomain
	reg, err := LoadDomainRegistry(registryPath)
	switch {
	case err == nil:
		var ok bool
		if rd, ok = reg.Domains[domain]; !ok {
			if attestation == nil {
				return AdmissionDecision{}, &AdmissionRefusedError{
					RequestedDomain: domain,
					Reason:          "domain is not registered; register it before publishing to it",
				}
			}
			if rd, err = attestation.Bind(domain); err != nil {
				return AdmissionDecision{}, err
			}
			source = AdmissionSourceGitHubActions
		}
	case attestation != nil:
		if rd, err = attestation.Bind(domain); err != nil {
			return AdmissionDecision{}, err
		}
		source = AdmissionSourceGitHubActions
	default:
		return AdmissionDecision{}, &AdmissionRefusedError{
			RequestedDomain: domain,
			Reason: fmt.Sprintf("no readable domain registry at %s (%v) — a domain cannot be "+
				"resolved from the working directory, because that is exactly what "+
				"published the wrong corpus", registryPath, err),
		}
	}

	if err := admitAgainst(domain, inputDirs, rd); err != nil {
		return AdmissionDecision{}, err
	}
	return decide(rd, source), nil
}

// admitAgainst runs the identity proof itself. Unchanged by the source: every
// refusal below predates CI admission and applies identically to it.
func admitAgainst(domain string, inputDirs []string, rd RegisteredDomain) error {
	for _, dir := range inputDirs {
		id, ierr := ResolveSourceIdentity(dir)
		if ierr != nil {
			return &AdmissionRefusedError{
				RequestedDomain: domain, Expected: rd.RepositoryIdentity,
				Reason: ierr.Error(),
			}
		}
		if !strings.EqualFold(id.RepositoryIdentity, rd.RepositoryIdentity) {
			// The canonical refusal — the case that destroyed the services slice
			// three times.
			return &AdmissionRefusedError{
				RequestedDomain: domain,
				Expected:        rd.RepositoryIdentity,
				Actual:          id.RepositoryIdentity,
				Reason:          "repository/domain identity mismatch: corpus " + dir + " does not belong to this domain",
			}
		}
		if len(rd.AllowedCorpusRoots) > 0 && !allowedRoot(id.CorpusRelPath, rd.AllowedCorpusRoots) {
			return &AdmissionRefusedError{
				RequestedDomain: domain, Expected: rd.RepositoryIdentity, Actual: id.RepositoryIdentity,
				Reason: fmt.Sprintf("corpus root %q is not in the domain's allowed roots %v",
					id.CorpusRelPath, rd.AllowedCorpusRoots),
			}
		}
		if id.Dirty && !rd.AllowDirtyWorktree {
			return &AdmissionRefusedError{
				RequestedDomain: domain, Expected: rd.RepositoryIdentity, Actual: id.RepositoryIdentity,
				// Do NOT advertise allow_dirty_worktree here. It is a known
				// false-certification path until the receipt binds content
				// identity, and an error message that points the operator at the
				// hole is how the hole stays in use.
				Reason: fmt.Sprintf("corpus %s has uncommitted changes: publishing would certify "+
					"revision %s while shipping bytes that revision does not contain. "+
					"Commit the corpus and publish the committed revision.",
					id.CorpusRelPath, shortRev(id.Revision)),
			}
		}
	}
	return nil
}

func allowedRoot(rel string, allowed []string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	for _, a := range allowed {
		a = strings.Trim(filepath.ToSlash(strings.TrimSpace(a)), "/")
		if a == "" {
			continue
		}
		if rel == a || strings.HasPrefix(rel, a+"/") {
			return true
		}
	}
	return false
}

func shortRev(r string) string {
	if len(r) > 12 {
		return r[:12]
	}
	return r
}
