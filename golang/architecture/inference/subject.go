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

import "unicode"

// isArchitecturalSubject reports whether a claim subject names something the
// architecture can be asked about: a qualified symbol (`doctor.configuredCommands`),
// a path (`internal/doctor/doctor.go`), an id (`component.golang.rigor`), or an
// exported bare identifier (`ProtectedPaths`).
//
// A BARE UNEXPORTED identifier is not. It is a name that exists only inside one
// function body, so no test, runtime observation, or source reading anyone could
// supply would establish or refute a claim about it.
//
// The test is deliberately structural rather than a length or vocabulary
// heuristic: `seen` and `lit` are rejected for the same reason as any other
// bare lowercase name, and a package-qualified unexported symbol is KEPT,
// because a package-level declaration is a real architectural surface even when
// it is not exported.
func isArchitecturalSubject(subject string) bool {
	if subject == "" {
		return false
	}
	for _, r := range subject {
		// Qualification of any kind — package, path, or id namespace — means the
		// name refers to something outside a single function body.
		if r == '.' || r == '/' || r == ':' || r == '#' || r == ' ' {
			return true
		}
	}
	first := []rune(subject)[0]
	return unicode.IsUpper(first)
}
