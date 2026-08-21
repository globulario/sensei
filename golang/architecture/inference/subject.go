// SPDX-License-Identifier: AGPL-3.0-only

package inference

// What a derived claim is allowed to be ABOUT.
//
// A claim's subject is the thing the architecture is being asked about, and an
// unsupported claim does not stay quiet: it becomes an OpenQuestion, an
// unanswered OpenQuestion blocks closure, and closure gates admission. So a
// claim about implementation trivia is not merely noise — it is a question
// nobody can answer, standing in front of a bounded change (#230).
//
// Observed live: 743 claims of the form "seen has_observed_writer_set …", where
// `seen` is a local variable, and 88 claims whose subject was one to three
// characters — `lit`, `m`, `id`, `fn`, `t`, `v`. The proposition that thirteen
// unrelated functions each declare a local called `seen` is true, trivial, and
// unanswerable as architecture.
//
// The facts are untouched. A local write is a real observation and stays in the
// document as one; what stops is promoting it into a proposition about the
// architecture.

// isArchitecturalSubject reports whether a claim subject names something the
// architecture can be asked about: a qualified symbol
// (`doctor.configuredCommands`), a path (`internal/doctor/doctor.go`), or an id
// (`component.golang.rigor`).
//
// A BARE identifier is not — exported or otherwise, and the exported case is
// the one worth explaining, because accepting it was this filter's first draft.
//
// `invariantWriteTarget` records a selector write as its final segment only, so
// `report.Authority.State` and an unrelated `status.State` both arrive as the
// bare subject `State`. Accepting bare exported names would therefore keep
// exactly the cross-file conflation this filter exists to remove: one claim,
// one writer set, assembled from writes to different things that happen to
// share a field name — and its unsupported claim can still expand and block an
// unrelated task.
//
// So the test is qualification, not capitalisation. It is structural rather
// than a length or vocabulary heuristic, and it cuts in both directions: a
// package-qualified UNEXPORTED symbol is kept, because a package-level
// declaration is a real surface even when it is not exported, while a bare
// EXPORTED one is not, because nothing in it says which thing it names.
//
// The narrower repair — teaching the extractor to keep the full selector path —
// would make those subjects qualified and admissible again. It changes fact
// identity, and therefore claim identity across the whole corpus, so it is not
// folded into this fix.
func isArchitecturalSubject(subject string) bool {
	for _, r := range subject {
		// Qualification of any kind — package, path, or id namespace — means the
		// name refers to something outside a single function body, and says
		// which thing it refers to.
		if r == '.' || r == '/' || r == ':' || r == '#' || r == ' ' {
			return true
		}
	}
	return false
}
