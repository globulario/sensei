// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	rules map[string]Rule
}

func DefaultRegistry() (*Registry, error) {
	return NewRegistry([]Rule{
		ComponentDependencyCrossingRule{},
		ExportedAPITestedBehaviorRule{},
		InterfaceImplementationSurfaceRule{},
		ObservedGuardRule{},
		RuleSignalingTestExpectationRule{},
		SharedEntrypointBehaviorPathRule{},
		TestedFailureBoundaryRule{},
		TestedMonotonicStateRule{},
		ObservedWriterSetRule{},
	})
}

func NewRegistry(rules []Rule) (*Registry, error) {
	r := &Registry{rules: map[string]Rule{}}
	for _, rule := range rules {
		d := rule.Descriptor()
		if strings.TrimSpace(d.ID) == "" {
			return nil, fmt.Errorf("rule id is required")
		}
		if !strings.Contains(d.ID, ".v") || strings.TrimSpace(d.Version) == "" {
			return nil, fmt.Errorf("rule %s must be versioned", d.ID)
		}
		if _, exists := r.rules[d.ID]; exists {
			return nil, fmt.Errorf("duplicate rule id %s", d.ID)
		}
		r.rules[d.ID] = rule
	}
	return r, nil
}

func (r *Registry) Descriptors() []RuleDescriptor {
	ids := r.RuleIDs()
	out := make([]RuleDescriptor, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.rules[id].Descriptor())
	}
	return out
}

func (r *Registry) RuleIDs() []string {
	ids := make([]string, 0, len(r.rules))
	for id := range r.rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) Select(ids []string) ([]Rule, error) {
	if len(ids) == 0 {
		return r.rulesInOrder(r.RuleIDs()), nil
	}
	seen := map[string]bool{}
	var want []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := r.rules[id]; !ok {
			return nil, fmt.Errorf("unknown rule id %s", id)
		}
		seen[id] = true
		want = append(want, id)
	}
	sort.Strings(want)
	return r.rulesInOrder(want), nil
}

func (r *Registry) rulesInOrder(ids []string) []Rule {
	out := make([]Rule, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.rules[id])
	}
	return out
}
