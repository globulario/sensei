// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

// A composed statement may quote the law an item hangs off, never where that
// law applies. source_file and test rows are the graph's account of WHERE an
// item governs, and putting them in front of an adjudicator hands over the
// answer key by another route — the blinding rule is about the information,
// not about the field it arrives in.
func TestAComposedStatementMayNotQuoteFileOrTestRelations(t *testing.T) {
	for _, leaky := range []string{"source_file", "test", "code_symbol", "symbol"} {
		if relatedGoverningClasses[leaky] {
			t.Fatalf("class %q may be quoted into a composed statement, which tells the adjudicator where an item applies", leaky)
		}
	}
	for _, governing := range []string{"invariant", "failure_mode", "forbidden_fix", "contract"} {
		if !relatedGoverningClasses[governing] {
			t.Fatalf("class %q cannot be quoted, so items whose only meaning is next door stay unjudgeable", governing)
		}
	}
}

// The slug is graph content; humanizing only changes how it reads.
func TestHumanizeSlugKeepsTheIdentifiersMeaning(t *testing.T) {
	got := humanizeSlug("aggregate_scoped_value_into_global_without_aggregation_function")
	if !strings.Contains(got, "aggregate scoped value into global") {
		t.Fatalf("the slug lost its words: %q", got)
	}
	if strings.Contains(got, "_") {
		t.Fatalf("the slug is still an identifier rather than a phrase: %q", got)
	}
	if humanizeSlug("meta.assertions_carry_scope") != "meta · assertions carry scope" {
		t.Fatalf("a dotted namespace did not survive humanizing: %q", humanizeSlug("meta.assertions_carry_scope"))
	}
}

// resolve takes the class name production accepts, which is the snake_case one
// by_class already returned. Translating to the CamelCase the help text
// advertises made every multi-word class resolve at 0% while the single-word
// ones worked, so it read as a graph with no detail.
func TestResolveUsesTheClassNameProductionAccepts(t *testing.T) {
	for _, class := range governingClasses {
		if got := resolveClass(class); got != class {
			t.Fatalf("class %q is sent to resolve as %q; production rejects anything but the name by_class returned", class, got)
		}
		if strings.ToLower(class) != class {
			t.Fatalf("governing class %q is not the snake_case name production uses", class)
		}
	}
}
