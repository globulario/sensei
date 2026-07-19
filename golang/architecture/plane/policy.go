// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

var currentStatuses = []string{"accepted", "active", "approved", "current"}
var historicalStatuses = []string{"deprecated", "historical", "retired", "superseded"}

func DefaultPolicies() ([]Policy, error) {
	policies := []Policy{
		{
			Plane:                    architecture.PlaneObserved,
			AllowedFactKinds:         []string{"authority_observation", "guard", "persistence", "read", "schema_constraint", "transition", "write"},
			AllowedFactPredicates:    []string{"*", "controls_lifecycle", "exposes_route", "mutates_state", "reads", "refuses_when", "rejects_transition_when", "writes"},
			RequiresCurrentEvidence:  true,
			TruthLayerIsSeparateAxis: true,
			KnownLimitations:         []string{"observed facts do not prove intent, desired state, global completeness, or correctness"},
		},
		{
			Plane:                    architecture.PlaneEnforced,
			AllowedFactKinds:         []string{"assertion", "ci_gate"},
			AllowedFactPredicates:    []string{"*", "asserts_architectural_rule", "rejects", "requires", "validates"},
			AllowedGovernedClasses:   []string{"evidence"},
			RequiresCurrentEvidence:  true,
			TruthLayerIsSeparateAxis: true,
			KnownLimitations:         []string{"test and gate evidence proves only its exercised scope"},
		},
		{
			Plane:                    architecture.PlaneIntended,
			AllowedGovernedClasses:   []string{"contract", "decision", "intent", "invariant"},
			RequiredNodeStatuses:     append([]string{}, currentStatuses...),
			TruthLayerIsSeparateAxis: true,
			KnownLimitations:         []string{"intended architecture does not prove implementation conformance"},
		},
		{
			Plane:                    architecture.PlaneHistorical,
			AllowedFactKinds:         []string{"historical_removal"},
			AllowedGovernedClasses:   []string{"contract", "decision", "evidence", "intent", "invariant"},
			RequiredNodeStatuses:     append([]string{}, historicalStatuses...),
			TruthLayerIsSeparateAxis: true,
			KnownLimitations:         []string{"historical records must not guide current implementation as active rules"},
		},
		{
			Plane:                           architecture.PlaneDesired,
			AllowedGovernedClasses:          []string{"contract", "decision", "intent"},
			RequiredNodeStatuses:            append([]string{}, currentStatuses...),
			RequiresExplicitPlaneAnnotation: true,
			TruthLayerIsSeparateAxis:        true,
			KnownLimitations:                []string{"desired architecture requires explicit authored direction"},
		},
	}
	for i := range policies {
		sort.Strings(policies[i].AllowedFactKinds)
		sort.Strings(policies[i].AllowedFactPredicates)
		sort.Strings(policies[i].AllowedGovernedClasses)
		sort.Strings(policies[i].RequiredNodeStatuses)
		sort.Strings(policies[i].KnownLimitations)
	}
	return policies, nil
}

func PolicyFor(plane string) (Policy, bool) {
	policies, err := DefaultPolicies()
	if err != nil {
		return Policy{}, false
	}
	for _, p := range policies {
		if p.Plane == strings.TrimSpace(plane) {
			return p, true
		}
	}
	return Policy{}, false
}

func ValidateGovernedPlaneAnnotation(class, status, explicitPlane string, supersededBy []string) error {
	class = normalizeClassName(class)
	status = strings.ToLower(strings.TrimSpace(status))
	explicitPlane = strings.TrimSpace(explicitPlane)
	if explicitPlane == "" {
		return nil
	}
	if !isGovernedAnnotationClass(class) {
		return fmt.Errorf("architectural_plane is not supported on %s", class)
	}
	switch explicitPlane {
	case architecture.PlaneObserved, architecture.PlaneEnforced:
		return fmt.Errorf("%s plane cannot be authored on governed YAML; it requires evidence-derived claims", explicitPlane)
	case architecture.PlaneIntended:
		if isHistoricalStatus(status) {
			return errors.New("intended plane rejects historical, superseded, retired, or deprecated node")
		}
	case architecture.PlaneHistorical:
		if !isHistoricalStatus(status) && len(nonEmptyStrings(supersededBy)) == 0 {
			return errors.New("historical plane requires historical status or supersession")
		}
	case architecture.PlaneDesired:
		if class == "invariant" {
			return errors.New("desired plane is not allowed on Invariant")
		}
		if isHistoricalStatus(status) || len(nonEmptyStrings(supersededBy)) > 0 {
			return errors.New("desired plane rejects historical or superseded node")
		}
	default:
		return fmt.Errorf("unknown architectural_plane %q", explicitPlane)
	}
	return nil
}

func normalizeClassName(class string) string {
	switch strings.TrimSpace(class) {
	case rdf.ClassInvariant:
		return "invariant"
	case rdf.ClassContract:
		return "contract"
	case rdf.ClassDecision:
		return "decision"
	case rdf.ClassIntent:
		return "intent"
	}
	return classToken(class)
}

func isGovernedAnnotationClass(class string) bool {
	switch normalizeClassName(class) {
	case "invariant", "contract", "decision", "intent":
		return true
	default:
		return false
	}
}

func isCurrentStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	for _, s := range currentStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func isHistoricalStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	for _, s := range historicalStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}
