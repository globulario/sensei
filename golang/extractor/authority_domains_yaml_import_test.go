// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"bytes"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

func authorityDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

// Detection: the authority_domains top-level key routes to the new importer.
func TestAuthorityDomains_DetectedByTopLevelKey(t *testing.T) {
	out, report := authorityDir(t, map[string]string{
		"authority_domains.yaml": `
authority_domains:
  - id: authority.example
    label: Example domain
    status: active
    owner_service: example service
    covers_paths:
      - golang/example/example_server/
`,
	})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d (skipped=%d)", len(report.Imported()), len(report.Skipped()))
	}
	if got := report.Imported()[0].Schema; got != "authority_domains" {
		t.Errorf("schema: want authority_domains, got %q", got)
	}
}

// Typed node + every field family emits its dedicated predicate.
func TestAuthorityDomains_FullFieldEmission(t *testing.T) {
	out, _ := authorityDir(t, map[string]string{
		"authority_domains.yaml": `
authority_domains:
  - id: authority.example
    label: Example domain
    status: active
    truth_layer: repository
    owner_service: example service
    covers_paths:
      - golang/example/example_server/
    owns_state:
      - example manifest
    may_write:
      - example service via workflow
    may_write_role_ids:
      - role.repository_repair_agent
    may_read:
      - controller
    may_read_role_ids:
      - role.repository_reader
    must_mutate_via:
      - example typed RPC
    must_mutate_via_ids:
      - mutation_path.repository_edit
    must_read_via:
      - example resolver RPC
    must_read_via_ids:
      - observation_path.repository_read
    observes_via:
      - example probe
    observes_via_ids:
      - observation_path.repository_probe
    forbids_bypass:
      - object presence as truth
    evidence_freshness: must be fresher than one sweep
    notes: |
      Context line.
`,
	})

	subj := rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassAuthorityDomain)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropLabel)+` "Example domain"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropStatus)+` "active"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasTruthLayer)+` "repository"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropOwnerService)+` "example service"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCoversPath)+` "golang/example/example_server/"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropOwnsState)+` "example manifest"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMayWrite)+` "example service via workflow"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMayWriteRoleID)+` "role.repository_repair_agent"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMayRead)+` "controller"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMayReadRoleID)+` "role.repository_reader"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustMutateVia)+` "example typed RPC"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustMutateViaID)+` "mutation_path.repository_edit"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustReadVia)+` "example resolver RPC"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMustReadViaID)+` "observation_path.repository_read"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropObservesVia)+` "example probe"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropObservesViaID)+` "observation_path.repository_probe"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForbidsBypass)+` "object presence as truth"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasEvidenceFreshnessWindow)+` "must be fresher than one sweep"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropComment)+` "Context line."`)
}

// Entries without an id are soft-skipped; the rest of the file still imports.
func TestAuthorityDomains_EmptyIDEntrySoftSkipped(t *testing.T) {
	out, report := authorityDir(t, map[string]string{
		"authority_domains.yaml": `
authority_domains:
  - label: nameless — must not emit
    owner_service: ghost service
  - id: authority.named
    label: Named domain
`,
	})

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	named := rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.named")
	mustContain(t, out, named+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassAuthorityDomain)+" .")
	mustNotContain(t, out, `"ghost service"`)
}
