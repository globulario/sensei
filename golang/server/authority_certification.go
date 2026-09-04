// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/seedmeta"
)

// Certification answers "is the generation this server serves for this domain
// one that a publication actually certified".
//
// TWO MECHANISMS, ORDERED — NOT INTERCHANGEABLE EVIDENCE.
//
// The per-domain publication receipt is the stronger record by construction:
// its identity is a digest over every field it carries, the server RECOMPUTES
// that identity and refuses a receipt found at any other IRI, and the
// generation commits to the receipt by containing it. Nothing in that pair can
// be changed without breaking the other.
//
// The v1 transaction stamp is weaker and narrower. Its authored model is the
// awareness-graph/services pair — the proto names certified_awareness_graph_commit
// and certified_services_repo_commit, and buildTransactionTSV is hardwired
// around agRepo and svcRepo — and its evaluator checks only that the seed digest
// and triple count agree. It remains valid where that model applies and is not
// a general domain-publication certificate.
//
// So the weaker mechanism is admissible ONLY where the stronger one has nothing
// to say. A plain OR would let a matching stamp route around a publication
// record that is actively reporting a problem, which is the weaker evidence
// overruling the stronger precisely when the stronger one matters most.
//
// resolveCurrentPublication already refuses to collapse ABSENT into UNREADABLE:
// "no publication has been recorded" and "one exists and this reader cannot
// vouch for it" are different worlds. This preserves that distinction one layer
// up, where it decides whether a fallback exists at all.

// standaloneRepository is the sentinel the self-only build writes for a
// topology that genuinely has no paired services checkout. It is a STATED fact,
// which is what separates it from "missing" — the value appendRepoState emits
// when it could not resolve a repository at all.
const standaloneRepository = "standalone"

// missingRepository is what the v1 writer emits for a repository it could not
// resolve. For a project domain it emits this for BOTH slots while the seed
// digest still agrees.
const missingRepository = "missing"

// legacyCrossRepoDomains is the closed set of domains the v1 transaction format
// was authored to describe, read by membership.
//
// Applicability is a property of the topology, never of whether a stamp happens
// to carry two labels. A stamp is a claim; this is the question of whether the
// format can make a true claim about this domain at all.
var legacyCrossRepoDomains = map[string]bool{
	"github.com/globulario/sensei":   true,
	"github.com/globulario/services": true,
	"globular":                       true,
}

// certifyPublication is the certification conjunct for one referent.
//
// It returns the resolved publication so the caller can project the same read
// the verdict rests on, rather than resolving a second time.
func certifyPublication(
	ctx context.Context,
	s *server,
	effectiveDomain string,
	snap graphFreshnessSnapshot,
	stamp seedmeta.TransactionStamp,
	transactionMatchesSeed bool,
	transactionDetail string,
) (bool, string, *awarenesspb.DomainPublication) {
	domain := strings.TrimSpace(effectiveDomain)
	if domain == "" {
		return false, "this server declares no domain, so nothing can certify what it serves", nil
	}

	pub := resolveCurrentPublication(ctx, s, domain)
	switch pub.GetResolution() {
	case awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED:
		ok, why := certifiesThisGeneration(pub, domain, snap)
		return ok, why, pub

	case awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT:
		// Nothing was published for this domain, so there is no stronger record
		// to route around. This is the only door the legacy path fits through.
		applicable, why := legacyTransactionApplies(domain, stamp)
		if !applicable {
			return false, "no publication receipt exists for " + domain + ", and " + why, pub
		}
		if !transactionMatchesSeed {
			return false, "no publication receipt exists for " + domain +
				", and its legacy transaction certification: " + transactionDetail, pub
		}
		return true, "the legacy cross-repo transaction certifies this publication", pub

	case awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE:
		// Publication evidence exists and cannot be vouched for. A legacy stamp
		// does not get to answer over it.
		return false, "the publication evidence for " + domain +
			" exists and could not be verified (" + pub.GetDetail() +
			"), and no weaker certification may stand in for it", pub

	default:
		return false, "no publication resolution was reached for " + domain +
			", which is not the same as none existing", pub
	}
}

// certifiesThisGeneration checks what a VERIFIED receipt still has to satisfy:
// it must answer for this referent, describe this generation, and be entitled
// to claim its revision.
func certifiesThisGeneration(pub *awarenesspb.DomainPublication, domain string, snap graphFreshnessSnapshot) (bool, string) {
	// THE DOMAIN IS NOT RE-CHECKED HERE, ON PURPOSE.
	//
	// resolveCurrentPublication already refuses a pointer that resolves to a
	// receipt for another domain -- it returns UNREADABLE, so no VERIFIED
	// publication can carry a foreign domain. Restating that here would be a
	// second place the same invariant lives, and the referent this conjunct
	// asks about is structurally the one closure asks about: both take
	// effectiveDomain, computed once. A duplicate check would read as diligence
	// while being unreachable, which is how an invariant quietly acquires two
	// homes and then two behaviours.
	if got := strings.TrimSpace(pub.GetSnapshotGeneration()); got != "" &&
		!strings.EqualFold(got, strings.TrimSpace(snap.verification.Live.Digest)) {
		return false, "the publication was read from generation " + shortDigest(got) +
			" while this verdict describes " + shortDigest(snap.verification.Live.Digest)
	}
	// ONLY CLEAN_EXACT MAY CLAIM ITS REVISION.
	//
	// This is the receipt's own authored contract, not a policy added here:
	// DIRTY records a revision because it is useful and must never be read as
	// "produced from that commit", and UNKNOWN is what the PUBLISHER downgrades
	// to when the checkout moved between compilation and publication or the
	// consumed bytes could not be proven against the revision. Certifying
	// either would turn an accurately recorded limitation into authority, and
	// would defeat a downgrade the publisher applied on purpose.
	if state := strings.TrimSpace(pub.GetSourceState()); state != string(publication.CleanExact) {
		return false, "the current publication is " + state +
			", and only CLEAN_EXACT may be read as produced from exactly its named revision"
	}
	return true, "the current publication receipt certifies this generation"
}

// legacyTransactionApplies reports whether the v1 stamp can make a true claim
// about this domain at all.
func legacyTransactionApplies(domain string, stamp seedmeta.TransactionStamp) (bool, string) {
	if !legacyCrossRepoDomains[strings.TrimSpace(domain)] {
		return false, "the v1 transaction stamp certifies the awareness-graph/services pair " +
			"its format is authored around, which cannot make a claim about " + domain
	}
	if !namesRepository(stamp.AwarenessGraphCommit) {
		return false, "its v1 transaction stamp names no awareness-graph repository identity"
	}
	// A standalone build states that it has no paired services checkout. That
	// is an answer; "missing" is the absence of one.
	if !namesRepository(stamp.ServicesCommit) &&
		strings.TrimSpace(stamp.ServicesCommit) != standaloneRepository {
		return false, "its v1 transaction stamp names no services repository identity"
	}
	return true, ""
}

// namesRepository reads the closed set of non-identities by membership, so a
// sentinel nobody anticipated is not silently accepted as a commit.
func namesRepository(v string) bool {
	switch strings.TrimSpace(v) {
	case "", missingRepository, standaloneRepository:
		return false
	}
	return true
}

// serverHomeDomain is the server's own declared domain, or empty.
func serverHomeDomain(s *server) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.homeDomain)
}
