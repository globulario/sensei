// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
)

type Engine struct {
	rules []Rule
}

func NewEngine(rules []Rule) *Engine {
	return &Engine{rules: append([]Rule{}, rules...)}
}

func (e *Engine) Apply(ctx Context) ([]Application, error) {
	facts, err := architecture.NormalizeFacts("", append([]architecture.Fact{}, ctx.Facts...))
	if err != nil {
		return nil, err
	}
	ctx.Facts = facts
	byID := map[string]architecture.Fact{}
	for _, f := range facts {
		byID[f.ID] = f
	}
	var apps []Application
	for _, rule := range e.rules {
		ruleID := rule.Descriptor().ID
		out, err := rule.Apply(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ruleID, err)
		}
		for _, app := range out {
			if err := validateApplication(ruleID, app, byID); err != nil {
				return nil, err
			}
			app.PremiseFactIDs = dedupeStrings(app.PremiseFactIDs)
			app.Claim.PremiseFacts = dedupeStrings(app.Claim.PremiseFacts)
			if app.Claim.ID == "" {
				app.Claim.ID = architecture.StableClaimID(app.Claim)
			}
			apps = append(apps, app)
		}
	}
	return normalizeApplications(apps)
}

func validateApplication(ruleID string, app Application, byID map[string]architecture.Fact) error {
	if app.RuleID != ruleID {
		return fmt.Errorf("rule id mismatch: descriptor %s emitted %s", ruleID, app.RuleID)
	}
	if app.Claim.InferenceRule != ruleID {
		return fmt.Errorf("application %s claim names another rule %s", app.GroupKey, app.Claim.InferenceRule)
	}
	if app.Claim.AssertionOrigin != architecture.OriginDerived {
		return fmt.Errorf("application %s emitted non-derived claim", app.GroupKey)
	}
	if app.Claim.PromotionStatus != architecture.PromotionCandidate {
		return fmt.Errorf("application %s emitted non-candidate claim", app.GroupKey)
	}
	if !app.Claim.HumanReviewRequired {
		return fmt.Errorf("application %s disabled human review", app.GroupKey)
	}
	for _, id := range app.PremiseFactIDs {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("application %s references unknown premise fact %s", app.GroupKey, id)
		}
	}
	for _, id := range app.Claim.PremiseFacts {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("claim references unknown premise fact %s", id)
		}
	}
	return nil
}

func normalizeApplications(in []Application) ([]Application, error) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].RuleID != in[j].RuleID {
			return in[i].RuleID < in[j].RuleID
		}
		if in[i].GroupKey != in[j].GroupKey {
			return in[i].GroupKey < in[j].GroupKey
		}
		return in[i].Claim.ID < in[j].Claim.ID
	})
	seenApps := map[string]string{}
	seenClaims := map[string]string{}
	var out []Application
	for _, app := range in {
		app.PremiseFactIDs = dedupeStrings(app.PremiseFactIDs)
		app.Claim.PremiseFacts = dedupeStrings(app.Claim.PremiseFacts)
		claims, err := architecture.NormalizeClaims([]architecture.Claim{app.Claim})
		if err != nil {
			return nil, err
		}
		app.Claim = claims[0]
		claimBytes, _ := json.Marshal(app.Claim)
		if existing, ok := seenClaims[app.Claim.ID]; ok && existing != string(claimBytes) {
			return nil, fmt.Errorf("claim id collision for %s", app.Claim.ID)
		}
		seenClaims[app.Claim.ID] = string(claimBytes)
		keyBytes, _ := json.Marshal(struct {
			RuleID   string
			GroupKey string
			Premises []string
			Claim    architecture.Claim
		}{app.RuleID, app.GroupKey, app.PremiseFactIDs, app.Claim})
		key := string(keyBytes)
		if _, ok := seenApps[key]; ok {
			continue
		}
		seenApps[key] = app.Claim.ID
		out = append(out, app)
	}
	return out, nil
}
