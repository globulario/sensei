// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

func TestArchitectureClaimSchemaDetected(t *testing.T) {
	_, report := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	if len(report.Imported()) != 1 || report.Imported()[0].Schema != "architecture_claims" {
		t.Fatalf("schema not detected/imported: %+v", report.Files)
	}
}

func TestArchitectureClaimImporterEmitsClaimType(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	requireTriplePart(t, nt, rdf.IRI(rdf.ClassArchitectureClaim))
}

func TestArchitectureClaimImporterEmitsFixedStatementLiterals(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	for _, prop := range []string{rdf.PropClaimSubject, rdf.PropClaimPredicate, rdf.PropClaimObject} {
		requireTriplePart(t, nt, rdf.IRI(prop))
	}
	if strings.Contains(nt, "mutates_state>") {
		t.Fatal("claim predicate was minted as an RDF predicate")
	}
}

func TestArchitectureClaimImporterEmitsEvidenceEdges(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, strings.Replace(validArchitectureClaimYAML(), "supporting_evidence: []", "supporting_evidence:\n        - evidence:ev.support", 1)+evidenceYAML())
	requireTriplePart(t, nt, rdf.IRI(rdf.PropSupportedByEvidence))
}

func TestArchitectureClaimImporterEmitsClaimDependencies(t *testing.T) {
	nt, report := importArchitectureClaimFixture(t, validDependentClaimsYAML())
	if len(report.Imported()) != 1 {
		t.Fatalf("dependent fixture was not imported: %+v", report.Files)
	}
	requireTriplePart(t, nt, rdf.IRI(rdf.PropDependsOnClaim))
}

func TestArchitectureClaimImporterEmitsOneWayAnchorsOnly(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	if !strings.Contains(nt, rdf.IRI(rdf.PropAnchoredIn)) {
		t.Fatal("missing claim anchoredIn edge")
	}
	if strings.Contains(nt, rdf.IRI(rdf.PropImplements)+" "+rdf.MintIRI(rdf.ClassArchitectureClaim, "claim.repository_publish_mutates_package_identity")) {
		t.Fatal("claim importer emitted reverse implements edge")
	}
}

func TestArchitectureClaimImporterRejectsMalformedDocument(t *testing.T) {
	_, report := importArchitectureClaimFixture(t, strings.ReplaceAll(validArchitectureClaimYAML(), "predicate: mutates_state", "predicate: \"bad predicate\""))
	if report.Files[0].Status != extractor.StatusInvalid {
		t.Fatalf("malformed claim imported: %+v", report.Files)
	}
}

func TestArchitectureClaimImporterRejectsAuthoredOrPromotedOrigin(t *testing.T) {
	_, report := importArchitectureClaimFixture(t, strings.Replace(validArchitectureClaimYAML(), "assertion_origin: derived", "assertion_origin: authored", 1))
	if report.Files[0].Status != extractor.StatusInvalid {
		t.Fatalf("authored origin imported: %+v", report.Files)
	}
}

func TestArchitectureClaimImporterNeverTypesReferencedTarget(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	target := rdf.MintIRI(rdf.ClassComponent, "component.repository")
	if strings.Contains(nt, target+" "+rdf.IRI(rdf.PropType)) {
		t.Fatal("about_node target was typed by claim importer")
	}
}

func TestArchitectureClaimImporterEmitsCandidateAndGeneratedSourceKind(t *testing.T) {
	nt, _ := importArchitectureClaimFixture(t, validArchitectureClaimYAML())
	requireTriplePart(t, nt, rdf.IRI(rdf.PropPromotionStatus)+" "+rdf.Lit("candidate"))
	requireTriplePart(t, nt, rdf.IRI(rdf.PropSourceKind)+" "+rdf.Lit("generated_candidate"))
}

func importArchitectureClaimFixture(t *testing.T, content string) (string, *extractor.ImportReport) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claims.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	em, report, err := extractor.ImportAwarenessDir(dir, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := em.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String(), report
}

func requireTriplePart(t *testing.T, nt, want string) {
	t.Helper()
	if !strings.Contains(nt, want) {
		t.Fatalf("missing %s in\n%s", want, nt)
	}
}

func validArchitectureClaimYAML() string {
	return `architecture_claims:
  schema_version: "1"
  generated_by: sensei architecture inference
  binding:
    repository_domain: github.com/example/project
    revision: "0123456789abcdef"
    revision_status: resolved
    graph_digest_sha256: "abcdef0123456789"
    graph_digest_status: resolved
  fact_receipts:
    - fact:
        id: fact.123456789abc
        kind: authority_observation
        subject: repository.Publish
        predicate: mutates_state
        object: package_identity
        scope:
          repository: github.com/example/project
          files: [golang/repository/repository.go]
          symbols: [repository.Publish]
        evidence:
          source_file: golang/repository/repository.go
          line_start: 120
          line_end: 141
        confidence: 0.55
        extractor: go_authority_extractor
      provenance:
        repository_domain: github.com/example/project
        repository_domain_status: resolved
        revision: "0123456789abcdef"
        revision_status: resolved
        source_digest: "0123456789abcdef"
        source_digest_status: resolved
        source_kind: source_file
  claims:
    - id: claim.repository_publish_mutates_package_identity
      label: Repository publish mutates package identity
      description: A non-authoritative proposition derived from the cited source fact.
      statement:
        subject: repository.Publish
        predicate: mutates_state
        object: package_identity
      scope:
        repo: github.com/example/project
        domain: repo
        files: [golang/repository/repository.go]
        symbols: [repository.Publish]
        components: [component.repository]
      architectural_plane: observed
      assertion_origin: derived
      epistemic_status: supported
      inference_rule: rule.direct_observation_projection
      premise_facts: [fact.123456789abc]
      depends_on_claims: []
      supporting_evidence: []
      refuting_evidence: []
      conflicts_with: []
      about_nodes: [component:component.repository]
      alternative_explanations:
        - The observed mutation does not prove sole ownership.
      unknowns:
        - Other writers may exist outside the inspected source set.
      invalidation_conditions:
        - The source digest changes.
      confidence: 0.55
      freshness: current
      human_review_required: true
      promotion_status: candidate
`
}

func validDependentClaimsYAML() string {
	out := strings.Replace(validArchitectureClaimYAML(), "  claims:\n    - id: claim.repository_publish_mutates_package_identity", `  claims:
    - id: claim.dep
      label: Dependency
      statement:
        subject: repository.Publish
        predicate: mutates_state
        object: package_identity
      scope:
        repo: github.com/example/project
        domain: repo
        files: [golang/repository/repository.go]
        symbols: [repository.Publish]
      architectural_plane: observed
      assertion_origin: derived
      epistemic_status: supported
      inference_rule: rule.direct_observation_projection
      premise_facts: [fact.123456789abc]
      invalidation_conditions: [source digest changes]
      confidence: 0.55
      freshness: current
      human_review_required: true
      promotion_status: candidate
    - id: claim.repository_publish_mutates_package_identity`, 1)
	return strings.Replace(out, "depends_on_claims: []", "depends_on_claims: [claim.dep]", 1)
}

func evidenceYAML() string {
	return `
evidence:
  - id: ev.support
    name: Supporting evidence
    kind: review
    status: pass
`
}
