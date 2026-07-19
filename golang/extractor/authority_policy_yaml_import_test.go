// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestActorRoles_DetectedByTopLevelKey(t *testing.T) {
	out, report := authorityDir(t, map[string]string{
		"actor_roles.yaml": `
actor_roles:
  - id: role.example
    label: Example role
    status: active
    allowed_actor_kinds: [agent]
`,
	})
	assertValidNT(t, out)
	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "actor_roles" {
		t.Fatalf("schema: got %q want actor_roles", got)
	}
}

func TestActorRoles_FullFieldEmission(t *testing.T) {
	out, _ := authorityDir(t, map[string]string{
		"actor_roles.yaml": `
actor_roles:
  - id: role.example
    label: Example role
    status: active
    allowed_actor_kinds: [agent, human]
    trusted_issuers: [sensei.local]
    aliases: [repository repair agent]
`,
	})
	subj := rdf.MintIRI(rdf.ClassActorRole, "role.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassActorRole)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowedActorKind)+` "agent"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowedActorKind)+` "human"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropTrustedIssuer)+` "sensei.local"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAlias)+` "repository repair agent"`)
}

func TestMutationAndObservationPaths_EmitTypedFields(t *testing.T) {
	out, _ := authorityDir(t, map[string]string{
		"mutation_paths.yaml": `
mutation_paths:
  - id: mutation_path.repository_edit
    label: Repository edit
    status: active
    mechanism_kind: repository_edit
    target_kinds: [source_file, governed_source]
`,
		"observation_paths.yaml": `
observation_paths:
  - id: observation_path.repository_read
    label: Repository read
    status: active
    mechanism_kind: repository_edit
    target_kinds: [source_file]
`,
	})
	mutation := rdf.MintIRI(rdf.ClassMutationPath, "mutation_path.repository_edit")
	observation := rdf.MintIRI(rdf.ClassObservationPath, "observation_path.repository_read")
	mustContain(t, out, mutation+" "+rdf.IRI(rdf.PropMechanismKind)+` "repository_edit"`)
	mustContain(t, out, mutation+" "+rdf.IRI(rdf.PropTargetKind)+` "governed_source"`)
	mustContain(t, out, observation+" "+rdf.IRI(rdf.PropMechanismKind)+` "repository_edit"`)
	mustContain(t, out, observation+" "+rdf.IRI(rdf.PropTargetKind)+` "source_file"`)
}

func TestDelegationPolicies_FullFieldEmission(t *testing.T) {
	out, _ := authorityDir(t, map[string]string{
		"delegation_policies.yaml": `
delegation_policies:
  - id: delegation_policy.repository_repair
    label: Repository repair
    status: active
    maximum_depth: 1
    maximum_duration: 24h
    allow_subdelegation: false
    allowed_actions: [read, modify]
    allowed_mechanism_ids: [mutation_path.repository_edit]
`,
	})
	subj := rdf.MintIRI(rdf.ClassDelegationPolicy, "delegation_policy.repository_repair")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMaximumDepth)+` "1"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMaximumDuration)+` "24h"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowSubdelegation)+` "false"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowedAction)+` "modify"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowedMechanismID)+` "mutation_path.repository_edit"`)
}

func TestAuthorityGrants_FullFieldEmission(t *testing.T) {
	out, _ := authorityDir(t, map[string]string{
		"authority_grants.yaml": `
authority_grants:
  - id: grant.example
    label: Example grant
    status: active
    actor_role_ids: [role.repository_repair_agent]
    authority_domain_ids: [authority.sensei_repository]
    actions: [read, modify]
    target_kinds: [source_file]
    required_mechanism_ids: [mutation_path.repository_edit]
    maximum_risk_class: architecture_sensitive
    valid_from: 2026-07-15T00:00:00Z
    valid_until: 2026-07-16T00:00:00Z
    delegable: true
    delegation_policy_id: delegation_policy.repository_repair
`,
	})
	subj := rdf.MintIRI(rdf.ClassAuthorityGrant, "grant.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropActorRoleID)+` "role.repository_repair_agent"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAuthorityDomainID)+` "authority.sensei_repository"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAllowedAction)+` "modify"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiredMechanismID)+` "mutation_path.repository_edit"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMaximumRiskClass)+` "architecture_sensitive"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDelegable)+` "true"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDelegationPolicyID)+` "delegation_policy.repository_repair"`)
}
