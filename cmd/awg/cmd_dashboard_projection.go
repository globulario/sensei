// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/dashboardprojection"
	"gopkg.in/yaml.v3"
)

func runDashboardProjection(args []string) int {
	fs := flag.NewFlagSet("sensei dashboard-projection", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", "", "repository root (default: auto-detect from cwd)")
	outFlag := fs.String("out", "", "write projection JSON to this file instead of stdout")
	public := fs.Bool("public", false, "build a public static-snapshot projection (session/task/PR context is redacted per the adopted contract)")
	checkOnly := fs.Bool("check", false, "validate only; write nothing, exit 1 on any validation error")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei dashboard-projection [flags]

Build a sensei.dashboard.projection.v1 document from this repository's
authored awareness corpus (issue globulario/sensei#115). It never infers
architecture from raw RDF, artifact counts, or heuristics: a field with no
honest authored source is left empty with a documented limitation.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repoRoot, err := resolveProjectRoot(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei dashboard-projection: %v\n", err)
		return 1
	}

	proj, err := buildDashboardProjection(repoRoot, *public, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei dashboard-projection: %v\n", err)
		return 1
	}

	data, err := json.MarshalIndent(proj, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei dashboard-projection: encode: %v\n", err)
		return 1
	}
	data = append(data, '\n')

	// Real JSON Schema instance validation against the canonical, vendored
	// dashboard-projection-v1.schema.json runs first: required fields,
	// enums, formats, patterns, and additionalProperties:false, none of
	// which the hand-written cross-record Validate() below checks. A typed
	// Go struct proves the shape compiles; it does not prove the adopted
	// closed schema is satisfied.
	schemaDir := filepath.Join(repoRoot, "docs", "schemas", "dashboard-projection", "v1")
	if err := dashboardprojection.ValidateProjectionSchema(schemaDir, data); err != nil {
		fmt.Fprintf(os.Stderr, "sensei dashboard-projection: JSON Schema validation failed: %v\n", err)
		return 1
	}

	errs := dashboardprojection.Validate(proj)
	if *public {
		errs = append(errs, dashboardprojection.ValidatePublicRedaction(proj)...)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "sensei dashboard-projection: %s\n", e.Error())
		}
		return 1
	}
	if *checkOnly {
		fmt.Fprintln(os.Stderr, "sensei dashboard-projection: OK (JSON Schema + producer validation both passed)")
		return 0
	}

	if *outFlag == "" {
		os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(*outFlag, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei dashboard-projection: write %s: %v\n", *outFlag, err)
		return 1
	}
	return 0
}

// --- minimal read-only mirrors of the authored awareness YAML shapes this
// producer consumes. These intentionally carry only the fields the
// projection maps; they are not a general-purpose corpus parser and must
// never become one — golang/extractor remains the sole importer of record
// for the compiled graph. ---

type yamlUML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
}

type yamlComponentEntry struct {
	ID                  string   `yaml:"id"`
	Name                string   `yaml:"name"`
	Description         string   `yaml:"description"`
	Kind                string   `yaml:"kind"`
	Owner               string   `yaml:"owner"`
	OwnsInvariants      []string `yaml:"owns_invariants"`
	ExposesContracts    []string `yaml:"exposes_contracts"`
	ProtectedBy         []string `yaml:"protected_by"`
	SupportedByEvidence []string `yaml:"supported_by_evidence"`
	SourceFiles         []string `yaml:"source_files"`
}

type yamlComponentsFile struct {
	Components []yamlComponentEntry `yaml:"components"`
}

type yamlBoundaryEntry struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Kind        string   `yaml:"kind"`
	Status      string   `yaml:"status"`
	Separates   []string `yaml:"separates"`
	Protects    []string `yaml:"protects"`
	SourceFiles []string `yaml:"source_files"`
}

type yamlBoundariesFile struct {
	Boundaries []yamlBoundaryEntry `yaml:"boundaries"`
}

type yamlContractEntry struct {
	ID                      string   `yaml:"id"`
	Name                    string   `yaml:"name"`
	Description             string   `yaml:"description"`
	Kind                    string   `yaml:"kind"`
	Stability               string   `yaml:"stability"`
	ReadOrWrite             string   `yaml:"read_or_write"`
	Assertion               string   `yaml:"assertion"`
	ExposedBy               []string `yaml:"exposed_by"`
	ConstrainedByInvariants []string `yaml:"constrained_by_invariants"`
	SourceFiles             []string `yaml:"source_files"`
}

type yamlContractsFile struct {
	Contracts []yamlContractEntry `yaml:"contracts"`
}

type yamlFailureModeEntry struct {
	ID       string `yaml:"id"`
	Title    string `yaml:"title"`
	Severity string `yaml:"severity"`
}

type yamlFailureModesFile struct {
	FailureModes []yamlFailureModeEntry `yaml:"failure_modes"`
}

type yamlForbiddenFixEntry struct {
	ID      string `yaml:"id"`
	Title   string `yaml:"title"`
	Summary string `yaml:"summary"`
	Reason  string `yaml:"reason"`
}

type yamlForbiddenFixesFile struct {
	ForbiddenFixes []yamlForbiddenFixEntry `yaml:"forbidden_fixes"`
}

func loadYAML[T any](path string) (T, error) {
	var out T
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// severityFromAuthored maps the authored severity vocabularies actually found
// in this repo's corpus (failure_modes.yaml uses "warning" in addition to the
// dashboard schema's own enum values) onto the adopted dashboard schema's
// closed severity enum. An unrecognized value maps to "unknown" rather than a
// guess.
func severityFromAuthored(raw string) dashboardprojection.Severity {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical":
		return dashboardprojection.SeverityCritical
	case "high":
		return dashboardprojection.SeverityHigh
	case "warning", "medium":
		return dashboardprojection.SeverityMedium
	case "low":
		return dashboardprojection.SeverityLow
	case "info":
		return dashboardprojection.SeverityInfo
	default:
		return dashboardprojection.SeverityUnknown
	}
}

func stableID(prefix string) string {
	return prefix
}

// ungroupedRegionID is the stable id of the single synthetic placeholder
// region this producer emits when components exist but no owner-authored
// region entity does yet (see buildDashboardProjection). It is deliberately
// named so it can never collide with a real authored region id later.
const ungroupedRegionID = "region.ungrouped.synthetic-placeholder"

// nz turns a possibly-nil string slice into a non-nil empty slice. The
// adopted schema requires every *_refs array to be present (type: array,
// no null in the union) even when empty; YAML fields the corpus author left
// out unmarshal to a nil Go slice, which would otherwise marshal to JSON
// null and fail validation.
func nz(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func emptyProvenance(sourceFile string) dashboardprojection.Provenance {
	p := dashboardprojection.Provenance{EvidenceRefs: []string{}}
	if sourceFile != "" {
		p.SourceRefs = []string{sourceFile}
	}
	return p
}

func sourceLinksFrom(files []string) []dashboardprojection.SourceLink {
	if len(files) == 0 {
		return nil
	}
	links := make([]dashboardprojection.SourceLink, 0, len(files))
	for _, f := range files {
		links = append(links, dashboardprojection.SourceLink{Label: filepath.Base(f), Target: f})
	}
	return links
}

// buildDashboardProjection assembles a sensei.dashboard.projection.v1 value
// entirely from this repository's own committed, authored sources: the
// awareness architecture corpus (components/boundaries/contracts,
// failure_modes, forbidden_fixes) and git/graph-build evidence. now is
// injected so callers (including tests) control generated_at rather than
// this function reading the clock itself.
func buildDashboardProjection(repoRoot string, public bool, now time.Time) (dashboardprojection.Projection, error) {
	var limitations []string
	availState := dashboardprojection.Available

	head, err := gitHead(repoRoot)
	if err != nil {
		return dashboardprojection.Projection{}, fmt.Errorf("resolve revision: %w", err)
	}
	branch, _ := gitCurrentBranch(repoRoot)
	committedAt, _ := gitCommitTimestamp(repoRoot, head)

	moduleName := readGoModuleName(repoRoot)
	displayName := moduleName
	if idx := strings.LastIndex(moduleName, "/"); idx >= 0 {
		displayName = moduleName[idx+1:]
	}
	repoURL := "https://" + moduleName

	// --- graph_authority / projection_integrity: reuse the real `sensei
	// rebuild --check` command as a subprocess rather than re-driving
	// graphbuild directly. A hand-rolled SourceRoot/Policy reimplementation
	// was tried and proven wrong against ground truth during development
	// (it reported a false STALE — 8065 vs 7629 lines — on a checkout
	// `sensei rebuild --check` correctly reports as "fresh (no changes)").
	// Reusing the canonical, already-verified command guarantees this
	// producer never fabricates an integrity verdict from a subtly
	// mis-configured reimplementation. If `sensei`/`awg` is not on PATH, the
	// honest fallback is "unknown", not a guess.
	graphAuthority := dashboardprojection.GraphAuthority{
		Observed: dashboardprojection.TriUnknown,
		Current:  dashboardprojection.TriUnknown,
		Summary:  "graph freshness not evaluated",
	}
	integrity := dashboardprojection.Assessment{
		State: dashboardprojection.StateUnknown, Label: "Projection integrity", Severity: dashboardprojection.SeverityNotApplicable,
		Summary:    "embedded-graph freshness was not evaluated (sensei binary not available to this producer run)",
		Provenance: emptyProvenance(""),
	}

	if fresh, detail, checkErr := checkEmbeddedSeedFreshness(repoRoot); checkErr != nil {
		limitations = append(limitations, fmt.Sprintf("embeddata-freshness could not be evaluated: %v", checkErr))
		availState = dashboardprojection.Partial
	} else {
		graphAuthority.Observed = dashboardprojection.TriYes
		graphAuthority.Summary = detail
		integrity.Provenance = dashboardprojection.Provenance{
			EvidenceRefs: []string{},
			SourceRefs:   []string{"golang/server/embeddata/awareness.nt"},
			Limitations:  []string{"scoped to the embeddata-freshness dimension only (via `sensei rebuild --check`); the full sensei audit check suite (yaml-validity, seed-orphans, coverage-gaps, stale-file-refs, test-coverage) is not yet wired into this producer"},
		}
		integrity.Summary = "embeddata-freshness (sensei rebuild --check): " + detail
		if fresh {
			integrity.State = dashboardprojection.StateHealthy
			integrity.Severity = dashboardprojection.SeverityNotApplicable
			graphAuthority.Current = dashboardprojection.TriYes
		} else {
			integrity.State = dashboardprojection.StateContested
			integrity.Severity = dashboardprojection.SeverityHigh
			graphAuthority.Current = dashboardprojection.TriNo
		}
	}

	// --- architecture_health: intentionally left unknown for v1. Wiring a
	// real score requires repoeval.Evaluate's full Inputs, whose gathering
	// (~1000 lines: coverage stats, contract stats, invariant stats) is
	// private to cmd/awg/cmd_repo_eval.go and was judged too large to safely
	// replicate or refactor within this bounded change. Feeding it partial or
	// zeroed inputs would produce a real-looking but misleading number, which
	// is worse than an honest unknown. See PR description: non-blocking note.
	health := dashboardprojection.Assessment{
		State: dashboardprojection.StateUnknown, Severity: dashboardprojection.SeverityNotApplicable,
		Label:   "Architecture health",
		Summary: "not yet evaluated by this producer; wiring requires exporting sensei repo-eval's input-gathering surface (deferred, see PR non-blocking notes)",
		Provenance: dashboardprojection.Provenance{
			EvidenceRefs: []string{},
			Limitations:  []string{"architecture_health is not computed by this producer version; preserved as unknown rather than guessed"},
		},
	}

	// --- components / boundaries / contracts / attention: direct, honest
	// mapping from the authored Stage A architecture corpus. ---
	components, componentFocus, err := loadComponents(repoRoot)
	if err != nil {
		limitations = append(limitations, err.Error())
		availState = dashboardprojection.Partial
	}
	boundaries, boundaryFocus, err := loadBoundaries(repoRoot)
	if err != nil {
		limitations = append(limitations, err.Error())
		availState = dashboardprojection.Partial
	}
	contracts, contractFocus, err := loadContracts(repoRoot)
	if err != nil {
		limitations = append(limitations, err.Error())
		availState = dashboardprojection.Partial
	}
	attention, err := loadAttention(repoRoot)
	if err != nil {
		limitations = append(limitations, err.Error())
		availState = dashboardprojection.Partial
	}

	// The adopted schema requires every component to carry a non-empty
	// region_ref (regions are not optional grouping — every component must
	// belong to exactly one). No owner-authored region entity exists yet
	// (see limitation below), so rather than leave region_ref empty
	// (schema-invalid) or invent a plausible-sounding region name
	// (fabricated architecture), this producer emits exactly one explicitly
	// synthetic "ungrouped" region whose name and responsibility say only
	// that grouping has not happened yet — never a claim about what the
	// regions actually are.
	regions := []dashboardprojection.Region{}
	var regionFocus []dashboardprojection.FocusRecord
	if len(components) > 0 {
		componentIDs := make([]string, len(components))
		for i, c := range components {
			componentIDs[i] = c.ID
		}
		regionProv := dashboardprojection.Provenance{
			EvidenceRefs: []string{},
			Limitations:  []string{"synthetic placeholder: no owner-authored region entity exists yet for this repository (see availability.limitations)"},
		}
		regions = append(regions, dashboardprojection.Region{
			ID: ungroupedRegionID, Name: "Ungrouped",
			Responsibility: "Components not yet assigned to an owner-authored architectural region. This is a placeholder grouping, not a claim about the system's real region structure.",
			State:          dashboardprojection.StateUnknown,
			ComponentRefs:  componentIDs,
			VisualAnchor:   dashboardprojection.VisualAnchor{Order: 0},
			Provenance:     regionProv,
		})
		regionFocus = append(regionFocus, dashboardprojection.FocusRecord{
			ElementRef: ungroupedRegionID, ElementKind: "region", Name: "Ungrouped",
			Responsibility: "Components not yet assigned to an owner-authored architectural region.",
			State:          dashboardprojection.StateUnknown,
			OwnerRefs:      []string{}, OwnedRefs: componentIDs, ContractRefs: []string{}, FlowRefs: []string{},
			AttentionRefs: []string{}, DecisionRefs: []string{}, Provenance: regionProv,
		})
	}

	focusRecords := append(append([]dashboardprojection.FocusRecord{}, regionFocus...), componentFocus...)
	focusRecords = append(focusRecords, boundaryFocus...)
	focusRecords = append(focusRecords, contractFocus...)

	observed := len(components) + len(boundaries) + len(contracts) + len(attention)
	observationSeverity := dashboardprojection.SeverityMedium
	observationState := dashboardprojection.StateAttention
	limitations = append(limitations,
		"regions are not authored: components are grouped under one synthetic 'Ungrouped' placeholder region because no owner-authored region entity exists yet in this repository's awareness corpus (components.yaml carries a bare 'owner' tag with no name/responsibility text) — see PR non-blocking note",
		"flows are not populated: docs/awareness/architecture/contract_realizations.yaml (the natural flow source) currently has zero authored realizations — see PR non-blocking note",
		"architecture_health is unknown: see assessments.architecture_health.provenance.limitations",
	)

	// Regions, flows, and architecture_health are material parts of the V1
	// architectural view (the map's primary grouping, its behavioral layer,
	// and its headline quality signal), not decorative optional metadata.
	// This producer version never has an authored source for any of the
	// three, so a projection it builds from this repository can never
	// honestly claim `available` ("the view was fully constructible") —
	// it is always, structurally, partial. These three booleans exist so
	// that claim is enforced by the code, not just by comment.
	const (
		regionsAuthored = false // only a synthetic, explicitly non-authoritative placeholder is emitted
		flowsAuthored   = false // docs/awareness/architecture/contract_realizations.yaml has zero authored realizations
		healthEvaluated = false // architecture_health stays unknown in this producer version
	)
	sourceStates := []dashboardprojection.SourceState{
		{Owner: "docs/awareness/architecture", Availability: dashboardprojection.Available, Summary: "authored components.yaml, boundaries.yaml, and contracts.yaml corpus"},
		{Owner: "regions", Availability: dashboardprojection.Unavailable, Summary: "no owner-authored region entity exists yet; a synthetic, explicitly non-authoritative placeholder is emitted instead"},
		{Owner: "flows", Availability: dashboardprojection.Unavailable, Summary: "contract_realizations.yaml has zero authored realizations"},
		{Owner: "architecture_health", Availability: dashboardprojection.Unavailable, Summary: "not evaluated by this producer version"},
	}
	if (!regionsAuthored || !flowsAuthored || !healthEvaluated) && availState == dashboardprojection.Available {
		availState = dashboardprojection.Partial
	}

	identity := dashboardprojection.Identity{
		ProjectionID: stableID("projection." + head),
		Repository: dashboardprojection.Repository{
			Key:         moduleName,
			DisplayName: displayName,
			URL:         &repoURL,
		},
		Revision: dashboardprojection.Revision{
			ID:          head,
			Ref:         nonEmptyPtr(branch),
			CommittedAt: nonEmptyPtr(committedAt),
		},
		GraphAuthority: graphAuthority,
		GeneratedAt:    now.Format(time.RFC3339),
	}

	availSummary := "projection is usable but incomplete: components, boundaries, contracts, and attention items are drawn from the authored corpus, but regions and flows have no authored source and architecture_health is not evaluated in this producer version — see limitations and per-source availability"
	if availState == dashboardprojection.Unavailable {
		availSummary = "projection unavailable: one or more required sources failed entirely; see limitations"
	}

	briefing := []dashboardprojection.Briefing{
		{
			ID:   "briefing.orientation",
			Kind: "orientation",
			Text: fmt.Sprintf(
				"Revision %s of %s. The authored architecture corpus documents %d component(s), %d boundary(ies), and %d contract(s); %d attention item(s) (failure modes and forbidden fixes) are tracked. Regions and flows have no authored source yet. Architecture-health scoring is not yet wired into this producer.",
				shortSHA(head), moduleName, len(components), len(boundaries), len(contracts), len(attention),
			),
			Severity:    dashboardprojection.SeverityInfo,
			ElementRefs: []string{},
			Provenance:  emptyProvenance(""),
		},
	}

	proj := dashboardprojection.Projection{
		SchemaVersion: dashboardprojection.SchemaVersion,
		Identity:      identity,
		Availability: dashboardprojection.Availability{
			State:       availState,
			Summary:     availSummary,
			Limitations: limitations,
			Sources:     sourceStates,
		},
		Assessments: dashboardprojection.Assessments{
			ArchitectureHealth:  health,
			ProjectionIntegrity: integrity,
			ObservationCompleteness: dashboardprojection.ObservationAssessment{
				State: observationState, Severity: observationSeverity,
				Label:   "Observation completeness",
				Summary: "coverage is the count of authored architectural elements in this revision; the total real architectural surface of the repository is not computed, so total is left unknown rather than estimated",
				Coverage: dashboardprojection.Coverage{
					Observed: intPtr(observed),
					Total:    nil,
					Unit:     "authored architectural elements",
				},
				Provenance: emptyProvenance(""),
			},
		},
		ActiveContext: nil,
		Briefing:      briefing,
		Regions:       regions,
		Components:    components,
		Boundaries:    boundaries,
		Contracts:     contracts,
		Flows:         []dashboardprojection.Flow{},
		Attention:     attention,
		Evolution: dashboardprojection.Evolution{
			Availability: dashboardprojection.Available,
			BaseRevision: nil,
			HeadRevision: head,
			Summary:      strPtr("first observed revision for this producer; no prior authoritative projection exists to compare against"),
			Changes:      []dashboardprojection.Change{},
		},
		FocusRecords: focusRecords,
		Capabilities: &dashboardprojection.Capabilities{
			LiveRefresh:     false,
			RevisionCompare: false,
			AgentHandoff:    handoffCapability(public),
			SourceDeepLinks: true,
		},
	}

	return proj, nil
}

func handoffCapability(public bool) dashboardprojection.AgentHandoffCapability {
	if public {
		return dashboardprojection.HandoffExport
	}
	return dashboardprojection.HandoffNone
}

// checkEmbeddedSeedFreshness shells out to the real `sensei rebuild --check`
// (self-only, no runtime reload) and reports whether the committed
// golang/server/embeddata/awareness.nt matches a fresh compile of this
// checkout's authored sources. `sensei rebuild --check` exits 1 on stale per
// its own documented CI contract, so the exit code is the primary signal;
// the "Check mode:" line (if present) is carried as the human-readable
// detail. It looks for "sensei" first, then the legacy "awg" alias.
func checkEmbeddedSeedFreshness(repoRoot string) (fresh bool, detail string, err error) {
	// Use the exact currently-running binary, never an arbitrary installed
	// `sensei`/`awg` resolved from PATH. Graph-authority truth must be bound
	// to the exact executable and checkout performing this build: a
	// different installed version on PATH could silently answer with a
	// different build's notion of "fresh", which this producer must not
	// present as its own authority.
	bin, lookErr := os.Executable()
	if lookErr != nil {
		return false, "", fmt.Errorf("resolve current executable: %w", lookErr)
	}
	// Guard against invoking a non-CLI host binary (a `go test` binary is
	// the case that matters: it runs under this exact package, so a naive
	// os.Executable() re-invocation would recursively re-run this whole test
	// suite as a subprocess, which itself does the same thing again).
	// Refuse anything whose base name is not literally the CLI binary.
	base := filepath.Base(bin)
	if base != "sensei" && base != "awg" {
		return false, "", fmt.Errorf("current executable %s (base name %q) is not the sensei/awg CLI; refusing to invoke it as one", bin, base)
	}

	cmd := exec.Command(bin, "rebuild", "--check", "--no-runtime-reload")
	cmd.Dir = repoRoot
	out, runErr := cmd.CombinedOutput()
	text := string(out)

	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Check mode:") {
			line = strings.TrimSpace(l)
			break
		}
	}
	if line == "" {
		// The current executable did not produce a recognizable `sensei
		// rebuild --check` status line — e.g. it is a `go test` binary
		// exercising this function directly, not the sensei CLI. Report
		// honestly that freshness could not be evaluated rather than
		// guessing stale from an exit code that means something else here.
		return false, "", fmt.Errorf("current executable %s did not produce a `Check mode:` line for `rebuild --check` (output: %q)", bin, strings.TrimSpace(text))
	}

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return true, line, nil
	case errors.As(runErr, &exitErr):
		return false, line, nil
	default:
		return false, "", fmt.Errorf("running %s rebuild --check: %w", bin, runErr)
	}
}

func loadComponents(repoRoot string) ([]dashboardprojection.Component, []dashboardprojection.FocusRecord, error) {
	path := filepath.Join(repoRoot, "docs", "awareness", "architecture", "components.yaml")
	f, err := loadYAML[yamlComponentsFile](path)
	if err != nil {
		return nil, nil, fmt.Errorf("components: %w", err)
	}
	sort.Slice(f.Components, func(i, j int) bool { return f.Components[i].ID < f.Components[j].ID })

	components := make([]dashboardprojection.Component, 0, len(f.Components))
	focus := make([]dashboardprojection.FocusRecord, 0, len(f.Components))
	for i, c := range f.Components {
		state := dashboardprojection.StateOpen
		if len(c.SupportedByEvidence) > 0 {
			state = dashboardprojection.StateProven
		}
		prov := emptyProvenance(strings.Join(c.SourceFiles, ""))
		prov.EvidenceRefs = nz(c.SupportedByEvidence)
		prov.SourceRefs = c.SourceFiles

		components = append(components, dashboardprojection.Component{
			ID: c.ID, Name: c.Name, RegionRef: ungroupedRegionID, Responsibility: c.Description, State: state,
			AuthorityRefs: nz(c.ProtectedBy),
			VisualAnchor:  dashboardprojection.VisualAnchor{Order: i},
			Provenance:    prov,
		})
		focus = append(focus, dashboardprojection.FocusRecord{
			ElementRef: c.ID, ElementKind: "component", Name: c.Name, Responsibility: c.Description, State: state,
			OwnerRefs: []string{}, ContractRefs: nz(c.ExposesContracts), FlowRefs: []string{}, AttentionRefs: []string{},
			DecisionRefs: []string{}, SourceLinks: sourceLinksFrom(c.SourceFiles), Provenance: prov,
		})
	}
	return components, focus, nil
}

func loadBoundaries(repoRoot string) ([]dashboardprojection.Boundary, []dashboardprojection.FocusRecord, error) {
	path := filepath.Join(repoRoot, "docs", "awareness", "architecture", "boundaries.yaml")
	f, err := loadYAML[yamlBoundariesFile](path)
	if err != nil {
		return nil, nil, fmt.Errorf("boundaries: %w", err)
	}
	sort.Slice(f.Boundaries, func(i, j int) bool { return f.Boundaries[i].ID < f.Boundaries[j].ID })

	boundaries := make([]dashboardprojection.Boundary, 0, len(f.Boundaries))
	focus := make([]dashboardprojection.FocusRecord, 0, len(f.Boundaries))
	for _, b := range f.Boundaries {
		state := dashboardprojection.StateOpen
		switch strings.ToLower(b.Status) {
		case "active":
			state = dashboardprojection.StateHealthy
		case "deprecated", "retired":
			state = dashboardprojection.StateDegraded
		}
		kind := b.Kind
		if !validBoundaryKind(kind) {
			kind = "other"
		}
		prov := emptyProvenance("")
		prov.SourceRefs = b.SourceFiles
		summary := b.Description
		if summary == "" {
			summary = b.Name
		}

		boundaries = append(boundaries, dashboardprojection.Boundary{
			ID: b.ID, Name: b.Name, Kind: kind, MemberRefs: nz(b.Separates), State: state, Summary: summary, Provenance: prov,
		})
		focus = append(focus, dashboardprojection.FocusRecord{
			ElementRef: b.ID, ElementKind: "boundary", Name: b.Name, Responsibility: summary, State: state,
			OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{},
			DecisionRefs: []string{}, SourceLinks: sourceLinksFrom(b.SourceFiles), Provenance: prov,
		})
	}
	return boundaries, focus, nil
}

func validBoundaryKind(k string) bool {
	switch k {
	case "authority", "ownership", "domain", "trust", "deployment", "other":
		return true
	}
	return false
}

// loadContracts merges the two authored contract sources this repo actually
// carries: the hand-authored non-proto contracts (contracts.yaml) and the
// generator-derived gRPC contracts extracted from proto/awareness_graph.proto
// (awareness_graph_proto_contracts.yaml, assertion: inferred — see that
// file's own header comment). components.yaml's exposes_contracts refs span
// both files, so both must be loaded for focus/reference integrity to hold.
func loadContracts(repoRoot string) ([]dashboardprojection.Contract, []dashboardprojection.FocusRecord, error) {
	authored, err := loadYAML[yamlContractsFile](filepath.Join(repoRoot, "docs", "awareness", "architecture", "contracts.yaml"))
	if err != nil {
		return nil, nil, fmt.Errorf("contracts: %w", err)
	}
	inferred, err := loadYAML[yamlContractsFile](filepath.Join(repoRoot, "docs", "awareness", "architecture", "awareness_graph_proto_contracts.yaml"))
	if err != nil {
		return nil, nil, fmt.Errorf("proto contracts: %w", err)
	}
	all := append(append([]yamlContractEntry{}, authored.Contracts...), inferred.Contracts...)
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	contracts := make([]dashboardprojection.Contract, 0, len(all))
	focus := make([]dashboardprojection.FocusRecord, 0, len(all))
	for _, c := range all {
		if len(c.ExposedBy) == 0 {
			// No exposer to anchor source_ref/target_ref to (schema requires
			// both non-empty) — skip rather than emit an invalid or guessed ref.
			continue
		}
		prov := emptyProvenance("")
		prov.SourceRefs = c.SourceFiles
		if c.Assertion == "inferred" {
			prov.Limitations = []string{"generator-derived from proto/awareness_graph.proto (assertion: inferred), not independently governed"}
		}
		// "open" for both: hand-authored contracts are documented-but-not-
		// independently-verified, and inferred contracts are real, evidenced
		// (proto/awareness_graph.proto), mechanically-extracted facts — neither
		// is "proven" by this producer's own verification, but both are
		// observed, so "unobserved" would be inaccurate.
		state := dashboardprojection.StateOpen
		anchor := c.ExposedBy[0]
		contracts = append(contracts, dashboardprojection.Contract{
			ID: c.ID, Name: c.Name, SourceRef: anchor, TargetRef: anchor, Kind: c.Kind,
			Direction: "undirected", State: state, Summary: c.Description, Provenance: prov,
		})
		focus = append(focus, dashboardprojection.FocusRecord{
			ElementRef: c.ID, ElementKind: "contract", Name: c.Name, Responsibility: c.Description, State: state,
			OwnerRefs: []string{}, ContractRefs: []string{}, FlowRefs: []string{}, AttentionRefs: []string{},
			DecisionRefs: []string{}, SourceLinks: sourceLinksFrom(c.SourceFiles), Provenance: prov,
		})
	}
	return contracts, focus, nil
}

func loadAttention(repoRoot string) ([]dashboardprojection.AttentionItem, error) {
	items := []dashboardprojection.AttentionItem{}

	fmPath := filepath.Join(repoRoot, "docs", "awareness", "failure_modes.yaml")
	fm, err := loadYAML[yamlFailureModesFile](fmPath)
	if err != nil {
		return nil, fmt.Errorf("failure_modes: %w", err)
	}
	sort.Slice(fm.FailureModes, func(i, j int) bool { return fm.FailureModes[i].ID < fm.FailureModes[j].ID })
	for _, m := range fm.FailureModes {
		items = append(items, dashboardprojection.AttentionItem{
			ID: "failure_mode." + m.ID, Kind: "failure_mode", Title: m.Title, Summary: m.Title,
			Severity: severityFromAuthored(m.Severity), State: dashboardprojection.StateOpen,
			ElementRefs: []string{}, Provenance: emptyProvenance("docs/awareness/failure_modes.yaml"),
		})
	}

	ffPath := filepath.Join(repoRoot, "docs", "awareness", "forbidden_fixes.yaml")
	ff, err := loadYAML[yamlForbiddenFixesFile](ffPath)
	if err != nil {
		return nil, fmt.Errorf("forbidden_fixes: %w", err)
	}
	sort.Slice(ff.ForbiddenFixes, func(i, j int) bool { return ff.ForbiddenFixes[i].ID < ff.ForbiddenFixes[j].ID })
	for _, m := range ff.ForbiddenFixes {
		summary := m.Summary
		if summary == "" {
			summary = m.Reason
		}
		items = append(items, dashboardprojection.AttentionItem{
			ID: "forbidden_fix." + m.ID, Kind: "forbidden_move", Title: m.Title, Summary: summary,
			Severity: dashboardprojection.SeverityMedium, State: dashboardprojection.StateOpen,
			ElementRefs: []string{}, Provenance: emptyProvenance("docs/awareness/forbidden_fixes.yaml"),
		})
	}

	return items, nil
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func gitCurrentBranch(repo string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		return "", nil // detached HEAD: no meaningful branch name
	}
	return branch, nil
}

func gitCommitTimestamp(repo, revision string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "log", "-1", "--format=%cI", revision)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func readGoModuleName(repoRoot string) string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return "unknown"
}
