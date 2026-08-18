// SPDX-License-Identifier: AGPL-3.0-only

// Package extractbudget is the closed resource contract for one HOW
// extraction, and the machinery that makes it bind.
//
// It exists because the previous "budget" was a map[string]string the caller
// filled in and the receipt faithfully reproduced. Nothing read it. Full-
// repository HOW extraction did not finish inside a 60-second wall clock, and
// the receipt still recorded whatever limits the caller had claimed -- a
// document that looks like evidence of bounded work and is evidence only of
// what someone typed.
//
// Two rules shape everything here:
//
//  1. A limit is only a limit if something refuses to exceed it. Every field
//     below is consumed mechanically by the extraction path, and the
//     Consumption a run reports is measured, never echoed. What each limit
//     bounds is stated precisely rather than uniformly -- see
//     gosemantics.ExtractBounded: the file/byte/scope limits bound which files
//     may produce observations and everything downstream of that, while only
//     MaxWallClock bounds the package load. A contract that claimed all seven
//     bound the same stage would be a more comfortable and less true
//     description of what a bounded run costs.
//
//  2. A partial result is valid evidence, and must never be mistaken for a
//     complete one. Truncation is deterministic, recorded in the receipt as an
//     explicit limitation naming what was NOT searched, and moves the status
//     off "completed". Silence about a cut is the failure mode this package
//     exists to prevent: a caller reading a partial document as complete draws
//     a conclusion from absence that the absence does not support.
package extractbudget

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// Status is the closed disposition vocabulary for one bounded extraction.
// These are deliberately distinct: "we stopped because the budget said so" and
// "the extractor could not run" are different facts about a repository, and a
// caller that collapses them will retry the wrong one.
type Status string

const (
	// StatusCompleted means the extractor examined everything the scopes
	// admitted and no limit was reached.
	StatusCompleted Status = "completed"
	// StatusPartial means work was skipped for a reason other than a budget
	// limit (an unparseable package, an unavailable sub-extractor) while the
	// run itself finished.
	StatusPartial Status = "partial"
	// StatusBudgetExhausted means a limit in this contract stopped the run.
	StatusBudgetExhausted Status = "budget_exhausted"
	// StatusUnavailable means the extraction could not produce a result at
	// all. It is not a zero-observation success.
	StatusUnavailable Status = "unavailable"
	// StatusCancelled means the caller's context ended the run. Distinct from
	// budget exhaustion: the budget was honoured, the caller changed its mind.
	StatusCancelled Status = "cancelled"
)

// IsValidStatus reports whether s is one of the closed vocabulary above.
func IsValidStatus(s Status) bool {
	switch s {
	case StatusCompleted, StatusPartial, StatusBudgetExhausted, StatusUnavailable, StatusCancelled:
		return true
	default:
		return false
	}
}

// Budget is the closed resource contract. A zero value in any numeric field
// means "unbounded for this dimension" -- so the zero Budget is exactly
// today's unbounded behaviour, and adopting this contract is opt-in per
// dimension rather than a cliff every existing caller falls off.
//
// Scopes are repo-relative, slash-separated path prefixes. They are part of
// the contract, not a convenience: a run that searched half a repository and a
// run that searched all of a deliberately narrowed one produce the same
// observations for entirely different reasons, and only the recorded scope
// tells them apart.
type Budget struct {
	MaxWallClock            time.Duration
	MaxFiles                int
	MaxSourceBytes          int64
	MaxPackages             int
	MaxObservations         int
	MaxEvidenceReceipts     int
	MaxCapturedContentBytes int64
	IncludePaths            []string
	ExcludePaths            []string
}

// Bounded reports whether this budget constrains anything at all.
func (b Budget) Bounded() bool {
	return b.MaxWallClock > 0 || b.MaxFiles > 0 || b.MaxSourceBytes > 0 ||
		b.MaxPackages > 0 || b.MaxObservations > 0 || b.MaxEvidenceReceipts > 0 ||
		b.MaxCapturedContentBytes > 0 || len(b.IncludePaths) > 0 || len(b.ExcludePaths) > 0
}

// Normalize trims, de-duplicates, and sorts the scopes so two callers that
// expressed the same intent produce byte-identical receipts. It does not
// invent defaults: an unbounded dimension stays unbounded, visibly.
func (b Budget) Normalize() Budget {
	b.IncludePaths = normalizeScopes(b.IncludePaths)
	b.ExcludePaths = normalizeScopes(b.ExcludePaths)
	if b.MaxWallClock < 0 {
		b.MaxWallClock = 0
	}
	for _, p := range []*int{&b.MaxFiles, &b.MaxPackages, &b.MaxObservations, &b.MaxEvidenceReceipts} {
		if *p < 0 {
			*p = 0
		}
	}
	for _, p := range []*int64{&b.MaxSourceBytes, &b.MaxCapturedContentBytes} {
		if *p < 0 {
			*p = 0
		}
	}
	return b
}

// Validate refuses a budget that cannot be honoured as written. An
// include/exclude pair that excludes everything the include admitted is the
// interesting case: it would silently produce an empty, "completed" extraction
// of a repository that was never searched.
func (b Budget) Validate() error {
	// Checked on the RAW scopes, before normalization. Normalization would
	// happily turn "/etc" into "etc" -- quietly reinterpreting an absolute
	// path as a repo-relative one and searching a directory the caller did not
	// name. A scope that cannot be honoured as written is refused, not
	// repaired into something plausible.
	for _, raw := range [...]struct {
		kind  string
		paths []string
	}{{"include", b.IncludePaths}, {"exclude", b.ExcludePaths}} {
		for _, p := range raw.paths {
			p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
			if strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") {
				return fmt.Errorf("extractbudget: %s scope %q must be a repo-relative path that does not escape the root", raw.kind, p)
			}
		}
	}
	b = b.Normalize()
	if len(b.IncludePaths) > 0 {
		admitted := false
		for _, in := range b.IncludePaths {
			if !excludedBy(in, b.ExcludePaths) {
				admitted = true
				break
			}
		}
		if !admitted {
			return fmt.Errorf("extractbudget: every include scope is excluded; this would report an empty extraction as a complete one")
		}
	}
	return nil
}

// InScope reports whether a repo-relative, slash-separated path is admitted by
// this budget's scopes. Exclude wins over include, because the narrower,
// negative statement is the one a caller is more likely to have meant
// literally.
func (b Budget) InScope(relSlash string) bool {
	relSlash = strings.TrimPrefix(path.Clean(relSlash), "./")
	if excludedBy(relSlash, b.ExcludePaths) {
		return false
	}
	if len(b.IncludePaths) == 0 {
		return true
	}
	for _, in := range b.IncludePaths {
		if underPrefix(relSlash, in) {
			return true
		}
	}
	return false
}

// Consumption is what a run actually used. Every field is measured by the
// extraction path; none is copied from Budget. A receipt whose consumption
// equals its budget by construction would prove nothing.
//
// Elapsed wall clock is deliberately ABSENT. These receipts are compared by
// digest and asserted deep-equal across identical runs, and a measured
// duration is never equal twice -- recording it would make every HOW document
// nondeterministic to report a number nobody can act on. Whether the wall
// clock bound the run is reported instead, as a disposition, which is the part
// that carries meaning.
type Consumption struct {
	Files                int   `json:"files" yaml:"files"`
	SourceBytes          int64 `json:"source_bytes" yaml:"source_bytes"`
	Packages             int   `json:"packages" yaml:"packages"`
	Observations         int   `json:"observations" yaml:"observations"`
	EvidenceReceipts     int   `json:"evidence_receipts" yaml:"evidence_receipts"`
	CapturedContentBytes int64 `json:"captured_content_bytes" yaml:"captured_content_bytes"`
}

// Exceeded returns the names of every dimension in which consumption reached
// or passed its bound, in stable order. It is the single place that decides
// "the budget stopped this", so a caller cannot reach that conclusion by
// eyeballing two numbers and getting the comparison backwards.
func (b Budget) Exceeded(c Consumption) []string {
	var hit []string
	add := func(name string, limit, used int64) {
		if limit > 0 && used >= limit {
			hit = append(hit, name)
		}
	}
	add("max_files", int64(b.MaxFiles), int64(c.Files))
	add("max_source_bytes", b.MaxSourceBytes, c.SourceBytes)
	add("max_packages", int64(b.MaxPackages), int64(c.Packages))
	add("max_observations", int64(b.MaxObservations), int64(c.Observations))
	add("max_evidence_receipts", int64(b.MaxEvidenceReceipts), int64(c.EvidenceReceipts))
	add("max_captured_content_bytes", b.MaxCapturedContentBytes, c.CapturedContentBytes)
	sort.Strings(hit)
	return hit
}

func normalizeScopes(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		s = strings.Trim(strings.TrimPrefix(path.Clean(strings.ReplaceAll(s, "\\", "/")), "./"), "/")
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func excludedBy(relSlash string, excludes []string) bool {
	for _, ex := range excludes {
		if underPrefix(relSlash, ex) {
			return true
		}
	}
	return false
}

// underPrefix matches on path segments, never on raw string prefixes: an
// exclude of "internal" must not also swallow "internal_docs/".
func underPrefix(relSlash, prefix string) bool {
	if prefix == "" {
		return true
	}
	if relSlash == prefix {
		return true
	}
	return strings.HasPrefix(relSlash, prefix+"/")
}
