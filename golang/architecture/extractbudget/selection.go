// SPDX-License-Identifier: AGPL-3.0-only

package extractbudget

import (
	"fmt"
	"sort"
	"time"

	"github.com/globulario/sensei/golang/architecture"
)

// Candidate is one file the extractor could examine, before the budget has
// had a say.
type Candidate struct {
	RelPath string // repo-relative, slash-separated
	AbsPath string
	Size    int64
}

// Selection is the deterministic, budget-bounded file set for one extraction.
//
// FirstUnsearched is the whole reason this is honest rather than merely
// bounded. Selection is sorted, so "everything from this path onward was not
// searched" is a complete statement of what the run did not look at -- a
// bounded string that says exactly as much as an unbounded list of skipped
// paths would, and can be written into a receipt without the receipt becoming
// the thing that exhausts the budget.
type Selection struct {
	Files           []string // absolute paths, sorted
	Consumption     Consumption
	Truncated       []string // budget dimensions that caused a cut, sorted
	FirstUnsearched string   // repo-relative path of the first file NOT searched
	UnsearchedFiles int
	UnsearchedBytes int64
	OutOfScopeFiles int
	// ScopesMatchedNothing is true when scopes were in force, candidates
	// existed, and none survived them. Without this the run would report
	// "completed" over zero observations -- which reads as evidence that the
	// repository contains nothing of interest, when the real fact is that a
	// scope was written that names nothing.
	ScopesMatchedNothing bool
}

// Select applies the budget's scopes and then its file/byte ceilings to a
// candidate set, deterministically.
//
// Order matters and is deliberate: scopes first, ceilings second. A caller who
// narrowed the search to one directory should get all of that directory before
// a global file ceiling starts cutting, not a ceiling consumed by files the
// scope had already ruled out.
//
// The cut is taken at a whole file. Half a file is not evidence, and a
// partially-read source file would produce observations attributed to a
// position that does not mean what the extractor thinks it means.
func Select(candidates []Candidate, b Budget) Selection {
	b = b.Normalize()

	inScope := make([]Candidate, 0, len(candidates))
	outOfScope := 0
	for _, c := range candidates {
		if b.InScope(c.RelPath) {
			inScope = append(inScope, c)
			continue
		}
		outOfScope++
	}
	sort.Slice(inScope, func(i, j int) bool { return inScope[i].RelPath < inScope[j].RelPath })

	sel := Selection{OutOfScopeFiles: outOfScope}
	truncated := map[string]bool{}
	for i, c := range inScope {
		if b.MaxFiles > 0 && len(sel.Files) >= b.MaxFiles {
			truncated["max_files"] = true
		}
		if b.MaxSourceBytes > 0 && sel.Consumption.SourceBytes+c.Size > b.MaxSourceBytes {
			truncated["max_source_bytes"] = true
		}
		// Break rather than skip. Skipping a file that overflows the byte
		// ceiling and taking a later, smaller one would make the selection
		// non-contiguous, and then "everything from FirstUnsearched onward was
		// not searched" -- the whole reason the cut is reportable in a bounded
		// string -- would simply be false.
		if len(truncated) > 0 {
			sel.FirstUnsearched = c.RelPath
			sel.UnsearchedFiles = len(inScope) - i
			for _, rest := range inScope[i:] {
				sel.UnsearchedBytes += rest.Size
			}
			break
		}
		sel.Files = append(sel.Files, c.AbsPath)
		sel.Consumption.Files++
		sel.Consumption.SourceBytes += c.Size
	}
	for name := range truncated {
		sel.Truncated = append(sel.Truncated, name)
	}
	sort.Strings(sel.Truncated)
	sel.ScopesMatchedNothing = len(sel.Files) == 0 && len(inScope) == 0 && outOfScope > 0
	return sel
}

// Limitations renders the selection's cuts as architecture limitations. They
// are non-blocking on purpose: a bounded extraction is valid evidence about
// what it searched. What makes it safe is that it says what it did not.
func (s Selection) Limitations(source string) []architecture.Limitation {
	var out []architecture.Limitation
	if len(s.Truncated) > 0 {
		out = append(out, architecture.Limitation{
			Source: source,
			Scope:  "repository",
			Reason: fmt.Sprintf("extraction budget reached (%v): %d file(s) totalling %d byte(s) were NOT searched, beginning at %s; observations describe only the searched subset",
				s.Truncated, s.UnsearchedFiles, s.UnsearchedBytes, s.FirstUnsearched),
			Blocking: false,
		})
	}
	if s.OutOfScopeFiles > 0 {
		out = append(out, architecture.Limitation{
			Source:   source,
			Scope:    "repository",
			Reason:   fmt.Sprintf("%d file(s) were outside the bound include/exclude scopes and were not searched", s.OutOfScopeFiles),
			Blocking: false,
		})
	}
	if s.ScopesMatchedNothing {
		out = append(out, architecture.Limitation{
			Source:   source,
			Scope:    "repository",
			Reason:   "the bound include/exclude scopes matched no source file; this document describes nothing, which is a fact about the scopes and not about the repository",
			Blocking: false,
		})
	}
	return out
}

// Receipt binds the budget a run was given, what it actually consumed, the
// scopes in force, and the disposition that follows from those three. It is
// the document that lets a later reader tell a complete extraction from a
// bounded one without re-running anything.
type Receipt struct {
	SchemaVersion string      `json:"schema_version" yaml:"schema_version"`
	Budget        Budget      `json:"budget" yaml:"budget"`
	Consumption   Consumption `json:"consumption" yaml:"consumption"`
	Status        Status      `json:"status" yaml:"status"`
	// ExhaustedDimensions names the limits that were reached, and is empty
	// unless Status is budget_exhausted -- so "which limit was too small" is
	// answerable without diffing budget against consumption by hand.
	ExhaustedDimensions []string `json:"exhausted_dimensions,omitempty" yaml:"exhausted_dimensions,omitempty"`
	IncludePaths        []string `json:"include_paths,omitempty" yaml:"include_paths,omitempty"`
	ExcludePaths        []string `json:"exclude_paths,omitempty" yaml:"exclude_paths,omitempty"`
	Detail              string   `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// ReceiptSchemaVersion versions the receipt shape above.
const ReceiptSchemaVersion = "sensei.extractbudget.receipt.v1"

// ComposeReceipt derives the run's disposition from measured consumption
// rather than accepting one.
//
// The precedence is the point. Cancellation outranks budget exhaustion
// because a cancelled run may not have reached a limit it would otherwise
// have hit, and reporting "budget_exhausted" would blame a limit for a
// decision the caller made. Unavailability outranks everything: a run that
// produced nothing has no consumption worth interpreting.
func ComposeReceipt(b Budget, c Consumption, outcome RunOutcome) Receipt {
	b = b.Normalize()
	r := Receipt{
		SchemaVersion: ReceiptSchemaVersion,
		Budget:        b,
		Consumption:   c,
		IncludePaths:  b.IncludePaths,
		ExcludePaths:  b.ExcludePaths,
	}
	hit := b.Exceeded(c)
	if outcome.WallClockExhausted {
		hit = append([]string{"max_wall_clock"}, hit...)
		sort.Strings(hit)
	}
	switch {
	case outcome.UnavailableReason != "":
		r.Status = StatusUnavailable
		r.Detail = outcome.UnavailableReason
	case outcome.Cancelled:
		r.Status = StatusCancelled
		r.Detail = "the caller's context ended the run; the budget was not the constraint"
	case len(hit) > 0:
		r.Status = StatusBudgetExhausted
		r.ExhaustedDimensions = hit
		r.Detail = fmt.Sprintf("stopped at the bound %v; the result describes only what was searched", hit)
	case outcome.Degraded:
		r.Status = StatusPartial
		r.Detail = "the run finished but some work was skipped for reasons other than the budget; see limitations"
	default:
		r.Status = StatusCompleted
	}
	return r
}

// RunOutcome is what only the run itself can report: the facts that are not
// derivable from comparing consumption against a budget.
//
// Cancelled and WallClockExhausted are separate on purpose, and the separation
// is the reason elapsed time is not in Consumption. Both end the run through
// the same context error, but they mean opposite things to whoever reads the
// receipt: one says "widen max_wall_clock", the other says "the caller stopped
// this, the budget was never the constraint". Deriving them from a measured
// duration would be both nondeterministic and unable to tell them apart.
type RunOutcome struct {
	Cancelled          bool
	WallClockExhausted bool
	UnavailableReason  string
	Degraded           bool
}

// Deadline returns the wall-clock deadline this budget implies from start, and
// whether it bounds anything.
func (b Budget) Deadline(start time.Time) (time.Time, bool) {
	if b.MaxWallClock <= 0 {
		return time.Time{}, false
	}
	return start.Add(b.MaxWallClock), true
}
