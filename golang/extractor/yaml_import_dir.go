// SPDX-License-Identifier: Apache-2.0

// Package extractor — Phase A: recursive directory walker with schema
// classification.
//
// This file extends the original three-file importer (yaml_import.go) with:
//   - A recursive walk over the full awareness directory tree.
//   - Schema detection by top-level YAML key.
//   - A detailed per-file ImportReport so nothing is silently skipped.
//   - A --strict mode contract exposed through ImportReport.HasSkipped().
//
// Phases B and C will add new schemaEntry records with importable=true
// and register their importers in the classifyAndImport switch. Phase A
// deliberately leaves all non-invariant/failure_mode/incident_pattern schemas
// as KnownUnsupported so coverage gaps are visible immediately.
package extractor

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// ── Status types ─────────────────────────────────────────────────────────────

// FileStatus categorizes how a discovered YAML file was handled.
type FileStatus string

const (
	// StatusImported means triples were written for this file.
	StatusImported FileStatus = "imported"
	// StatusIgnored means the file is recognized as deliberate non-authority
	// pipeline/config metadata and is therefore excluded from graph import.
	StatusIgnored FileStatus = "ignored"
	// StatusKnownUnsupported means the schema is recognized but its importer
	// is not yet implemented. The Phase field indicates which release will add it.
	StatusKnownUnsupported FileStatus = "known_unsupported"
	// StatusUnknownSchema means the file parsed as YAML but its top-level
	// key(s) do not match any entry in the schema table.
	StatusUnknownSchema FileStatus = "unknown_schema"
	// StatusInvalid means the file could not be read or could not be parsed
	// as YAML.
	StatusInvalid FileStatus = "invalid"
)

// ── Per-file and aggregate report types ──────────────────────────────────────

// FileReport records the disposition of one YAML file during an import walk.
type FileReport struct {
	Path   string     // path as returned by filepath.WalkDir
	Status FileStatus // outcome
	Schema string     // detected schema name (top-level key or composite ID)
	Phase  string     // "A", "B", or "C"; empty if unknown/invalid
	Reason string     // human-readable explanation for non-Imported statuses
	Count  int        // triples emitted; non-zero only for StatusImported
}

// ImportReport summarises a full recursive walk.
type ImportReport struct {
	Files []FileReport
}

// Imported returns files with StatusImported.
func (r *ImportReport) Imported() []FileReport {
	var out []FileReport
	for _, f := range r.Files {
		if f.Status == StatusImported {
			out = append(out, f)
		}
	}
	return out
}

// Ignored returns files intentionally excluded from authoritative import.
func (r *ImportReport) Ignored() []FileReport {
	var out []FileReport
	for _, f := range r.Files {
		if f.Status == StatusIgnored {
			out = append(out, f)
		}
	}
	return out
}

// Skipped returns files that were not imported (any non-Imported status).
func (r *ImportReport) Skipped() []FileReport {
	var out []FileReport
	for _, f := range r.Files {
		if f.Status != StatusImported && f.Status != StatusIgnored {
			out = append(out, f)
		}
	}
	return out
}

// HasUnknown reports whether any file had an unrecognized top-level schema.
func (r *ImportReport) HasUnknown() bool {
	for _, f := range r.Files {
		if f.Status == StatusUnknownSchema {
			return true
		}
	}
	return false
}

// HasInvalid reports whether any file failed to parse.
func (r *ImportReport) HasInvalid() bool {
	for _, f := range r.Files {
		if f.Status == StatusInvalid {
			return true
		}
	}
	return false
}

// HasSkipped reports whether any file was not imported.
func (r *ImportReport) HasSkipped() bool {
	return len(r.Skipped()) > 0
}

// ── Schema detection table ────────────────────────────────────────────────────

type schemaEntry struct {
	name        string // machine-readable schema identifier
	importable  bool   // true when an importer is registered in classifyAndImport
	ignored     bool   // true when the file is recognized but deliberately non-authoritative
	phase       string // "A", "B", or "C"
	description string // one-line description for report output
}

type AwarenessSourceDescriptor struct {
	Path                      string
	Schema                    string
	Phase                     string
	ImporterID                string
	SourceClass               string
	CanonicalMutationEligible bool
}

// keySchemas is an ordered (priority) table mapping top-level YAML keys to
// schema entries. The first matching key wins. Importable schemas are listed
// first so they are never shadowed by a broader match.
//
// To add a new schema: insert its entry here (importable=false, phase="B")
// AND register it in the classifyAndImport switch once the importer is ready.
var keySchemas = []struct {
	key   string
	entry schemaEntry
}{
	// ── Phase A — importable now ──────────────────────────────────────────
	{"invariants", schemaEntry{"invariants", true, false, "A", "invariant rules"}},
	{"failure_modes", schemaEntry{"failure_modes", true, false, "A", "failure mode catalogue"}},
	{"incident_patterns", schemaEntry{"incident_patterns", true, false, "A", "incident pattern library"}},
	{"architecture_claims", schemaEntry{"architecture_claims", true, false, "A", "generated non-authoritative architecture claims"}},
	{"architecture_dialogue", schemaEntry{"architecture_dialogue", true, false, "A", "generated non-authoritative architecture dialogue"}},
	{"architecture_evidence_probes", schemaEntry{"architecture_evidence_probes", true, false, "A", "generated non-authoritative evidence probe plans"}},

	// ── Phase B — importers now implemented ──────────────────────────────
	{"forbidden_fixes", schemaEntry{"forbidden_fixes", true, false, "B", "forbidden fix registry"}},
	{"required_tests", schemaEntry{"required_tests", true, false, "B", "required test registry"}},
	{"decisions", schemaEntry{"decisions", true, false, "B", "architecture decisions"}},
	{"guardrails", schemaEntry{"guardrails", true, false, "B", "operational guardrails"}},
	{"patterns", schemaEntry{"patterns", true, false, "B", "design patterns"}},
	{"design_patterns", schemaEntry{"design_patterns", true, false, "B", "design patterns (verbose)"}},
	{"services", schemaEntry{"services", true, false, "B", "service catalogue"}},
	{"authority_domains", schemaEntry{"authority_domains", true, false, "B", "state-ownership authority domains"}},
	{"actor_roles", schemaEntry{"actor_roles", true, false, "B", "typed actor role policy"}},
	{"authority_grants", schemaEntry{"authority_grants", true, false, "B", "typed authority grants"}},
	{"delegation_policies", schemaEntry{"delegation_policies", true, false, "B", "typed delegation policies"}},
	{"mutation_paths", schemaEntry{"mutation_paths", true, false, "B", "typed mutation path policy"}},
	{"observation_paths", schemaEntry{"observation_paths", true, false, "B", "typed observation path policy"}},
	{"runtime_evidence", schemaEntry{"runtime_evidence", true, false, "B", "runtime evidence profiles"}},
	{"proof_obligations", schemaEntry{"proof_obligations", true, false, "B", "proof obligations derived from authority surfaces"}},
	{"learning_event", schemaEntry{"learning_event", true, false, "B", "mode-d learning and certification event"}},
	{"file_annotations", schemaEntry{"file_annotations", true, false, "B", "source file to invariant mappings"}},
	{"source_patterns", schemaEntry{"source_patterns", true, false, "B", "regex-based structural enforcement rules"}},
	{"rendering_groups", schemaEntry{"rendering_groups", true, false, "B", "cross-file rendering consistency groups"}},
	{"authority_rules", schemaEntry{"authority_rules", false, false, "B", "authority rules"}},
	{"authorities", schemaEntry{"authority_rules", false, false, "B", "authority rules"}},
	{"namespaces", schemaEntry{"namespaces", false, false, "B", "namespace ownership metadata"}},
	{"subsystem_boundaries", schemaEntry{"subsystem_boundaries", false, false, "B", "subsystem boundary contracts"}},
	{"fix_cases", schemaEntry{"fix_cases", false, false, "B", "fix case registry"}},
	{"forbidden_assumptions", schemaEntry{"forbidden_assumptions", false, false, "B", "forbidden assumption registry"}},
	{"preflight_questions", schemaEntry{"preflight_questions", false, false, "B", "preflight question library"}},
	{"remediation_contracts", schemaEntry{"remediation_contracts", false, false, "B", "remediation contract library"}},
	{"rules", schemaEntry{"rules", true, false, "B", "operational rules"}},
	{"contract_set_version", schemaEntry{"frozen_contract_set", true, false, "B", "mode-d frozen contract set"}},
	{"detector_mappings", schemaEntry{"detector_mappings", false, false, "B", "detector mapping registry"}},
	{"causal_rules", schemaEntry{"causal_rules", false, false, "B", "causal rule library"}},
	{"playbooks", schemaEntry{"playbooks", false, false, "B", "agent playbook library"}},
	{"decision_rules", schemaEntry{"decision_rules", false, false, "B", "operational decision rules"}},
	{"dns_zones", schemaEntry{"dns_zones", false, false, "B", "DNS zone knowledge"}},
	{"queries", schemaEntry{"queries", false, false, "B", "metric query library"}},
	{"thresholds", schemaEntry{"thresholds", false, false, "B", "metric threshold library"}},
	{"trust", schemaEntry{"trust", false, false, "B", "path weight / trust scoring"}},
	{"allowlist", schemaEntry{"allowlist", false, false, "B", "scan allowlist"}},
	{"suppressions", schemaEntry{"suppressions", false, false, "B", "audit suppression list"}},
	{"aliases", schemaEntry{"aliases", false, false, "B", "context alias map"}},
	{"last_updated", schemaEntry{"status_tracker", false, true, "B", "status tracker"}},
	{"files", schemaEntry{"high_risk_files", true, false, "B", "high-risk file list"}},
	{"activation_rules", schemaEntry{"activation_rules", true, false, "B", "awareness activation rule registry"}},
	{"version", schemaEntry{"versioned_doc", true, false, "B", "versioned contract document"}},
	{"incidents", schemaEntry{"incidents", true, false, "B", "incident records"}},
	{"incident_id", schemaEntry{"incident", true, false, "B", "individual incident record"}},

	// ── Architectural spine (Stage A) — MUST be last among importable schemas ──
	// Their keys (components / boundaries / evidence / contracts) are generic
	// English words that ALSO appear as DATA fields in existing corpus files —
	// an incident's `evidence:` list, a path-weights `evidence:` map, a versioned
	// `contracts:` doc. detectSchema returns the first matching key in table
	// order, so the spine entries come after the specific schemas (incident_id,
	// trust, version, …) that own those files; otherwise the spine importer
	// would hijack and break them. The spine corpus files carry only their own
	// key, so they still match here.
	{"components", schemaEntry{"components", true, false, "A", "architectural components (units of ownership)"}},
	{"boundaries", schemaEntry{"boundaries", true, false, "A", "architectural boundaries"}},
	{"evidence", schemaEntry{"evidence", true, false, "A", "evidence (proof a rule is alive)"}},
	{"meta_principle_links", schemaEntry{"meta_principle_links", true, false, "A", "meta-principle architectural edges"}},
	{"uml_profiles", schemaEntry{"uml_profiles", true, false, "A", "optional UML classification overlay"}},
	{"contracts", schemaEntry{"architecture_contracts", true, false, "A", "architectural API/proto/schema/CLI contracts"}},
	{"contract_realizations", schemaEntry{"contract_realizations", true, false, "A", "implementation→architectural contract realization links (authoritative + candidates)"}},

	// ── Phase C — intent files ────────────────────────────────────────────
	// Detected via secondary key check (id + level); see detectSchema.
	// Not listed here because the primary-key lookup cannot match them.

	// ── Phase C — generated code-annotation files ─────────────────────────
	{"code_symbols", schemaEntry{"code_symbols", true, false, "C", "generated code symbol annotations"}},
	{"code_edges", schemaEntry{"code_edges", true, false, "C", "generated code annotation edge list"}},
	{"code_references", schemaEntry{"code_references", true, false, "C", "generated symbol reference edges (from SCIP)"}},

	// ── Meta / diagnostic files — never importable ────────────────────────
	// These schemas appear in generated/ and docs/intent/meta/ directories.
	// They are infrastructure for the pipeline, not awareness data.
	{"scanned_files", schemaEntry{"annotation_report", false, true, "C", "annotation scanner diagnostic output"}},
	{"schema", schemaEntry{"meta_schema", false, true, "C", "intent meta-schema definition"}},
	{"categories", schemaEntry{"change_risk_classifier", false, true, "C", "change risk classifier"}},
	{"report_shape", schemaEntry{"runtime_evidence_schema", false, true, "C", "runtime evidence schema"}},
	{"namespaces", schemaEntry{"namespace_registry", false, true, "C", "namespace registry — pipeline config, not awareness data"}},
	{"architectural_declarations", schemaEntry{"architectural_declarations", false, true, "C", "per-service architectural-principle declarations — consumed by the completeness gate, not graph authority"}},
	{"meta_principle_coverage", schemaEntry{"meta_principle_coverage", false, true, "C", "meta-principle enforcement-tier coverage map — consumed by the coverage gate, not graph authority"}},
}

// detectSchema infers the schema of a YAML file from its parsed top-level map.
// The second return value is false when no schema could be matched.
//
// Composite-key detection runs FIRST. Intent files carry a required_tests:
// field as a data attribute, so the primary key table would mistakenly
// classify them as required_tests schema if composite detection ran second.
// Composite patterns are more specific and take priority.
func detectSchema(raw map[string]any) (schemaEntry, bool) {
	if raw["apiVersion"] == "awareness.globular.io/v1" && raw["kind"] == "AwarenessContract" {
		return schemaEntry{"package_awareness_contract", true, false, "B", "Globular package awareness contract"}, true
	}
	if _, ok := raw["authorities"]; ok {
		return schemaEntry{"authority_rules", false, false, "B", "authority rules"}, true
	}

	// Composite-key detection: single-entity documents identified by id plus
	// a discriminating second key. These take priority over the primary table
	// because entity documents may contain schema-named fields as data
	// (e.g. intent files carry a required_tests: list of test references).
	if _, hasID := raw["id"]; hasID {
		if cls, ok := raw["class"].(string); ok {
			// Class-discriminated single-entity documents. Matched BEFORE the
			// level/type checks so id+class always wins.
			switch cls {
			case "ImplementationPattern":
				return schemaEntry{"implementation_pattern", true, false, "B", "implementation pattern recipe"}, true
			case "DesignPattern":
				return schemaEntry{"design_pattern", true, false, "A", "design pattern (the how layer)"}, true
			case "PatternMisuse":
				return schemaEntry{"pattern_misuse", true, false, "A", "design-pattern misuse"}, true
			case "OutcomeFeedback":
				return schemaEntry{"outcome_feedback", true, false, "B", "agent/operator outcome feedback snapshot"}, true
			case "RepairPlan":
				return schemaEntry{"repair_plan", true, false, "B", "repair plan for a failure class"}, true
			}
		}
		if _, hasLevel := raw["level"]; hasLevel {
			// Intent files carry id + level (e.g. level: safety_rule).
			return schemaEntry{"intent", true, false, "C", "design or operational intent"}, true
		}
		if _, hasType := raw["type"]; hasType {
			// Failure-graph seed files carry id + type (e.g. type: ErrorCategory).
			return schemaEntry{"failuregraph_seed", false, false, "B", "failure graph seed entity"}, true
		}
		if _, hasKind := raw["kind"]; hasKind {
			// Meta intent contract files carry id + kind (not level/type).
			return schemaEntry{"meta_intent_contract", false, true, "C", "meta intent contract"}, true
		}
	}

	// Primary: first matching top-level key wins.
	for _, ks := range keySchemas {
		if _, ok := raw[ks.key]; ok {
			return ks.entry, true
		}
	}

	// Heuristic: if any top-level value is a list of objects containing a "file"
	// key with "enforces" or "protects", treat as file_annotations schema.
	// This catches multi-section annotation files like sdk_annotations.yaml
	// and web_annotations.yaml that use domain-specific grouping keys
	// (sdk_files, core_files, cluster_pages, etc.) instead of the canonical
	// file_annotations key.
	if looksLikeFileAnnotations(raw) {
		return schemaEntry{"file_annotations", true, false, "B", "source file to invariant mappings (multi-section)"}, true
	}

	return schemaEntry{}, false
}

func DescribeAwarenessSource(path string) (AwarenessSourceDescriptor, bool, error) {
	rawPath := path
	path = normalizeAwarenessDescriptorPath(path)
	if !strings.HasSuffix(path, ".yaml") {
		return AwarenessSourceDescriptor{}, false, nil
	}
	data, err := os.ReadFile(rawPath)
	if err != nil {
		return AwarenessSourceDescriptor{}, false, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return AwarenessSourceDescriptor{}, false, err
	}
	if raw == nil {
		return AwarenessSourceDescriptor{}, false, nil
	}
	entry, ok := detectSchema(raw)
	if !ok || !entry.importable {
		return AwarenessSourceDescriptor{}, false, nil
	}
	desc, ok := awarenessMutationDescriptorFor(filepath.ToSlash(path), entry)
	if !ok {
		return AwarenessSourceDescriptor{}, false, nil
	}
	return desc, true, nil
}

func awarenessMutationDescriptorFor(path string, entry schemaEntry) (AwarenessSourceDescriptor, bool) {
	path = normalizeAwarenessDescriptorPath(path)
	if path == "" || strings.Contains(path, "/candidates/") || strings.Contains(path, "/generated/") || strings.HasPrefix(path, ".sensei/") {
		return AwarenessSourceDescriptor{}, false
	}
	desc := AwarenessSourceDescriptor{
		Path:        path,
		Schema:      entry.name,
		Phase:       entry.phase,
		ImporterID:  awarenessMutationImporterID(entry.name),
		SourceClass: awarenessMutationSourceClass(entry.name),
	}
	if desc.ImporterID == "" || desc.SourceClass == "" {
		return AwarenessSourceDescriptor{}, false
	}
	switch entry.name {
	case "invariants", "failure_modes":
		desc.CanonicalMutationEligible = path == "docs/awareness/"+filepath.Base(path)
	case "components", "boundaries", "evidence", "architecture_contracts":
		desc.CanonicalMutationEligible = strings.HasPrefix(path, "docs/awareness/architecture/") && strings.HasSuffix(path, ".yaml")
	case "intent":
		desc.CanonicalMutationEligible = strings.HasPrefix(path, "docs/intent/") && strings.HasSuffix(path, ".yaml")
	default:
		desc.CanonicalMutationEligible = false
	}
	if !desc.CanonicalMutationEligible {
		return AwarenessSourceDescriptor{}, false
	}
	return desc, true
}

func normalizeAwarenessDescriptorPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if idx := strings.Index(path, "/docs/"); idx >= 0 {
		return strings.TrimPrefix(path[idx+1:], "./")
	}
	if idx := strings.Index(path, "/.sensei/"); idx >= 0 {
		return strings.TrimPrefix(path[idx+1:], "./")
	}
	return strings.TrimPrefix(path, "./")
}

func awarenessMutationImporterID(schema string) string {
	switch strings.TrimSpace(schema) {
	case "invariants":
		return "awareness.invariant_yaml_import.v1"
	case "failure_modes":
		return "awareness.failure_mode_yaml_import.v1"
	case "components":
		return "awareness.component_yaml_import.v1"
	case "boundaries":
		return "awareness.boundary_yaml_import.v1"
	case "architecture_contracts":
		return "awareness.architecture_contract_yaml_import.v1"
	case "evidence":
		return "awareness.evidence_yaml_import.v1"
	case "intent":
		return "awareness.intent_yaml_import.v1"
	default:
		return ""
	}
}

func awarenessMutationSourceClass(schema string) string {
	switch strings.TrimSpace(schema) {
	case "invariants":
		return "canonical_awareness_invariant_registry"
	case "failure_modes":
		return "canonical_awareness_failure_mode_registry"
	case "components":
		return "canonical_awareness_component_registry"
	case "boundaries":
		return "canonical_awareness_boundary_registry"
	case "architecture_contracts":
		return "canonical_awareness_contract_registry"
	case "evidence":
		return "canonical_awareness_evidence_registry"
	case "intent":
		return "canonical_awareness_intent_document"
	default:
		return ""
	}
}

// classifyAndImport reads one YAML file, detects its schema, and imports it
// if a registered importer exists. It is the inner loop of ImportAwarenessDir.
//
// Per-file parse or import errors are encoded in the returned FileReport and
// do NOT cause the overall walk to abort — partial coverage is better than no
// coverage.
func classifyAndImport(e *rdf.Emitter, path string) FileReport {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileReport{
			Path:   path,
			Status: StatusInvalid,
			Reason: "read: " + err.Error(),
		}
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return FileReport{
			Path:   path,
			Status: StatusInvalid,
			Reason: "yaml parse: " + err.Error(),
		}
	}

	if raw == nil {
		// File is entirely comments or whitespace — YAML parses to nil map.
		return FileReport{
			Path:   path,
			Status: StatusUnknownSchema,
			Reason: "empty document (comments or whitespace only)",
		}
	}

	entry, known := detectSchema(raw)
	if !known {
		if isPackageConfigYAML(path) {
			return FileReport{
				Path:   path,
				Status: StatusIgnored,
				Schema: "package_config",
				Phase:  "B",
				Reason: "package runtime config — explicit non-authority input",
			}
		}
		keys := topLevelKeys(raw)
		return FileReport{
			Path:   path,
			Status: StatusUnknownSchema,
			Schema: strings.Join(keys, ","),
			Reason: "top-level key(s) [" + strings.Join(keys, ", ") + "] not recognized",
		}
	}

	if !entry.importable {
		if entry.ignored {
			return FileReport{
				Path:   path,
				Status: StatusIgnored,
				Schema: entry.name,
				Phase:  entry.phase,
				Reason: entry.description + " — explicit non-authority input",
			}
		}
		return FileReport{
			Path:   path,
			Status: StatusKnownUnsupported,
			Schema: entry.name,
			Phase:  entry.phase,
			Reason: entry.description + " — importer due Phase " + entry.phase,
		}
	}

	// Importable schema: route to the registered per-schema importer.
	// To add an importer: set entry.importable=true in keySchemas, add
	// a case here, and add the importer function in the appropriate
	// yaml_import_phase*.go file.
	before := e.Triples
	var importErr error
	switch entry.name {
	// Phase A
	case "invariants":
		importErr = importInvariants(e, path)
	case "failure_modes":
		importErr = importFailureModes(e, path)
	case "incident_patterns":
		importErr = importIncidentPatterns(e, path)
	case "architecture_claims":
		importErr = importArchitectureClaims(e, path)
	case "architecture_dialogue":
		importErr = importArchitectureDialogue(e, path)
	case "architecture_evidence_probes":
		importErr = importArchitectureEvidenceProbes(e, path)
	// Phase B
	case "forbidden_fixes":
		importErr = importForbiddenFixes(e, path)
	case "required_tests":
		importErr = importRequiredTests(e, path)
	case "versioned_doc":
		importErr = importContracts(e, path)
	case "incident":
		importErr = importIncident(e, path)
	case "incidents":
		importErr = importIncidents(e, path)
	case "decisions":
		importErr = importDecisions(e, path)
	case "guardrails":
		importErr = importGuardrails(e, path)
	case "rules":
		importErr = importRules(e, path)
	case "frozen_contract_set":
		importErr = importFrozenContractSet(e, path)
	case "patterns", "design_patterns":
		importErr = importPatterns(e, path)
	case "services":
		importErr = importServices(e, path)
	case "implementation_pattern":
		importErr = importImplementationPattern(e, path)
	case "design_pattern":
		importErr = importDesignPattern(e, path)
	case "pattern_misuse":
		importErr = importPatternMisuse(e, path)
	case "outcome_feedback":
		importErr = importOutcomeFeedback(e, path)
	case "authority_domains":
		importErr = importAuthorityDomains(e, path)
	case "actor_roles":
		importErr = importActorRoles(e, path)
	case "authority_grants":
		importErr = importAuthorityGrants(e, path)
	case "delegation_policies":
		importErr = importDelegationPolicies(e, path)
	case "mutation_paths":
		importErr = importMutationPaths(e, path)
	case "observation_paths":
		importErr = importObservationPaths(e, path)
	case "repair_plan":
		importErr = importRepairPlan(e, path)
	case "runtime_evidence":
		importErr = importRuntimeEvidence(e, path)
	case "proof_obligations":
		importErr = importProofObligations(e, path)
	case "learning_event":
		importErr = importLearningEvent(e, path)
	case "high_risk_files":
		importErr = importHighRiskFiles(e, path)
	case "activation_rules":
		importErr = importActivationRules(e, path)
	// Architectural spine (Stage A)
	case "components":
		importErr = importComponents(e, path)
	case "boundaries":
		importErr = importBoundaries(e, path)
	case "evidence":
		importErr = importEvidence(e, path)
	case "meta_principle_links":
		importErr = importMetaPrincipleLinks(e, path)
	case "uml_profiles":
		importErr = importUMLProfiles(e, path)
	case "architecture_contracts":
		importErr = importArchitectureContracts(e, path)
	case "package_awareness_contract":
		importErr = importPackageAwarenessContract(e, path)
	case "contract_realizations":
		importErr = importContractRealizations(e, path)
	case "file_annotations":
		importErr = importFileAnnotations(e, path)
	case "source_patterns":
		importErr = importSourcePatterns(e, path)
	case "rendering_groups":
		importErr = importRenderingGroups(e, path)
	// Phase C
	case "intent":
		importErr = importIntent(e, path)
	case "code_symbols":
		importErr = importCodeSymbols(e, path)
	case "code_edges":
		importErr = importCodeEdges(e, path)
	case "code_references":
		importErr = importCodeReferences(e, path)
	default:
		importErr = fmt.Errorf("internal: no importer registered for schema %q (importable=true but switch missing)", entry.name)
	}

	if importErr != nil {
		return FileReport{
			Path:   path,
			Status: StatusInvalid,
			Schema: entry.name,
			Phase:  entry.phase,
			Reason: importErr.Error(),
		}
	}

	return FileReport{
		Path:   path,
		Status: StatusImported,
		Schema: entry.name,
		Phase:  entry.phase,
		Count:  e.Triples - before,
	}
}

func isPackageConfigYAML(path string) bool {
	p := filepath.ToSlash(path)
	return strings.Contains(p, "/metadata/") && strings.Contains(p, "/config/")
}

// looksLikeFileAnnotations returns true if the YAML map contains at least one
// top-level key whose value is a list of objects with "file" + ("enforces" or
// "protects") keys — the hallmark of multi-section annotation files.
func looksLikeFileAnnotations(raw map[string]any) bool {
	for _, v := range raw {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, hasFile := m["file"]; hasFile {
				if _, hasE := m["enforces"]; hasE {
					return true
				}
				if _, hasP := m["protects"]; hasP {
					return true
				}
			}
		}
	}
	return false
}

// topLevelKeys returns the sorted top-level keys of a YAML map, capped at 5
// to keep error messages readable.
func topLevelKeys(raw map[string]any) []string {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	return keys
}

// ── Public entry point ────────────────────────────────────────────────────────

// ImportDirOptions configures ImportAwarenessDirWithOpts.
type ImportDirOptions struct {
	// StripPathPrefixes lists path prefixes to strip from authoredIn literals.
	// The longest matching prefix wins, making the seed deterministic across
	// checkouts with different absolute paths.
	StripPathPrefixes []string

	// DefaultRepo/DefaultDomain/DefaultSourceSet name a default domain scope for
	// nodes that carry NO inline scope of their own. This is how a foreign repo's
	// domain-agnostic structural extractor output is scoped in one place: set
	// DefaultRepo to e.g. "github.com/cli/cli" and every untagged node imported
	// from docsDir is tagged to that repo instead of the home domain. Empty →
	// unchanged home-domain behaviour.
	DefaultRepo      string
	DefaultDomain    string
	DefaultSourceSet string

	// SkipNestedGenerated excludes nested generated/ directories when walking a
	// parent awareness tree. Ownership-aware build paths set this so generated
	// artifacts are imported only from explicit generated roots, while generic
	// extractor callers keep the historical recursive behaviour.
	SkipNestedGenerated bool
}

// ImportAwarenessDir walks docsDir recursively, classifies every .yaml file,
// imports files with a registered importer, and returns both the emitter and a
// detailed per-file ImportReport.
//
// Walk errors (e.g. permission denied on a subdirectory) are returned as Go
// errors. Per-file parse or import failures are recorded in the report and do
// NOT abort the walk — the caller sees every file's outcome even when some
// files fail.
//
// The caller should call extractor.ValidateNTriples on the output buffer for a
// full N-Triples correctness check after this function returns.
func ImportAwarenessDir(docsDir string, w io.Writer) (*rdf.Emitter, *ImportReport, error) {
	return ImportAwarenessDirWithOpts(docsDir, w, ImportDirOptions{})
}

// ImportAwarenessDirWithOpts is the options-aware variant of ImportAwarenessDir.
func ImportAwarenessDirWithOpts(docsDir string, w io.Writer, opts ImportDirOptions) (*rdf.Emitter, *ImportReport, error) {
	e := rdf.NewEmitter(w)
	e.PathStripPrefixes = opts.StripPathPrefixes
	e.DefaultRepo = opts.DefaultRepo
	e.DefaultDomain = opts.DefaultDomain
	e.DefaultSourceSet = opts.DefaultSourceSet
	report := &ImportReport{}

	err := filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Skip the candidates/ review queue entirely. Files in there
			// are reviewable session-discovered facts that have NOT been
			// promoted to canonical knowledge. The promotion script
			// (services/scripts/promote-awareness-candidate.py) moves a
			// candidate into the canonical YAML; only then does the
			// build pipeline pick it up. Until then, the candidate must
			// not influence the awareness graph.
			//
			// Skip-rule is by directory name, not by status field — we
			// don't want a typo in `status:` to silently land a
			// candidate as canonical. The whole tree under any directory
			// named "candidates" is ignored, regardless of depth.
			if d.Name() == "candidates" {
				return filepath.SkipDir
			}
			// generated/ is imported as its own explicit input root. When walking a
			// parent awareness directory, skip nested generated/ so ownership and
			// freshness are attached to the source repo that passed the generated
			// dir explicitly instead of whichever parent happened to recurse into it.
			if opts.SkipNestedGenerated && path != docsDir && d.Name() == "generated" {
				return filepath.SkipDir
			}
			return nil
		}
		// JSONL agent-run logs (Phase 2G) are imported line-by-line, not via the
		// YAML schema router. Scoped to an agent_runs/ directory so other .jsonl
		// files (e.g. docs/intent/meta/audit_history.jsonl) are not misread as
		// agent runs.
		if strings.HasSuffix(path, ".jsonl") {
			if strings.Contains(filepath.ToSlash(path), "/agent_runs/") {
				fr := importAgentRunsFile(e, path)
				report.Files = append(report.Files, fr)
			}
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		fr := classifyAndImport(e, path)
		report.Files = append(report.Files, fr)
		return nil
	})

	if err != nil {
		return e, report, fmt.Errorf("walk %s: %w", docsDir, err)
	}
	// Attribute the structural extractor output (SourceFile, CodeSymbol, Test, …)
	// to DefaultRepo too — those nodes never call emitDomainScope, so without this
	// they'd leak into the untagged home domain. No-op when DefaultRepo is empty.
	e.FinalizeDefaultScope()
	if err := e.Flush(); err != nil {
		return e, report, fmt.Errorf("flush: %w", err)
	}
	return e, report, nil
}
