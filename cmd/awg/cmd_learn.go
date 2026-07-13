// SPDX-License-Identifier: AGPL-3.0-only

package main

// cmd_learn.go — `sensei learn`: the single governed write-back command.
//
// A scar/principle is only "learned" when it has been authored, deterministically
// regenerated, validated against source, embedded, and proven coherent — never when
// it is merely remembered. This command performs the whole write-back path so an
// agent (or human) does not rely on remembering five separate commands:
//
//   rebuild embeddata (deterministic, no Oxigraph dependency)
//     -> validate corpus (dangling refs / dup ids / missing sources)
//       -> audit -check (freshness + coherence)
//         -> stage the regenerated artifact
//
// It refuses to report success if any step fails, and it reports the directive's
// explicit result states rather than false certainty:
//
//   enforced_clean            rebuilt + validated + audited; embeddata byte-stable
//   drift_detected            embeddata stale after a real rebuild (nondeterminism)
//   blocked_by_dangling_refs  validation failed; refs must resolve before enforcement
//   blocked_by_rebuild        generation itself failed
//
// Idempotent: running it twice with no source changes produces no diff (rebuild only
// writes the seed when its content hash changes). That is the property that lets the
// freshness gate be authoritative — see `sensei audit -check`.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func runLearn(args []string) int {
	fs := flag.NewFlagSet("sensei learn", flag.ContinueOnError)
	svcRepo := fs.String("services-repo", "", "path to services repo (auto-detected if empty)")
	agRepo := fs.String("ag-repo", "", "path to awareness-graph repo (defaults to current dir for staging)")
	stage := fs.Bool("stage", false, "git add the regenerated embeddata after a clean rebuild")
	checkOnly := fs.Bool("check", false, "verify only: compare/validate/audit, never write the seed (CI/preflight)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: sensei learn [--services-repo P] [--ag-repo P] [--stage] [--check]")
		fmt.Fprintln(os.Stderr, "Governed write-back: rebuild -> validate -> audit -> stage. Refuses success on any failure.")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	pass := func(svc, ag bool) []string {
		var a []string
		if svc && *svcRepo != "" {
			a = append(a, "-services-repo", *svcRepo)
		}
		if ag && *agRepo != "" {
			a = append(a, "-ag-repo", *agRepo)
		}
		return a
	}

	fmt.Println("sensei learn — governed write-back (rebuild -> validate -> audit -> stage)")

	// 1. Rebuild embeddata. -no-runtime-reload keeps it headless-safe (no Oxigraph
	//    dependency); --check makes it compare-only (no write).
	fmt.Println("\n[1/4] rebuild embeddata (deterministic)...")
	rebuildArgs := append([]string{"-no-runtime-reload", "--combined"}, pass(true, true)...)
	if *checkOnly {
		rebuildArgs = append(rebuildArgs, "-check")
	}
	if rc := runRebuild(rebuildArgs); rc != 0 {
		fmt.Fprintln(os.Stderr, "\nsensei learn: blocked_by_rebuild — generation failed; the rule cannot be embedded.")
		return rc
	}

	// 2. Validate: dangling related refs, duplicate ids, missing source files.
	//    validate uses -repo-root (not -services-repo) for the services corpus.
	fmt.Println("\n[2/4] validate corpus (refs / ids / sources)...")
	valArgs := []string{}
	if *svcRepo != "" {
		valArgs = append(valArgs, "-repo-root", *svcRepo)
	}
	if *agRepo != "" {
		valArgs = append(valArgs, "-ag-repo", *agRepo)
	}
	if rc := runValidate(valArgs); rc != 0 {
		fmt.Fprintln(os.Stderr, "\nsensei learn: blocked_by_dangling_refs — fix references before the rule can be enforced.")
		return rc
	}

	// 3. Audit -check: freshness + coherence. After a real rebuild this MUST be clean;
	//    if it is not, the generator is nondeterministic or the seed is stale.
	fmt.Println("\n[3/4] audit (freshness + coherence)...")
	auditArgs := append([]string{"-check"}, pass(true, true)...)
	if rc := runAudit(auditArgs); rc != 0 {
		fmt.Fprintln(os.Stderr, "\nsensei learn: drift_detected — embeddata stale after rebuild (nondeterministic generator?). Refusing to report 'learned'.")
		return rc
	}

	// 4. Stage / report.
	if *checkOnly {
		fmt.Println("\nsensei learn: advisory_clean — corpus is fresh, valid, and coherent (--check: no writes).")
		return 0
	}
	if *stage {
		fmt.Println("\n[4/4] stage regenerated embeddata...")
		ag := *agRepo
		if ag == "" {
			ag = "."
		}
		seed := filepath.Join("golang", "server", "embeddata", "awareness.nt")
		cmd := exec.Command("git", "-C", ag, "add", "--", seed)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "sensei learn: staged rebuild OK but `git add %s` failed: %v\n", seed, err)
			fmt.Fprintln(os.Stderr, "  stage it manually, then commit. (The authored source YAML in the services repo must be committed there.)")
			return 1
		}
		fmt.Printf("  staged %s\n", seed)
		fmt.Println("  NOTE: commit the authored source YAML (services/docs/awareness/*) in the services repo too —")
		fmt.Println("        the seed and its source must land together.")
	} else {
		fmt.Println("\n[4/4] staging skipped (pass --stage to git add the embeddata).")
	}

	fmt.Println("\nsensei learn: enforced_clean — rebuilt, validated, audited; embeddata is byte-stable.")
	fmt.Println("  A new/changed rule is now generated + embedded + validated. It becomes ENFORCED")
	fmt.Println("  once the awg-audit freshness gate runs in CI (hard gate) on the committed seed.")
	return 0
}
