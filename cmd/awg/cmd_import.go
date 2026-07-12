// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// runImport is the one-command foreign-repo onboarding wrapper. It composes the
// existing extractors in the ONE correct order so no caller has to remember it:
//
//	clone -> contract extraction (pristine!) -> structural -> [history] -> load
//
// The ordering is load-bearing: contract extraction runs on the pristine clone,
// BEFORE `bootstrap` scaffolds Sensei's own charter into the tree (guardrail 7
// of the sensei-import skill; also backstopped in the intent gatherer).
//
// It never promotes: every extractor writes candidates/intents for human review.
// It never touches a store unless an explicit --store-url is given.
func runImport(args []string) int {
	fs := flag.NewFlagSet("sensei import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	domain := fs.String("domain", "", "domain to tag the repo's nodes, e.g. github.com/gin-gonic/gin (default: derived from the URL)")
	depth := fs.String("depth", "full", "extraction depth: basic (structural only) | full (adds LLM contract extraction + optional history)")
	dir := fs.String("dir", "", "checkout destination for a URL (default: a temp dir); ignored when the target is an existing path")
	storeURL := fs.String("store-url", "", "load the slice into this store; when empty, print the exact build command instead of touching any store")
	markerFile := fs.String("graph-marker-file", "", "server's graph marker file; pass this with --store-url so a served store stays fresh for briefing")
	drafter := fs.String("drafter", "llm", "contract drafter for full depth: llm (needs ANTHROPIC_API_KEY) | echo (deterministic, shallow)")
	maxN := fs.Int("max", 12, "max contract candidates to propose (full depth)")
	repoSlug := fs.String("repo-slug", "", "owner/name for PR-review history mining (full depth; needs gh auth + full history)")
	dryRun := fs.Bool("dry-run", false, "print the plan and stop; run nothing")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei import <git-url | path> --domain <domain> [flags]

Onboard a foreign repository into Sensei in one command, in the correct order:
clone -> contract extraction (on the pristine clone) -> structural extraction ->
optional history mining -> (optionally) load the domain-scoped slice.

Never auto-promotes: extractors write candidates/intents for you to review and
promote yourself. Never touches a store unless --store-url is given.

Flags:
`)
		fs.PrintDefaults()
	}
	// Parse flags that may appear before OR after the positional target: Go's
	// flag package stops at the first non-flag arg, so pull positionals out and
	// keep parsing the remainder.
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	var positional []string
	for fs.NArg() > 0 {
		positional = append(positional, fs.Arg(0))
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "sensei import: exactly one <git-url | path> argument is required")
		fs.Usage()
		return 2
	}
	target := positional[0]

	full := false
	switch strings.ToLower(strings.TrimSpace(*depth)) {
	case "full":
		full = true
	case "basic":
		full = false
	default:
		fmt.Fprintf(os.Stderr, "sensei import: --depth must be 'basic' or 'full', got %q\n", *depth)
		return 2
	}

	dom := strings.TrimSpace(*domain)
	if dom == "" {
		dom = deriveDomain(target)
	}
	if dom == "" {
		fmt.Fprintln(os.Stderr, "sensei import: --domain is required (could not derive it from the target)")
		return 2
	}
	slug := strings.TrimSpace(*repoSlug)
	if slug == "" {
		slug = deriveSlug(dom)
	}

	// Resolve the checkout: an existing path is used in place; a URL is cloned.
	checkout, cloned, code := resolveImportCheckout(target, strings.TrimSpace(*dir), *dryRun)
	if code != 0 {
		return code
	}

	haveKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != ""
	wantContracts := full && (*drafter != "llm" || haveKey)
	skipContractReason := ""
	if full && *drafter == "llm" && !haveKey {
		skipContractReason = "ANTHROPIC_API_KEY not set — contract layer skipped (run again with a key, or --drafter echo)"
	}

	fmt.Fprintf(os.Stderr, "sensei import: %s\n  domain:   %s\n  checkout: %s\n  depth:    %s\n",
		target, dom, checkout, *depth)
	if *dryRun {
		fmt.Fprintln(os.Stderr, "  (dry-run: nothing executed)")
		printImportPlan(checkout, dom, slug, full, wantContracts, skipContractReason, *storeURL)
		return 0
	}

	// 1) Contracts FIRST — on the pristine clone, before bootstrap scaffolds.
	if wantContracts {
		fmt.Fprintln(os.Stderr, "\n== [1/4] contract extraction (pristine clone) ==")
		ia := []string{"--repo", checkout, "--sources", "docs,comments,tests",
			"--drafter", *drafter, "--max", strconv.Itoa(*maxN), "--apply"}
		if rc := runIntentMine(ia); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: contract extraction failed — continuing with structure only")
		}
	} else if skipContractReason != "" {
		fmt.Fprintf(os.Stderr, "\n== [1/4] contract extraction: %s ==\n", skipContractReason)
	}

	// 2) Structural extraction — now safe to scaffold the checkout.
	fmt.Fprintln(os.Stderr, "\n== [2/4] structural extraction ==")
	if rc := runBootstrap([]string{"--repo", checkout, "--skip-history", "--skip-build"}); rc != 0 {
		fmt.Fprintln(os.Stderr, "sensei import: structural extraction failed")
		return 1
	}

	// 3) History / PR mining — full depth only, best-effort.
	if full && slug != "" {
		fmt.Fprintln(os.Stderr, "\n== [3/4] day-0 history / PR mining ==")
		if rc := runColdBootstrap([]string{"--repo", checkout, "--repo-slug", slug, "--auto-window"}); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: history mining produced nothing usable (expected on quiet repos)")
		}
	} else if full {
		fmt.Fprintln(os.Stderr, "\n== [3/4] history mining skipped (no --repo-slug) ==")
	}

	// 4) Load the domain-scoped slice, or print the command to do it.
	awarenessDir := filepath.Join(checkout, "docs", "awareness")
	generatedDir := filepath.Join(awarenessDir, "generated")
	if strings.TrimSpace(*storeURL) == "" {
		fmt.Fprintln(os.Stderr, "\n== [4/4] load: no --store-url given; run this against your store ==")
		fmt.Fprintf(os.Stderr, "  sensei build --input %s --input %s --repo %s --store-url <url>\n",
			awarenessDir, generatedDir, dom)
		fmt.Fprintln(os.Stderr, "  (fresh store? seed once with `sensei build --all` first.)")
	} else {
		fmt.Fprintln(os.Stderr, "\n== [4/4] load domain-scoped slice ==")
		ba := []string{"--input", awarenessDir, "--input", generatedDir, "--repo", dom, "--store-url", *storeURL}
		// Forward ONLY the marker file, never a transaction file. The marker is
		// written before the transaction publish, so it keeps a served store fresh
		// for briefing. A foreign-only slice carries no seed marker, so its runtime
		// transaction cannot be certified — passing --graph-transaction-file would
		// turn that expected condition into a hard build failure; omitting it makes
		// it a soft skip (transaction=uncertified) while the load still succeeds.
		if m := strings.TrimSpace(*markerFile); m != "" {
			ba = append(ba, "--graph-marker-file", m)
		} else {
			fmt.Fprintln(os.Stderr, "  note: no --graph-marker-file given; a live/served store may report freshness-stale for briefing until re-certified")
		}
		if rc := runBuild(ba); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: load failed — a scoped --repo update needs a non-empty store; seed with `sensei build --all` first")
			return 1
		}
		fmt.Fprintln(os.Stderr, "  (foreign slice: runtime transaction is uncertified by design; the briefing still serves)")
	}

	printImportSummary(checkout, dom, cloned, wantContracts)
	return 0
}

// resolveImportCheckout returns the working-tree path to extract from. An
// existing directory is used in place; anything else is treated as a git URL and
// cloned. cloned reports whether a fresh clone was made.
func resolveImportCheckout(target, dir string, dryRun bool) (checkout string, cloned bool, code int) {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		abs, aerr := filepath.Abs(target)
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "sensei import: %v\n", aerr)
			return "", false, 1
		}
		return abs, false, 0
	}
	dest := dir
	if dest == "" {
		dest = filepath.Join(os.TempDir(), "sensei-import-"+sanitizeName(deriveRepoBase(target)))
	}
	if dryRun {
		return dest, true, 0
	}
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(os.Stderr, "sensei import: checkout dir %s already exists; using it in place\n", dest)
		abs, _ := filepath.Abs(dest)
		return abs, false, 0
	}
	fmt.Fprintf(os.Stderr, "sensei import: cloning %s -> %s\n", target, dest)
	cmd := exec.Command("git", "clone", "--depth", "1", target, dest)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sensei import: git clone failed: %v\n", err)
		return "", false, 1
	}
	abs, _ := filepath.Abs(dest)
	return abs, true, 0
}

// deriveDomain turns a git URL into a domain tag, e.g.
// https://github.com/gin-gonic/gin(.git) -> github.com/gin-gonic/gin.
func deriveDomain(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if at := strings.Index(s, "@"); at >= 0 { // scp-style git@host:owner/repo
		s = s[at+1:]
	}
	s = strings.Replace(s, ":", "/", 1)
	s = strings.Trim(s, "/")
	if s == "" || !strings.Contains(s, "/") {
		return ""
	}
	return s
}

// deriveSlug returns owner/name from a domain like github.com/gin-gonic/gin.
func deriveSlug(domain string) string {
	parts := strings.Split(strings.Trim(domain, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func deriveRepoBase(url string) string {
	s := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	s = strings.Trim(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "repo"
	}
	return b.String()
}

func printImportPlan(checkout, domain, slug string, full, wantContracts bool, skipReason, storeURL string) {
	fmt.Fprintln(os.Stderr, "\nplan:")
	if full && wantContracts {
		fmt.Fprintf(os.Stderr, "  1. sensei intent-mine --repo %s --sources docs,comments,tests --drafter llm --max N --apply\n", checkout)
	} else if skipReason != "" {
		fmt.Fprintf(os.Stderr, "  1. (contracts skipped: %s)\n", skipReason)
	} else {
		fmt.Fprintln(os.Stderr, "  1. (basic depth: no contract extraction)")
	}
	fmt.Fprintf(os.Stderr, "  2. sensei bootstrap --repo %s --skip-history --skip-build\n", checkout)
	if full && slug != "" {
		fmt.Fprintf(os.Stderr, "  3. sensei cold-bootstrap --repo %s --repo-slug %s --auto-window\n", checkout, slug)
	} else {
		fmt.Fprintln(os.Stderr, "  3. (history mining skipped)")
	}
	store := storeURL
	if store == "" {
		store = "<url>"
	}
	fmt.Fprintf(os.Stderr, "  4. sensei build --input %s/docs/awareness --input %s/docs/awareness/generated --repo %s --store-url %s\n",
		checkout, checkout, domain, store)
}

func printImportSummary(checkout, domain string, cloned, wantContracts bool) {
	fmt.Fprintln(os.Stderr, "\nsensei import: done — nothing was promoted.")
	if wantContracts {
		fmt.Fprintf(os.Stderr, "  contracts/intents: %s/docs/awareness/intent_*.yaml (+ candidates/)\n", checkout)
	}
	fmt.Fprintf(os.Stderr, "  candidates for review: %s/docs/awareness/candidates/\n", checkout)
	fmt.Fprintf(os.Stderr, "  next: review, then `sensei promote --repo %s ...`; verify with `sensei briefing --file <f> --domain %s`\n", domain, domain)
	if cloned {
		fmt.Fprintf(os.Stderr, "  checkout kept at %s (delete when done)\n", checkout)
	}
}
