// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/extractor"
)

var canonicalIDPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// Meta-principles are dual-typed meta.* invariants that live under the
// `invariants:` key of the portable pack (docs/awareness/generic/...), not the
// product canonical dir — so their target is a subpath, not a bare filename.
const metaPrincipleTarget = "generic/state_authority_invariants.yaml"

var promoteClassToTarget = map[string]string{
	"invariant": "invariants.yaml", "failure_mode": "failure_modes.yaml",
	"incident_pattern": "incident_patterns.yaml", "intent": "intents.yaml",
	"meta_principle": metaPrincipleTarget,
}
var promoteTargetToListKey = map[string]string{
	"invariants.yaml": "invariants", "failure_modes.yaml": "failure_modes",
	"incident_patterns.yaml": "incident_patterns", "intents.yaml": "intents",
	metaPrincipleTarget: "invariants",
}
var promoteTargetToClass = map[string]string{
	"invariants.yaml": "invariant", "failure_modes.yaml": "failure_mode",
	"incident_patterns.yaml": "incident_pattern", "intents.yaml": "intent",
	metaPrincipleTarget: "meta_principle",
}

func runPromote(args []string) int {
	fs := flag.NewFlagSet("sensei promote", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	target := fs.String("target", "", "target canonical YAML file (auto-detected from class)")
	dryRun := fs.Bool("dry-run", false, "validate only, do not modify files")
	noRebuild := fs.Bool("no-rebuild", false, "skip automatic rebuild after promotion")
	noCheck := fs.Bool("no-check", false, "skip the coherence gate (validate + audit) after rebuild")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	repoFlag := fs.String("repo", "", "PILOT: foreign repo domain, e.g. github.com/caddyserver/caddy — routes the promotion into pilot/<repo>/ as a separate domain-scoped graph")
	domainFlag := fs.String("domain", "", "PILOT: domain kind (repo|shared); defaults to repo when --repo is set")
	sourceSetFlag := fs.String("source-set", "", "PILOT: source-set namespace (default: pilot/<repo-slug>)")
	oxigraphURLFlag := fs.String("oxigraph-url", "http://localhost:7878/store?default", "PILOT: Oxigraph endpoint to additively load the pilot graph into")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei promote <candidate-id> [flags]

Promotes a candidate into the matching canonical YAML file. Validates naming,
status, confidence, evidence.

Default (home domain): reads docs/awareness/candidates/, writes the product
repo's canonical YAML, and rebuilds the embedded seed.

class:meta_principle candidates (id must be meta.*) route to the portable pack
docs/awareness/generic/state_authority_invariants.yaml (the awareness-graph repo
when it owns the pack), where they are dual-typed Invariant+MetaPrinciple.

Pilot (--repo set): reads pilot/<repo-slug>/candidates/, writes a domain-tagged
entry into pilot/<repo-slug>/<class>.yaml, and ADDITIVELY loads that pilot graph
into the running Oxigraph store — never touching the embedded seed. This is the
ONLY path that admits a foreign repo's rules into a served graph.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "sensei promote: requires exactly 1 arg: <candidate-id>")
		return 2
	}
	candidateID := fs.Arg(0)

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	if svcRepo == "" {
		root, _ := resolveProjectRoot("")
		svcRepo = root
	}
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)

	pilotRepo := strings.TrimSpace(*repoFlag)
	pilot := pilotRepo != ""

	// Resolve where candidates are read from and where canonical YAML is
	// written. Home domain → the product repo's docs/awareness. Pilot → a
	// separate pilot/<repo-slug>/ tree in the awareness-graph repo, kept out
	// of the embedded seed by living outside docs/awareness + docs/intent.
	var candidatesDir, awarenessDir, baseRepo string
	if pilot {
		if agRepo == "" {
			fmt.Fprintln(os.Stderr, "sensei promote: --repo set but awareness-graph repo not found (use --ag-repo)")
			return 1
		}
		pilotDir := filepath.Join(agRepo, "pilot", pilotSlug(pilotRepo))
		candidatesDir = filepath.Join(pilotDir, "candidates")
		awarenessDir = pilotDir
		baseRepo = agRepo
	} else {
		awarenessDir = filepath.Join(svcRepo, "docs", "awareness")
		candidatesDir = filepath.Join(awarenessDir, "candidates")
		baseRepo = svcRepo
	}

	// Find candidate.
	candidatePath, candidate, err := findCandidateEntry(candidatesDir, candidateID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: %v\n", err)
		return 1
	}
	fmt.Printf("candidate found: %s\n", relTo(baseRepo, candidatePath))

	// Resolve target.
	targetFilename, err := resolvePromoteTarget(*target, candidate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: %v\n", err)
		return 1
	}
	// Meta-principles belong in the awareness-graph-owned portable pack, not the
	// product canonical dir. When promoting one and the ag repo is distinct and
	// actually holds the pack, write there instead of the services repo.
	candidateClass, _ := candidate["class"].(string)
	if !pilot && candidateClass == "meta_principle" && agRepo != "" {
		agAwareness := filepath.Join(agRepo, "docs", "awareness")
		if fileExists(filepath.Join(agAwareness, targetFilename)) {
			awarenessDir = agAwareness
			baseRepo = agRepo
		}
	}
	targetPath := filepath.Join(awarenessDir, targetFilename)
	listKey := promoteTargetToListKey[targetFilename]

	// Validate.
	if err := validateCandidateEntry(candidate, targetFilename, awarenessDir); err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: validation failed: %v\n", err)
		return 1
	}
	if pilot {
		if err := validatePilotScope(candidate, pilotRepo); err != nil {
			fmt.Fprintf(os.Stderr, "sensei promote: pilot validation failed: %v\n", err)
			return 1
		}
	}
	fmt.Println("validation: OK")

	// Transform.
	canonical := toCanonicalEntry(candidate)
	if pilot {
		applyPilotScope(canonical, pilotRepo, *domainFlag, *sourceSetFlag)
	}

	if *dryRun {
		out, _ := yaml.Marshal(map[string]interface{}{listKey: []interface{}{canonical}})
		fmt.Println()
		fmt.Println("[dry-run] would append to", relTo(baseRepo, targetPath)+":")
		fmt.Println(string(out))
		return 0
	}

	// Append.
	if err := appendToCanonicalFile(targetPath, listKey, canonical); err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: %v\n", err)
		return 1
	}
	fmt.Printf("appended to %s\n", relTo(baseRepo, targetPath))

	// Remove from candidate file.
	if err := removeCandidateEntry(candidatePath, candidateID); err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: %v\n", err)
		return 1
	}
	fmt.Printf("removed %s from %s\n", candidateID, relTo(baseRepo, candidatePath))

	// Pilot: load ONLY the pilot graph into the running store, additively.
	// Never rebuild the embedded seed — foreign-repo rules must not ride in
	// Globular's shipped binary. The scope filter keeps them isolated at serve
	// time; this keeps them isolated at build time.
	if pilot {
		if *noRebuild {
			fmt.Printf("\nnext step: load pilot graph — sensei build --input %s, then POST to Oxigraph\n", relTo(baseRepo, awarenessDir))
			return 0
		}
		fmt.Println("\nLoading pilot graph into Oxigraph (additive; embedded seed untouched)...")
		if err := loadPilotGraph(awarenessDir, agRepo, svcRepo, *oxigraphURLFlag); err != nil {
			fmt.Fprintf(os.Stderr, "sensei promote: pilot graph load failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "  the YAML is promoted; re-run the load once Oxigraph is reachable.\n")
			return 1
		}
		fmt.Printf("pilot graph loaded for domain %q\n", pilotRepo)
		return 0
	}

	// Rebuild (home domain).
	if *noRebuild {
		fmt.Println("\nnext step: sensei rebuild")
		return 0
	}
	if err := ensureCrossRepoRebuildPrereqs(agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "sensei promote: %v\n", err)
		return 1
	}
	fmt.Println("\nTriggering rebuild...")
	var rebuildArgs []string
	if svcRepo != "" {
		rebuildArgs = append(rebuildArgs, "--services-repo", svcRepo)
	}
	if agRepo != "" {
		rebuildArgs = append(rebuildArgs, "--ag-repo", agRepo)
	}
	if rc := runRebuild(rebuildArgs); rc != 0 {
		return rc
	}
	// WB-1: promotion -> rebuild -> checks is one automatic action. Fire the
	// coherence gate so the promoter gets the same verdict sensei learn gives,
	// without having to remember to run validate/audit separately.
	if *noCheck {
		fmt.Println("\ncoherence checks skipped (--no-check); run `sensei learn --check` before committing.")
		return 0
	}
	return runPromotionChecks(svcRepo, agRepo)
}

// runPromotionChecks fires the coherence gate after a promotion's rebuild so
// "promotion -> rebuild -> checks" is one automatic action (WB-1): validate
// (dangling refs / dup ids / missing sources) then audit -check (freshness +
// coherence, incl. the seed-orphans gate). Mirrors the sensei learn harness so a
// promoter gets the same fail-closed verdict without remembering separate
// commands. Returns the first non-zero check code, or 0 when coherent.
func runPromotionChecks(svcRepo, agRepo string) int {
	fmt.Println("\nValidating corpus (refs / ids / sources)...")
	var valArgs []string
	if svcRepo != "" {
		valArgs = append(valArgs, "-repo-root", svcRepo)
	}
	if agRepo != "" {
		valArgs = append(valArgs, "-ag-repo", agRepo)
	}
	if rc := runValidate(valArgs); rc != 0 {
		fmt.Fprintln(os.Stderr, "sensei promote: corpus validation failed — resolve references/ids before the rule is enforced.")
		return rc
	}

	fmt.Println("\nAuditing (freshness + coherence)...")
	var auditArgs []string
	if svcRepo != "" {
		auditArgs = append(auditArgs, "-services-repo", svcRepo)
	}
	if agRepo != "" {
		auditArgs = append(auditArgs, "-ag-repo", agRepo)
	}
	auditArgs = append(auditArgs, "-check")
	if rc := runAudit(auditArgs); rc != 0 {
		fmt.Fprintln(os.Stderr, "sensei promote: audit failed — seed stale or incoherent after rebuild.")
		return rc
	}

	fmt.Println("\nsensei promote: promoted, rebuilt, validated, audited — the rule is coherent and ready to commit.")
	return 0
}

// pilotSlug derives a filesystem-safe directory name from a repo domain. The
// last path segment is used for readability ("github.com/caddyserver/caddy" →
// "caddy"); if that is empty the full repo is slugified.
func pilotSlug(repo string) string {
	repo = strings.TrimRight(strings.TrimSpace(repo), "/")
	if repo == "" {
		return "repo"
	}
	if i := strings.LastIndex(repo, "/"); i >= 0 && i < len(repo)-1 {
		return sanitizeSlug(repo[i+1:])
	}
	return sanitizeSlug(repo)
}

func sanitizeSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "repo"
	}
	return b.String()
}

// validatePilotScope enforces the pilot promotion contract: a foreign rule must
// carry its repo domain and the provenance receipt that earned it. Promotion is
// the gate where a foreign rule becomes traceable — refuse to mint an
// untraceable one.
func validatePilotScope(candidate map[string]interface{}, repoFlag string) error {
	repo := strFieldVal(candidate, "repo")
	if repo != "" && repo != strings.TrimSpace(repoFlag) {
		return fmt.Errorf("candidate repo %q does not match --repo %q", repo, repoFlag)
	}
	domain := strings.ToLower(strFieldVal(candidate, "domain"))
	if domain == rdfDomainShared {
		return fmt.Errorf("a --repo pilot promotion must be domain=repo, not shared (shared meta-principles are promoted on the home path)")
	}
	prov, ok := candidate["provenance"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("pilot promotion requires a provenance block (bundle_id, commit_range, citations, review_label)")
	}
	if strings.TrimSpace(toStr(prov["bundle_id"])) == "" {
		return fmt.Errorf("provenance.bundle_id is required for a pilot promotion")
	}
	if strings.TrimSpace(toStr(prov["commit_range"])) == "" {
		return fmt.Errorf("provenance.commit_range is required for a pilot promotion")
	}
	cits, _ := prov["citations"].([]interface{})
	if len(cits) == 0 {
		return fmt.Errorf("provenance.citations must list at least one supporting reference")
	}
	if strings.TrimSpace(toStr(prov["review_label"])) == "" {
		return fmt.Errorf("provenance.review_label is required (the human label assigned at review)")
	}
	return nil
}

// rdfDomainShared mirrors rdf.DomainShared without importing the rdf package
// into the CLI command layer.
const rdfDomainShared = "shared"

// applyPilotScope stamps the canonical entry with the resolved repo domain.
// Flags are authoritative for routing (repo always; domain/source_set when
// given); provenance/origin were already copied verbatim from the candidate by
// toCanonicalEntry.
func applyPilotScope(entry map[string]interface{}, repo, domainFlag, sourceSetFlag string) {
	entry["repo"] = strings.TrimSpace(repo)
	domain := strings.ToLower(strings.TrimSpace(domainFlag))
	if domain == "" {
		if d := strings.ToLower(strFieldVal(entry, "domain")); d != "" {
			domain = d
		} else {
			domain = "repo"
		}
	}
	entry["domain"] = domain
	if ss := strings.TrimSpace(sourceSetFlag); ss != "" {
		entry["source_set"] = ss
	} else if strFieldVal(entry, "source_set") == "" {
		entry["source_set"] = "pilot/" + pilotSlug(repo)
	}
}

func toStr(v interface{}) string {
	s, _ := v.(string)
	return s
}

// loadPilotGraph compiles the pilot directory to N-Triples and ADDITIVELY loads
// it into the running Oxigraph store (Graph Store Protocol POST = merge). It
// never writes embeddata/awareness.nt, so the embedded seed shipped in Globular
// stays free of foreign-repo rules. The importer skips the candidates/ subtree,
// so only promoted (domain-tagged) entries are loaded.
func loadPilotGraph(pilotDir, agRepo, svcRepo, oxigraphURL string) error {
	var buf bytes.Buffer
	opts := extractor.ImportDirOptions{StripPathPrefixes: []string{agRepo, svcRepo}}
	emitter, _, err := extractor.ImportAwarenessDirWithOpts(pilotDir, &buf, opts)
	if err != nil {
		return fmt.Errorf("import pilot dir: %w", err)
	}
	nt, unique, _ := extractor.DedupNTriples(buf.Bytes())
	if errs := extractor.ValidateNTriples(bytes.NewReader(nt)); len(errs) > 0 {
		return fmt.Errorf("pilot N-Triples invalid: %d errors (first: %s)", len(errs), errs[0])
	}
	fmt.Printf("  pilot triples: %d (%d unique)\n", emitter.Triples, unique)
	return reloadOxigraphStore(nt, oxigraphURL)
}

func findCandidateEntry(dir, id string) (string, map[string]interface{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, fmt.Errorf("cannot read candidates dir: %w", err)
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			continue
		}
		candidates, ok := doc["candidates"].([]interface{})
		if !ok {
			continue
		}
		for _, c := range candidates {
			m, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if cid, _ := m["id"].(string); cid == id {
				return path, m, nil
			}
		}
	}
	return "", nil, fmt.Errorf("candidate %q not found in %s", id, dir)
}

func validateCandidateEntry(candidate map[string]interface{}, targetFilename, awarenessDir string) error {
	id, _ := candidate["id"].(string)
	if !canonicalIDPattern.MatchString(id) {
		return fmt.Errorf("id %q does not match canonical naming", id)
	}
	expectedClass := promoteTargetToClass[targetFilename]
	candidateClass, _ := candidate["class"].(string)
	if candidateClass != expectedClass {
		return fmt.Errorf("class mismatch: %q vs expected %q", candidateClass, expectedClass)
	}
	// A meta-principle is dual-typed via its id: only a meta.* id is typed
	// MetaPrinciple by the importer, so reject anything else landing in the
	// portable pack as an invariant masquerading as a principle.
	if candidateClass == "meta_principle" && !strings.HasPrefix(id, "meta.") {
		return fmt.Errorf("meta_principle id %q must start with 'meta.' (dual-typing convention)", id)
	}
	if status, _ := candidate["status"].(string); status != "candidate" {
		return fmt.Errorf("status=%q, expected 'candidate'", status)
	}
	if confidence, _ := candidate["confidence"].(string); confidence == "low" {
		return fmt.Errorf("confidence=low — gather more evidence")
	}
	if evidence, _ := candidate["evidence"].(string); strings.TrimSpace(evidence) == "" {
		return fmt.Errorf("evidence is empty")
	}
	if df, _ := candidate["discovered_from"].(string); strings.TrimSpace(df) == "" {
		return fmt.Errorf("discovered_from is empty")
	}
	// Check for duplicates.
	existing := allCanonicalIDs(awarenessDir)
	if existing[id] {
		return fmt.Errorf("duplicate: %q already exists", id)
	}
	return nil
}

func allCanonicalIDs(dir string) map[string]bool {
	ids := make(map[string]bool)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ids
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			continue
		}
		for _, key := range []string{"invariants", "failure_modes", "incident_patterns", "intents", "forbidden_fixes", "required_tests"} {
			entries, ok := doc[key].([]interface{})
			if !ok {
				continue
			}
			for _, e := range entries {
				m, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				if id, ok := m["id"].(string); ok {
					ids[id] = true
				}
			}
		}
	}
	return ids
}

func resolvePromoteTarget(explicit string, candidate map[string]interface{}) (string, error) {
	if explicit != "" {
		// A target may be a bare filename or a subpath (meta-principles live at
		// generic/state_authority_invariants.yaml) — accept either spelling.
		if _, ok := promoteTargetToListKey[explicit]; ok {
			return explicit, nil
		}
		base := filepath.Base(explicit)
		if _, ok := promoteTargetToListKey[base]; ok {
			return base, nil
		}
		return "", fmt.Errorf("unknown target %q", explicit)
	}
	class, _ := candidate["class"].(string)
	target, ok := promoteClassToTarget[class]
	if !ok {
		return "", fmt.Errorf("unknown class %q; use --target", class)
	}
	return target, nil
}

func toCanonicalEntry(candidate map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{
		"id":       candidate["id"],
		"title":    strFieldVal(candidate, "label"),
		"severity": strFieldVal(candidate, "risk"),
		"status":   "active",
	}
	if entry["severity"] == "" {
		entry["severity"] = "medium"
	}
	for _, key := range []string{
		"summary", "protects", "symptoms", "root_cause", "architecture_fix",
		"forbidden_fixes", "related_invariants", "related_services",
		"required_tests", "failure_mode", "lesson", "edit_shapes",
		"wrong_fixes", "files", "related_symbols", "enforcement",
		// Domain scope — present only on repo-scoped (pilot) candidates; absent
		// on home-domain candidates, so this copies nothing for the existing
		// path. NOTE: "provenance" is intentionally NOT copied here — it is
		// merged below so the candidate's evidence (bundle_id, commit_range,
		// citations, review_label) is preserved AND annotated with the
		// promotion metadata, rather than overwritten by it.
		"repo", "domain", "source_set", "origin",
	} {
		if v, ok := candidate[key]; ok {
			entry[key] = v
		}
	}
	if prt := strFieldVal(candidate, "proposed_required_test"); prt != "" {
		entry["proposed_required_test"] = prt
	}
	// Merge provenance: preserve the candidate's evidence receipt (bundle_id,
	// commit_range, citations, review_label) and annotate it with the promotion
	// metadata. Overwriting it here would strip exactly the chain of custody the
	// pilot is meant to carry.
	prov := map[string]interface{}{}
	if existing, ok := candidate["provenance"].(map[string]interface{}); ok {
		for k, v := range existing {
			prov[k] = v
		}
	}
	prov["promoted_from"] = "candidate"
	if df := strFieldVal(candidate, "discovered_from"); df != "" {
		prov["discovered_from"] = df
	}
	if c := strFieldVal(candidate, "confidence"); c != "" {
		prov["confidence_at_promotion"] = c
	}
	entry["provenance"] = prov
	return entry
}

func strFieldVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func appendToCanonicalFile(targetPath, listKey string, newEntry map[string]interface{}) error {
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", targetPath, err)
		}
		// First promotion into a new (e.g. pilot) canonical file — start empty
		// and create the parent directory if needed.
		raw = nil
		if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0o755); mkErr != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(targetPath), mkErr)
		}
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		doc = make(map[string]interface{})
	}
	if doc == nil {
		doc = make(map[string]interface{})
	}
	entries, _ := doc[listKey].([]interface{})
	entries = append(entries, newEntry)
	sort.SliceStable(entries, func(i, j int) bool {
		a, _ := entries[i].(map[string]interface{})
		b, _ := entries[j].(map[string]interface{})
		ai, _ := a["id"].(string)
		bi, _ := b["id"].(string)
		return ai < bi
	})
	doc[listKey] = entries
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, out, 0o644)
}

func removeCandidateEntry(path, id string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	candidates, _ := doc["candidates"].([]interface{})
	var remaining []interface{}
	for _, c := range candidates {
		m, ok := c.(map[string]interface{})
		if !ok {
			remaining = append(remaining, c)
			continue
		}
		if cid, _ := m["id"].(string); cid != id {
			remaining = append(remaining, c)
		}
	}
	doc["candidates"] = remaining
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
