// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/extractor"
)

func runValidate(args []string) int {
	fs_ := flag.NewFlagSet("awg validate", flag.ContinueOnError)
	fs_.SetOutput(os.Stderr)
	var dirs multiString
	fs_.Var(&dirs, "dir", "directories to scan (repeat; default: docs/awareness + docs/intent)")
	repoRoot := fs_.String("repo-root", "", "repo root for resolving relative paths (auto-detect)")
	agRepo := fs_.String("ag-repo", "", "awareness-graph repo root providing the shared meta corpus as definition-only context (auto-detect: current working directory)")
	scope := fs_.String("scope", string(validateScopeLocal), "validation scope: local | full")
	format := fs_.String("format", "table", "output format: table | json")
	failOnWarn := fs_.Bool("fail-on-warn", false, "exit non-zero on warnings too")
	var extraRoots multiString
	fs_.Var(&extraRoots, "extra-root", "additional repo root for resolving cross-repo source/test file references, e.g. the Globular gateway repo (repeat; accepts a path or name=...,path=...)")
	fs_.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg validate [flags]

Static check of awareness YAML sources. Read-only — never modifies files.

Checks:
  - dangling related_invariants/related_failure_modes references
  - missing source files (expressed_by / affected_files .go paths)
  - missing reference files (ImplementationPattern reference_files)
  - duplicate IDs across files
  - off-vocabulary severity (not critical|high|warning|info|degraded)

Flags:
`)
		fs_.PrintDefaults()
	}

	if err := fs_.Parse(args); err != nil {
		return 2
	}
	valScope, err := parseValidateScope(*scope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg validate: %v\n", err)
		return 2
	}

	root, err := resolveProjectRoot(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg validate: %v\n", err)
		return 1
	}

	scanDirs := []string(dirs)
	if len(scanDirs) == 0 {
		scanDirs = []string{
			filepath.Join(root, "docs/awareness"),
			filepath.Join(root, "docs/intent"),
		}
	}

	// The shared meta-principle corpus lives in the awareness-graph repo (moved
	// out of services 2026-06-13). When validating another repo (e.g. services),
	// load that corpus as definition-only context so cross-repo meta.* references
	// resolve instead of dangling. Default to the current working directory,
	// which is the awareness-graph checkout when CI runs `awg validate`.
	agRoot := strings.TrimSpace(*agRepo)
	if agRoot == "" {
		if cwd, werr := os.Getwd(); werr == nil {
			agRoot = cwd
		}
	}
	var extraDefDirs []string
	if agRoot != "" {
		agAwareness := filepath.Join(agRoot, "docs/awareness")
		// Only add it when it is a DIFFERENT tree than what we are validating,
		// to avoid double-recording the same files.
		if absA, _ := filepath.Abs(agAwareness); absA != "" {
			if absRoot, _ := filepath.Abs(root); !strings.HasPrefix(absA, absRoot+string(filepath.Separator)) && absA != filepath.Join(absRoot, "docs/awareness") {
				extraDefDirs = append(extraDefDirs, agAwareness)
			}
		}
	}
	sourceRoots := []string{root}
	if servicesRoot, _ := resolveServicesRepo(""); servicesRoot != "" {
		if absSvc, _ := filepath.Abs(servicesRoot); absSvc != "" {
			if absRoot, _ := filepath.Abs(root); absSvc != absRoot {
				extraDefDirs = appendExistingDir(extraDefDirs,
					filepath.Join(servicesRoot, "docs", "awareness"),
					filepath.Join(servicesRoot, "docs", "intent"),
					filepath.Join(servicesRoot, "docs", "awareness", "generated"),
				)
				sourceRoots = append(sourceRoots, servicesRoot)
			}
		}
	}

	// Extra source roots for cross-repo file/test references — the Globular
	// gateway repo hosts handlers/tests that awareness-graph HTTP contracts
	// (Contract Spine v1) reference. Explicit via --extra-root; the sibling
	// Globular checkout is also auto-detected so default validation resolves
	// gateway paths without a flag.
	absRoot, _ := filepath.Abs(root)
	for _, er := range extraRoots {
		if p := parseExtraRoot(er); p != "" {
			if abs, aerr := filepath.Abs(p); aerr == nil {
				sourceRoots = append(sourceRoots, abs)
				// An extra repo root also contributes its awareness/intent
				// corpus as definition-only context, so cross-repo invariant
				// and failure-mode references resolve — not just .go file paths.
				if abs != absRoot {
					extraDefDirs = appendExistingDir(extraDefDirs,
						filepath.Join(abs, "docs", "awareness"),
						filepath.Join(abs, "docs", "intent"),
					)
				}
			}
		}
	}
	if g := siblingRepo(root, "Globular"); g != "" {
		sourceRoots = append(sourceRoots, g)
	}
	// Auto-detect a sibling awareness-graph checkout so the shared meta /
	// generic corpus (invariants + industry failure modes) resolves by default
	// when validating another repo (e.g. services), without requiring --ag-repo.
	// Skipped when we ARE the awareness-graph repo (siblingRepo returns this
	// repo, and absAG == absRoot below excludes it — the corpus is already in
	// scanDirs).
	if ag := siblingRepo(root, "awareness-graph"); ag != "" {
		if absAG, _ := filepath.Abs(ag); absAG != absRoot {
			extraDefDirs = appendExistingDir(extraDefDirs,
				filepath.Join(ag, "docs", "awareness"),
				filepath.Join(ag, "docs", "intent"),
			)
		}
	}

	report, err := doValidate(root, scanDirs, extraDefDirs, sourceRoots, valScope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg validate: %v\n", err)
		return 1
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	default:
		printValidateTable(report)
	}

	errCount, warnCount := 0, 0
	for _, f := range report.Findings {
		if f.Severity == "error" {
			errCount++
		} else if f.Severity == "warn" {
			warnCount++
		}
	}
	if errCount > 0 || (*failOnWarn && warnCount > 0) {
		return 1
	}
	return 0
}

// ── types ────────────────────────────────────────────────────────────────

type validateFinding struct {
	Severity string `json:"severity"`
	Check    string `json:"check"`
	Owner    string `json:"owner"`
	File     string `json:"file"`
	EntityID string `json:"entity_id,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Message  string `json:"message"`
}

type validateReport struct {
	RepoRoot string            `json:"repo_root"`
	Scope    string            `json:"scope"`
	Scanned  []string          `json:"scanned_dirs"`
	Findings []validateFinding `json:"findings"`
	Counts   map[string]int    `json:"counts_by_check"`
}

// validSeverityVocab is the AG-native closed set of severity values, matching
// the public api-reference.md contract (critical | high | warning | info |
// degraded). Any entity that carries a severity field MUST use one of these —
// off-vocabulary values (medium/low leftovers, case variants like ERROR/HIGH,
// typos like "warn") are a hard finding. Case-sensitive by design: "HIGH" is a
// data defect, not an alias. This gate is what stops the malformed-severity
// drift CG-1 fixed by hand from silently recurring. The audit's gating tier
// (critical/high) is a subset; warning/info/degraded are descriptive non-gating
// levels.
var validSeverityVocab = map[string]bool{
	"critical": true,
	"high":     true,
	"warning":  true,
	"info":     true,
	"degraded": true,
}

type validateScope string

const (
	validateScopeLocal validateScope = "local"
	validateScopeFull  validateScope = "full"
)

func parseValidateScope(s string) (validateScope, error) {
	switch validateScope(strings.TrimSpace(s)) {
	case validateScopeLocal:
		return validateScopeLocal, nil
	case validateScopeFull:
		return validateScopeFull, nil
	default:
		return "", fmt.Errorf("scope must be local or full, got %q", s)
	}
}

type validateOwner string

const (
	validateOwnerRepoLocal     validateOwner = "repo_local"
	validateOwnerSharedGeneric validateOwner = "shared_generic"
)

func classifyValidateOwner(relFile string) validateOwner {
	normalized := filepath.ToSlash(relFile)
	if strings.HasPrefix(normalized, "docs/awareness/generic/") {
		return validateOwnerSharedGeneric
	}
	return validateOwnerRepoLocal
}

type valIDIndex struct {
	byClass map[string]map[string][]string
	aliases map[string]map[string]bool
	// external holds ids defined OUTSIDE the validated repo (e.g. the shared
	// meta-principle corpus that lives in the awareness-graph repo). They resolve
	// references but are NOT subject to duplicate-id checks and are never reported
	// as scanned — they are definition-only context.
	external map[string]map[string]bool
}

func newValIDIndex() *valIDIndex {
	return &valIDIndex{
		byClass:  map[string]map[string][]string{},
		aliases:  map[string]map[string]bool{},
		external: map[string]map[string]bool{},
	}
}

func (i *valIDIndex) record(class, id, source string) {
	if i.byClass[class] == nil {
		i.byClass[class] = map[string][]string{}
	}
	i.byClass[class][id] = append(i.byClass[class][id], source)
}

func (i *valIDIndex) recordAlias(class, id string) {
	if i.aliases[class] == nil {
		i.aliases[class] = map[string]bool{}
	}
	i.aliases[class][id] = true
}

// recordExternal registers a definition that lives outside the validated repo.
// It satisfies references via has() but does not participate in duplicate
// detection or the scanned-file report.
func (i *valIDIndex) recordExternal(class, id string) {
	if i.external[class] == nil {
		i.external[class] = map[string]bool{}
	}
	i.external[class][id] = true
}

func (i *valIDIndex) has(class, id string) bool {
	if i.byClass[class] != nil {
		if _, ok := i.byClass[class][id]; ok {
			return true
		}
	}
	if i.external[class] != nil && i.external[class][id] {
		return true
	}
	if i.aliases[class] != nil && i.aliases[class][id] {
		return true
	}
	return false
}

// ── core ─────────────────────────────────────────────────────────────────

func doValidate(repoRoot string, dirs []string, extraDefDirs []string, sourceRoots []string, scope validateScope) (*validateReport, error) {
	report := &validateReport{RepoRoot: repoRoot, Scope: string(scope), Counts: map[string]int{}}

	files, err := collectYAMLFiles(dirs)
	if err != nil {
		return nil, err
	}

	index := newValIDIndex()

	// Load definition-only context from outside the validated repo (e.g. the
	// shared meta-principle corpus that now lives in the awareness-graph repo).
	// These ids resolve references but are not validated or duplicate-checked.
	if extraFiles, derr := collectYAMLFiles(extraDefDirs); derr == nil {
		for _, f := range extraFiles {
			doc, perr := parseValYAMLDoc(f, repoRoot)
			if perr != nil {
				continue // a parse error in external context is not the validated repo's fault
			}
			for _, e := range doc.entities {
				if e.id != "" {
					index.recordExternal(e.class, e.id)
					if e.class == "test" || e.class == "required_test" {
						if short := shortTestID(e.id); short != "" && short != e.id {
							index.recordAlias(e.class, short)
						}
					}
				}
			}
		}
	}
	docs := make(map[string]*valYAMLDoc, len(files))
	for _, f := range files {
		doc, err := parseValYAMLDoc(f, repoRoot)
		if err != nil {
			report.Findings = append(report.Findings, validateFinding{
				Severity: "warn", Check: "yaml_parse_failed",
				File: relTo(repoRoot, f), Message: err.Error(),
			})
			continue
		}
		docs[f] = doc
		for _, e := range doc.entities {
			if e.id != "" {
				index.record(e.class, e.id, relTo(repoRoot, f))
				if e.class == "test" || e.class == "required_test" {
					if short := shortTestID(e.id); short != "" && short != e.id {
						index.recordAlias(e.class, short)
					}
				}
			}
		}
		report.Scanned = append(report.Scanned, relTo(repoRoot, f))
	}

	for _, f := range files {
		doc := docs[f]
		if doc == nil {
			continue
		}
		relFile := relTo(repoRoot, f)
		for _, e := range doc.entities {
			validateEntity(report, index, sourceRoots, relFile, scope, e)
		}
	}

	// Duplicate ID check.
	for class, ids := range index.byClass {
		for id, sources := range ids {
			if len(sources) > 1 {
				// Generated files intentionally overlap: bootstrap emits component
				// nodes, import-scan enriches the same IDs with dependency edges.
				// In the RDF layer these merge cleanly. Only flag when at least one
				// conflicting definition comes from a non-generated (authored) file.
				if allGeneratedPaths(sources) {
					continue
				}
				report.Findings = append(report.Findings, validateFinding{
					Severity: "error", Check: "duplicate_id",
					File: sources[0], EntityID: id,
					Message: fmt.Sprintf("%s id %q defined in %d files: %s",
						class, id, len(sources), strings.Join(sources, ", ")),
				})
			}
		}
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Check != b.Check {
			return a.Check < b.Check
		}
		return a.Ref < b.Ref
	})
	for _, f := range report.Findings {
		report.Counts[f.Check]++
	}
	return report, nil
}

func validateEntity(report *validateReport, idx *valIDIndex, sourceRoots []string, file string, scope validateScope, e valEntity) {
	owner := classifyValidateOwner(file)
	for _, ref := range e.relatedInvariants {
		if !idx.has("invariant", ref) {
			appendValidateFinding(report, scope, owner, "dangling_invariant_ref", "external_dangling_invariant_ref",
				file, e.id, ref, fmt.Sprintf("related_invariants references invariant %q which does not exist", ref))
		}
	}
	for _, ref := range e.relatedFailureModes {
		if !idx.has("failure_mode", ref) {
			appendValidateFinding(report, scope, owner, "dangling_failure_mode_ref", "external_dangling_failure_mode_ref",
				file, e.id, ref, fmt.Sprintf("related_failure_modes references failure_mode %q which does not exist", ref))
		}
	}
	for _, path := range e.referencedFiles {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		if !valPathExists(sourceRoots, path) {
			appendValidateFinding(report, scope, owner, "missing_source_file", "external_missing_source_file",
				file, e.id, path, fmt.Sprintf("path %q referenced from entity %q does not exist", path, e.id))
		}
	}
	// Severity vocabulary: when an entity declares a severity it must be in the
	// AG-native closed set. Fail-closed so a malformed value (medium/low/ERROR/
	// HIGH/warn) cannot re-enter the corpus the way CG-1 fixed by hand.
	if e.severity != "" && !validSeverityVocab[e.severity] {
		report.Findings = append(report.Findings, validateFinding{
			Severity: "error", Check: "invalid_severity",
			File: file, EntityID: e.id, Ref: e.severity,
			Message: fmt.Sprintf("severity %q is not one of critical|high|warning|info|degraded", e.severity),
		})
	}
	// Optional UML profile: kind/view must be in the v1 closed sets when present.
	if e.umlKind != "" && !extractor.ValidUMLKinds[e.umlKind] {
		report.Findings = append(report.Findings, validateFinding{
			Severity: "error", Check: "invalid_uml_kind",
			File: file, EntityID: e.id, Ref: e.umlKind,
			Message: fmt.Sprintf("uml.kind %q is not a supported UML kind", e.umlKind),
		})
	}
	if e.umlView != "" && !extractor.ValidUMLViews[e.umlView] {
		report.Findings = append(report.Findings, validateFinding{
			Severity: "error", Check: "invalid_uml_view",
			File: file, EntityID: e.id, Ref: e.umlView,
			Message: fmt.Sprintf("uml.view %q is not a supported UML view", e.umlView),
		})
	}
	// DesignPattern negative-rule gate: a pattern must say when NOT to use it and
	// what it prevents/forbids — no pattern worship, no decoration.
	if e.class == "design_pattern" {
		var missing []string
		if !e.dpAppliesWhen {
			missing = append(missing, "applies_when")
		}
		if !e.dpDoesNotApplyWhen {
			missing = append(missing, "does_not_apply_when")
		}
		if !e.dpFailurePrevented {
			missing = append(missing, "failure_modes_prevented")
		}
		if !e.dpForbiddenMisuse {
			missing = append(missing, "forbidden_misuses_or_forbidden_by")
		}
		if len(missing) > 0 {
			report.Findings = append(report.Findings, validateFinding{
				Severity: "error", Check: "design_pattern_missing_negative_rule",
				File: file, EntityID: e.id, Ref: strings.Join(missing, ","),
				Message: "DesignPattern must include applies_when, does_not_apply_when, failure_modes_prevented, and a forbidden misuse",
			})
		}
	}
	// Architectural-spine cross-references (Stage A): a component's protected_by,
	// a decision's defines_contracts, etc. must resolve to a defined node.
	for _, cr := range e.crossRefs {
		if !crossRefExists(idx, cr.class, cr.field, cr.ref, sourceRoots) {
			localCheck := "dangling_" + cr.class + "_ref"
			externalCheck := "external_dangling_" + cr.class + "_ref"
			appendValidateFinding(report, scope, owner, localCheck, externalCheck,
				file, e.id, cr.ref, fmt.Sprintf("%s references %s %q which does not exist", cr.field, cr.class, cr.ref))
		}
	}
}

func appendValidateFinding(report *validateReport, scope validateScope, owner validateOwner, localCheck, externalCheck, file, entityID, ref, msg string) {
	check := localCheck
	severity := "error"
	if owner == validateOwnerSharedGeneric {
		check = externalCheck
		if scope == validateScopeLocal {
			severity = "warn"
		}
	}
	report.Findings = append(report.Findings, validateFinding{
		Severity: severity,
		Check:    check,
		Owner:    string(owner),
		File:     file,
		EntityID: entityID,
		Ref:      ref,
		Message:  msg,
	})
}

func crossRefExists(idx *valIDIndex, class, field, ref string, sourceRoots []string) bool {
	if idx.has(class, ref) {
		return true
	}
	if field == "tests" || field == "produced_by_tests" {
		if idx.has("test", ref) {
			return true
		}
		// A path-style test ref ("path/to/file_test.go:TestName") resolves when
		// the test FILE exists under a source root — the test physically exists
		// even if it is not declared as a required_test node. This is what lets a
		// cross-repo gateway test reference (Globular) be machine-checked once the
		// Globular root is supplied via --extra-root / sibling auto-detect.
		if f, ok := testRefFile(ref); ok && valPathExists(sourceRoots, f) {
			return true
		}
	}
	return false
}

// testRefFile extracts the ".go" file path from a "path/to/file.go:TestName"
// test reference. Returns ("", false) when the ref is a bare test id.
func testRefFile(ref string) (string, bool) {
	if i := strings.Index(ref, ".go:"); i >= 0 {
		return ref[:i+len(".go")], true
	}
	return "", false
}

// parseExtraRoot accepts either a bare path or a "name=…,path=…" form and
// returns the path component.
func parseExtraRoot(v string) string {
	v = strings.TrimSpace(v)
	if strings.Contains(v, "path=") {
		for _, part := range strings.Split(v, ",") {
			if p, ok := strings.CutPrefix(strings.TrimSpace(part), "path="); ok {
				return strings.TrimSpace(p)
			}
		}
		return ""
	}
	return v
}

// siblingRepo returns the path to a sibling repository (e.g. "Globular") next to
// root, or "" when it does not exist.
func siblingRepo(root, name string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	cand := filepath.Join(filepath.Dir(abs), name)
	if fi, serr := os.Stat(cand); serr == nil && fi.IsDir() {
		return cand
	}
	return ""
}

// ── YAML parsing ─────────────────────────────────────────────────────────

type valEntity struct {
	class               string
	id                  string
	severity            string
	relatedInvariants   []string
	relatedFailureModes []string
	referencedFiles     []string
	crossRefs           []valCrossRef
	umlKind             string
	umlView             string
	// DesignPattern negative-rule presence flags.
	dpAppliesWhen      bool
	dpDoesNotApplyWhen bool
	dpFailurePrevented bool
	dpForbiddenMisuse  bool
}

// valCrossRef is a cross-spine reference (e.g. a Component's protected_by →
// Boundary) that must resolve to a defined node of the given class.
type valCrossRef struct {
	class string
	ref   string
	field string
}

type valYAMLDoc struct {
	entities []valEntity
}

var valClassByKey = map[string]string{
	"invariants": "invariant", "failure_modes": "failure_mode",
	"incident_patterns": "incident_pattern", "forbidden_fixes": "forbidden_fix",
	"required_tests": "required_test", "decisions": "decision",
	"guardrails": "guardrail", "patterns": "pattern",
	"design_patterns": "pattern", "services": "service",
	"tests": "test",
	// Architectural spine (Stage A).
	"components": "component", "boundaries": "boundary",
	"contracts": "contract", "evidence": "evidence",
	"meta_principle_links": "meta_principle_link",
}

// valSpineRefFields maps a spine reference field to the class its bare ids must
// resolve to. Invariant- and failure-mode-referencing fields are folded into
// the existing relatedInvariants / relatedFailureModes checks instead (see
// extractValEntity); these are the cross-spine references.
var valSpineRefFields = []struct{ field, class string }{
	{"protected_by", "boundary"},
	{"exposes_contracts", "contract"},
	{"depends_on", "component"},
	{"reads_from", "component"},
	{"writes_to", "component"},
	{"separates", "component"},
	{"defines_boundaries", "boundary"},
	{"defines_contracts", "contract"},
	{"affects_components", "component"},
	{"applies_to_components", "component"},
	{"applies_to_boundaries", "boundary"},
	{"applies_to_contracts", "contract"},
	{"exposed_by", "component"},
	{"consumed_by", "component"},
	{"validates_components", "component"},
	{"superseded_by", "decision"},
	{"constrains_decisions", "decision"},
	{"rejects", "forbidden_fix"},
	{"tests", "required_test"},
	{"produced_by_tests", "required_test"},
	{"supported_by_evidence", "evidence"},
	{"implements_intents", "intent"},
	{"explains_intents", "intent"},
	// Design-pattern awareness.
	{"related_components", "component"},
	{"related_boundaries", "boundary"},
	{"related_contracts", "contract"},
	{"related_decisions", "decision"},
	{"forbidden_misuses", "pattern_misuse"},
	{"misused_patterns", "design_pattern"},
	{"recommends_design_patterns", "design_pattern"},
	{"avoided_by", "implementation_pattern"},
	{"used_by_components", "component"},
	{"enforces_contracts", "contract"},
	{"protects_boundaries", "boundary"},
	{"implements_decisions", "decision"},
}

// strField returns m[key] as a trimmed string, or "" if absent/non-string.
func strField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func parseValYAMLDoc(path, repoRoot string) (*valYAMLDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	if raw == nil {
		return &valYAMLDoc{}, nil
	}
	doc := &valYAMLDoc{}

	for key, class := range valClassByKey {
		v, ok := raw[key]
		if !ok {
			continue
		}
		list, ok := v.([]interface{})
		if !ok {
			continue
		}
		for _, item := range list {
			e := extractValEntity(item, class)
			if e.id != "" || len(e.relatedInvariants) > 0 || len(e.relatedFailureModes) > 0 || len(e.referencedFiles) > 0 {
				doc.entities = append(doc.entities, e)
			}
		}
	}

	// uml_profiles overlay: each entry carries a target node id + uml fields.
	// Validate the kind/view enums (refs are checked at the RDF layer).
	if v, ok := raw["uml_profiles"].([]interface{}); ok {
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			ent := valEntity{class: "uml_profile"}
			if n, ok := m["node"].(string); ok {
				ent.id = strings.TrimSpace(n)
			}
			if k, ok := m["kind"].(string); ok {
				ent.umlKind = strings.TrimSpace(k)
			}
			if vw, ok := m["view"].(string); ok {
				ent.umlView = strings.TrimSpace(vw)
			}
			if ent.umlKind != "" || ent.umlView != "" {
				doc.entities = append(doc.entities, ent)
			}
		}
	}

	if id, ok := raw["id"].(string); ok && id != "" {
		class := ""
		if _, has := raw["level"]; has {
			class = "intent"
		}
		switch raw["class"] {
		case "ImplementationPattern":
			class = "implementation_pattern"
		case "DesignPattern":
			class = "design_pattern"
		case "PatternMisuse":
			class = "pattern_misuse"
		}
		if class != "" {
			e := extractValEntity(raw, class)
			e.id = id
			doc.entities = append(doc.entities, e)
		}
	}

	return doc, nil
}

func extractValEntity(node interface{}, class string) valEntity {
	m, ok := node.(map[string]interface{})
	if !ok {
		return valEntity{class: class}
	}
	e := valEntity{class: class}
	if id, ok := m["id"].(string); ok {
		e.id = id
	}
	e.severity = strField(m, "severity")
	e.relatedInvariants = stringsField(m, "related_invariants")
	e.relatedFailureModes = stringsField(m, "related_failure_modes")
	e.referencedFiles = append(e.referencedFiles, stringsField(m, "expressed_by")...)
	e.referencedFiles = append(e.referencedFiles, stringsField(m, "affected_files")...)
	if v, ok := m["reference_files"].([]interface{}); ok {
		for _, item := range v {
			if mm, ok := item.(map[string]interface{}); ok {
				if p, ok := mm["path"].(string); ok {
					e.referencedFiles = append(e.referencedFiles, p)
				}
			}
		}
	}
	if v, ok := m["protects"].(map[string]interface{}); ok {
		e.referencedFiles = append(e.referencedFiles, stringsField(v, "files")...)
	}

	// Optional inline uml block — capture kind/view for enum validation.
	if uml, ok := m["uml"].(map[string]interface{}); ok {
		if k, ok := uml["kind"].(string); ok {
			e.umlKind = strings.TrimSpace(k)
		}
		if v, ok := uml["view"].(string); ok {
			e.umlView = strings.TrimSpace(v)
		}
	}

	// ── Architectural-spine references (Stage A) ──
	// Invariant- and failure-mode-referencing fields fold into the existing
	// dangling_invariant_ref / dangling_failure_mode_ref checks. Meta-principle
	// refs are meta.* invariants, so they resolve as invariants too.
	// Note: related_invariants is already captured above; do not re-add it here.
	for _, k := range []string{"owns_invariants", "constrained_by_invariants",
		"generates_invariants", "satisfies_meta_principles", "violates_meta_principles",
		"satisfies_invariants", "violates_invariants", "related_meta_principles"} {
		e.relatedInvariants = append(e.relatedInvariants, stringsField(m, k)...)
	}
	for _, k := range []string{"vulnerable_to", "mitigates", "confirms",
		"failure_modes_prevented", "prevents_failure_modes", "causes_failure_modes"} {
		e.relatedFailureModes = append(e.relatedFailureModes, stringsField(m, k)...)
	}
	// DesignPattern negative-rule presence (only checked for class design_pattern).
	e.dpAppliesWhen = strField(m, "applies_when") != ""
	e.dpDoesNotApplyWhen = strField(m, "does_not_apply_when") != ""
	e.dpFailurePrevented = len(stringsField(m, "failure_modes_prevented")) > 0
	e.dpForbiddenMisuse = len(stringsField(m, "forbidden_misuses")) > 0 ||
		len(stringsField(m, "forbidden_by")) > 0
	// A Boundary's `protects:` is a list of invariant ids (distinct from the map
	// form invariants/forbidden_fixes use for file anchors, handled above).
	if _, isList := m["protects"].([]interface{}); isList {
		e.relatedInvariants = append(e.relatedInvariants, stringsField(m, "protects")...)
	}
	e.referencedFiles = append(e.referencedFiles, stringsField(m, "source_files")...)
	// Cross-spine references (each must resolve to a defined node of its class).
	for _, sr := range valSpineRefFields {
		for _, ref := range stringsField(m, sr.field) {
			e.crossRefs = append(e.crossRefs, valCrossRef{class: sr.class, ref: ref, field: sr.field})
		}
	}
	return e
}

func valPathExists(sourceRoots []string, path string) bool {
	seen := map[string]bool{}
	var candidates []string
	for _, root := range sourceRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, path),
			filepath.Join(root, strings.TrimPrefix(path, "services/")),
		)
	}
	for _, c := range candidates {
		if seen[c] {
			continue
		}
		seen[c] = true
		if strings.ContainsAny(c, "*?[") {
			matches, _ := filepath.Glob(c)
			if len(matches) > 0 {
				return true
			}
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

func shortTestID(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 && i+1 < len(id) {
		return strings.TrimSpace(id[i+1:])
	}
	return ""
}

// allGeneratedPaths reports whether every path in sources is inside a
// "generated" directory. Used to suppress duplicate_id for generated/generated
// collisions (bootstrap nodes + import-graph edges sharing the same ID).
func allGeneratedPaths(sources []string) bool {
	for _, s := range sources {
		if !strings.Contains(filepath.ToSlash(s), "/generated/") {
			return false
		}
	}
	return true
}

// ── helpers ──────────────────────────────────────────────────────────────

func collectYAMLFiles(dirs []string) ([]string, error) {
	var files []string
	for _, d := range dirs {
		root := d
		_ = filepath.WalkDir(d, func(p string, info fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if filepath.Base(p) == "cache" {
					return filepath.SkipDir
				}
				if filepath.Base(p) == "generated" && p != root {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") {
				files = append(files, p)
			}
			return nil
		})
	}
	sort.Strings(files)
	return files, nil
}

func printValidateTable(r *validateReport) {
	if len(r.Findings) == 0 {
		fmt.Printf("awg validate (%s): scanned %d files in %s — no findings\n",
			r.Scope, len(r.Scanned), r.RepoRoot)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tOWNER\tCHECK\tFILE\tENTITY\tREF\tMESSAGE")
	for _, f := range r.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			strings.ToUpper(f.Severity), f.Owner, f.Check, f.File,
			truncate(f.EntityID, 50), truncate(f.Ref, 60), truncate(f.Message, 100))
	}
	tw.Flush()
	fmt.Printf("\nawg validate (%s): scanned %d files, %d finding(s):\n",
		r.Scope, len(r.Scanned), len(r.Findings))
	for check, n := range r.Counts {
		fmt.Printf("  %s: %d\n", check, n)
	}
}

// multiString implements flag.Value for repeatable -dir flags.
type multiString []string

func (m *multiString) String() string { return strings.Join(*m, ",") }
func (m *multiString) Set(v string) error {
	*m = append(*m, v)
	return nil
}
