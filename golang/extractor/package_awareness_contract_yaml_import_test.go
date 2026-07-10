// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

func TestPackageAwarenessContract_ImportedAsContract(t *testing.T) {
	out, report := outcomeDir(t, map[string]string{
		"awareness.yaml": `
apiVersion: awareness.globular.io/v1
kind: AwarenessContract
service: cluster-controller
package: cluster-controller
package_kind: service
summary: Controller owns desired state and must mutate through workflows.
owns:
  etcd_keys:
    - /globular/resources/DesiredService/{name}
reads:
  etcd_keys:
    - /globular/system/config
writes:
  etcd_keys:
    - /globular/resources/ServiceRelease/{name}
depends_on:
  - service: etcd
    phase: bootstrap
    required: true
    reason: etcd is sole source of truth.
invariants:
  - controller.all_mutations_via_workflow
forbidden_fixes:
  - inline_state_change_bypassing_workflow
known_failure_modes:
  - id: leader_election_stall
    diagnosis: Check leases.
    remedy: Restore quorum.
safe_degraded_modes:
  - Running services continue.
remediation_workflows:
  - leader-election-recovery
required_tests:
  - TestAllMutationsViaWorkflow
required_permissions:
  - etcd:write:/globular/resources/
admission:
  strict: true
  allow_unknown_dependencies: false
`,
	})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "package_awareness_contract" {
		t.Fatalf("schema: want package_awareness_contract, got %q", got)
	}

	subj := rdf.MintIRI(rdf.ClassContract, "contract.package.cluster-controller.awareness")
	component := rdf.MintIRI(rdf.ClassComponent, "component.package.cluster-controller")
	etcd := rdf.MintIRI(rdf.ClassComponent, "component.package.etcd")
	invariant := rdf.MintIRI(rdf.ClassInvariant, "controller.all_mutations_via_workflow")
	forbidden := rdf.MintIRI(rdf.ClassForbiddenFix, "inline_state_change_bypassing_workflow")
	testRef := rdf.MintIRI(rdf.ClassTest, "TestAllMutationsViaWorkflow")
	fm := rdf.MintIRI(rdf.ClassFailureMode, "leader_election_stall")

	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassContract)+" .")
	mustContain(t, out, component+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassComponent)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropKind)+` "service"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropExposedBy)+" "+component+" .")
	mustContain(t, out, component+" "+rdf.IRI(rdf.PropExposesContract)+" "+subj+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDependsOn)+" "+etcd+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropConstrainedByInvariant)+" "+invariant+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForbids)+" "+forbidden+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresTest)+" "+testRef+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropViolatedBy)+" "+fm+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropOwnsState)+` "owns.etcd=/globular/resources/DesiredService/{name}"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresVerification)+` "writes.etcd=/globular/resources/ServiceRelease/{name}"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresVerification)+` "admission.strict=true; admission.allow_unknown_dependencies=false"`)
}

func TestPackageConfigYAML_IgnoredAsNonAuthority(t *testing.T) {
	_, report := outcomeDir(t, map[string]string{
		"metadata/envoy/config/envoy/envoy.yaml": `
admin:
  address:
    socket_address:
      address: 0.0.0.0
`,
	})

	if len(report.Ignored()) != 1 {
		t.Fatalf("expected 1 ignored file, got %d", len(report.Ignored()))
	}
	if got := report.Ignored()[0].Schema; got != "package_config" {
		t.Fatalf("schema: want package_config, got %q", got)
	}
	if skipped := report.Skipped(); len(skipped) != 0 {
		t.Fatalf("expected no skipped files, got %+v", skipped)
	}
}
