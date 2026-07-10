// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.meta_principle_coverage
// @awareness file_role=coverage_attestation_for_the_meta_principle_set

// Coverage attestation for the meta-principle SET — declare-then-conform applied
// one level up, to the principles themselves.
//
// Every meta.* principle must have a known ENFORCEMENT TIER. `code_scanner`
// remains AUTO-DERIVED from an un-fakeable local fact: the principle has a
// scanner gated in principle-check-all (parsed from the Makefile +
// related_invariants links). All other tiers are declared in
// docs/awareness-control/meta_principle_coverage.yaml, including `declaration`
// when the validating artifact lives in another repo. That keeps principle
// ownership in awareness-graph while cross-repo gates continue to validate
// integration.
//
// The gate FAILS if any meta.* is neither auto-derived NOR listed in the
// registry: a new principle cannot land unclassified. That is
// meta.negative_result_requires_coverage_attestation applied to the principle
// set — the coverage map maintains itself instead of requiring a code roundtrip
// to discover gaps.
//
// Run `go test ... -run TestMetaPrincipleCoverage -v` to print the full map.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// coverageDefect is a self-coherence violation within a single coverage file:
// a principle declared more than once.
type coverageDefect struct {
	Principle string
	Kind      string // "duplicate" (same tier) | "conflict" (different tier)
	Detail    string
}

// detectCoverageSelfDefects finds principles declared more than once in one
// coverage registry. The registry is consumed elsewhere as a map, so a second
// entry silently OVERWRITES the first — a duplicate, or worse a conflicting
// tier, vanishes without a trace (this is how a drifted duplicate block once
// hid 12 conflicting classifications). This catches it before the map swallows
// it. Defects are returned in input order for deterministic output.
func detectCoverageSelfDefects(entries []coverageEntry) []coverageDefect {
	seen := map[string]coverageEntry{}
	var out []coverageDefect
	for _, e := range entries {
		if e.Principle == "" {
			continue
		}
		if prev, dup := seen[e.Principle]; dup {
			if prev.Tier != e.Tier || prev.IntendedTier != e.IntendedTier {
				out = append(out, coverageDefect{e.Principle, "conflict",
					fmt.Sprintf("%s/intended=%q vs %s/intended=%q", prev.Tier, prev.IntendedTier, e.Tier, e.IntendedTier)})
			} else {
				out = append(out, coverageDefect{e.Principle, "duplicate",
					fmt.Sprintf("declared twice with tier %s", e.Tier)})
			}
		}
		seen[e.Principle] = e
	}
	return out
}

type coverageEntry struct {
	Principle    string   `yaml:"principle"`
	Tier         string   `yaml:"tier"`
	Reason       string   `yaml:"reason"`
	IntendedTier string   `yaml:"intended_tier"`
	Tests        []string `yaml:"tests"`
}

type coverageRegistry struct {
	Coverage []coverageEntry `yaml:"meta_principle_coverage"`
	Ratchet  struct {
		// MaxReviewOnly caps the terminal (review_only) tier so the unenforced
		// pile cannot grow silently. 0 (unset) disables the ratchet.
		MaxReviewOnly int `yaml:"max_review_only"`
	} `yaml:"enforcement_ratchet"`
}

var validResidualTiers = map[string]bool{
	"behavioral":  true,
	"declaration": true,
	"planned":     true,
	"review_only": true,
}

func metaPrincipleCoveragePath() string {
	return filepath.Join(agRepoRoot(), "docs", "awareness-control", "meta_principle_coverage.yaml")
}

// walkYAML invokes fn on every map node in a parsed YAML tree.
func walkYAML(node interface{}, fn func(map[string]interface{})) {
	switch v := node.(type) {
	case map[string]interface{}:
		fn(v)
		for _, child := range v {
			walkYAML(child, fn)
		}
	case []interface{}:
		for _, child := range v {
			walkYAML(child, fn)
		}
	}
}

func loadAwarenessYAMLs(t *testing.T, dir string) (metas map[string]bool, instToMeta map[string]string) {
	metas = map[string]bool{}
	instToMeta = map[string]string{}
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no awareness YAMLs under %s: %v", dir, err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var doc interface{}
		if yaml.Unmarshal(raw, &doc) != nil {
			continue
		}
		walkYAML(doc, func(m map[string]interface{}) {
			id, _ := m["id"].(string)
			if id == "" {
				return
			}
			if strings.HasPrefix(id, "meta.") {
				if _, hasStatus := m["status"]; hasStatus {
					metas[id] = true
				}
			}
			if rel, ok := m["related_invariants"].([]interface{}); ok {
				for _, r := range rel {
					if rs, ok := r.(string); ok && strings.HasPrefix(rs, "meta.") {
						if _, seen := instToMeta[id]; !seen {
							instToMeta[id] = rs
						}
						break
					}
				}
			}
		})
	}
	return
}

// gatedInstancesFromMakefile returns the principle ids wired into
// principle-check-all: the RULEGUARD_INSTANCES list plus every `-principle X`
// regex target.
func gatedInstancesFromMakefile(t *testing.T) []string {
	raw, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	mk := string(raw)
	set := map[string]bool{}
	for _, m := range regexp.MustCompile(`-principle (\S+)`).FindAllStringSubmatch(mk, -1) {
		id := m[1]
		if strings.HasPrefix(id, "$") || id == "proof" {
			continue
		}
		set[id] = true
	}
	if blk := regexp.MustCompile(`(?s)RULEGUARD_INSTANCES :=(.*?)\n\n`).FindStringSubmatch(mk); blk != nil {
		for _, tok := range regexp.MustCompile(`[A-Za-z0-9_.]+`).FindAllString(blk[1], -1) {
			if strings.Contains(tok, "_") || strings.Contains(tok, ".") {
				set[tok] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func TestMetaPrincipleCoverage(t *testing.T) {
	// The generic meta.* corpus lives in awareness-graph. The coverage registry is
	// AWG-owned too. If a services checkout is available, merge its instance →
	// parent links so local ruleguard instances can still inherit richer
	// cross-repo context, but the ownership gate itself must not require that
	// checkout to exist.
	metas, instToMeta := loadAwarenessYAMLs(t, filepath.Join(agRepoRoot(), "docs", "awareness", "generic"))
	if repo := servicesRepoRoot(); repo != "" {
		if fi, err := os.Stat(repo); err == nil && fi.IsDir() {
			svcMetas, svcInst := loadAwarenessYAMLs(t, filepath.Join(repo, "docs", "awareness"))
			for k := range svcMetas {
				metas[k] = true
			}
			for k, v := range svcInst {
				if _, ok := instToMeta[k]; !ok {
					instToMeta[k] = v
				}
			}
		}
	}
	if len(metas) == 0 {
		t.Fatal("found no meta.* principles")
	}

	// AUTO tier 1: code_scanner — gated instance -> parent meta.
	codeScanner := map[string]bool{}
	for _, inst := range gatedInstancesFromMakefile(t) {
		if strings.HasPrefix(inst, "meta.") {
			codeScanner[inst] = true
		} else if mp, ok := instToMeta[inst]; ok {
			codeScanner[mp] = true
		} else {
			t.Logf("note: gated instance %q has no meta parent via related_invariants", inst)
		}
	}

	// Registry: the residual classifications.
	registry := map[string]coverageEntry{}
	raw, err := os.ReadFile(metaPrincipleCoveragePath())
	if err != nil {
		t.Fatalf("read meta_principle_coverage.yaml: %v", err)
	}
	var reg coverageRegistry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("parse meta_principle_coverage.yaml: %v", err)
	}
	for _, e := range reg.Coverage {
		if e.Principle == "" {
			t.Errorf("a coverage entry is missing `principle:`")
			continue
		}
		if !validResidualTiers[e.Tier] {
			t.Errorf("%s: tier %q is not a valid tier (behavioral|declaration|planned|review_only). "+
				"code_scanner remains auto-derived; every other tier is declared here.", e.Principle, e.Tier)
		}
		if strings.TrimSpace(e.Reason) == "" {
			t.Errorf("%s: tier %q requires a reason", e.Principle, e.Tier)
		}
		if e.Tier == "behavioral" && len(e.Tests) == 0 {
			t.Errorf("%s: behavioral tier must cite the gating test(s)", e.Principle)
		}
		if e.Tier == "planned" && e.IntendedTier == "" {
			t.Errorf("%s: planned tier must name an `intended_tier`", e.Principle)
		}
		registry[e.Principle] = e
	}

	// SELF-COHERENCE: a principle may be declared exactly once. Detected via the
	// shared helper (positive-controlled by TestMetaPrincipleCoverageSelfDefectDetectorIsLive)
	// so the check cannot silently die and report a dirty file as clean.
	for _, d := range detectCoverageSelfDefects(reg.Coverage) {
		if d.Kind == "conflict" {
			t.Errorf("CONFLICTING TIER: %s is declared twice with different classifications (%s). "+
				"A principle must have exactly one tier; the map keeps last-wins, hiding the conflict. "+
				"Remove the drifted duplicate (de-dup against the authoritative entry — do not guess).",
				d.Principle, d.Detail)
		} else {
			t.Errorf("DUPLICATE: %s is declared more than once in meta_principle_coverage.yaml (%s). "+
				"Remove the redundant entry.", d.Principle, d.Detail)
		}
	}

	// COMPLETENESS: every meta.* must have a known tier.
	tierOf := map[string]string{}
	var unclassified []string
	for p := range metas {
		switch {
		case codeScanner[p]:
			tierOf[p] = "code_scanner"
		case registry[p].Principle != "":
			tierOf[p] = registry[p].Tier
		default:
			unclassified = append(unclassified, p)
		}
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Errorf("%d meta.* principle(s) have NO enforcement tier — neither auto-derived "+
			"(code_scanner) nor classified in meta_principle_coverage.yaml:\n  %s\n"+
			"A principle cannot land unclassified. Add a registry entry (behavioral/declaration/planned/review_only) "+
			"naming how it is — or is not yet — protected (meta.negative_result_requires_coverage_attestation).",
			len(unclassified), strings.Join(unclassified, "\n  "))
	}

	// Registry entries for principles that are now auto-covered are stale (redundant).
	for p, e := range registry {
		if !metas[p] {
			t.Errorf("meta_principle_coverage.yaml lists %q which is not a known meta.* principle (typo or removed)", p)
		}
		if codeScanner[p] {
			t.Logf("note: %s is now auto-covered (code_scanner); its registry entry (%s) is redundant and can be removed",
				p, e.Tier)
		}
	}

	// REPORT.
	counts := map[string]int{}
	for _, tier := range tierOf {
		counts[tier]++
	}
	t.Logf("meta-principle coverage map (%d principles):", len(metas))
	for _, tier := range []string{"code_scanner", "declaration", "behavioral", "planned", "review_only"} {
		t.Logf("  %-12s %d", tier, counts[tier])
	}
	mechanized := counts["code_scanner"] + counts["declaration"] + counts["behavioral"]
	total := len(metas)
	reviewOnly := counts["review_only"]
	t.Logf("  mechanically enforced: %d/%d (%d%%) | planned (tracked gap): %d | review-only (terminal): %d (%d%%)",
		mechanized, total, pct(mechanized, total), counts["planned"], reviewOnly, pct(reviewOnly, total))

	// GATE-TO-PRINCIPLE RATCHET. AWG's value is mechanical enforcement, not
	// declared advice; the failure mode is the unenforced pile growing
	// silently. The ceiling (enforcement_ratchet.max_review_only in
	// meta_principle_coverage.yaml) freezes the terminal tier — growing it
	// requires a conscious bump in the same commit, the moment to ask
	// "should this be mechanized instead?".
	ceiling := reg.Ratchet.MaxReviewOnly
	switch {
	case ceiling <= 0:
		t.Errorf("enforcement_ratchet.max_review_only is unset in meta_principle_coverage.yaml — "+
			"the keep-it-honest ratchet is disabled. Set it to the current count (%d).", reviewOnly)
	case reviewOnly > ceiling:
		t.Errorf("RATCHET TRIPPED: review_only is %d, ceiling is %d.\n"+
			"  The unenforced (review_only) pile grew. Either:\n"+
			"    (a) MECHANIZE one — convert it to behavioral (cite a gating test) or a\n"+
			"        code_scanner — which is the point of the principle existing; or\n"+
			"    (b) if it is genuinely terminal (no artifact can prove it), bump\n"+
			"        enforcement_ratchet.max_review_only to %d in the SAME commit.\n"+
			"  Bumping is allowed — but it is a conscious, reviewable act, not a default.",
			reviewOnly, ceiling, reviewOnly)
	case reviewOnly < ceiling:
		t.Logf("ratchet: review_only is %d, below the ceiling of %d — tighten it "+
			"(lower max_review_only to %d) to lock in the gain.", reviewOnly, ceiling, reviewOnly)
	default:
		t.Logf("ratchet: review_only at ceiling (%d) — held.", ceiling)
	}
	if reviewOnly*2 > total {
		t.Logf("NOTE: review-only is the majority (%d/%d). That is honest today — the GUI "+
			"categories (perception/composition/structure) await a frontend codebase to "+
			"enforce against — but the ratio only improves by mechanizing, not by adding. "+
			"Watch it.", reviewOnly, total)
	}
}

// pct returns n/d as an integer percent, 0 when d == 0.
func pct(n, d int) int {
	if d == 0 {
		return 0
	}
	return n * 100 / d
}

// TestMetaPrincipleCoverageSelfDefectDetectorIsLive is the positive control for the
// self-coherence detector: on a synthetic registry it MUST flag both a plain
// duplicate and a conflicting tier. Without this, a refactor could silently
// neuter detectCoverageSelfDefects and every coverage gate would report a dirty
// file as clean — exactly the dead-scanner failure
// meta.negative_result_requires_coverage_attestation forbids.
func TestMetaPrincipleCoverageSelfDefectDetectorIsLive(t *testing.T) {
	defects := detectCoverageSelfDefects([]coverageEntry{
		{Principle: "meta.x", Tier: "behavioral"},
		{Principle: "meta.x", Tier: "behavioral"}, // duplicate (same tier)
		{Principle: "meta.y", Tier: "planned", IntendedTier: "behavioral"},
		{Principle: "meta.y", Tier: "review_only"}, // conflict (different tier)
	})
	var sawDup, sawConflict bool
	for _, d := range defects {
		switch d.Kind {
		case "duplicate":
			sawDup = true
		case "conflict":
			sawConflict = true
		}
	}
	if !sawDup || !sawConflict {
		t.Fatalf("self-coherence detector is DEAD: expected a duplicate and a conflict, got %+v", defects)
	}
}

// parseCoverageFile reads and unmarshals a meta_principle_coverage.yaml.
func parseCoverageFile(t *testing.T, path string) coverageRegistry {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var reg coverageRegistry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return reg
}

// TestMetaPrincipleCoverageMirrorCoherence enforces the single-source rule for
// the coverage registry. The canonical registry is AWG-owned and lives at
// docs/awareness-control/meta_principle_coverage.yaml; the services repo carries
// a human-facing MIRROR at docs/awareness/meta_principle_coverage.yaml. Nothing
// programmatic reads the mirror — TestMetaPrincipleCoverage enforces ONLY the
// canonical copy — so the mirror can drift silently. It did: the two copies
// diverged in BOTH directions (conflicting tiers AND a stale review_only
// ceiling), the exact "registries drifting in opposite directions" defect that
// detection caught after the fact but no gate prevented.
//
// Rules follow the established cross-repo ownership model (the embeddata
// freshness gate tolerates services lag behind the AWG-owned source). The two
// repos merge independently, so the mirror is routinely BEHIND canonical between
// an AG change and the services resync PR; a hard gate on every difference would
// couple AG's CI to services' merge timing and deadlock. So:
//   - MIRROR-ONLY (the mirror declares a principle canonical does NOT own):
//     HARD FAIL — the only direction that cannot be "just lag"; the mirror
//     cannot invent authority canonical lacks.
//   - within-file DUPLICATE / CONFLICTING TIER (a principle declared twice in
//     ONE file): HARD FAIL via detectCoverageSelfDefects — a real self-defect,
//     not cross-repo lag.
//   - tier differs / missing / ceiling differs (mirror trails canonical):
//     WARN, not fail — tolerated, reported (not silent), cleared by the services
//     resync PR. canonical is the sole authority the coverage test enforces;
//     the mirror is human-facing documentation.
//
// Resolution is always a resync toward the canonical oracle, never a tier guess.
func TestMetaPrincipleCoverageMirrorCoherence(t *testing.T) {
	svcRoot := requireServicesRepo(t) // skips when no services checkout (AG-only runs)
	mirrorPath := filepath.Join(svcRoot, "docs", "awareness", "meta_principle_coverage.yaml")
	if _, err := os.Stat(mirrorPath); err != nil {
		t.Skipf("no services coverage mirror at %q", mirrorPath)
	}
	canon := parseCoverageFile(t, metaPrincipleCoveragePath())
	mirror := parseCoverageFile(t, mirrorPath)

	type classification struct{ Tier, Intended string }
	index := func(reg coverageRegistry, who string) map[string]classification {
		for _, d := range detectCoverageSelfDefects(reg.Coverage) {
			t.Errorf("%s: %s entry for %s (%s)", who, strings.ToUpper(d.Kind), d.Principle, d.Detail)
		}
		m := map[string]classification{}
		for _, e := range reg.Coverage {
			if e.Principle == "" {
				continue
			}
			m[e.Principle] = classification{e.Tier, e.IntendedTier}
		}
		return m
	}
	canonM := index(canon, "canonical")
	mirrorM := index(mirror, "services-mirror")

	var tierLag, mirrorOnly, lag int
	for p, mt := range mirrorM {
		ct, ok := canonM[p]
		if !ok {
			mirrorOnly++
			t.Errorf("MIRROR-ONLY: services declares %s (%s) which the canonical registry does not own. "+
				"The mirror cannot invent a classification — add it to the canonical "+
				"awareness-control copy or remove it from the mirror.", p, mt.Tier)
			continue
		}
		if ct != mt {
			tierLag++
			t.Logf("LAG (tier): %s — canonical=%s(intended %q), services mirror=%s(intended %q); the mirror "+
				"trails canonical (the AWG-owned authority). Resync the mirror — tolerated, not a hard block.",
				p, ct.Tier, ct.Intended, mt.Tier, mt.Intended)
		}
	}
	for p, ct := range canonM {
		if _, ok := mirrorM[p]; !ok {
			lag++
			t.Logf("LAG (missing): canonical owns %s (%s) not yet mirrored into services; resync the mirror.",
				p, ct.Tier)
		}
	}
	if canon.Ratchet.MaxReviewOnly != mirror.Ratchet.MaxReviewOnly {
		t.Logf("LAG (ceiling): canonical max_review_only=%d, services mirror=%d; the mirror trails canonical. "+
			"Resync the mirror — tolerated, not a hard block.",
			canon.Ratchet.MaxReviewOnly, mirror.Ratchet.MaxReviewOnly)
	}
	if mirrorOnly == 0 && (tierLag > 0 || lag > 0) {
		t.Logf("mirror trails canonical: %d tier-lag, %d missing (resync via the services mirror PR).", tierLag, lag)
	}
	t.Logf("mirror coherence: %d mirror-only(HARD), %d tier-lag, %d missing (canonical=%d, mirror=%d entries)",
		mirrorOnly, tierLag, lag, len(canonM), len(mirrorM))
}
