// SPDX-License-Identifier: Apache-2.0

package inference

import "github.com/globulario/sensei/golang/architecture"

type RuleDescriptor struct {
	ID                  string   `json:"id" yaml:"id"`
	Version             string   `json:"version" yaml:"version"`
	Title               string   `json:"title" yaml:"title"`
	Description         string   `json:"description" yaml:"description"`
	RequiredFactKinds   []string `json:"required_fact_kinds" yaml:"required_fact_kinds"`
	RequiredPredicates  []string `json:"required_predicates" yaml:"required_predicates"`
	OutputPlane         string   `json:"output_plane" yaml:"output_plane"`
	OutputPredicate     string   `json:"output_predicate" yaml:"output_predicate"`
	ConfidencePolicy    string   `json:"confidence_policy" yaml:"confidence_policy"`
	HumanReviewRequired bool     `json:"human_review_required" yaml:"human_review_required"`
	KnownLimitations    []string `json:"known_limitations" yaml:"known_limitations"`
}

type Rule interface {
	Descriptor() RuleDescriptor
	Apply(Context) ([]Application, error)
}

type Context struct {
	Binding     architecture.ClaimDocumentBinding
	Facts       []architecture.Fact
	Limitations []architecture.Limitation
}

type Application struct {
	RuleID         string
	GroupKey       string
	PremiseFactIDs []string
	Claim          architecture.Claim
}

const confidencePolicy = "minimum premise confidence capped by rule maximum; fact count does not increase confidence"

func ConservativeConfidence(facts []architecture.Fact, cap float64) float64 {
	if len(facts) == 0 {
		return 0
	}
	min := facts[0].Confidence
	for _, f := range facts[1:] {
		if f.Confidence < min {
			min = f.Confidence
		}
	}
	if min > cap {
		min = cap
	}
	if min < 0 {
		return 0
	}
	if min > 1 {
		return 1
	}
	return min
}
