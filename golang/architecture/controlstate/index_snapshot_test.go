// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func catalog(reg Registry, n int) CatalogSnapshot {
	classes := []string{rdf.ClassContract, rdf.ClassInvariant, rdf.ClassComponent, rdf.ClassBoundary}
	var arts []ArtifactSummary
	for i := 0; i < n; i++ {
		cls := classes[i%len(classes)]
		p, _ := reg.classByIRI(cls)
		iri := fmt.Sprintf("aw:node/%03d", i)
		arts = append(arts, ArtifactSummary{
			Identity: ArtifactIdentity{NodeIRI: iri, CanonicalClass: cls, ObservedClasses: []string{cls}, RepositoryIdentity: tRepo, DomainIdentity: tRepo, GraphAuthorityIdentity: tAuth},
			Label:    iri, Family: p.Family, Class: cls, Coverage: p.Coverage, Lifecycle: LifecycleUnknown,
			Closure: ClosureUnknown, Availability: AvailabilityAvailable,
		})
	}
	regDigest, _ := reg.Digest()
	return CatalogSnapshot{RepositoryIdentity: tRepo, DomainIdentity: tRepo, GraphAuthorityIdentity: tAuth, SnapshotIdentity: "snap-1", RegistryDigest: regDigest,
		Source:          SourceStatus{Owner: "controlstate.catalog", Schema: "catalog_enumeration", Identity: "snap-1", Availability: SourceAvailable, Impact: ImpactPrimary},
		AuthoritySource: SourceStatus{Owner: "graph_authority", Schema: "graph_authority", Identity: tAuth, Availability: SourceAvailable, Impact: ImpactRequired},
		DiscoverySource: SourceStatus{Owner: "controlstate.catalog", Schema: "unclassified_discovery", Identity: "snap-1", Availability: SourceAvailable, Impact: ImpactRelevant},
		Artifacts:       arts}
}

func indexReq(pageSize int) ArtifactIndexRequest {
	return ArtifactIndexRequest{RepositoryIdentity: tRepo, Domain: tRepo, PageSize: pageSize}
}

func TestArtifactIndex_StablePaginationAndCursor(t *testing.T) {
	reg := DefaultRegistry()
	cat := catalog(reg, 250)
	page1, err := BuildArtifactIndex(reg, indexReq(100), cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Page) != 100 || !page1.Truncated || page1.NextCursor == "" {
		t.Fatalf("page1 shape wrong: len=%d trunc=%v cursor=%v", len(page1.Page), page1.Truncated, page1.NextCursor != "")
	}
	again, _ := BuildArtifactIndex(reg, indexReq(100), cat)
	if again.DigestSHA256 != page1.DigestSHA256 {
		t.Fatal("index is not deterministic")
	}
	req2 := indexReq(100)
	req2.Cursor = page1.NextCursor
	page2, err := BuildArtifactIndex(reg, req2, cat)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range page1.Page {
		seen[a.Identity.NodeIRI] = true
	}
	for _, a := range page2.Page {
		if seen[a.Identity.NodeIRI] {
			t.Fatalf("page 2 overlaps page 1 at %q", a.Identity.NodeIRI)
		}
	}
}

func TestArtifactIndex_CursorReplayAgainstDifferentSnapshotFails(t *testing.T) {
	reg := DefaultRegistry()
	page1, _ := BuildArtifactIndex(reg, indexReq(50), catalog(reg, 120))
	other := catalog(reg, 120)
	other.SnapshotIdentity = "snap-2"
	req := indexReq(50)
	req.Cursor = page1.NextCursor
	if _, err := BuildArtifactIndex(reg, req, other); err == nil {
		t.Fatal("cursor bound to snap-1 must be rejected against snap-2")
	}
}

func TestArtifactIndex_PageSizeFailsClosedAboveMax(t *testing.T) {
	reg := DefaultRegistry()
	if _, err := BuildArtifactIndex(reg, indexReq(maxPageSize+1), catalog(reg, 10)); err == nil {
		t.Fatal("page size above the maximum must fail closed")
	}
	idx, err := BuildArtifactIndex(reg, indexReq(0), catalog(reg, 10))
	if err != nil || len(idx.Page) != 10 {
		t.Fatalf("non-positive page size should default: %v len=%d", err, len(idx.Page))
	}
}

func TestArtifactIndex_ScopeAndFilterValidation(t *testing.T) {
	reg := DefaultRegistry()
	// Foreign-repository artifact rejected.
	cat := catalog(reg, 4)
	cat.Artifacts[0].Identity.RepositoryIdentity = "github.com/other/repo"
	if _, err := BuildArtifactIndex(reg, indexReq(10), cat); err == nil {
		t.Fatal("foreign-repository catalog artifact must be rejected")
	}
	// Non-empty search text rejected.
	req := indexReq(10)
	req.SearchText = "foo"
	if _, err := BuildArtifactIndex(reg, req, catalog(reg, 4)); err == nil {
		t.Fatal("non-empty search text must be rejected")
	}
	// Invalid filters rejected.
	req2 := indexReq(10)
	req2.FamilyFilter = "no-such-family"
	if _, err := BuildArtifactIndex(reg, req2, catalog(reg, 4)); err == nil {
		t.Fatal("invalid family filter must be rejected")
	}
	req3 := indexReq(10)
	req3.ClosureFilter = "made_up"
	if _, err := BuildArtifactIndex(reg, req3, catalog(reg, 4)); err == nil {
		t.Fatal("invalid closure filter must be rejected")
	}
}

func TestArtifactIndex_GlobalDuplicateSplitAcrossPageBoundary(t *testing.T) {
	reg := DefaultRegistry()
	cat := catalog(reg, 3)
	// Force a global duplicate identity that (after ordering) could land on different pages.
	dup := cat.Artifacts[0]
	cat.Artifacts = append(cat.Artifacts, dup)
	if _, err := BuildArtifactIndex(reg, indexReq(2), cat); err == nil {
		t.Fatal("global duplicate identity must be rejected before pagination")
	}
}

func TestControlSnapshot_MissingSourceIsUnknownNotZero(t *testing.T) {
	reg := DefaultRegistry()
	snap, err := BuildControlSnapshot(reg, ControlSnapshotInput{
		RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.OpenQuestions != nil || snap.Contradictions != nil {
		t.Fatal("unobserved counts must be nil (unknown), not zero")
	}
	// A missing relevant source makes the snapshot partial.
	if snap.Availability != AvailabilityPartial {
		t.Fatalf("missing relevant source → partial, got %q", snap.Availability)
	}
	// When supplied available, zero is real data.
	snap2, _ := BuildControlSnapshot(reg, ControlSnapshotInput{
		RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention:       AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		OpenQuestions:   &CountObservation{Owner: "questiondisposition", Schema: "oq", Identity: "count.oq", Availability: SourceAvailable, Count: 0},
		Contradictions:  &CountObservation{Owner: "extractor.contradiction", Schema: "c", Identity: "count.c", Availability: SourceAvailable, Count: 0},
		MissingEvidence: &CountObservation{Owner: "closure.evidence", Schema: "e", Identity: "count.e", Availability: SourceAvailable, Count: 0}, MissingTests: &CountObservation{Owner: "closure.verification", Schema: "t", Identity: "count.t", Availability: SourceAvailable, Count: 0}, MissingEnforcement: &CountObservation{Owner: "closure.enforcement", Schema: "en", Identity: "count.en", Availability: SourceAvailable, Count: 0},
		Coverage:  &CoverageObservation{Owner: "coverage", Schema: "cov", Identity: "coverage.summary", Availability: SourceAvailable},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth},
	})
	if snap2.OpenQuestions == nil || *snap2.OpenQuestions != 0 {
		t.Fatal("an observed zero must be present as data")
	}
	if snap2.Availability != AvailabilityAvailable {
		t.Fatalf("all sources available → available, got %q", snap2.Availability)
	}
}

func TestControlSnapshot_MissingAuthoritySourceDegradesAvailability(t *testing.T) {
	reg := DefaultRegistry()
	snap, _ := BuildControlSnapshot(reg, ControlSnapshotInput{
		RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Authority: GraphAuthoritySummary{Observed: false}, // authority not observed
	})
	if snap.Availability == AvailabilityAvailable {
		t.Fatal("missing graph-authority source must degrade snapshot availability")
	}
}

func TestControlSnapshot_NoFeedbackCollectionNoScore(t *testing.T) {
	reg := DefaultRegistry()
	snap, _ := BuildControlSnapshot(reg, ControlSnapshotInput{
		RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Feedback:  &FeedbackObservation{Owner: "briefingfeedback", Schema: "fb", Identity: "feedback.scope", Availability: SourceAvailable, Context: FeedbackContext{Capable: true, Availability: "feedback_available"}},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth},
	})
	blob, _ := json.Marshal(snap)
	if strings.Contains(string(blob), "verified_record_ids") || strings.Contains(string(blob), "promoted") {
		t.Fatalf("snapshot must not carry promoted-feedback records: %s", blob)
	}
	if strings.Contains(string(blob), "\"score\"") || strings.Contains(strings.ToLower(string(blob)), "certified") {
		t.Fatalf("snapshot must not carry a score or certification claim: %s", blob)
	}
	if snap.FeedbackContext == nil || !snap.FeedbackContext.Capable {
		t.Fatal("feedback context capability should be exposed")
	}
}
