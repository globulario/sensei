// SPDX-License-Identifier: Apache-2.0

// Phase C importer for intent YAML files.
//
// Intent files capture the design intent behind the project — WHY code exists,
// WHAT it protects, and HOW it relates to neighbouring concepts. They are
// distinct from operational awareness (invariants, failure modes, etc.) and
// must be imported as a separate hierarchy so SPARQL queries can filter by
// knowledge type.
//
// Each intent file produces one Intent node typed with both aw:Intent and the
// most specific subclass derived from the "level" field:
//
//	principle / pattern                 → aw:DesignIntent
//	mechanism / operator_model / impl   → aw:OperationalIntent
//	vision                              → aw:ProductIntent
//	invariant / contract / safety_rule / constraint → aw:ConstraintIntent
//	(unknown level)                     → aw:Intent only
//
// Cross-links (zooms_out_to, zooms_in_to, related_to) point to other intent
// nodes by ID. related_invariants cross-link to existing aw:Invariant nodes.
// expressed_by entries become aw:SourceFile nodes.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/rdf"
)

// ── YAML shape ────────────────────────────────────────────────────────────────

type yamlIntent struct {
	domainScope        `yaml:",inline"`
	adoption.Receipt   `yaml:",inline"`
	ID                 string   `yaml:"id"`
	Level              string   `yaml:"level"`
	Title              string   `yaml:"title"`
	Intent             string   `yaml:"intent"`
	AgentGuidance      string   `yaml:"agent_guidance"`
	BadSmells          []string `yaml:"bad_smells"`
	ActivationTriggers []string `yaml:"activation_triggers"`
	ExpressedBy        []string `yaml:"expressed_by"`
	RequiredTests      []string `yaml:"required_tests"`
	ZoomsOutTo         []string `yaml:"zooms_out_to"`
	ZoomsInTo          []string `yaml:"zooms_in_to"`
	RelatedTo          []string `yaml:"related_to"`
	RelatedInvariants  []string `yaml:"related_invariants"`
}

// intentSubclass maps the level string to the most specific RDF subclass.
// All intent nodes also carry rdf:type aw:Intent as the base class.
var intentSubclass = map[string]string{
	"principle":      rdf.ClassDesignIntent,
	"pattern":        rdf.ClassDesignIntent,
	"mechanism":      rdf.ClassOperationalIntent,
	"operator_model": rdf.ClassOperationalIntent,
	"implementation": rdf.ClassOperationalIntent,
	"vision":         rdf.ClassProductIntent,
	"invariant":      rdf.ClassConstraintIntent,
	"contract":       rdf.ClassConstraintIntent,
	"safety_rule":    rdf.ClassConstraintIntent,
	"constraint":     rdf.ClassConstraintIntent,
}

// importIntent imports a single intent YAML file and emits its typed node
// with all cross-links. A file whose id is empty produces no triples.
func importIntent(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var intent yamlIntent
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}

	if intent.ID == "" {
		return nil
	}

	subj := rdf.MintIRI(rdf.ClassIntent, intent.ID)

	// Base class — always aw:Intent.
	e.Typed(subj, rdf.ClassIntent)

	// Specific subclass based on level (no subclass triple if level is unknown).
	if cls, ok := intentSubclass[intent.Level]; ok {
		e.Typed(subj, cls)
	}
	emitDomainScope(e, subj, intent.domainScope)

	// Core literals.
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(intent.Title, intent.ID)))

	if intent.Intent != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(intent.Intent)))
	}
	if intent.Level != "" {
		e.Triple(subj, rdf.IRI(rdf.PropLevel), rdf.Lit(intent.Level))
	}
	if err := emitAdoptionReceipt(e, subj, rdf.ClassIntent, intent.ID, intent.Receipt); err != nil {
		return fmt.Errorf("adoption receipt: %w", err)
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	// Multi-value literals.
	for _, s := range intent.ActivationTriggers {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropActivationTrigger), rdf.Lit(s))
		}
	}
	for _, s := range intent.BadSmells {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropBadSmell), rdf.Lit(s))
		}
	}

	// Hierarchical links to other intent nodes.
	for _, id := range intent.ZoomsOutTo {
		if id = strings.TrimSpace(id); id != "" {
			e.Triple(subj, rdf.IRI(rdf.PropZoomsOutTo), rdf.MintIRI(rdf.ClassIntent, id))
		}
	}
	for _, id := range intent.ZoomsInTo {
		if id = strings.TrimSpace(id); id != "" {
			e.Triple(subj, rdf.IRI(rdf.PropZoomsInto), rdf.MintIRI(rdf.ClassIntent, id))
		}
	}
	for _, id := range intent.RelatedTo {
		if id = strings.TrimSpace(id); id != "" {
			e.Triple(subj, rdf.IRI(rdf.PropRelatedTo), rdf.MintIRI(rdf.ClassIntent, id))
		}
	}

	// Cross-links to awareness invariant nodes.
	for _, id := range intent.RelatedInvariants {
		if id = strings.TrimSpace(id); id != "" {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, id))
		}
	}
	for _, testID := range intent.RequiredTests {
		if testID = strings.TrimSpace(testID); testID != "" {
			ensureNode(e, rdf.ClassTest, testID)
			e.Triple(subj, rdf.IRI(rdf.PropRequiresTest), rdf.MintIRI(rdf.ClassTest, testID))
		}
	}

	// Source file provenance — expressed_by paths become aw:SourceFile nodes.
	// The reverse implements edge lets briefing-by-file surface this intent
	// as a Direct anchor without requiring an @awareness annotation on the file.
	//
	// Intent files are authored with paths relative to the repo root that
	// contains both services/ and awareness-graph/ directories, so they often
	// carry a "services/" prefix. Strip it so IRIs match the paths used by
	// the annotation scanner (which runs from within services/).
	for _, f := range intent.ExpressedBy {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		f = strings.TrimPrefix(f, "services/")
		fileSubj := rdf.MintIRI(rdf.ClassSourceFile, f)
		e.Typed(fileSubj, rdf.ClassSourceFile)
		e.Triple(subj, rdf.IRI(rdf.PropExpressedBy), fileSubj)
		e.Triple(fileSubj, rdf.IRI(rdf.PropImplements), subj)
	}

	return nil
}
