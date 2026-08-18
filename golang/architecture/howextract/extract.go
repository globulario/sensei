// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"fmt"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/extractbudget"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// Options binds deterministic extraction inputs supplied by the orchestrator.
type Options struct {
	CapturedAt string
	Repository architecture.ClaimDocumentBinding
	// ResourceLimits is the legacy caller-declared string map. It is recorded
	// verbatim and enforces nothing. Kept because existing receipts carry it;
	// use Budget for anything that must actually bind.
	ResourceLimits map[string]string
	// Budget is the enforced resource contract. Its zero value is unbounded,
	// so this is additive: a caller that supplies nothing gets exactly the
	// behaviour it had before, and one that supplies limits gets a run that
	// refuses to exceed them and a receipt that says what it did not search.
	Budget extractbudget.Budget
	// Diff, when set, asks for an incremental extraction bound to an exact
	// base/head pair. It narrows which files may produce observations; it
	// never narrows the semantic inputs, because a changed file's types can
	// come from anywhere in the module.
	Diff *DiffBinding
}

// Extract parses the codebase using explicit deterministic inputs and returns
// a complete normalized Phase 10 investigation Document.
func Extract(root string, opts Options) (investigation.Document, error) {
	return ExtractContext(context.Background(), root, opts)
}

// ExtractContext is Extract with a caller-owned context, so a wall-clock
// budget and an external cancellation reach the type-checker rather than
// being noticed only after it finishes.
//
// A cancelled run is reported as a cancelled run. It is never a
// zero-observation success, and never blamed on a budget limit it may not
// have reached -- widening a limit that was not the constraint is exactly the
// wrong response, and a receipt that suggested it would cause that.
func ExtractContext(ctx context.Context, root string, opts Options) (investigation.Document, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := time.Parse(time.RFC3339, opts.CapturedAt); err != nil {
		return investigation.Document{}, fmt.Errorf("captured_at must be an explicit RFC3339 input: %w", err)
	}
	// Validate BEFORE normalizing. Normalization would turn "/etc" into "etc"
	// and search a directory the caller never named -- a budget that cannot be
	// honoured as written is refused, not repaired into a plausible one.
	if err := opts.Budget.Validate(); err != nil {
		return investigation.Document{}, err
	}
	// The wall-clock ceiling binds the WHOLE extraction, not just the package
	// load. It previously derived a deadline inside the semantic extractor and
	// discarded it on return, so the AST walk, source-manifest hashing, and
	// evidence capture all ran unbounded afterwards -- and a caller
	// cancellation after the load was ignored entirely. A limit documented as
	// a wall-clock ceiling that stops applying partway through is worse than
	// no limit: it is a ceiling the receipt claims was enforced.
	if deadline, bounded := opts.Budget.Deadline(time.Now()); bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	opts.Budget = opts.Budget.Normalize()
	return extractAll(ctx, root, opts)
}
