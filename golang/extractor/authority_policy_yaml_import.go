// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

type yamlActorRole struct {
	ID                string   `yaml:"id"`
	Label             string   `yaml:"label"`
	Status            string   `yaml:"status"`
	AllowedActorKinds []string `yaml:"allowed_actor_kinds"`
	TrustedIssuers    []string `yaml:"trusted_issuers"`
	Aliases           []string `yaml:"aliases"`
}

type yamlActorRolesDoc struct {
	ActorRoles []yamlActorRole `yaml:"actor_roles"`
}

type yamlMutationPath struct {
	ID            string   `yaml:"id"`
	Label         string   `yaml:"label"`
	Status        string   `yaml:"status"`
	MechanismKind string   `yaml:"mechanism_kind"`
	TargetKinds   []string `yaml:"target_kinds"`
}

type yamlMutationPathsDoc struct {
	MutationPaths []yamlMutationPath `yaml:"mutation_paths"`
}

type yamlObservationPath struct {
	ID            string   `yaml:"id"`
	Label         string   `yaml:"label"`
	Status        string   `yaml:"status"`
	MechanismKind string   `yaml:"mechanism_kind"`
	TargetKinds   []string `yaml:"target_kinds"`
}

type yamlObservationPathsDoc struct {
	ObservationPaths []yamlObservationPath `yaml:"observation_paths"`
}

type yamlDelegationPolicy struct {
	ID                  string   `yaml:"id"`
	Label               string   `yaml:"label"`
	Status              string   `yaml:"status"`
	MaximumDepth        *int     `yaml:"maximum_depth"`
	MaximumDuration     string   `yaml:"maximum_duration"`
	AllowSubdelegation  *bool    `yaml:"allow_subdelegation"`
	AllowedActions      []string `yaml:"allowed_actions"`
	AllowedMechanismIDs []string `yaml:"allowed_mechanism_ids"`
}

type yamlDelegationPoliciesDoc struct {
	DelegationPolicies []yamlDelegationPolicy `yaml:"delegation_policies"`
}

type yamlAuthorityGrant struct {
	ID                   string   `yaml:"id"`
	Label                string   `yaml:"label"`
	Status               string   `yaml:"status"`
	ActorRoleIDs         []string `yaml:"actor_role_ids"`
	AuthorityDomainIDs   []string `yaml:"authority_domain_ids"`
	Actions              []string `yaml:"actions"`
	TargetKinds          []string `yaml:"target_kinds"`
	RequiredMechanismIDs []string `yaml:"required_mechanism_ids"`
	MaximumRiskClass     string   `yaml:"maximum_risk_class"`
	ValidFrom            string   `yaml:"valid_from"`
	ValidUntil           string   `yaml:"valid_until"`
	Delegable            *bool    `yaml:"delegable"`
	DelegationPolicyID   string   `yaml:"delegation_policy_id"`
}

type yamlAuthorityGrantsDoc struct {
	AuthorityGrants []yamlAuthorityGrant `yaml:"authority_grants"`
}

func importActorRoles(e *rdf.Emitter, path string) error {
	var doc yamlActorRolesDoc
	if err := loadYAMLDoc(path, &doc); err != nil {
		return err
	}
	for _, role := range doc.ActorRoles {
		if strings.TrimSpace(role.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassActorRole, role.ID)
		e.Typed(subj, rdf.ClassActorRole)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(role.Label, role.ID)))
		emitOptLit(e, subj, rdf.PropStatus, role.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		emitOptLits(e, subj, rdf.PropAllowedActorKind, role.AllowedActorKinds)
		emitOptLits(e, subj, rdf.PropTrustedIssuer, role.TrustedIssuers)
		emitOptLits(e, subj, rdf.PropAlias, role.Aliases)
	}
	return nil
}

func importMutationPaths(e *rdf.Emitter, path string) error {
	var doc yamlMutationPathsDoc
	if err := loadYAMLDoc(path, &doc); err != nil {
		return err
	}
	for _, item := range doc.MutationPaths {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassMutationPath, item.ID)
		e.Typed(subj, rdf.ClassMutationPath)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(item.Label, item.ID)))
		emitOptLit(e, subj, rdf.PropStatus, item.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		emitOptLit(e, subj, rdf.PropMechanismKind, item.MechanismKind)
		emitOptLits(e, subj, rdf.PropTargetKind, item.TargetKinds)
	}
	return nil
}

func importObservationPaths(e *rdf.Emitter, path string) error {
	var doc yamlObservationPathsDoc
	if err := loadYAMLDoc(path, &doc); err != nil {
		return err
	}
	for _, item := range doc.ObservationPaths {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassObservationPath, item.ID)
		e.Typed(subj, rdf.ClassObservationPath)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(item.Label, item.ID)))
		emitOptLit(e, subj, rdf.PropStatus, item.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		emitOptLit(e, subj, rdf.PropMechanismKind, item.MechanismKind)
		emitOptLits(e, subj, rdf.PropTargetKind, item.TargetKinds)
	}
	return nil
}

func importDelegationPolicies(e *rdf.Emitter, path string) error {
	var doc yamlDelegationPoliciesDoc
	if err := loadYAMLDoc(path, &doc); err != nil {
		return err
	}
	for _, item := range doc.DelegationPolicies {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassDelegationPolicy, item.ID)
		e.Typed(subj, rdf.ClassDelegationPolicy)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(item.Label, item.ID)))
		emitOptLit(e, subj, rdf.PropStatus, item.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		if item.MaximumDepth != nil {
			emitOptLit(e, subj, rdf.PropMaximumDepth, strconv.Itoa(*item.MaximumDepth))
		}
		emitOptLit(e, subj, rdf.PropMaximumDuration, item.MaximumDuration)
		if item.AllowSubdelegation != nil {
			emitOptLit(e, subj, rdf.PropAllowSubdelegation, strconv.FormatBool(*item.AllowSubdelegation))
		}
		emitOptLits(e, subj, rdf.PropAllowedAction, item.AllowedActions)
		emitOptLits(e, subj, rdf.PropAllowedMechanismID, item.AllowedMechanismIDs)
	}
	return nil
}

func importAuthorityGrants(e *rdf.Emitter, path string) error {
	var doc yamlAuthorityGrantsDoc
	if err := loadYAMLDoc(path, &doc); err != nil {
		return err
	}
	for _, item := range doc.AuthorityGrants {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassAuthorityGrant, item.ID)
		e.Typed(subj, rdf.ClassAuthorityGrant)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(item.Label, item.ID)))
		emitOptLit(e, subj, rdf.PropStatus, item.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		emitOptLits(e, subj, rdf.PropActorRoleID, item.ActorRoleIDs)
		emitOptLits(e, subj, rdf.PropAuthorityDomainID, item.AuthorityDomainIDs)
		emitOptLits(e, subj, rdf.PropAllowedAction, item.Actions)
		emitOptLits(e, subj, rdf.PropTargetKind, item.TargetKinds)
		emitOptLits(e, subj, rdf.PropRequiredMechanismID, item.RequiredMechanismIDs)
		emitOptLit(e, subj, rdf.PropMaximumRiskClass, item.MaximumRiskClass)
		emitOptLit(e, subj, rdf.PropValidFrom, item.ValidFrom)
		emitOptLit(e, subj, rdf.PropValidUntil, item.ValidUntil)
		if item.Delegable != nil {
			emitOptLit(e, subj, rdf.PropDelegable, strconv.FormatBool(*item.Delegable))
		}
		emitOptLit(e, subj, rdf.PropDelegationPolicyID, item.DelegationPolicyID)
	}
	return nil
}

func loadYAMLDoc(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	return nil
}
