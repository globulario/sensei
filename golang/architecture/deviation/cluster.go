// SPDX-License-Identifier: AGPL-3.0-only

package deviation

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Cluster groups exact receipts by structured architectural shape. Duplicate
// task/change occurrences never increase recurrence.
func Cluster(receipts []Receipt, minimumIndependentOccurrences int) ([]Pattern, error) {
	if minimumIndependentOccurrences < 2 {
		return nil, errors.New("repeated-deviation threshold must be at least two independent occurrences")
	}
	normalized, err := NormalizeReceipts(receipts)
	if err != nil {
		return nil, err
	}
	groups := map[string][]Receipt{}
	for _, receipt := range normalized {
		key := patternIdentityDescriptor(receipt.Kind, receipt.Scope, receipt.Shape)
		groups[key] = append(groups[key], receipt)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	patterns := make([]Pattern, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		byOccurrence := map[string]Receipt{}
		for _, receipt := range group {
			if existing, ok := byOccurrence[receipt.IndependenceKey]; ok {
				if existing.SemanticDigestSHA256 != receipt.SemanticDigestSHA256 {
					return nil, fmt.Errorf("independence key %s binds multiple deviation events", receipt.IndependenceKey)
				}
				continue
			}
			byOccurrence[receipt.IndependenceKey] = receipt
		}
		occurrenceKeys := make([]string, 0, len(byOccurrence))
		for occurrenceKey := range byOccurrence {
			occurrenceKeys = append(occurrenceKeys, occurrenceKey)
		}
		sort.Strings(occurrenceKeys)
		if len(occurrenceKeys) == 0 {
			continue
		}

		first := byOccurrence[occurrenceKeys[0]]
		pattern := Pattern{
			Kind: first.Kind,
			Scope: first.Scope,
			Shape: first.Shape,
			IndependenceKeys: occurrenceKeys,
			MinimumIndependentOccurrences: minimumIndependentOccurrences,
		}
		for _, occurrenceKey := range occurrenceKeys {
			receipt := byOccurrence[occurrenceKey]
			pattern.ReceiptIDs = append(pattern.ReceiptIDs, receipt.ID)
			pattern.AgentIDs = append(pattern.AgentIDs, receipt.AgentID)
			pattern.TaskIDs = append(pattern.TaskIDs, receipt.TaskID)
			pattern.RelatedClaimIDs = append(pattern.RelatedClaimIDs, receipt.RelatedClaimIDs...)
			pattern.EvidenceRefs = append(pattern.EvidenceRefs, receipt.EvidenceRefs...)
			if pattern.FirstObservedAt == "" || receipt.RecordedAt < pattern.FirstObservedAt {
				pattern.FirstObservedAt = receipt.RecordedAt
			}
			if receipt.RecordedAt > pattern.LastObservedAt {
				pattern.LastObservedAt = receipt.RecordedAt
			}
		}
		pattern.ReceiptIDs = cleanStrings(pattern.ReceiptIDs)
		pattern.AgentIDs = cleanStrings(pattern.AgentIDs)
		pattern.TaskIDs = cleanStrings(pattern.TaskIDs)
		pattern.RelatedClaimIDs = cleanStrings(pattern.RelatedClaimIDs)
		pattern.EvidenceRefs = cleanClassRefs(pattern.EvidenceRefs)
		pattern.IndependentOccurrenceCount = len(pattern.IndependenceKeys)
		pattern.CandidateEligible = pattern.IndependentOccurrenceCount >= minimumIndependentOccurrences
		pattern.ID = expectedPatternID(pattern)
		digest, digestErr := PatternDigest(pattern)
		if digestErr != nil {
			return nil, digestErr
		}
		pattern.SemanticDigestSHA256 = digest
		if validateErr := ValidatePattern(pattern); validateErr != nil {
			return nil, validateErr
		}
		patterns = append(patterns, pattern)
	}
	sort.SliceStable(patterns, func(i, j int) bool { return patterns[i].ID < patterns[j].ID })
	return patterns, nil
}

// ValidatePattern proves that recurrence counts only independent exact events.
func ValidatePattern(in Pattern) error {
	pattern := canonicalizePattern(in)
	var errs []string
	if !IsValidKind(pattern.Kind) {
		errs = append(errs, "unknown deviation pattern kind")
	}
	if pattern.MinimumIndependentOccurrences < 2 {
		errs = append(errs, "pattern minimum independent occurrences must be at least two")
	}
	if pattern.IndependentOccurrenceCount != len(pattern.IndependenceKeys) ||
		pattern.IndependentOccurrenceCount != len(pattern.ReceiptIDs) {
		errs = append(errs, "pattern occurrence count must exactly match independent receipts")
	}
	if pattern.CandidateEligible != (pattern.IndependentOccurrenceCount >= pattern.MinimumIndependentOccurrences) {
		errs = append(errs, "pattern candidate eligibility must exactly match recurrence threshold")
	}
	if pattern.FirstObservedAt == "" || pattern.LastObservedAt == "" || pattern.FirstObservedAt > pattern.LastObservedAt {
		errs = append(errs, "pattern observation interval is invalid")
	}
	if pattern.ID == "" || pattern.ID != expectedPatternID(pattern) {
		errs = append(errs, "pattern id must exactly match structured deviation shape")
	}
	if len(pattern.EvidenceRefs) == 0 {
		errs = append(errs, "pattern requires evidence references")
	}
	if err := validateScope(pattern.Scope); err != nil {
		errs = append(errs, err.Error())
	}
	actualDigest, err := PatternDigest(pattern)
	if err != nil {
		errs = append(errs, err.Error())
	} else if pattern.SemanticDigestSHA256 != actualDigest {
		errs = append(errs, "pattern semantic digest mismatch")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func PatternDigest(in Pattern) (string, error) {
	pattern := canonicalizePattern(in)
	pattern.SemanticDigestSHA256 = ""
	data, err := json.Marshal(pattern)
	if err != nil {
		return "", err
	}
	return sha256Bytes(data), nil
}

func expectedPatternID(in Pattern) string {
	pattern := canonicalizePattern(in)
	descriptor := patternIdentityDescriptor(pattern.Kind, pattern.Scope, pattern.Shape)
	return "deviation_pattern." + sha256String(descriptor)[:24]
}

func patternIdentityDescriptor(kind Kind, scope interface{ }) string {
	return ""
}

func canonicalizePattern(in Pattern) Pattern {
	pattern := in
	pattern.ID = strings.TrimSpace(pattern.ID)
	pattern.Scope = canonicalizeScope(pattern.Scope)
	pattern.Shape.Subject = strings.TrimSpace(pattern.Shape.Subject)
	pattern.Shape.Predicate = strings.TrimSpace(pattern.Shape.Predicate)
	pattern.Shape.Object = strings.TrimSpace(pattern.Shape.Object)
	pattern.ReceiptIDs = cleanStrings(pattern.ReceiptIDs)
	pattern.IndependenceKeys = cleanStrings(pattern.IndependenceKeys)
	pattern.AgentIDs = cleanStrings(pattern.AgentIDs)
	pattern.TaskIDs = cleanStrings(pattern.TaskIDs)
	pattern.RelatedClaimIDs = cleanStrings(pattern.RelatedClaimIDs)
	pattern.EvidenceRefs = cleanClassRefs(pattern.EvidenceRefs)
	pattern.FirstObservedAt = strings.TrimSpace(pattern.FirstObservedAt)
	pattern.LastObservedAt = strings.TrimSpace(pattern.LastObservedAt)
	pattern.SemanticDigestSHA256 = strings.TrimSpace(pattern.SemanticDigestSHA256)
	return pattern
}
