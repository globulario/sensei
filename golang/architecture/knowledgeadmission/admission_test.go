// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/changebinding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

const (
	testRevision    = "1111111111111111111111111111111111111111"
	testGraphDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	admittingRole   = "role.knowledge_admitter"
	// The governance issuer. Local enrollment cannot assert this — that is the
	// entire point of the anchor.
	governedIssuer = "sensei.governance"
	// identity.DefaultIssuer: what `sensei identity enroll` self-issues as.
	localIssuer = "sensei.local"
)

type bundle struct{ root string }

func newBundle(t *testing.T) *bundle {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "artifacts", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &bundle{root: root}
}

func (b *bundle) resolver() *authority.LocalBundleResolver {
	return authority.NewLocalBundleResolver(b.root)
}

func (b *bundle) write(t *testing.T, digest string, value any) {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "artifacts", "sha256", digest+".yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (b *bundle) storeArtifact(t *testing.T, data []byte) closureprotocol.LedgerPayloadRef {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	rel := filepath.ToSlash(filepath.Join("artifacts", "sha256", digest+".bin"))
	if err := os.WriteFile(filepath.Join(b.root, filepath.FromSlash(rel)), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return closureprotocol.LedgerPayloadRef{Path: rel, MediaType: "application/octet-stream", DigestSHA256: digest}
}

// structFieldNames lists a struct's field names, including promoted ones, so a
// test can assert what the type does NOT carry.
func structFieldNames(v any) []string {
	t := reflect.TypeOf(v)
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			for j := 0; j < f.Type.NumField(); j++ {
				out = append(out, f.Type.Field(j).Name)
			}
			continue
		}
		out = append(out, f.Name)
	}
	return out
}

// binding builds a fully valid actor binding issued by issuer and holding roles.
func (b *bundle) binding(t *testing.T, issuer string, roles []string) closureprotocol.ActorBinding {
	t.Helper()
	artifact := b.storeArtifact(t, []byte("authn-evidence"))
	authn := closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.test.actor-1",
		PrincipalID:            "actor.test.session-1",
		Issuer:                 issuer,
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-08-14T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	}
	authnDigest, err := closureprotocol.AuthenticationReceiptDigest(authn)
	if err != nil {
		t.Fatal(err)
	}
	authn.ReceiptDigestSHA256 = authnDigest
	b.write(t, authnDigest, authn)

	role := closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.test.actor-1",
		PrincipalID:                       "actor.test.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            issuer,
		RoleIDs:                           roles,
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-08-14T12:00:00Z",
		ValidUntil:                        "2026-08-15T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	}
	roleDigest, err := closureprotocol.RoleAttestationReceiptDigest(role)
	if err != nil {
		t.Fatal(err)
	}
	role.ReceiptDigestSHA256 = roleDigest
	b.write(t, roleDigest, role)

	return closureprotocol.ActorBinding{
		PrincipalID:                       "actor.test.session-1",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             roles,
		Issuer:                            issuer,
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
	}
}

// testIndex grants the admitting role ONLY to the governance issuer. This is
// the anchor: docs/awareness/actor_roles.yaml currently trusts sensei.local for
// the repair/maintainer roles, and `sensei identity enroll` self-issues as
// sensei.local with caller-chosen roles. An admitting role that trusted it
// would let anyone who can run sensei in the checkout mint authority.
func testIndex() authority.PolicyIndex {
	return authority.PolicyIndex{
		ActorRoles: map[string]authority.ActorRole{
			admittingRole: {
				ID:                admittingRole,
				Status:            "active",
				TrustedIssuers:    []string{governedIssuer},
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent},
			},
			"role.repository_repair_agent": {
				ID:                "role.repository_repair_agent",
				Status:            "active",
				TrustedIssuers:    []string{governedIssuer, localIssuer},
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent},
			},
		},
	}
}

func testContext(b *bundle) Context {
	return Context{
		GraphDigest: testGraphDigest,
		EvaluatedAt: time.Date(2026, 8, 14, 13, 0, 0, 0, time.UTC),
		Index:       testIndex(),
		Resolver:    b.resolver(),
	}
}

func boundReceipt() adoption.Receipt {
	return adoption.Receipt{
		ValidForRevision:    testRevision,
		ValidForGraphDigest: testGraphDigest,
	}
}

func governedRecord(id string) Record {
	return Record{Identity: id, Disposition: DispositionGoverned, Receipt: boundReceipt()}
}

func manifest(b *testing.T, bun *bundle, records ...Record) Manifest {
	b.Helper()
	return Manifest{
		SchemaVersion: SchemaVersion,
		PolicyID:      "knowledge.admission.v1",
		AdmittingRole: admittingRole,
		ActorBinding:  bun.binding(b, governedIssuer, []string{admittingRole}),
		Records:       records,
	}
}

// ── #166 acceptance test 1 ────────────────────────────────────────────────
// Candidate knowledge remains discoverable but is not authoritative-projection-
// required.
func TestCandidateIsDiscoverableButNotAuthoritative(t *testing.T) {
	b := newBundle(t)
	m := manifest(t, b,
		governedRecord("invariant.real.one"),
		Record{Identity: "candidate.invariant.authority.abort", Disposition: DispositionCandidate},
	)
	admitted, err := verify(m, testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if admitted.IsAuthoritativelyAdmitted("candidate.invariant.authority.abort") {
		t.Fatal("candidate knowledge was treated as authoritative")
	}
	// Still discoverable: the decision exists and says "candidate".
	d, ok := admitted.Disposition("candidate.invariant.authority.abort")
	if !ok || d != DispositionCandidate {
		t.Fatalf("disposition = %q (present=%v), want candidate", d, ok)
	}
	if !admitted.IsAuthoritativelyAdmitted("invariant.real.one") {
		t.Fatal("governed knowledge was not authoritative")
	}
}

// ── #166 acceptance test 2 ────────────────────────────────────────────────
// Generating new candidates does not by itself degrade an otherwise
// authoritative repository. This is the self-defeating onboarding loop #166
// opens with: `sensei import --refresh` writes candidates, and closure must not
// start failing because of them.
func TestGeneratingCandidatesDoesNotDegradeAuthority(t *testing.T) {
	b := newBundle(t)
	before, err := verify(manifest(t, b, governedRecord("invariant.real.one")), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	records := []Record{governedRecord("invariant.real.one")}
	for _, id := range []string{"candidate.invariant.authority.aa", "candidate.invariant.authority.ab", "candidate.invariant.authority.abort"} {
		records = append(records, Record{Identity: id, Disposition: DispositionCandidate})
	}
	after, err := verify(manifest(t, b, records...), testContext(b))
	if err != nil {
		t.Fatalf("verify after candidate generation: %v", err)
	}

	if got, want := len(after.GovernedIdentities()), len(before.GovernedIdentities()); got != want {
		t.Fatalf("governed set changed from %d to %d when only candidates were added", want, got)
	}
}

// ── #166 acceptance test 3 ────────────────────────────────────────────────
// Owner-governed adoption/promotion makes that knowledge authoritative-
// projection-required.
func TestOwnerGovernedPromotionConfersAuthority(t *testing.T) {
	b := newBundle(t)
	id := "invariant.promoted.one"

	candidate, err := verify(manifest(t, b, Record{Identity: id, Disposition: DispositionCandidate}), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if candidate.IsAuthoritativelyAdmitted(id) {
		t.Fatal("candidate was authoritative before promotion")
	}

	promoted, err := verify(manifest(t, b, governedRecord(id)), testContext(b))
	if err != nil {
		t.Fatalf("verify promoted: %v", err)
	}
	if !promoted.IsAuthoritativelyAdmitted(id) {
		t.Fatal("owner-governed promotion did not confer authority")
	}
}

// ── #166 acceptance test 5 ────────────────────────────────────────────────
// Moving candidate content between directories does not alter its authority
// disposition.
//
// The admission record is keyed by stable identity and carries no path at all,
// so this holds structurally. The test pins that: if anyone reintroduces a path
// into Record, they have to delete this test to do it.
func TestPathIsNotPartOfTheAuthorityDecision(t *testing.T) {
	b := newBundle(t)
	id := "invariant.hb.probe.minted_from_candidates_dir"
	admitted, err := verify(manifest(t, b, Record{Identity: id, Disposition: DispositionCandidate}), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if admitted.IsAuthoritativelyAdmitted(id) {
		t.Fatal("candidate became authoritative")
	}
	for _, field := range structFieldNames(Record{}) {
		if strings.Contains(strings.ToLower(field), "path") || strings.Contains(strings.ToLower(field), "file") {
			t.Fatalf("Record carries %q — authority must not depend on location", field)
		}
	}
}

// ── #166 acceptance test 6 — LOAD-BEARING ─────────────────────────────────
// Editing a caller-controlled status: field cannot manufacture governing
// authority.
//
// The receipt is inlined into the record, so `status:` and `promotion_status:`
// are literally editable in the same document. They must not matter: authority
// comes from the verified binding plus the disposition, and nothing else.
func TestEditingCallerControlledStatusCannotManufactureAuthority(t *testing.T) {
	b := newBundle(t)
	id := "candidate.invariant.authority.abort"

	forged := Record{Identity: id, Disposition: DispositionCandidate, Receipt: adoption.Receipt{
		Status:              adoption.PromotionMachineAdopted,
		PromotionStatus:     "adopted",
		EpistemicStatus:     "supported",
		ReviewStatus:        "human_reviewed",
		DecisionActor:       "actor.test.session-1",
		DecisionPolicy:      "knowledge.admission.v1",
		DecisionTimestamp:   "2026-08-14T12:00:00Z",
		ValidForRevision:    testRevision,
		ValidForGraphDigest: testGraphDigest,
	}}

	admitted, err := verify(manifest(t, b, forged), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if admitted.IsAuthoritativelyAdmitted(id) {
		t.Fatal("a caller-editable status field manufactured governing authority")
	}
}

// The same property one level up: the whole manifest is a file someone can
// write. Without a binding that verifies against governed policy, NOTHING is
// admitted — the records are not partially believed.
func TestManifestWithoutVerifiableProvenanceAdmitsNothing(t *testing.T) {
	b := newBundle(t)
	m := manifest(t, b, governedRecord("invariant.real.one"))
	m.ActorBinding.RoleAttestationReceiptDigests = []string{strings.Repeat("ab", 32)}

	if _, err := verify(m, testContext(b)); err == nil {
		t.Fatal("manifest with an unresolvable role attestation was accepted")
	}
}

// The anchor itself. A locally enrolled identity self-issues as sensei.local
// with roles of its own choosing, so the admitting role must not trust it.
func TestLocallyEnrolledIssuerCannotAdmitKnowledge(t *testing.T) {
	b := newBundle(t)
	m := Manifest{
		SchemaVersion: SchemaVersion,
		PolicyID:      "knowledge.admission.v1",
		AdmittingRole: admittingRole,
		ActorBinding:  b.binding(t, localIssuer, []string{admittingRole}),
		Records:       []Record{governedRecord("invariant.real.one")},
	}
	if _, err := verify(m, testContext(b)); err == nil {
		t.Fatal("a locally enrolled identity admitted knowledge")
	}
}

// Holding SOME verified role is not holding the admitting role.
func TestActorWithoutAdmittingRoleCannotAdmit(t *testing.T) {
	b := newBundle(t)
	m := Manifest{
		SchemaVersion: SchemaVersion,
		PolicyID:      "knowledge.admission.v1",
		AdmittingRole: admittingRole,
		ActorBinding:  b.binding(t, governedIssuer, []string{"role.repository_repair_agent"}),
		Records:       []Record{governedRecord("invariant.real.one")},
	}
	if _, err := verify(m, testContext(b)); err == nil {
		t.Fatal("an actor without the admitting role admitted knowledge")
	}
}

// ── #166 acceptance test 7 ────────────────────────────────────────────────
// Rejected, stale, superseded and other non-governing dispositions remain
// outside the required authority set.
func TestNonGoverningDispositionsStayOutsideTheAuthoritySet(t *testing.T) {
	b := newBundle(t)
	records := []Record{governedRecord("invariant.real.one")}
	for id, d := range map[string]Disposition{
		"invariant.rejected.one":   DispositionRejected,
		"invariant.stale.one":      DispositionStale,
		"invariant.superseded.one": DispositionSuperseded,
		"invariant.candidate.one":  DispositionCandidate,
	} {
		records = append(records, Record{Identity: id, Disposition: d, Receipt: boundReceipt()})
	}
	admitted, err := verify(manifest(t, b, records...), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got := admitted.GovernedIdentities(); len(got) != 1 || got[0] != "invariant.real.one" {
		t.Fatalf("governed set = %v, want only invariant.real.one", got)
	}
}

// Contextual binding. A real past decision must not be replayed over changed
// knowledge — but binding alone is not provenance, which is what the issuer and
// role tests above cover.
func TestStaleGraphDigestIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Record)
	}{
		{"stale graph digest", func(r *Record) { r.Receipt.ValidForGraphDigest = strings.Repeat("b", 64) }},
		{"absent binding", func(r *Record) { r.Receipt = adoption.Receipt{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBundle(t)
			rec := governedRecord("invariant.real.one")
			tc.mutate(&rec)
			if _, err := verify(manifest(t, b, rec), testContext(b)); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

// Two decisions about one identity is an unresolved disagreement. Picking
// either silently would invent an answer nobody recorded.
func TestDuplicateIdentityIsRejected(t *testing.T) {
	b := newBundle(t)
	m := manifest(t, b,
		governedRecord("invariant.real.one"),
		Record{Identity: "invariant.real.one", Disposition: DispositionRejected},
	)
	if _, err := verify(m, testContext(b)); err == nil {
		t.Fatal("duplicate admission records were accepted")
	}
}

// Absence is not a disposition. "Nobody ruled on this" and "this was ruled a
// candidate" are different states, and collapsing them would let unreviewed
// knowledge inherit a decision it never received.
func TestUnknownIdentityIsDistinctFromCandidate(t *testing.T) {
	b := newBundle(t)
	admitted, err := verify(manifest(t, b, governedRecord("invariant.real.one")), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, ok := admitted.Disposition("invariant.never.mentioned"); ok {
		t.Fatal("an unmentioned identity reported a disposition")
	}
	if admitted.IsAuthoritativelyAdmitted("invariant.never.mentioned") {
		t.Fatal("an unmentioned identity was authoritative")
	}
}

// The trust boundary itself: nothing must be admissible on the strength of a
// receipt naming a trusted issuer. Policy trusting the STRING
// "sensei.governance" is not sensei.governance having issued anything.
//
// This hand-authors both receipts with issuer: sensei.governance, computes every
// structural digest correctly, and supplies a correctly hashed authentication
// artifact. Nothing is malformed — it is exactly what a caller with write access
// to the repository and bundle can produce, because EnrollOptions.Issuer is
// caller-supplied and sensei.local is only a default.
//
// It was red until issuer authenticity existed. What closes it is not a better
// string check: the caller still writes whatever issuer it likes, and still
// cannot produce a signature for a governance key it does not hold. The private
// key lives outside the checkout, so repository mutation alone is insufficient.
func TestSelfAuthoredGovernanceIssuerCannotAdmitKnowledge(t *testing.T) {
	b := newBundle(t)
	s := newSigner(t)
	m := Manifest{
		SchemaVersion: SchemaVersion,
		PolicyID:      "knowledge.admission.v1",
		AdmittingRole: admittingRole,
		// Self-authored, claiming the governance issuer.
		ActorBinding: b.binding(t, governedIssuer, []string{admittingRole}),
		Records:      []Record{governedRecord("invariant.real.one")},
	}

	// The caller signs with its own key, since it does not hold a governance one.
	forged := newSigner(t).sign(t, m)

	admitted, prov, err := VerifySigned(forged, s.trustStore(testKeyID, "active"), testContext(b))
	if err == nil && admitted.IsAuthoritativelyAdmitted("invariant.real.one") {
		t.Fatal("a self-authored receipt claiming issuer=sensei.governance admitted knowledge: " +
			"policy trusts the issuer STRING, and nothing authenticates the issuer")
	}
	if prov.Verification == changebinding.ProvenanceVerified {
		t.Fatal("self-authored manifest reported ProvenanceVerified")
	}
}

// Admission binds to the graph digest, NOT to a git revision. A commit that
// changes only code must not invalidate knowledge that did not change —
// otherwise every unrelated commit would force all 485 baseline identities to be
// re-signed, and a committed manifest naming its own HEAD would be stale the
// instant it landed.
func TestAdmissionSurvivesACodeOnlyRevisionChange(t *testing.T) {
	b := newBundle(t)
	rec := governedRecord("invariant.real.one")
	// Whatever revision this decision was made at, the knowledge is unchanged.
	rec.Receipt.ValidForRevision = "0000000000000000000000000000000000000000"

	admitted, err := verify(manifest(t, b, rec), testContext(b))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !admitted.IsAuthoritativelyAdmitted("invariant.real.one") {
		t.Fatal("a code-only revision change invalidated unchanged knowledge")
	}
}

// The corresponding negative: when the KNOWLEDGE changes, the graph digest moves
// and the decision is stale.
func TestAdmissionDoesNotSurviveAKnowledgeChange(t *testing.T) {
	b := newBundle(t)
	ctx := testContext(b)
	ctx.GraphDigest = strings.Repeat("f", 64) // corpus changed under the decision

	if _, err := verify(manifest(t, b, governedRecord("invariant.real.one")), ctx); err == nil {
		t.Fatal("a decision made for a different corpus was still authoritative")
	}
}
