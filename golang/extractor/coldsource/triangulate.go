// SPDX-License-Identifier: Apache-2.0

package coldsource

import "sort"

// Bundle is the set of cold signals grouped under one theme, plus the distinct
// source types that contributed. A bundle is "triangulated" (eligible to be
// drafted) ONLY when >=2 distinct evidence CHANNELS corroborate it — a commit
// signal AND a review signal — the cold analog of the runtime promotion
// pipeline's ">=2 supporting outcomes" rule. Two signals from the same channel
// (two commits, or two comments on one thread) are NOT corroboration.
// Single-channel themes are held back, never drafted: one channel is a guess,
// not cross-checked evidence.
type Bundle struct {
	ThemeKey    string
	Signals     []ColdSignal
	SourceTypes []SourceType // distinct, sorted
}

// IsTriangulated reports whether the bundle has signals from >=2 distinct
// evidence channels (see channel()). For the original {revert_commit, pr_review}
// pair this is identical to ">=2 distinct source types"; it additionally lets a
// weak conventional_commit corroborate with a review signal while refusing
// commit-only or review-only bundles.
func (b Bundle) IsTriangulated() bool {
	chans := map[string]bool{}
	for _, t := range b.SourceTypes {
		chans[channel(t)] = true
	}
	return len(chans) >= 2
}

// AllowedCitations is the exact set of citation strings a drafted candidate may
// cite for this bundle. Anything outside this set is a fabricated citation and
// is rejected by ValidateDraft.
func (b Bundle) AllowedCitations() map[string]bool {
	out := map[string]bool{}
	for _, s := range b.Signals {
		for _, c := range s.Citations() {
			out[c] = true
		}
	}
	return out
}

// Triangulate groups signals by theme and splits them into eligible (>=2
// distinct source types) and heldBack (single-source) bundles. Deterministic:
// signals within a bundle keep input order; bundles are sorted by theme key.
func Triangulate(signals []ColdSignal) (eligible, heldBack []Bundle) {
	byTheme := map[string][]ColdSignal{}
	order := []string{}
	for _, s := range signals {
		if s.ThemeKey == "" {
			continue
		}
		if _, ok := byTheme[s.ThemeKey]; !ok {
			order = append(order, s.ThemeKey)
		}
		byTheme[s.ThemeKey] = append(byTheme[s.ThemeKey], s)
	}
	sort.Strings(order)

	for _, theme := range order {
		group := byTheme[theme]
		typeSet := map[SourceType]bool{}
		for _, s := range group {
			typeSet[s.SourceType] = true
		}
		types := make([]SourceType, 0, len(typeSet))
		for t := range typeSet {
			types = append(types, t)
		}
		sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

		b := Bundle{ThemeKey: theme, Signals: group, SourceTypes: types}
		if b.IsTriangulated() {
			eligible = append(eligible, b)
		} else {
			heldBack = append(heldBack, b)
		}
	}
	return eligible, heldBack
}
