// SPDX-License-Identifier: AGPL-3.0-only

package store

import "testing"

// The ONE shared lexical IRI validator: canonical project IRI forms pass; anything that could
// escape a SPARQL <...> IRIREF token, look like a filesystem path, or smuggle whitespace/control
// characters is rejected.
func TestValidateQueryIRI(t *testing.T) {
	accept := []string{
		"aw:contract/example",
		"invariant:example",
		"urn:sensei:example",
		"https://globular.io/awareness/example",
		"https://globular.io/awareness#Contract",
		"aw:node/003",
	}
	for _, iri := range accept {
		if err := ValidateQueryIRI(iri); err != nil {
			t.Errorf("canonical IRI %q must be accepted: %v", iri, err)
		}
	}
	reject := []string{
		"",
		" aw:padded ",
		"aw:with space",
		"aw:tab\there",
		"aw:newline\n",
		"aw:x> . ?s ?p ?o . FILTER(<aw:y", // SPARQL token escape
		"aw:x<y",
		`aw:x"y`,
		"aw:x{y}",
		"aw:x|y",
		"aw:x^y",
		"aw:x`y",
		`aw:back\slash`,
		"/etc/passwd",
		`C:/Users/x`,
		"noscheme",
		":nobody",
		"9bad:scheme",
		"aw:",
		"aw:x\x00y",
	}
	for _, iri := range reject {
		if err := ValidateQueryIRI(iri); err == nil {
			t.Errorf("malicious/malformed IRI %q must be rejected", iri)
		}
	}
}

// The validator rejects ALL Unicode whitespace/control characters — not only ASCII — while
// legal non-ASCII IRI characters stay accepted.
func TestValidateQueryIRI_UnicodeControls(t *testing.T) {
	reject := []string{
		"aw:x\u0085y", // NEXT LINE (C1 control)
		"aw:x\u009fy", // APPLICATION PROGRAM COMMAND (C1 control)
		"aw:x\u00a0y", // NO-BREAK SPACE
		"aw:x\u2028y", // LINE SEPARATOR
		"aw:x\u2003y", // EM SPACE
	}
	for _, iri := range reject {
		if err := ValidateQueryIRI(iri); err == nil {
			t.Errorf("Unicode whitespace/control IRI %q must be rejected", iri)
		}
	}
	accept := []string{
		"aw:contrat/\u00e9valuation",                    // accented ucschar (é)
		"https://globular.io/awareness#Gr\u00fc\u00dfe", // non-ASCII fragment (üß)
		"urn:sensei:\u4f8b",                             // CJK ucschar (例)
	}
	for _, iri := range accept {
		if err := ValidateQueryIRI(iri); err != nil {
			t.Errorf("legal non-ASCII IRI %q must stay accepted: %v", iri, err)
		}
	}
}
