// SPDX-License-Identifier: AGPL-3.0-only

package maintenance

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

func normalizeReport(in Report) Report {
	r := in
	r.SchemaVersion = SchemaVersion
	if r.GeneratedBy == "" {
		r.GeneratedBy = GeneratedBy
	}
	sort.SliceStable(r.ClaimEvaluations, func(i, j int) bool { return r.ClaimEvaluations[i].ClaimID < r.ClaimEvaluations[j].ClaimID })
	for i := range r.ClaimEvaluations {
		r.ClaimEvaluations[i].Reasons = dedupeReasons(r.ClaimEvaluations[i].Reasons)
		r.ClaimEvaluations[i].ProofLanes = normalizeProofLanes(r.ClaimEvaluations[i].ProofLanes)
		sort.SliceStable(r.ClaimEvaluations[i].OpenQuestions, func(a, b int) bool {
			return r.ClaimEvaluations[i].OpenQuestions[a].QuestionID < r.ClaimEvaluations[i].OpenQuestions[b].QuestionID
		})
	}
	sort.SliceStable(r.RetiredClaims, func(i, j int) bool { return r.RetiredClaims[i].ClaimID < r.RetiredClaims[j].ClaimID })
	return r
}

func normalizeProofLanes(l ProofLanes) ProofLanes {
	l.Binding.Reasons = dedupeReasons(l.Binding.Reasons)
	l.PremiseFacts.Reasons = dedupeReasons(l.PremiseFacts.Reasons)
	l.Dependencies.Reasons = dedupeReasons(l.Dependencies.Reasons)
	l.SupportingEvidence.Reasons = dedupeReasons(l.SupportingEvidence.Reasons)
	l.RefutingEvidence.Reasons = dedupeReasons(l.RefutingEvidence.Reasons)
	l.Conflict.Reasons = dedupeReasons(l.Conflict.Reasons)
	l.Supersession.Reasons = dedupeReasons(l.Supersession.Reasons)
	return l
}

func mergeLane(a, b LaneState) LaneState {
	state := LaneCurrent
	if a.State == LaneStale || b.State == LaneStale {
		state = LaneStale
	} else if a.State == LaneUnknown || b.State == LaneUnknown {
		state = LaneUnknown
	}
	return LaneState{State: state, Reasons: dedupeReasons(append(a.Reasons, b.Reasons...))}
}

func stateFromBooleans(present, current, stale, unknown bool, currentCode string) LaneState {
	switch {
	case !present:
		return LaneState{State: LaneAbsent}
	case stale:
		return LaneState{State: LaneStale}
	case unknown:
		return LaneState{State: LaneUnknown}
	case current:
		return LaneState{State: LaneCurrent, Reasons: []Reason{{Code: currentCode}}}
	default:
		return LaneState{State: LaneInactive}
	}
}

func evidenceLaneState(present, active, stale, unknown bool, reasons []Reason) LaneState {
	switch {
	case !present:
		return LaneState{State: LaneAbsent}
	case active:
		return LaneState{State: LaneActive, Reasons: dedupeReasons(reasons)}
	case stale:
		return LaneState{State: LaneStale, Reasons: dedupeReasons(reasons)}
	case unknown:
		return LaneState{State: LaneUnknown, Reasons: dedupeReasons(reasons)}
	default:
		return LaneState{State: LaneInactive, Reasons: dedupeReasons(reasons)}
	}
}

func sortedEvaluations(in map[string]ClaimEvaluation) []ClaimEvaluation {
	ids := make([]string, 0, len(in))
	for id := range in {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ClaimEvaluation, 0, len(ids))
	for _, id := range ids {
		out = append(out, in[id])
	}
	return out
}

func dedupeReasons(in []Reason) []Reason {
	seen := map[string]Reason{}
	var keys []string
	for _, r := range in {
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

func filterReasons(in []Reason, prefix string) []Reason {
	var out []Reason
	for _, r := range in {
		if strings.HasPrefix(r.Code, prefix) {
			out = append(out, r)
		}
	}
	return dedupeReasons(out)
}

func reasonDetailsForUnknowns(status string, reasons []Reason) []string {
	if status != architecture.StatusUnknown && status != architecture.StatusStale {
		return nil
	}
	var out []string
	for _, r := range reasons {
		if r.Detail != "" && (strings.Contains(r.Code, "unavailable") || strings.Contains(r.Code, "mismatch") || strings.Contains(r.Code, "unknown") || strings.Contains(r.Code, "stale")) {
			out = append(out, r.Code+": "+r.Detail)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			seen[strings.TrimSpace(s)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func contains(in []string, want string) bool {
	for _, item := range in {
		if item == want {
			return true
		}
	}
	return false
}

func sameEvidenceInBoth(a, b []string) bool {
	seen := map[string]bool{}
	for _, item := range a {
		seen[strings.TrimPrefix(item, "evidence:")] = true
	}
	for _, item := range b {
		if seen[strings.TrimPrefix(item, "evidence:")] {
			return true
		}
	}
	return false
}

func debugClaim(c architecture.Claim) string {
	return fmt.Sprintf("%s %s %s", c.Statement.Subject, c.Statement.Predicate, c.Statement.Object)
}
