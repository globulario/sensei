// SPDX-License-Identifier: AGPL-3.0-only

// Package governedmutation is the single reusable owner of the canonical
// governed-source mutation policy: the closed governed-kind vocabulary and
// source routing, schema validation, canonical record normalization, stable ID
// derivation, exact-replay vs same-ID-contradiction classification, the
// governed-source manifest / compare-and-swap token, canonical mutation bytes and
// identity, and the atomic temp-write+rename of the canonical YAML source.
//
// It is the primitive that both `sensei propose` (a thin CLI adapter) and the
// Phase-8.1b promotion transaction call. It mutates canonical governed YAML only:
// it never compiles or persists a graph, never writes the embedded seed, a
// promotion journal, or a receipt, and never establishes reusable truth by
// itself. Lock ownership is composable — Apply performs no locking of its own so
// the promotion transaction can hold one repository lock continuously across this
// mutation and its later graph verification (see AcquireLock).
package governedmutation

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/propose"
)

// Disposition is the closed set of governed-mutation outcomes.
type Disposition string

const (
	// DispositionApplied: a new canonical governed record was appended.
	DispositionApplied Disposition = "applied"
	// DispositionReplay: the exact canonical ID + a byte/semantic-equivalent body
	// already exists; nothing was written.
	DispositionReplay Disposition = "replay"
	// DispositionCandidateQueued: a contract_unknown entry was queued under
	// candidates/ (not a governed source, not rebuilt).
	DispositionCandidateQueued Disposition = "candidate_queued"
)

// Request is a governed-source mutation request. The proposal is the validated
// feedback payload; the owner re-validates it. ExpectedManifestDigestSHA256 is an
// optional compare-and-swap token — when set, the mutation fails closed unless
// the current governed-source manifest matches it.
type Request struct {
	RepositoryRoot               string
	Proposal                     propose.Request
	ExpectedManifestDigestSHA256 string
}

// Result is the typed outcome of a governed-source mutation. Every digest is
// recomputed by the owner; none is caller-supplied.
type Result struct {
	Kind                     string
	CanonicalID              string
	TargetRelPath            string // repo-relative, e.g. docs/awareness/invariants.yaml
	TopKey                   string
	Disposition              Disposition
	IsCandidate              bool
	Preview                  string // rendered canonical list item
	MutationDigestSHA256     string // semantic digest of the canonical record body
	PreManifestDigestSHA256  string
	PostManifestDigestSHA256 string
}

// ValidationError reports contract-first / schema validation failures. No file
// was mutated.
type ValidationError struct{ Errors []string }

func (e *ValidationError) Error() string {
	return "governed mutation validation failed: " + strings.Join(e.Errors, "; ")
}

// ContradictionError reports that the canonical ID already names a governed
// record whose body differs in semantics. The owner never overwrites; the caller
// must resolve the contradiction.
type ContradictionError struct {
	CanonicalID    string
	TargetRelPath  string
	ExistingDigest string
	ProposedDigest string
}

func (e *ContradictionError) Error() string {
	return fmt.Sprintf("governed record %q in %s already exists with a different body (existing %s, proposed %s)",
		e.CanonicalID, e.TargetRelPath, short(e.ExistingDigest), short(e.ProposedDigest))
}

// StaleManifestError reports that the governed-source manifest changed out from
// under a compare-and-swap mutation.
type StaleManifestError struct{ Expected, Actual string }

func (e *StaleManifestError) Error() string {
	return fmt.Sprintf("governed-source manifest is stale (expected %s, actual %s)", short(e.Expected), short(e.Actual))
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// kindRoute is the canonical source file + top-level list key for a governed kind.
type kindRoute struct {
	file string // path under docs/awareness/
	key  string // top-level list key
}

// governedKinds is the closed governed-kind → canonical source routing. It is
// the single source of this mapping (lifted out of cmd/awg).
var governedKinds = map[string]kindRoute{
	"failure_mode":  {"failure_modes.yaml", "failure_modes"},
	"invariant":     {"invariants.yaml", "invariants"},
	"required_test": {"required_tests.yaml", "required_tests"},
	"forbidden_fix": {"forbidden_fixes.yaml", "forbidden_fixes"},
	"decision":      {filepath.Join("architecture", "decisions.yaml"), "decisions"},
}

// GovernedKinds returns the closed set of governed-source kinds this owner can
// promote into (excludes the contract_unknown candidate queue).
func GovernedKinds() []string {
	out := make([]string, 0, len(governedKinds))
	for k := range governedKinds {
		out = append(out, k)
	}
	return out
}

// IsGovernedKind reports whether kind routes to a canonical governed source.
func IsGovernedKind(kind string) bool {
	_, ok := governedKinds[strings.TrimSpace(kind)]
	return ok
}

// ── canonical ID derivation ────────────────────────────────────────────────

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRun.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "_")
	}
	if s == "" {
		s = "entry"
	}
	return s
}

var idPrefixByKind = map[string]string{
	"failure_mode":     "failure",
	"invariant":        "invariant",
	"forbidden_fix":    "forbidden_fix",
	"decision":         "decision",
	"contract_unknown": "contract_unknown",
}

// DeriveID returns the explicit id, or a deterministic one derived from kind + a
// domain/repo hint + the title slug. required_test always carries an explicit id
// (enforced by validation).
func DeriveID(p propose.Request) string {
	if strings.TrimSpace(p.ID) != "" {
		return p.ID
	}
	prefix := idPrefixByKind[p.Kind]
	if prefix == "" {
		prefix = "feedback"
	}
	if hint := domainHint(p); hint != "" {
		prefix = prefix + "." + hint
	}
	return prefix + "." + slugify(p.Title)
}

func domainHint(p propose.Request) string {
	src := firstNonEmpty(p.Domain, p.Repo)
	if src == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(src, "/"), "/")
	last := parts[len(parts)-1]
	return slugify(last)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
