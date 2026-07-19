// SPDX-License-Identifier: Apache-2.0

package authority

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

type actorRolesDoc struct {
	ActorRoles []struct {
		ID                string   `yaml:"id"`
		Status            string   `yaml:"status"`
		AllowedActorKinds []string `yaml:"allowed_actor_kinds"`
		TrustedIssuers    []string `yaml:"trusted_issuers"`
		Aliases           []string `yaml:"aliases"`
	} `yaml:"actor_roles"`
}

type mutationPathsDoc struct {
	MutationPaths []struct {
		ID            string   `yaml:"id"`
		Status        string   `yaml:"status"`
		MechanismKind string   `yaml:"mechanism_kind"`
		TargetKinds   []string `yaml:"target_kinds"`
	} `yaml:"mutation_paths"`
}

type observationPathsDoc struct {
	ObservationPaths []struct {
		ID            string   `yaml:"id"`
		Status        string   `yaml:"status"`
		MechanismKind string   `yaml:"mechanism_kind"`
		TargetKinds   []string `yaml:"target_kinds"`
	} `yaml:"observation_paths"`
}

type delegationPoliciesDoc struct {
	DelegationPolicies []struct {
		ID                  string   `yaml:"id"`
		Status              string   `yaml:"status"`
		MaximumDepth        int      `yaml:"maximum_depth"`
		MaximumDuration     string   `yaml:"maximum_duration"`
		AllowSubdelegation  bool     `yaml:"allow_subdelegation"`
		AllowedActions      []string `yaml:"allowed_actions"`
		AllowedMechanismIDs []string `yaml:"allowed_mechanism_ids"`
	} `yaml:"delegation_policies"`
}

type authorityGrantsDoc struct {
	AuthorityGrants []struct {
		ID                   string   `yaml:"id"`
		Status               string   `yaml:"status"`
		ActorRoleIDs         []string `yaml:"actor_role_ids"`
		AuthorityDomainIDs   []string `yaml:"authority_domain_ids"`
		Actions              []string `yaml:"actions"`
		TargetKinds          []string `yaml:"target_kinds"`
		RequiredMechanismIDs []string `yaml:"required_mechanism_ids"`
		MaximumRiskClass     string   `yaml:"maximum_risk_class"`
		ValidFrom            string   `yaml:"valid_from"`
		ValidUntil           string   `yaml:"valid_until"`
		Delegable            bool     `yaml:"delegable"`
		DelegationPolicyID   string   `yaml:"delegation_policy_id"`
	} `yaml:"authority_grants"`
}

type authorityDomainsDoc struct {
	AuthorityDomains []struct {
		ID               string   `yaml:"id"`
		Status           string   `yaml:"status"`
		MayWrite         []string `yaml:"may_write"`
		MayWriteRoleIDs  []string `yaml:"may_write_role_ids"`
		MayRead          []string `yaml:"may_read"`
		MayReadRoleIDs   []string `yaml:"may_read_role_ids"`
		MustMutateVia    []string `yaml:"must_mutate_via"`
		MustMutateViaIDs []string `yaml:"must_mutate_via_ids"`
		MustReadVia      []string `yaml:"must_read_via"`
		MustReadViaIDs   []string `yaml:"must_read_via_ids"`
		ObservesVia      []string `yaml:"observes_via"`
		ObservesViaIDs   []string `yaml:"observes_via_ids"`
	} `yaml:"authority_domains"`
}

func LoadPolicyIndex(repoRoot string) (PolicyIndex, error) {
	root, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil {
		return PolicyIndex{}, err
	}
	idx := PolicyIndex{
		ActorRoles:         map[string]ActorRole{},
		MutationPaths:      map[string]MutationPath{},
		ObservationPaths:   map[string]ObservationPath{},
		DelegationPolicies: map[string]DelegationPolicy{},
		AuthorityGrants:    map[string]AuthorityGrant{},
		AuthorityDomains:   map[string]AuthorityDomain{},
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "actor_roles.yaml"), &actorRolesDoc{}, func(doc any) {
		for _, item := range doc.(*actorRolesDoc).ActorRoles {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			role := ActorRole{ID: strings.TrimSpace(item.ID), Status: strings.TrimSpace(item.Status), TrustedIssuers: cleanSet(item.TrustedIssuers), Aliases: cleanSet(item.Aliases)}
			for _, kind := range cleanSet(item.AllowedActorKinds) {
				role.AllowedActorKinds = append(role.AllowedActorKinds, closureprotocol.ActorKind(kind))
			}
			idx.ActorRoles[role.ID] = role
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "mutation_paths.yaml"), &mutationPathsDoc{}, func(doc any) {
		for _, item := range doc.(*mutationPathsDoc).MutationPaths {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			idx.MutationPaths[strings.TrimSpace(item.ID)] = MutationPath{
				ID:            strings.TrimSpace(item.ID),
				Status:        strings.TrimSpace(item.Status),
				MechanismKind: closureprotocol.MechanismKind(strings.TrimSpace(item.MechanismKind)),
				TargetKinds:   cleanSet(item.TargetKinds),
			}
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "observation_paths.yaml"), &observationPathsDoc{}, func(doc any) {
		for _, item := range doc.(*observationPathsDoc).ObservationPaths {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			idx.ObservationPaths[strings.TrimSpace(item.ID)] = ObservationPath{
				ID:            strings.TrimSpace(item.ID),
				Status:        strings.TrimSpace(item.Status),
				MechanismKind: closureprotocol.MechanismKind(strings.TrimSpace(item.MechanismKind)),
				TargetKinds:   cleanSet(item.TargetKinds),
			}
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "delegation_policies.yaml"), &delegationPoliciesDoc{}, func(doc any) {
		for _, item := range doc.(*delegationPoliciesDoc).DelegationPolicies {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			policy := DelegationPolicy{
				ID:                  strings.TrimSpace(item.ID),
				Status:              strings.TrimSpace(item.Status),
				MaximumDepth:        item.MaximumDepth,
				MaximumDuration:     strings.TrimSpace(item.MaximumDuration),
				AllowSubdelegation:  item.AllowSubdelegation,
				AllowedMechanismIDs: cleanSet(item.AllowedMechanismIDs),
			}
			for _, action := range cleanSet(item.AllowedActions) {
				policy.AllowedActions = append(policy.AllowedActions, closureprotocol.OperationKind(action))
			}
			idx.DelegationPolicies[policy.ID] = policy
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "authority_grants.yaml"), &authorityGrantsDoc{}, func(doc any) {
		for _, item := range doc.(*authorityGrantsDoc).AuthorityGrants {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			grant := AuthorityGrant{
				ID:                   strings.TrimSpace(item.ID),
				Status:               strings.TrimSpace(item.Status),
				ActorRoleIDs:         cleanSet(item.ActorRoleIDs),
				AuthorityDomainIDs:   cleanSet(item.AuthorityDomainIDs),
				TargetKinds:          cleanSet(item.TargetKinds),
				RequiredMechanismIDs: cleanSet(item.RequiredMechanismIDs),
				MaximumRiskClass:     strings.TrimSpace(item.MaximumRiskClass),
				ValidFrom:            strings.TrimSpace(item.ValidFrom),
				ValidUntil:           strings.TrimSpace(item.ValidUntil),
				Delegable:            item.Delegable,
				DelegationPolicyID:   strings.TrimSpace(item.DelegationPolicyID),
			}
			for _, action := range cleanSet(item.Actions) {
				grant.Actions = append(grant.Actions, closureprotocol.OperationKind(action))
			}
			idx.AuthorityGrants[grant.ID] = grant
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	if err := loadOptionalYAML(filepath.Join(root, "docs", "awareness", "authority_domains.yaml"), &authorityDomainsDoc{}, func(doc any) {
		for _, item := range doc.(*authorityDomainsDoc).AuthorityDomains {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			idx.AuthorityDomains[strings.TrimSpace(item.ID)] = AuthorityDomain{
				ID:                  strings.TrimSpace(item.ID),
				Status:              strings.TrimSpace(item.Status),
				MayWriteRoleIDs:     cleanSet(item.MayWriteRoleIDs),
				MayReadRoleIDs:      cleanSet(item.MayReadRoleIDs),
				MustMutateViaIDs:    cleanSet(item.MustMutateViaIDs),
				MustReadViaIDs:      cleanSet(item.MustReadViaIDs),
				ObservesViaIDs:      cleanSet(item.ObservesViaIDs),
				LegacyMayWrite:      cleanSet(item.MayWrite),
				LegacyMayRead:       cleanSet(item.MayRead),
				LegacyMustMutateVia: cleanSet(item.MustMutateVia),
				LegacyMustReadVia:   cleanSet(item.MustReadVia),
				LegacyObservesVia:   cleanSet(item.ObservesVia),
			}
		}
	}); err != nil {
		return PolicyIndex{}, err
	}
	return idx, nil
}

func loadOptionalYAML(path string, out any, apply func(any)) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	apply(out)
	return nil
}

func cleanSet(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func (idx PolicyIndex) ResolveRoleIDOrAlias(v string) (string, bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false, nil
	}
	if role, ok := idx.ActorRoles[v]; ok && role.Status == "active" {
		return role.ID, true, nil
	}
	var matches []string
	for _, role := range idx.ActorRoles {
		if role.Status != "active" {
			continue
		}
		for _, alias := range role.Aliases {
			if alias == v {
				matches = append(matches, role.ID)
			}
		}
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("ambiguous actor role alias %q", v)
	}
	return matches[0], true, nil
}
