// SPDX-License-Identifier: AGPL-3.0-only

// Package knowledgeadmission answers one question for the whole system:
// is this stable knowledge identity authoritatively admitted right now?
//
// It exists because publication, closure and knowledge adoption each used to
// answer that question themselves, by pathname, and disagreed. A file carrying
// a governed schema key under docs/awareness/candidates/ was skipped by the
// importer (directory name), required by closure (top-level key), and its own
// `status: candidate` was read by neither — so moving the file between
// directories silently changed whether its knowledge governed the repository.
//
// The rule this package replaces:
//
//	path != candidates/  =>  authoritative
//
// Path is organisation. It is not authority, and neither is any field a caller
// can edit in the knowledge document itself. Authority comes from an admission
// decision recorded OUTSIDE the knowledge, made by an actor whose roles were
// verified against governed policy, and bound to the revision and graph digest
// it was made for.
//
// See docs/awareness/decisions/closure-disposition-authority.md.
package knowledgeadmission

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// SchemaVersion is the admission manifest schema this package reads.
const SchemaVersion = "1"

// Disposition is the canonical authority disposition of one knowledge identity.
// Only Governed confers authority; every other value is explicitly non-governing
// so that "not admitted" and "admitted as non-governing" stay distinguishable.
type Disposition string

const (
	// DispositionGoverned is the ONLY authoritative disposition.
	DispositionGoverned Disposition = "governed"
	// DispositionCandidate is discoverable knowledge that has not earned authority.
	DispositionCandidate Disposition = "candidate"
	// DispositionRejected was considered and refused.
	DispositionRejected Disposition = "rejected"
	// DispositionSuperseded was governing and has been replaced.
	DispositionSuperseded Disposition = "superseded"
	// DispositionStale was governing and no longer describes the system.
	DispositionStale Disposition = "stale"
)

var dispositions = map[Disposition]struct{}{
	DispositionGoverned:   {},
	DispositionCandidate:  {},
	DispositionRejected:   {},
	DispositionSuperseded: {},
	DispositionStale:      {},
}

// Record is one admission decision about one stable knowledge identity.
//
// Keyed by identity, never by path: acceptance test 5 requires that moving
// candidate content between directories does not alter its disposition, and a
// path-keyed record would reintroduce exactly the defect this package removes.
type Record struct {
	Identity         string      `yaml:"identity" json:"identity"`
	Disposition      Disposition `yaml:"disposition" json:"disposition"`
	adoption.Receipt `yaml:",inline" json:",inline"`
}

// Manifest is the governed admission artifact.
//
// The ActorBinding is the trust root. It is verified against governed policy
// before any record in the manifest is believed, so authority traces back to an
// actor holding an admitting role from an issuer that policy trusts — not to
// whoever last wrote a YAML file.
type Manifest struct {
	SchemaVersion string                       `yaml:"schema_version" json:"schema_version"`
	PolicyID      string                       `yaml:"policy_id" json:"policy_id"`
	AdmittingRole string                       `yaml:"admitting_role" json:"admitting_role"`
	ActorBinding  closureprotocol.ActorBinding `yaml:"actor_binding" json:"actor_binding"`
	Records       []Record                     `yaml:"records" json:"records"`
}

// Context is the governed context an admission decision is evaluated against.
//
// GraphDigest is supplied by the caller from the real corpus — never read out of
// the manifest. A receipt that carries its own idea of what it is valid for
// proves nothing; the check is that its claim matches what is actually true here
// and now.
//
// There is deliberately no git revision here. Admission is a statement about
// which KNOWLEDGE was admitted, and the graph digest pins exactly that: it is
// computed over the projected triples, so changing knowledge moves it and
// changing only code does not. Binding to a commit instead would over-bind
// (invalidating all admission on every unrelated code commit, forcing knowledge
// that did not change to be re-signed) and would be self-referential for a
// committed manifest, since committing it changes the very HEAD it names.
type Context struct {
	GraphDigest string
	EvaluatedAt time.Time
	Index       authority.PolicyIndex
	Resolver    authority.ArtifactResolver
}

// Admitted is a verified admission decision, ready to be consulted.
//
// Verification happens once, up front, and fails closed. After that the
// predicate is a pure lookup, so publication, closure and knowledge adoption
// cannot drift: they share one decision instead of reconstructing three.
type Admitted struct {
	governed map[string]Record
	all      map[string]Record
	actor    authority.VerifiedActor
}

// Actor reports the verified actor whose decision this is.
func (a Admitted) Actor() authority.VerifiedActor { return a.actor }

// IsAuthoritativelyAdmitted reports whether a stable knowledge identity is
// authoritatively admitted in the verified context.
//
// This is the single authority decision. A caller must not reimplement it, and
// must not soften it with a pathname test: an identity is authoritative because
// a verified actor admitted it as governed, or it is not authoritative at all.
func (a Admitted) IsAuthoritativelyAdmitted(identity string) bool {
	_, ok := a.governed[strings.TrimSpace(identity)]
	return ok
}

// Disposition reports the recorded disposition of an identity, and whether any
// admission decision exists for it at all.
//
// Absence is a distinct answer from DispositionCandidate: nobody has ruled on
// this identity, which is not the same as having ruled it non-governing.
func (a Admitted) Disposition(identity string) (Disposition, bool) {
	r, ok := a.all[strings.TrimSpace(identity)]
	if !ok {
		return "", false
	}
	return r.Disposition, true
}

// GovernedIdentities returns every authoritatively admitted identity, sorted.
func (a Admitted) GovernedIdentities() []string {
	out := make([]string, 0, len(a.governed))
	for id := range a.governed {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// verify checks contextual binding and the actor binding, then indexes the
// admitted identities. It fails closed: any error means nothing is admitted.
//
// Deliberately unexported. It is the INNER half of admission and establishes no
// issuer authenticity on its own — reaching it requires going through
// VerifySigned, which authenticates the manifest bytes first. Exporting it would
// leave a door into the authority decision that skips provenance entirely.
//
// Both halves are required, and neither substitutes for the other:
//
//   - provenance — the manifest's actor binding verifies against governed
//     policy, and the actor actually holds the admitting role. This is what a
//     caller who can only edit YAML cannot manufacture.
//   - binding — the decision was made for THIS graph digest. This is what stops
//     a real past decision being replayed over changed knowledge.
//
// A caller who can read the current digest can write a matching
// valid_for_graph_digest, which is why it is checked second and never alone.
func verify(m Manifest, ctx Context) (Admitted, error) {
	if strings.TrimSpace(m.SchemaVersion) != SchemaVersion {
		return Admitted{}, fmt.Errorf("admission manifest schema_version %q is not supported (want %s)", m.SchemaVersion, SchemaVersion)
	}
	role := strings.TrimSpace(m.AdmittingRole)
	if role == "" {
		return Admitted{}, fmt.Errorf("admission manifest declares no admitting_role")
	}
	if ctx.Resolver == nil {
		return Admitted{}, fmt.Errorf("admission context has no artifact resolver")
	}
	if strings.TrimSpace(ctx.GraphDigest) == "" {
		return Admitted{}, fmt.Errorf("admission context has no graph digest")
	}

	// Provenance. VerifyActorBinding resolves the authentication and
	// role-attestation receipts by content digest and rejects any role whose
	// issuer governed policy does not trust.
	actor, err := authority.VerifyActorBinding(m.ActorBinding, ctx.Resolver, ctx.Index, ctx.EvaluatedAt)
	if err != nil {
		return Admitted{}, fmt.Errorf("admission actor binding: %w", err)
	}
	if actor.Status != closureprotocol.ReceiptValid {
		return Admitted{}, fmt.Errorf("admission actor binding status is %s", actor.Status)
	}
	if !containsString(actor.VerifiedRoleIDs, role) {
		return Admitted{}, fmt.Errorf("admitting actor does not hold role %s (verified roles: %v)", role, actor.VerifiedRoleIDs)
	}

	governed := map[string]Record{}
	all := map[string]Record{}
	for i, raw := range m.Records {
		r := raw
		r.Identity = strings.TrimSpace(r.Identity)
		r.Disposition = Disposition(strings.ToLower(strings.TrimSpace(string(r.Disposition))))
		if r.Identity == "" {
			return Admitted{}, fmt.Errorf("admission record %d has no identity", i)
		}
		if _, ok := dispositions[r.Disposition]; !ok {
			return Admitted{}, fmt.Errorf("admission record %s has unsupported disposition %q", r.Identity, r.Disposition)
		}
		if _, dup := all[r.Identity]; dup {
			// Two decisions about one identity is an unresolved disagreement,
			// not a merge: picking either silently would invent an answer.
			return Admitted{}, fmt.Errorf("admission record %s is declared twice", r.Identity)
		}
		r.Receipt = adoption.Normalize(r.Receipt)
		all[r.Identity] = r

		if r.Disposition != DispositionGoverned {
			continue
		}
		if err := verifyGovernedRecord(r, ctx); err != nil {
			return Admitted{}, err
		}
		governed[r.Identity] = r
	}
	return Admitted{governed: governed, all: all, actor: actor}, nil
}

// verifyGovernedRecord enforces contextual binding for a record that claims
// authority. Non-governing records are not bound: refusing to believe a stale
// "this is a candidate" record would promote it by omission.
//
// ValidForRevision is deliberately NOT checked. See Context.
func verifyGovernedRecord(r Record, ctx Context) error {
	if got, want := r.Receipt.ValidForGraphDigest, strings.ToLower(strings.TrimSpace(ctx.GraphDigest)); got != want {
		return fmt.Errorf("admission record %s is valid for graph digest %q, not %q", r.Identity, got, want)
	}
	return nil
}

func containsString(haystack []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, v := range haystack {
		if strings.TrimSpace(v) == needle {
			return true
		}
	}
	return false
}
