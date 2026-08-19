// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph

import "strings"

// sparqlLiteral renders s as a SPARQL string literal with the escapes the
// grammar requires, so a path containing a quote or a backslash cannot
// terminate the literal early or inject a pattern.
func sparqlLiteral(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}
