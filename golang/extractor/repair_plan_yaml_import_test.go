// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

func repairDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

const sampleRepairPlan = `
id: globular.repair.example
class: RepairPlan
label: Example repair plan
status: active
confidence: high
blast_radius: cluster
approval_gate: human_approval_required
repairs_finding_classes:
  - example.stuck
repairs_failure_modes:
  - failure_mode:some.failure
applies_to_authority_domains:
  - authority_domain:authority.example
uses_implementation_patterns:
  - implementation_pattern:globular.pattern.example
must_not_violate_invariants:
  - invariant:example.invariant
governs:
  contracts:
    - contract.example
  invariants:
    - example.governing.invariant
  failure_modes:
    - some.other.failure
  forbidden_fixes:
    - forbidden.fix.example
expressed_by:
  files:
    - golang/workflow/engine/engine.go
  symbols:
    - workflow.executeForeach
affected_components:
  - component.workflow.engine
required_tests:
  - test:TestExample
preconditions:
  - check the owner rpc first
repair_steps:
  - do the safe thing
verification_steps:
  - re-check the owner state
rollback_steps:
  - revert through the owner path
notes: |
  Some context.
`

// Detection: id + class:RepairPlan routes to the repair_plan importer and emits
// a typed node with its scalar literals.
func TestRepairPlanYAMLImporter(t *testing.T) {
	out, report := repairDir(t, map[string]string{"r.yaml": sampleRepairPlan})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "repair_plan" {
		t.Errorf("schema: want repair_plan, got %q", got)
	}

	subj := rdf.MintIRI(rdf.ClassRepairPlan, "globular.repair.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassRepairPlan)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropLabel)+` "Example repair plan"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasConfidence)+` "high"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasBlastRadius)+` "cluster"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresApprovalGate)+` "human_approval_required"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresPrecondition)+` "check the owner rpc first"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasRepairStep)+` "do the safe thing"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresVerification)+` "re-check the owner state"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropHasRollbackStep)+` "revert through the owner path"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRepairsFindingClass)+` "example.stuck"`)
}

// The plan links to failure mode, authority domain, implementation pattern,
// invariant, and required test — and does NOT type any of those targets.
func TestRepairPlanLinksToPatternsAuthorityAndTests(t *testing.T) {
	out, _ := repairDir(t, map[string]string{"r.yaml": sampleRepairPlan})
	subj := rdf.MintIRI(rdf.ClassRepairPlan, "globular.repair.example")
	link := func(prop, target string) string {
		return subj + " " + rdf.IRI(prop) + " " + target + " ."
	}
	fm := rdf.MintIRI(rdf.ClassFailureMode, "some.failure")
	ad := rdf.MintIRI(rdf.ClassAuthorityDomain, "authority.example")
	ip := rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.example")
	inv := rdf.MintIRI(rdf.ClassInvariant, "example.invariant")
	test := rdf.MintIRI(rdf.ClassTest, "TestExample")
	contract := rdf.MintIRI(rdf.ClassContract, "contract.example")
	govInv := rdf.MintIRI(rdf.ClassInvariant, "example.governing.invariant")
	govFM := rdf.MintIRI(rdf.ClassFailureMode, "some.other.failure")
	ff := rdf.MintIRI(rdf.ClassForbiddenFix, "forbidden.fix.example")
	file := rdf.MintIRI(rdf.ClassSourceFile, "golang/workflow/engine/engine.go")
	sym := rdf.MintIRI(rdf.ClassCodeSymbol, "workflow.executeForeach")
	comp := rdf.MintIRI(rdf.ClassComponent, "component.workflow.engine")

	mustContain(t, out, link(rdf.PropRepairsFailureMode, fm))
	mustContain(t, out, link(rdf.PropRepairsFailureMode, govFM))
	mustContain(t, out, link(rdf.PropAppliesToAuthorityDomain, ad))
	mustContain(t, out, link(rdf.PropUsesImplementationPattern, ip))
	mustContain(t, out, link(rdf.PropMustNotViolateInvariant, inv))
	mustContain(t, out, link(rdf.PropMustNotViolateInvariant, govInv))
	mustContain(t, out, link(rdf.PropGovernedByContract, contract))
	mustContain(t, out, link(rdf.PropForbids, ff))
	mustContain(t, out, link(rdf.PropAffectsComponent, comp))
	mustContain(t, out, link(rdf.PropRequiresTest, test))
	mustContain(t, out, link(rdf.PropExpressedBy, file))
	mustContain(t, out, link(rdf.PropAnchoredIn, sym))
	mustContain(t, out, file+" "+rdf.IRI(rdf.PropImplements)+" "+subj+" .")

	// Linking is not authoring: the targets are not typed by this importer.
	mustNotContain(t, out, ad+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassAuthorityDomain))
	mustNotContain(t, out, inv+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassInvariant))
}
