// SPDX-License-Identifier: AGPL-3.0-only

// Phase B importers for awareness YAML schemas that were previously reported
// as known_unsupported. Each function follows the same contract as the Phase A
// importers in yaml_import.go:
//
//   - Takes an *rdf.Emitter and a file path.
//   - Silently skips missing files (os.IsNotExist).
//   - Returns an error only for read/parse failures, never for empty lists.
//   - Links to existing invariant/failure_mode/incident_pattern IDs via
//     aw:affects or aw:forbids — never mints typed nodes for them (ownership
//     stays with the originating file).
//
// To promote a schema from Phase B to importable:
//  1. Add its importer function here.
//  2. Set importable=true in yaml_import_dir.go keySchemas.
//  3. Add a case in classifyAndImport's switch.
//  4. Add test fixtures and assertions in yaml_import_phaseB_test.go.
package extractor

import (
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// ── YAML shapes ───────────────────────────────────────────────────────────────

type forbiddenFixProtects struct {
	Files []string `yaml:"files"`
}

type yamlForbiddenFix struct {
	ID                string               `yaml:"id"`
	Title             string               `yaml:"title"`
	Summary           string               `yaml:"summary"`
	Protects          forbiddenFixProtects `yaml:"protects"`
	RelatedInvariants []string             `yaml:"related_invariants"`
	AppliesTo         []string             `yaml:"applies_to"`
	SafeAlternative   string               `yaml:"safe_alternative"`
	SaferAlternative  string               `yaml:"safer_alternative"`
	Reason            string               `yaml:"reason"`
	Detect            detectRule           `yaml:"detect"`
	Uml               umlProfile           `yaml:"uml"`
	domainScope       `yaml:",inline"`
}

// forbiddenFixesFileRaw uses yaml.Node so list items that are bare strings
// (ID references from failuregraph_seed files) are skipped without error.
type forbiddenFixesFileRaw struct {
	ForbiddenFixes []yaml.Node `yaml:"forbidden_fixes"`
}

type yamlRequiredTestProtects struct {
	Invariants   []string `yaml:"invariants"`
	FailureModes []string `yaml:"failure_modes"`
	Files        []string `yaml:"files"`
}
type yamlRequiredTest struct {
	ID       string                   `yaml:"id"`
	Title    string                   `yaml:"title"`
	Protects yamlRequiredTestProtects `yaml:"protects"`
	Uml      umlProfile               `yaml:"uml"`
}
type requiredTestsFile struct {
	RequiredTests []yamlRequiredTest `yaml:"required_tests"`
}

type yamlContract struct {
	ID          string `yaml:"id"`
	Domain      string `yaml:"domain"`
	Service     string `yaml:"service"`
	Kind        string `yaml:"kind"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
}
type contractsFile struct {
	Schema    string         `yaml:"schema"`
	Contracts []yamlContract `yaml:"contracts"`
}

type yamlIncident struct {
	IncidentID   string   `yaml:"incident_id"`
	Title        string   `yaml:"title"`
	Status       string   `yaml:"status"`
	Severity     string   `yaml:"severity"`
	RelatedFiles []string `yaml:"related_files"`
}

type yamlDecision struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Status            string   `yaml:"status"`
	Rationale         string   `yaml:"rationale"`
	RelatedInvariants []string `yaml:"related_invariants"`
	// Architectural-spine fields (Stage A) — all optional and additive.
	Context              string     `yaml:"context"`
	Consequences         string     `yaml:"consequences"`
	AlternativesRejected []string   `yaml:"alternatives_rejected"`
	Assertion            string     `yaml:"assertion"`
	DefinesBoundaries    []string   `yaml:"defines_boundaries"`
	DefinesContracts     []string   `yaml:"defines_contracts"`
	AffectsComponents    []string   `yaml:"affects_components"`
	Mitigates            []string   `yaml:"mitigates"` // failure_modes
	Rejects              []string   `yaml:"rejects"`   // forbidden_fixes
	SupersededBy         []string   `yaml:"superseded_by"`
	SupportedByEvidence  []string   `yaml:"supported_by_evidence"`
	SourceFiles          []string   `yaml:"source_files"`
	CodeSymbols          []string   `yaml:"code_symbols"`
	Uml                  umlProfile `yaml:"uml"`
}
type decisionsFile struct {
	Decisions []yamlDecision `yaml:"decisions"`
}

type yamlGuardrail struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Priority string   `yaml:"priority"`
	Status   string   `yaml:"status"`
	Protects []string `yaml:"protects"`
}
type guardrailsFile struct {
	Guardrails []yamlGuardrail `yaml:"guardrails"`
}

type yamlRule struct {
	ID             string   `yaml:"id"`
	Summary        string   `yaml:"summary"`
	MetaPrinciples []string `yaml:"meta_principles"`
}
type rulesFile struct {
	Rules []yamlRule `yaml:"rules"`
}

type yamlPattern struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Definition string `yaml:"definition"`
}
type patternsFile struct {
	Patterns []yamlPattern `yaml:"patterns"`
}
type designPatternsFile struct {
	DesignPatterns []yamlPattern `yaml:"design_patterns"`
}

type yamlService struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}
type servicesFile struct {
	Services []yamlService `yaml:"services"`
}

type highRiskFilesFile struct {
	Files []string `yaml:"files"`
}

type yamlActivationRulesFile struct {
	ActivationRules yamlActivationRules `yaml:"activation_rules"`
}

type yamlActivationRules struct {
	Version     string                    `yaml:"version"`
	Rules       []yamlActivationRule      `yaml:"rules"`
	EmptyPolicy yamlActivationEmptyPolicy `yaml:"empty_policy"`
}

type yamlActivationRule struct {
	ID          string   `yaml:"id"`
	Trigger     string   `yaml:"trigger"`
	Enforcement string   `yaml:"enforcement"`
	Paths       []string `yaml:"paths"`
	Concepts    []string `yaml:"concepts"`
	Tools       []string `yaml:"tools"`
}

type yamlActivationEmptyPolicy struct {
	Tiers []yamlActivationPolicyTier `yaml:"tiers"`
}

type yamlActivationPolicyTier struct {
	Tier        string `yaml:"tier"`
	Description string `yaml:"description"`
	Action      string `yaml:"action"`
	Announce    bool   `yaml:"announce"`
}

// ── importForbiddenFixes ──────────────────────────────────────────────────────

// importForbiddenFixes imports files with the forbidden_fixes: schema.
// It enriches the aw:ForbiddenFix nodes (which the invariants importer
// already mints as stubs) with labels, prose, and cross-references.
//
// Some files (e.g. failuregraph seeds) carry a forbidden_fixes: field
// that is a list of bare string IDs, not full definitions. Items that
// are not YAML mappings are silently skipped so those files can be
// imported without error even though they produce 0 ForbiddenFix nodes.
func importForbiddenFixes(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw forbiddenFixesFileRaw
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, node := range raw.ForbiddenFixes {
		// Skip scalar items — they are bare ID references, not definitions.
		if node.Kind != yaml.MappingNode {
			continue
		}
		var ff yamlForbiddenFix
		if err := node.Decode(&ff); err != nil || ff.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassForbiddenFix, ff.ID)
		e.Typed(subj, rdf.ClassForbiddenFix)

		label := strings.TrimSpace(ff.Title)
		if label == "" {
			label = strings.TrimSpace(ff.Summary)
		}
		if label != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(label))
		}

		// Long prose goes to rdfs:comment.
		if sa := strings.TrimSpace(coalesce(ff.SafeAlternative, ff.SaferAlternative)); sa != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(sa))
		}
		if ff.Reason != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(ff.Reason)))
		}

		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		// Domain scope + provenance: tags a repo-scoped (pilot) forbidden fix
		// with aw:repo/aw:domain so the scope filter isolates it. Untagged
		// (home-domain) entries emit nothing here.
		emitDomainScope(e, subj, ff.domainScope)
		// Detect block: advisory bad-shape patterns for warning-level EditCheck.
		emitDetect(e, subj, ff.Detect)
		emitUML(e, subj, ff.Uml)

		// Link to related invariants. aw:affects carries the relationship.
		// The type of the object (Invariant) is not asserted here — it is
		// owned by invariants.yaml.
		for _, inv := range ff.RelatedInvariants {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
		for _, ref := range ff.AppliesTo {
			// applies_to can reference invariants or failure modes; without
			// type information we can't distinguish, so we emit a generic
			// aw:affects edge and let the type filter in SPARQL discriminate.
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, ref))
		}
		for _, f := range ff.Protects.Files {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
			// Reverse edge — lets briefing-by-file surface forbidden fixes as Direct anchors.
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
	}
	return nil
}

// ── importRequiredTests ───────────────────────────────────────────────────────

// importRequiredTests imports files with the required_tests: schema.
// It enriches aw:Test nodes with labels and links back to the invariants
// and failure modes they protect.
func importRequiredTests(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f requiredTestsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, rt := range f.RequiredTests {
		if rt.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassTest, rt.ID)
		e.Typed(subj, rdf.ClassTest)
		if rt.Title != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(rt.Title)))
		}
		emitUML(e, subj, rt.Uml)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		for _, inv := range rt.Protects.Invariants {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
		for _, fm := range rt.Protects.FailureModes {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassFailureMode, fm))
		}
		for _, f := range rt.Protects.Files {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
			// Reverse edge — lets briefing-by-file surface required tests as Direct anchors.
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
	}
	return nil
}

// ── importContracts ───────────────────────────────────────────────────────────

// importContracts imports files with the versioned_doc schema that carry a
// contracts: list (authority_contracts.yaml, service_contracts.yaml).
func importContracts(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f contractsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, c := range f.Contracts {
		if c.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassContract, c.ID)
		e.Typed(subj, rdf.ClassContract)

		label := strings.TrimSpace(coalesce(c.Summary, c.Description, c.ID))
		if label != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(label))
		}
		if c.Kind != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(c.Kind))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	}
	return nil
}

// ── importIncident ────────────────────────────────────────────────────────────

// importIncident imports a single incident file (top-level incident_id: scalar).
// Each incident file is one entity; the caller passes the file's parsed raw map
// to avoid a second read. This function IS called from classifyAndImport which
// has already read the raw map; to avoid two reads we parse from data bytes.
func importIncident(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var inc yamlIncident
	if err := yaml.Unmarshal(data, &inc); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if inc.IncidentID == "" {
		return nil
	}
	subj := rdf.MintIRI(rdf.ClassIncident, inc.IncidentID)
	e.Typed(subj, rdf.ClassIncident)
	if inc.Title != "" {
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(inc.Title)))
	}
	if inc.Severity != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSeverity), rdf.Lit(inc.Severity))
	}
	if inc.Status != "" {
		e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(inc.Status))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	for _, f := range inc.RelatedFiles {
		ensureNode(e, rdf.ClassSourceFile, f)
		e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
		e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
	}
	return nil
}

// ── importDecisions ───────────────────────────────────────────────────────────

func importDecisions(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f decisionsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, d := range f.Decisions {
		if d.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassDecision, d.ID)
		e.Typed(subj, rdf.ClassDecision)
		if d.Title != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(d.Title)))
		}
		if d.Status != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(d.Status))
		}
		if d.Rationale != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(d.Rationale)))
		}
		// context / consequences / rejected alternatives are prose → rdfs:comment.
		emitOptLit(e, subj, rdf.PropComment, d.Context)
		emitOptLit(e, subj, rdf.PropComment, d.Consequences)
		for _, alt := range d.AlternativesRejected {
			emitOptLit(e, subj, rdf.PropComment, alt)
		}
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(d.Assertion)))
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		for _, inv := range d.RelatedInvariants {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
		// Architectural-spine edges.
		emitBareEdges(e, subj, rdf.PropDefinesBoundary, rdf.ClassBoundary, d.DefinesBoundaries)
		emitBareEdges(e, subj, rdf.PropDefinesContract, rdf.ClassContract, d.DefinesContracts)
		emitBareEdges(e, subj, rdf.PropAffectsComponent, rdf.ClassComponent, d.AffectsComponents)
		emitBareEdges(e, subj, rdf.PropMitigates, rdf.ClassFailureMode, d.Mitigates)
		emitBareEdges(e, subj, rdf.PropRejects, rdf.ClassForbiddenFix, d.Rejects)
		emitBareEdges(e, subj, rdf.PropSupersededBy, rdf.ClassDecision, d.SupersededBy)
		emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, d.SupportedByEvidence)
		emitSpineAnchors(e, subj, d.SourceFiles, d.CodeSymbols)
		emitUML(e, subj, d.Uml)
	}
	return nil
}

// ── importGuardrails ──────────────────────────────────────────────────────────

func importGuardrails(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f guardrailsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, g := range f.Guardrails {
		if g.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassGuardrail, g.ID)
		e.Typed(subj, rdf.ClassGuardrail)
		if g.Title != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(g.Title)))
		}
		if g.Status != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(g.Status))
		}
		// Priority (P0/P1/P2) mapped to severity for uniformity.
		if g.Priority != "" {
			e.Triple(subj, rdf.IRI(rdf.PropSeverity), rdf.Lit(g.Priority))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		for _, inv := range g.Protects {
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
	}
	return nil
}

// ── importRules ───────────────────────────────────────────────────────────────

func importRules(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f rulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, r := range f.Rules {
		if r.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassGuardrail, r.ID)
		e.Typed(subj, rdf.ClassGuardrail)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(r.ID)))
		if summary := strings.TrimSpace(r.Summary); summary != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(summary))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		for _, mp := range r.MetaPrinciples {
			if mp = strings.TrimSpace(mp); mp != "" {
				e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, mp))
			}
		}
	}
	return nil
}

// ── importPatterns ────────────────────────────────────────────────────────────

// importPatterns handles both patterns: and design_patterns: schemas.
func importPatterns(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Try patterns: first, then design_patterns:.
	var pf patternsFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	items := pf.Patterns
	if len(items) == 0 {
		var dpf designPatternsFile
		if err := yaml.Unmarshal(data, &dpf); err != nil {
			return fmt.Errorf("parse design_patterns: %w", err)
		}
		items = dpf.DesignPatterns
	}
	for _, p := range items {
		if p.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassPattern, p.ID)
		e.Typed(subj, rdf.ClassPattern)
		if p.Title != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(p.Title)))
		}
		if p.Definition != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(p.Definition)))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	}
	return nil
}

// ── importServices ────────────────────────────────────────────────────────────

func importServices(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f servicesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, s := range f.Services {
		if s.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassService, s.ID)
		e.Typed(subj, rdf.ClassService)
		if s.Name != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(s.Name))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	}
	return nil
}

// ── importHighRiskFiles ───────────────────────────────────────────────────────

// importHighRiskFiles imports the briefing trigger's high-risk path registry.
// The registry is authoritative operational policy, so even an empty list emits
// a guardrail node carrying authoredIn provenance.
func importHighRiskFiles(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f highRiskFilesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	subj := rdf.MintIRI(rdf.ClassGuardrail, "awareness.high_risk_files")
	e.Typed(subj, rdf.ClassGuardrail)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit("High-risk files requiring awareness briefing"))
	e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit("active"))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	for _, p := range f.Files {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ensureNode(e, rdf.ClassSourceFile, p)
		e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, p))
		e.Triple(rdf.MintIRI(rdf.ClassSourceFile, p), rdf.IRI(rdf.PropImplements), subj)
	}
	return nil
}

// ── importActivationRules ────────────────────────────────────────────────────

// importActivationRules imports the policy that decides when agents must ask
// Sensei for context and how they treat EMPTY briefing results.
func importActivationRules(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f yamlActivationRulesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	root := rdf.MintIRI(rdf.ClassGuardrail, "awareness.activation_rules")
	e.Typed(root, rdf.ClassGuardrail)
	e.Triple(root, rdf.IRI(rdf.PropLabel), rdf.Lit("Awareness activation rules"))
	e.Triple(root, rdf.IRI(rdf.PropStatus), rdf.Lit("active"))
	if version := strings.TrimSpace(f.ActivationRules.Version); version != "" {
		e.Triple(root, rdf.IRI(rdf.PropComment), rdf.Lit("version="+version))
	}
	e.Triple(root, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	for _, r := range f.ActivationRules.Rules {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassGuardrail, "activation_rule."+id)
		e.Typed(subj, rdf.ClassGuardrail)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit("Activation rule: "+id))
		if r.Enforcement != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(strings.TrimSpace(r.Enforcement)))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		if detail := activationRuleComment(r); detail != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(detail))
		}
		e.Triple(root, rdf.IRI(rdf.PropAffects), subj)
		for _, p := range r.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			ensureNode(e, rdf.ClassSourceFile, p)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, p))
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, p), rdf.IRI(rdf.PropImplements), subj)
		}
	}

	for _, tier := range f.ActivationRules.EmptyPolicy.Tiers {
		name := strings.TrimSpace(tier.Tier)
		if name == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassGuardrail, "activation_empty_policy."+name)
		e.Typed(subj, rdf.ClassGuardrail)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit("Empty briefing policy: "+name))
		if tier.Action != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(strings.TrimSpace(tier.Action)))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		if desc := strings.TrimSpace(tier.Description); desc != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(desc))
		}
		e.Triple(root, rdf.IRI(rdf.PropAffects), subj)
	}
	return nil
}

func activationRuleComment(r yamlActivationRule) string {
	var parts []string
	if trigger := strings.TrimSpace(r.Trigger); trigger != "" {
		parts = append(parts, "trigger="+trigger)
	}
	if len(r.Concepts) > 0 {
		parts = append(parts, "concepts="+strings.Join(trimNonEmpty(r.Concepts), ","))
	}
	if len(r.Tools) > 0 {
		parts = append(parts, "tools="+strings.Join(trimNonEmpty(r.Tools), ","))
	}
	return strings.Join(parts, "; ")
}

func trimNonEmpty(vals []string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

// coalesce returns the first non-empty string from the arguments.
func coalesce(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
