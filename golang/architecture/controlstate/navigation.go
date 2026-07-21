// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"sort"
)

// NavigationClass mirrors a registry class policy for the client. It carries no policy invented
// during projection — every field is copied from the canonical registry.
type NavigationClass struct {
	ClassIRI           string             `json:"class_iri" yaml:"class_iri"`
	Label              string             `json:"label" yaml:"label"`
	Order              int                `json:"order" yaml:"order"`
	Coverage           AssessmentCoverage `json:"coverage" yaml:"coverage"`
	AssessableArtifact bool               `json:"assessable_artifact" yaml:"assessable_artifact"`
	QueryCapable       bool               `json:"query_capable" yaml:"query_capable"`
	ResolveCapable     bool               `json:"resolve_capable" yaml:"resolve_capable"`
	InspectorCapable   bool               `json:"inspector_capable" yaml:"inspector_capable"`
	QuestionCapable    bool               `json:"question_capable" yaml:"question_capable"`
	DefaultVisible     bool               `json:"default_visible" yaml:"default_visible"`
	OverviewVisible    bool               `json:"overview_visible" yaml:"overview_visible"`
}

// NavigationFamily is a grouped family with its ordered classes.
type NavigationFamily struct {
	ID      string            `json:"id" yaml:"id"`
	Label   string            `json:"label" yaml:"label"`
	Order   int               `json:"order" yaml:"order"`
	Classes []NavigationClass `json:"classes" yaml:"classes"`
}

// NavigationDescriptor is ontology.navigation_descriptor/v1: the authored ontology families,
// classes, and capabilities, mechanically derived from the canonical registry. Repository-
// independent except for schema/version metadata.
type NavigationDescriptor struct {
	ProjectionMeta       `json:",inline" yaml:",inline"`
	RegistryDigest       string             `json:"registry_digest" yaml:"registry_digest"`
	Families             []NavigationFamily `json:"families" yaml:"families"`
	UnknownClassFallback NavigationClass    `json:"unknown_class_fallback" yaml:"unknown_class_fallback"`
}

// BuildNavigationDescriptor mechanically derives the descriptor from the registry. It fails
// closed on an invalid registry and invents no policy.
func BuildNavigationDescriptor(reg Registry) (NavigationDescriptor, error) {
	if err := reg.Validate(); err != nil {
		return NavigationDescriptor{}, fmt.Errorf("invalid registry: %w", err)
	}
	regDigest, err := reg.Digest()
	if err != nil {
		return NavigationDescriptor{}, err
	}
	toNav := func(c ClassPolicy) NavigationClass {
		return NavigationClass{
			ClassIRI: c.ClassIRI, Label: c.Label, Order: c.Order, Coverage: c.Coverage,
			AssessableArtifact: c.Coverage == CoverageAssessable,
			QueryCapable:       c.QueryCapable, ResolveCapable: c.ResolveCapable,
			InspectorCapable: c.InspectorCapable, QuestionCapable: c.QuestionCapable,
			DefaultVisible: c.DefaultVisible, OverviewVisible: c.OverviewVisible,
		}
	}

	var families []NavigationFamily
	fams := append([]OntologyFamily(nil), reg.Families...)
	sort.SliceStable(fams, func(i, j int) bool { return fams[i].Order < fams[j].Order })
	for _, f := range fams {
		var classes []NavigationClass
		for _, c := range reg.Classes {
			if c.Family == f.ID && !c.Unclassified {
				classes = append(classes, toNav(c))
			}
		}
		sort.SliceStable(classes, func(i, j int) bool { return classes[i].Order < classes[j].Order })
		families = append(families, NavigationFamily{ID: f.ID, Label: f.Label, Order: f.Order, Classes: classes})
	}

	d := NavigationDescriptor{
		ProjectionMeta: newMeta(NavigationDescriptorSchema, "", "", AvailabilityAvailable,
			[]SourceStatus{srcStatus("controlstate.registry", "registry", regDigest, regDigest, SourceAvailable, ImpactPrimary, "")}, nil),
		RegistryDigest:       regDigest,
		Families:             families,
		UnknownClassFallback: toNav(reg.unclassifiedPolicy()),
	}
	dig, err := digestOf(d)
	if err != nil {
		return NavigationDescriptor{}, err
	}
	d.DigestSHA256 = dig
	if err := ValidateNavigationDescriptor(d); err != nil {
		return NavigationDescriptor{}, err
	}
	return d, nil
}

// ValidateNavigationDescriptor strictly validates the descriptor.
func ValidateNavigationDescriptor(d NavigationDescriptor) error {
	if err := validateMeta(d.ProjectionMeta, NavigationDescriptorSchema); err != nil {
		return err
	}
	if d.RegistryDigest == "" {
		return fmt.Errorf("navigation descriptor missing registry digest")
	}
	if !d.UnknownClassFallback.DefaultVisible || d.UnknownClassFallback.Coverage != CoverageUnknown {
		return fmt.Errorf("navigation descriptor unknown-class fallback is not a visible unknown policy")
	}
	// Families + classes canonically ordered, no duplicates.
	for i, f := range d.Families {
		if f.ID == "" {
			return fmt.Errorf("navigation family missing id")
		}
		if i > 0 && d.Families[i-1].Order >= f.Order {
			return fmt.Errorf("navigation families are not in canonical order")
		}
		for j, c := range f.Classes {
			if c.ClassIRI == "" {
				return fmt.Errorf("navigation class missing iri")
			}
			if j > 0 && f.Classes[j-1].Order >= c.Order {
				return fmt.Errorf("navigation classes in family %q are not in canonical order", f.ID)
			}
			if c.AssessableArtifact != (c.Coverage == CoverageAssessable) {
				return fmt.Errorf("navigation class %q assessable flag disagrees with coverage", c.ClassIRI)
			}
		}
	}
	want, err := computeNavDigest(d)
	if err != nil {
		return err
	}
	if d.DigestSHA256 != want {
		return fmt.Errorf("navigation descriptor digest does not match its content")
	}
	return nil
}

func computeNavDigest(d NavigationDescriptor) (string, error) {
	d.DigestSHA256 = ""
	return digestOf(d)
}
