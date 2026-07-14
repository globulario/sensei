// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"sort"
	"strings"
)

func normalizeReport(in Report) Report {
	r := in
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = GeneratedBy
	}
	sort.SliceStable(r.ClaimAssessments, func(i, j int) bool {
		return r.ClaimAssessments[i].ClaimID < r.ClaimAssessments[j].ClaimID
	})
	r.Summary = Summary{}
	for i := range r.ClaimAssessments {
		r.ClaimAssessments[i].Bases = normalizeBases(r.ClaimAssessments[i].Bases)
		r.ClaimAssessments[i].Reasons = dedupeReasons(r.ClaimAssessments[i].Reasons)
		r.ClaimAssessments[i].MaintenanceReasons = dedupeReasons(r.ClaimAssessments[i].MaintenanceReasons)
		r.ClaimAssessments[i].OpenQuestions = sortedUnique(r.ClaimAssessments[i].OpenQuestions)
		r.ClaimAssessments[i].ArchitectAnswers = sortedUnique(r.ClaimAssessments[i].ArchitectAnswers)
		switch r.ClaimAssessments[i].PlaneState {
		case StateJustified:
			r.Summary.Justified++
		case StateUnderSupported:
			r.Summary.UnderSupported++
		case StateInvalid:
			r.Summary.Invalid++
		case StateUnknown:
			r.Summary.Unknown++
		case StateStale:
			r.Summary.Stale++
		}
	}
	sort.SliceStable(r.PropositionGroups, func(i, j int) bool {
		return r.PropositionGroups[i].PropositionKey < r.PropositionGroups[j].PropositionKey
	})
	for i := range r.PropositionGroups {
		r.PropositionGroups[i].PresentPlanes = sortPlanes(r.PropositionGroups[i].PresentPlanes)
		r.PropositionGroups[i].MissingPlanes = sortPlanes(r.PropositionGroups[i].MissingPlanes)
		for plane, ids := range r.PropositionGroups[i].ClaimsByPlane {
			r.PropositionGroups[i].ClaimsByPlane[plane] = sortedUnique(ids)
		}
	}
	sort.SliceStable(r.Limitations, func(i, j int) bool {
		return r.Limitations[i].Source+r.Limitations[i].Scope+r.Limitations[i].Reason < r.Limitations[j].Source+r.Limitations[j].Scope+r.Limitations[j].Reason
	})
	return r
}

func normalizeBases(in []BasisAssessment) []BasisAssessment {
	out := append([]BasisAssessment{}, in...)
	sort.SliceStable(out, func(i, j int) bool {
		a := out[i].State + "\x00" + out[i].Basis.Kind + "\x00" + out[i].Basis.ID + "\x00" + out[i].Basis.Class + "\x00" + out[i].Basis.Detail
		b := out[j].State + "\x00" + out[j].Basis.Kind + "\x00" + out[j].Basis.ID + "\x00" + out[j].Basis.Class + "\x00" + out[j].Basis.Detail
		return a < b
	})
	return out
}

func dedupeReasons(in []Reason) []Reason {
	seen := map[string]Reason{}
	var keys []string
	for _, r := range in {
		r.Code = strings.TrimSpace(r.Code)
		r.Detail = strings.TrimSpace(r.Detail)
		if r.Code == "" {
			continue
		}
		key := r.Code + "\x00" + r.Detail
		if _, ok := seen[key]; !ok {
			seen[key] = r
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]Reason, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func sortPlanes(in []string) []string {
	seen := map[string]bool{}
	for _, p := range in {
		seen[p] = true
	}
	var out []string
	for _, p := range PlaneOrder {
		if seen[p] {
			out = append(out, p)
		}
	}
	return out
}
