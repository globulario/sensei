// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// A rule with a detect block must compile to the aw:detect* triples the
// EditCheck path reads. This is the producer half of warning-level enforcement:
// without these triples, EditCheck has no pattern to match against.
func TestDetect_InvariantWithDetectBlock_EmitsDetectTriples(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: caddy.reverseproxy.forwardauth_errf_preserves_location
    title: Use dispenser.Errf
    repo: github.com/caddyserver/caddy
    domain: repo
    protects:
      files:
        - modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go
    detect:
      applies_to_paths:
        - "modules/caddyhttp/**/caddyfile.go"
      forbidden_pattern: '\bfmt\.Errorf\('
      required_pattern: '\bdispenser\.Errf\('
      message: "Use dispenser.Errf so the Caddyfile error keeps its location."
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	ntContains(t, out, rdf.IRI(rdf.PropDetectForbiddenPattern), "forbidden pattern emitted")
	ntContains(t, out, `fmt`, "forbidden pattern value present")
	ntContains(t, out, rdf.IRI(rdf.PropDetectRequiredPattern), "required pattern emitted")
	ntContains(t, out, `dispenser`, "required pattern value present")
	ntContains(t, out, rdf.IRI(rdf.PropDetectAppliesToPath), "applies-to-path emitted")
	ntContains(t, out, rdf.IRI(rdf.PropDetectMessage), "detect message emitted")
}

// An inert detect block (no forbidden/required pattern) emits nothing — a
// message or path alone has nothing to match, so it must not pollute the graph.
func TestDetect_InertBlock_EmitsNothing(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: globular.some.rule
    title: A rule with no detectable shape
    detect:
      message: "advice with no pattern"
    protects:
      files:
        - golang/server/scope.go
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	ntOmits(t, out, rdf.IRI(rdf.PropDetectForbiddenPattern), "no forbidden pattern")
	ntOmits(t, out, rdf.IRI(rdf.PropDetectRequiredPattern), "no required pattern")
	ntOmits(t, out, rdf.IRI(rdf.PropDetectMessage), "inert block emits no message")
}

// enforcement: block emits aw:detectEnforcement; the default (warn / absent)
// emits nothing — so existing warn-level rules compile byte-identical.
func TestDetect_Enforcement_OnlyBlockEmits(t *testing.T) {
	root := makeDir(t, map[string]string{
		"invariants.yaml": `
invariants:
  - id: caddy.rule.block
    title: a blocking rule
    detect:
      forbidden_pattern: '\bfmt\.Errorf\('
      enforcement: block
  - id: caddy.rule.warn
    title: a warn-default rule
    detect:
      forbidden_pattern: '\bpanic\('
`,
	})
	out, _ := importDirToString(t, root)
	assertValidNT(t, out)
	ntContains(t, out, rdf.IRI(rdf.PropDetectEnforcement), "block rule emits aw:detectEnforcement")
	ntContains(t, out, `"block"`, "enforcement value is block")
	// Exactly one enforcement triple — the warn-default rule emits none.
	if n := strings.Count(out, rdf.IRI(rdf.PropDetectEnforcement)); n != 1 {
		t.Errorf("expected exactly 1 aw:detectEnforcement triple (block only), got %d", n)
	}
}
