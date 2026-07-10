// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// TestPatterns_TypesEdgesAndReverse imports a DesignPattern + ImplementationPattern
// + PatternMisuse and asserts the typed nodes, the realizes/realizedBy pair, the
// misuse edges, and the reverse aw:relatedPattern that makes patterns visible
// when resolving an invariant or component.
func TestPatterns_TypesEdgesAndReverse(t *testing.T) {
	root := makeDir(t, map[string]string{
		"dp.yaml": `
id: pattern.test_rop
class: DesignPattern
name: TestROP
category: UI
applies_when: when a UI shows backend state
does_not_apply_when: when the surface must write
failure_modes_prevented:
  - test.fm.authority
forbidden_misuses:
  - pattern_misuse.test_ui
related_invariants:
  - test.inv.compiled
related_components:
  - component.test_dash
`,
		"ip.yaml": `
id: implementation_pattern.test_rop_impl
class: ImplementationPattern
name: TestROPImpl
implements_pattern: pattern.test_rop
used_by_components:
  - component.test_dash
satisfies_invariants:
  - test.inv.compiled
blocks:
  - pattern_misuse:pattern_misuse.test_ui
`,
		"pm.yaml": `
id: pattern_misuse.test_ui
class: PatternMisuse
name: TestUIMisuse
status: guardrail
misused_pattern: pattern.test_rop
safer_pattern: pattern.test_rop
forbidden_by:
  - forbidden_fix:test.ff.ui_write
causes_failure_modes:
  - test.fm.authority
`,
	})
	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	dp := strings.Trim(rdf.MintIRI(rdf.ClassDesignPattern, "pattern.test_rop"), "<>")
	ip := strings.Trim(rdf.MintIRI(rdf.ClassImplementationPattern, "implementation_pattern.test_rop_impl"), "<>")
	pm := strings.Trim(rdf.MintIRI(rdf.ClassPatternMisuse, "pattern_misuse.test_ui"), "<>")
	inv := strings.Trim(rdf.MintIRI(rdf.ClassInvariant, "test.inv.compiled"), "<>")
	comp := strings.Trim(rdf.MintIRI(rdf.ClassComponent, "component.test_dash"), "<>")
	ff := strings.Trim(rdf.MintIRI(rdf.ClassForbiddenFix, "test.ff.ui_write"), "<>")

	// Types.
	wantTriple(t, out, "<"+dp+"> <"+rdf.PropType+"> <"+rdf.ClassDesignPattern+">", "DesignPattern typed")
	wantTriple(t, out, "<"+ip+"> <"+rdf.PropType+"> <"+rdf.ClassImplementationPattern+">", "ImplementationPattern typed")
	wantTriple(t, out, "<"+pm+"> <"+rdf.PropType+"> <"+rdf.ClassPatternMisuse+">", "PatternMisuse typed")

	// DesignPattern negative-rule literals.
	wantTriple(t, out, "<"+dp+"> <"+rdf.PropAppliesWhen+">", "DesignPattern appliesWhen")
	wantTriple(t, out, "<"+dp+"> <"+rdf.PropDoesNotApplyWhen+">", "DesignPattern doesNotApplyWhen")
	wantTriple(t, out, "<"+dp+"> <"+rdf.PropForbids+"> <"+pm+">", "DesignPattern forbids PatternMisuse")

	// realizes / realizedBy pair.
	wantTriple(t, out, "<"+ip+"> <"+rdf.PropRealizes+"> <"+dp+">", "IP realizes DP")
	wantTriple(t, out, "<"+dp+"> <"+rdf.PropRealizedBy+"> <"+ip+">", "DP realizedBy IP (reverse)")

	// Reverse relatedPattern — patterns visible when resolving the node.
	wantTriple(t, out, "<"+inv+"> <"+rdf.PropRelatedPattern+"> <"+dp+">", "invariant relatedPattern DP")
	wantTriple(t, out, "<"+comp+"> <"+rdf.PropRelatedPattern+"> <"+dp+">", "component relatedPattern DP")
	wantTriple(t, out, "<"+comp+"> <"+rdf.PropRelatedPattern+"> <"+ip+">", "component relatedPattern IP")

	// PatternMisuse edges.
	wantTriple(t, out, "<"+pm+"> <"+rdf.PropMisuses+"> <"+dp+">", "PatternMisuse misuses DP")
	wantTriple(t, out, "<"+pm+"> <"+rdf.PropForbiddenBy+"> <"+ff+">", "PatternMisuse forbiddenBy ForbiddenFix")
	wantTriple(t, out, "<"+ip+"> <"+rdf.PropBlocks+"> <"+pm+">", "IP blocks PatternMisuse")
	wantTriple(t, out, "<"+pm+"> <"+rdf.PropStatus+"> \"guardrail\"", "PatternMisuse status")
}
