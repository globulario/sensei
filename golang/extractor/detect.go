// SPDX-License-Identifier: Apache-2.0

// Detect-block emission — advisory rule metadata for warning-level enforcement.
//
// A rule MAY carry a narrow, deterministic `detect:` block describing a
// bad-shape edit. The serving-side EditCheck path matches these patterns
// against a proposed edit and emits a WARNING naming the rule — it never
// blocks, never gates CI, never edits code. This file is the producer: it
// turns the YAML `detect:` block into aw:detect* triples. A rule with no
// detect block emits nothing here (existing entries are unaffected).
package extractor

import (
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// detectRule is the YAML shape of a rule's `detect:` block. Embedded (named
// field, not inline) into the per-class shapes that support warnings.
type detectRule struct {
	AppliesToPaths   []string `yaml:"applies_to_paths"`
	ForbiddenPattern string   `yaml:"forbidden_pattern"`
	RequiredPattern  string   `yaml:"required_pattern"`
	Message          string   `yaml:"message"`
	// Enforcement is "warn" (default) or "block". It does NOT change EditCheck's
	// advisory behaviour; it only lets a gate caller tell a warn from a
	// would-block. Empty → warn.
	Enforcement string `yaml:"enforcement"`
}

// emitDetect writes the detect triples for one node. A block with neither a
// forbidden nor a required pattern is inert (nothing to match), so it emits
// nothing — applies_to_paths/message without a pattern would be meaningless.
func emitDetect(e *rdf.Emitter, subj string, d detectRule) {
	fp := strings.TrimSpace(d.ForbiddenPattern)
	rp := strings.TrimSpace(d.RequiredPattern)
	if fp == "" && rp == "" {
		return
	}
	if fp != "" {
		e.Triple(subj, rdf.IRI(rdf.PropDetectForbiddenPattern), rdf.Lit(fp))
	}
	if rp != "" {
		e.Triple(subj, rdf.IRI(rdf.PropDetectRequiredPattern), rdf.Lit(rp))
	}
	for _, p := range d.AppliesToPaths {
		if s := strings.TrimSpace(p); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropDetectAppliesToPath), rdf.Lit(s))
		}
	}
	if m := strings.TrimSpace(d.Message); m != "" {
		e.Triple(subj, rdf.IRI(rdf.PropDetectMessage), rdf.Lit(m))
	}
	// Only emit a non-default enforcement; "warn" is the implicit default, so
	// keeping it implicit leaves warn-level entries byte-identical.
	if enf := strings.ToLower(strings.TrimSpace(d.Enforcement)); enf == "block" {
		e.Triple(subj, rdf.IRI(rdf.PropDetectEnforcement), rdf.Lit("block"))
	}
}
