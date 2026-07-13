// SPDX-License-Identifier: Apache-2.0

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
	refresh := fs.Bool("refresh", false, "re-extract and optionally reload an existing checkout; never clones")
	dryRun := fs.Bool("dry-run", false, "print the plan and stop; run nothing")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage:
  sensei import <git-url | path> --domain <domain> [flags]
  sensei import --refresh <checkout-path> --domain <domain> [flags]

Onboard a foreign repository into Sensei in one command, in the correct order:
clone -> contract extraction (on the pristine clone) -> structural extraction ->
optional history mining -> (optionally) load the domain-scoped slice.

With --refresh, re-extract an existing checkout and optionally reload its
domain-scoped slice. Refresh never clones; it requires a checkout path.

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

	// Resolve the checkout: refresh only accepts an existing checkout; normal
	// import uses an existing path in place or clones a URL.
	var checkout string
	var cloned bool
	var code int
	if *refresh {
		checkout, code = resolveImportRefreshCheckout(target)
	} else {
		checkout, cloned, code = resolveImportCheckout(target, strings.TrimSpace(*dir), *dryRun)
	}
	if code != 0 {
		return code
	}

	dom := resolveImportDomain(*domain, target, checkout)
	if dom == "" {
		fmt.Fprintln(os.Stderr, "sensei import: --domain is required (could not derive it from the target or checkout remote)")
		return 2
	}
	slug := strings.TrimSpace(*repoSlug)
	if slug == "" {
		slug = deriveSlug(dom)
	}

	haveKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != ""
	wantContracts := full && (*drafter != "llm" || haveKey)
	skipContractReason := ""
	if full && *drafter == "llm" && !haveKey {
		skipContractReason = "ANTHROPIC_API_KEY not set — contract layer skipped (run again with a key, or --drafter echo)"
	}

	mode := "import"
	if *refresh {
		mode = "refresh"
	}
	fmt.Fprintf(os.Stderr, "sensei import: %s\n  mode:     %s\n  domain:   %s\n  checkout: %s\n  depth:    %s\n",
		target, mode, dom, checkout, *depth)
	if *dryRun {
		fmt.Fprintln(os.Stderr, "  (dry-run: nothing executed)")
		printImportPlan(checkout, dom, slug, full, wantContracts, skipContractReason, *storeURL, *refresh)
		return 0
	}

	// 1) Contracts FIRST for fresh imports — on the pristine clone, before
	// bootstrap scaffolds. Refresh reuses an existing checkout, so this stage is
	// a re-grounding pass over current files rather than a pristine-clone pass.
	if wantContracts {
		stage := "contract extraction (pristine clone)"
		if *refresh {
			stage = "contract refresh (existing checkout)"
		}
		fmt.Fprintf(os.Stderr, "\n== [1/4] %s ==\n", stage)
		ia := []string{"--path", checkout, "--sources", "docs,comments,tests",
			"--drafter", *drafter, "--max", strconv.Itoa(*maxN), "--apply"}
		if rc := runIntentMine(ia); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: contract extraction failed — continuing with structure only")
		}
	} else if skipContractReason != "" {
		fmt.Fprintf(os.Stderr, "\n== [1/4] contract extraction: %s ==\n", skipContractReason)
	}

	// 2) Structural extraction — now safe to scaffold the checkout.
	fmt.Fprintln(os.Stderr, "\n== [2/4] structural extraction ==")
	if rc := runBootstrap([]string{"--path", checkout, "--skip-history", "--skip-build"}); rc != 0 {
		fmt.Fprintln(os.Stderr, "sensei import: structural extraction failed")
		return 1
	}

	// 3) History / PR mining — full depth only, best-effort.
	if full && slug != "" {
		fmt.Fprintln(os.Stderr, "\n== [3/4] day-0 history / PR mining ==")
		if rc := runColdBootstrap([]string{"--path", checkout, "--repo-slug", slug, "--auto-window"}); rc != 0 {
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
		if m := strings.TrimSpace(*markerFile); m != "" {
			ba = append(ba, "--graph-marker-file", m)
		} else {
			fmt.Fprintln(os.Stderr, "  note: no --graph-marker-file given; a live/served store may report freshness-stale for briefing until re-certified")
		}
		if rc := runBuild(ba); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: load failed — a scoped --repo update needs a non-empty store; seed with `sensei build --all` first")
			return 1
		}
	}

	printImportSummary(checkout, dom, cloned, wantContracts, *refresh)
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

func resolveImportRefreshCheckout(target string) (checkout string, code int) {
	info, err := os.Stat(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: checkout path %s: %v\n", target, err)
		return "", 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: %s is not a directory\n", target)
		return "", 2
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: %v\n", err)
		return "", 1
	}
	return abs, 0
}

func resolveImportDomain(explicit, target, checkout string) string {
	if dom := strings.TrimSpace(explicit); dom != "" {
		return dom
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return gitRemoteDomain(checkout)
	}
	if dom := deriveDomain(target); dom != "" {
		return dom
	}
	return gitRemoteDomain(checkout)
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

func printImportPlan(checkout, domain, slug string, full, wantContracts bool, skipReason, storeURL string, refresh bool) {
	fmt.Fprintln(os.Stderr, "\nplan:")
	if refresh {
		fmt.Fprintf(os.Stderr, "  0. refresh existing checkout %s for domain %s\n", checkout, domain)
	}
	if full && wantContracts {
		fmt.Fprintf(os.Stderr, "  1. sensei intent-mine --path %s --sources docs,comments,tests --drafter llm --max N --apply\n", checkout)
	} else if skipReason != "" {
		fmt.Fprintf(os.Stderr, "  1. (contracts skipped: %s)\n", skipReason)
	} else {
		fmt.Fprintln(os.Stderr, "  1. (basic depth: no contract extraction)")
	}
	fmt.Fprintf(os.Stderr, "  2. sensei bootstrap --path %s --skip-history --skip-build\n", checkout)
	if full && slug != "" {
		fmt.Fprintf(os.Stderr, "  3. sensei cold-bootstrap --path %s --repo-slug %s --auto-window\n", checkout, slug)
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

func printImportSummary(checkout, domain string, cloned, wantContracts, refresh bool) {
	if refresh {
		fmt.Fprintln(os.Stderr, "\nsensei import --refresh: done — nothing was promoted.")
	} else {
		fmt.Fprintln(os.Stderr, "\nsensei import: done — nothing was promoted.")
	}
	if wantContracts {
		fmt.Fprintf(os.Stderr, "  contracts/intents: %s/docs/awareness/intent_*.yaml (+ candidates/)\n", checkout)
	}
	fmt.Fprintf(os.Stderr, "  candidates for review: %s/docs/awareness/candidates/\n", checkout)
	fmt.Fprintf(os.Stderr, "  next: review, then `sensei promote --repo %s ...`; verify with `sensei briefing --file <f> --domain %s`\n", domain, domain)
	if cloned {
		fmt.Fprintf(os.Stderr, "  checkout kept at %s (delete when done)\n", checkout)
	}
}
