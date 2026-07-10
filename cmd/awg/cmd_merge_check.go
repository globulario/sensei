// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.merge_check
// @awareness file_role=merge_authority_verifier
// @awareness enforces=ci.merge_authority_requires_explicit_check_success

// awg merge-check — merge-authority verifier.
//
// Codifies the merge discipline proven in the awareness-graph #103 / services
// #60 / awareness-graph #111 sequence: a PR is merge-authorized ONLY when every
// required check is explicitly successful on the CURRENT head AND GitHub reports
// the PR as cleanly mergeable. It exists to prevent the false-green class seen
// that day (failure mode ci.merge_authority_requires_explicit_check_success):
//
//   - `gh pr checks` green while the PR was still CONFLICTING (#103);
//   - a summarized watcher/status exit treated as authority while per-check or
//     mergeability state said otherwise;
//   - required checks green on an obsolete head;
//   - `gh pr edit --base` silently no-opping on a GraphQL warning.
//
// It NEVER merges. It authorizes (exit 0) or blocks (exit non-zero). It reads
// GitHub's ACTUAL PR metadata (mergeable + mergeStateStatus) and the per-check
// conclusions tied to the current head SHA — never a watcher/summary exit code.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ── Verdict vocabulary ───────────────────────────────────────────────────────

const (
	verdictAuthorized   = "MERGE_AUTHORIZED"
	verdictCheckFailure = "BLOCKED_BY_CHECK_FAILURE"
	verdictPending      = "BLOCKED_BY_PENDING_CHECK"
	verdictMissing      = "BLOCKED_BY_MISSING_REQUIRED_CHECK"
	verdictStale        = "BLOCKED_BY_STALE_CHECK"
	verdictConflict     = "BLOCKED_BY_CONFLICT"
	verdictWrongBase    = "BLOCKED_BY_WRONG_BASE"
	verdictUnknownState = "MERGE_STATE_UNKNOWN"
)

// ── Data model (the classifier input; fixture JSON unmarshals into prState) ──

// checkRun is one status-check result for a commit. It models BOTH the modern
// check-runs API (status/conclusion) and legacy commit statuses (mapped onto the
// same fields), plus the head SHA the check actually ran against — the field
// that exposes a stale (obsolete-head) green.
type checkRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | neutral | cancelled | timed_out | action_required | skipped | ""
	HeadSHA    string `json:"head_sha"`   // the SHA this run was produced for
}

// prState is the authoritative GitHub PR metadata the verdict is computed from.
type prState struct {
	Number           int        `json:"number"`
	Repo             string     `json:"repo"`
	BaseRef          string     `json:"baseRefName"`
	HeadRef          string     `json:"headRefName"`
	HeadSHA          string     `json:"headRefOid"`
	Mergeable        string     `json:"mergeable"`        // MERGEABLE | CONFLICTING | UNKNOWN
	MergeStateStatus string     `json:"mergeStateStatus"` // CLEAN | DIRTY | BLOCKED | BEHIND | UNSTABLE | HAS_HOOKS | DRAFT | UNKNOWN
	IsDraft          bool       `json:"isDraft"`
	State            string     `json:"state"` // OPEN | MERGED | CLOSED
	Checks           []checkRun `json:"checks"`
}

// mergeCheckConfig carries operator-supplied policy: the expected base and the
// authoritative required-check set (from --required or branch protection). When
// the required set cannot be determined (RequiredKnown=false), the verifier
// CANNOT prove completeness, so it cannot emit MISSING and says so.
type mergeCheckConfig struct {
	ExpectedBase   string
	RequiredChecks []string
	RequiredKnown  bool
}

// checkEval is the per-check evaluation surfaced in the report.
type checkEval struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	State    string `json:"state"` // pass | fail | pending | stale | missing
	HeadSHA  string `json:"head_sha"`
}

// mergeCheckResult is the full verdict + evidence (the report / JSON output).
type mergeCheckResult struct {
	PR             int         `json:"pr"`
	Repo           string      `json:"repo"`
	BaseRef        string      `json:"base_ref"`
	ExpectedBase   string      `json:"expected_base,omitempty"`
	HeadRef        string      `json:"head_ref"`
	HeadSHA        string      `json:"head_sha"`
	Mergeable      string      `json:"mergeable"`
	MergeState     string      `json:"merge_state_status"`
	RequiredKnown  bool        `json:"required_set_known"`
	RequiredChecks []string    `json:"required_checks"`
	Checks         []checkEval `json:"checks"`
	MissingChecks  []string    `json:"missing_required_checks"`
	StaleChecks    []string    `json:"stale_checks"`
	Verdict        string      `json:"verdict"`
	Reason         string      `json:"reason"`
}

// passingConclusion reports whether a completed check's conclusion is acceptable.
// success/neutral/skipped do not block; everything else (failure, cancelled,
// timed_out, action_required, stale-empty) does.
func passingConclusion(c string) bool {
	switch strings.ToLower(c) {
	case "success", "neutral", "skipped":
		return true
	default:
		return false
	}
}

// classifyMergeAuthority is the PURE verdict function — no I/O, fully
// deterministic, the unit-tested core. Precedence (most structural first):
//
//  1. MERGE_STATE_UNKNOWN  — mergeability not yet known / draft / blank.
//  2. BLOCKED_BY_WRONG_BASE — base != expected.
//  3. BLOCKED_BY_CONFLICT   — mergeable CONFLICTING or mergeStateStatus DIRTY.
//  4. BLOCKED_BY_STALE_CHECK — mergeStateStatus BEHIND (head behind base), or a
//     required check whose latest run is for a non-head SHA.
//  5. BLOCKED_BY_MISSING_REQUIRED_CHECK — a required check with no run at all,
//     or mergeStateStatus BLOCKED with no concrete per-check cause found.
//  6. BLOCKED_BY_CHECK_FAILURE — a required check concluded non-pass.
//  7. BLOCKED_BY_PENDING_CHECK — a required check not yet completed.
//  8. MERGE_AUTHORIZED — mergeable AND clean AND every required check passed on
//     the current head.
//
// A summarized/watcher exit code is never an input; only mergeable +
// mergeStateStatus + per-check state tied to pr.HeadSHA are.
func classifyMergeAuthority(pr prState, cfg mergeCheckConfig) mergeCheckResult {
	res := mergeCheckResult{
		PR: pr.Number, Repo: pr.Repo, BaseRef: pr.BaseRef, ExpectedBase: cfg.ExpectedBase,
		HeadRef: pr.HeadRef, HeadSHA: pr.HeadSHA, Mergeable: pr.Mergeable,
		MergeState: pr.MergeStateStatus, RequiredKnown: cfg.RequiredKnown,
		RequiredChecks: append([]string{}, cfg.RequiredChecks...),
	}
	sort.Strings(res.RequiredChecks)

	// Resolve the gate set: explicit required set when known, else every check
	// present on the head (conservative — any failure blocks, but completeness
	// cannot be proven so MISSING is not emitted).
	required := map[string]bool{}
	for _, n := range cfg.RequiredChecks {
		required[n] = true
	}

	// Latest run per check name that is FOR THE CURRENT HEAD, plus track names
	// that exist only on a non-head SHA (stale).
	currentByName := map[string]checkRun{}
	seenAnySHA := map[string]bool{}
	staleOnly := map[string]bool{}
	for _, c := range pr.Checks {
		seenAnySHA[c.Name] = true
		if c.HeadSHA == "" || c.HeadSHA == pr.HeadSHA {
			currentByName[c.Name] = c
			delete(staleOnly, c.Name)
		} else if _, ok := currentByName[c.Name]; !ok {
			staleOnly[c.Name] = true
		}
	}

	// Build the per-check report + classify each gate check's state.
	gateNames := map[string]bool{}
	if cfg.RequiredKnown {
		for n := range required {
			gateNames[n] = true
		}
	} else {
		for n := range seenAnySHA {
			gateNames[n] = true
		}
	}
	var missing, stale, failed, pending []string
	for name := range gateNames {
		ev := checkEval{Name: name, Required: !cfg.RequiredKnown || required[name]}
		switch {
		case !seenAnySHA[name]:
			ev.State = "missing"
			missing = append(missing, name)
		case staleOnly[name]:
			ev.State = "stale"
			ev.HeadSHA = staleHeadSHA(pr.Checks, name)
			stale = append(stale, name)
		default:
			c := currentByName[name]
			ev.Status, ev.HeadSHA = c.Status, c.HeadSHA
			switch {
			case c.Status != "completed":
				ev.State = "pending"
				pending = append(pending, name)
			case passingConclusion(c.Conclusion):
				ev.State = "pass"
			default:
				ev.State = "fail"
				failed = append(failed, name)
			}
		}
		res.Checks = append(res.Checks, ev)
	}
	sort.Slice(res.Checks, func(i, j int) bool { return res.Checks[i].Name < res.Checks[j].Name })
	sort.Strings(missing)
	sort.Strings(stale)
	sort.Strings(failed)
	sort.Strings(pending)
	res.MissingChecks, res.StaleChecks = missing, stale

	set := func(v, r string) mergeCheckResult { res.Verdict, res.Reason = v, r; return res }

	// 1. Mergeability not knowable → never authorize on partial info.
	ms := strings.ToUpper(pr.MergeStateStatus)
	if pr.IsDraft || ms == "DRAFT" {
		return set(verdictUnknownState, "PR is a draft; not an authorization candidate")
	}
	if strings.ToUpper(pr.Mergeable) == "UNKNOWN" || ms == "" || ms == "UNKNOWN" {
		return set(verdictUnknownState, "GitHub has not computed mergeability (mergeable/mergeStateStatus unknown) — re-fetch; do not treat as mergeable")
	}
	// 2. Wrong base.
	if cfg.ExpectedBase != "" && pr.BaseRef != cfg.ExpectedBase {
		return set(verdictWrongBase, fmt.Sprintf("base is %q, expected %q", pr.BaseRef, cfg.ExpectedBase))
	}
	// 3. Conflict.
	if strings.ToUpper(pr.Mergeable) == "CONFLICTING" || ms == "DIRTY" {
		return set(verdictConflict, "PR is not cleanly mergeable (mergeable=CONFLICTING / mergeStateStatus=DIRTY) — green checks do not override this")
	}
	// 4. Stale: head behind base, or a required check only on a non-head SHA.
	if ms == "BEHIND" {
		return set(verdictStale, "head is BEHIND base — checks did not run against the merge result; update the branch and re-check")
	}
	if len(stale) > 0 {
		return set(verdictStale, "required check(s) last ran on a non-head SHA (obsolete green): "+strings.Join(stale, ", "))
	}
	// 5. Missing (only provable when the required set is known).
	if len(missing) > 0 {
		return set(verdictMissing, "required check(s) have no run on the head: "+strings.Join(missing, ", "))
	}
	// 6/7. Failures then pending among gate checks.
	if len(failed) > 0 {
		return set(verdictCheckFailure, "required check(s) did not succeed: "+strings.Join(failed, ", "))
	}
	if len(pending) > 0 {
		return set(verdictPending, "required check(s) not yet completed: "+strings.Join(pending, ", "))
	}
	// GitHub still says BLOCKED with no per-check cause we can see → a required
	// gate (a check we don't know is required, or a review) is unsatisfied.
	if ms == "BLOCKED" {
		return set(verdictMissing, "mergeStateStatus=BLOCKED but no failing/pending per-check cause is visible — a required gate (check or review) is unsatisfied; do not merge")
	}
	// 8. Authorized: mergeable + clean(ish) + all gate checks passed on head.
	//    UNSTABLE/HAS_HOOKS are acceptable here ONLY because every check we treat
	//    as a gate already passed (a failing required check would have blocked
	//    above); the instability is a non-gate check.
	if strings.ToUpper(pr.Mergeable) != "MERGEABLE" {
		return set(verdictUnknownState, fmt.Sprintf("mergeable=%q is not MERGEABLE and not a recognized block", pr.Mergeable))
	}
	return set(verdictAuthorized, "every required check passed on the current head and the PR is cleanly mergeable")
}

// staleHeadSHA returns the SHA a stale-only check last ran against (for the report).
func staleHeadSHA(checks []checkRun, name string) string {
	for _, c := range checks {
		if c.Name == name {
			return c.HeadSHA
		}
	}
	return ""
}

// ── Live data fetch (gh) — kept thin; the classifier above is the tested core ─

func ghJSON(out interface{}, args ...string) error {
	cmd := exec.Command("gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("parse `gh %s` output: %w", strings.Join(args, " "), err)
	}
	return nil
}

// fetchPRStateRetry re-fetches while GitHub reports mergeable=UNKNOWN, which it
// does on the first query because mergeability is computed lazily. This waits
// for GitHub's ACTUAL metadata to settle (not a watcher exit) up to `attempts`
// times. If it never settles, the caller still gets UNKNOWN and blocks.
func fetchPRStateRetry(pr int, repo string, attempts int, wait time.Duration) (prState, error) {
	var st prState
	var err error
	for i := 0; i < attempts; i++ {
		st, err = fetchPRState(pr, repo)
		if err != nil {
			return st, err
		}
		if strings.ToUpper(st.Mergeable) != "UNKNOWN" && strings.ToUpper(st.MergeStateStatus) != "UNKNOWN" && st.MergeStateStatus != "" {
			return st, nil
		}
		if i < attempts-1 {
			time.Sleep(wait)
		}
	}
	return st, nil
}

// fetchPRState pulls the authoritative PR metadata + the per-check state bound to
// the current head SHA. Binding checks to headRefOid is what structurally
// prevents the obsolete-head false-green: stale runs are detected, not trusted.
func fetchPRState(pr int, repo string) (prState, error) {
	var meta struct {
		Number           int    `json:"number"`
		BaseRefName      string `json:"baseRefName"`
		HeadRefName      string `json:"headRefName"`
		HeadRefOid       string `json:"headRefOid"`
		Mergeable        string `json:"mergeable"`
		MergeStateStatus string `json:"mergeStateStatus"`
		IsDraft          bool   `json:"isDraft"`
		State            string `json:"state"`
	}
	if err := ghJSON(&meta, "pr", "view", fmt.Sprintf("%d", pr), "--repo", repo,
		"--json", "number,baseRefName,headRefName,headRefOid,mergeable,mergeStateStatus,isDraft,state"); err != nil {
		return prState{}, err
	}
	st := prState{
		Number: meta.Number, Repo: repo, BaseRef: meta.BaseRefName, HeadRef: meta.HeadRefName,
		HeadSHA: meta.HeadRefOid, Mergeable: meta.Mergeable, MergeStateStatus: meta.MergeStateStatus,
		IsDraft: meta.IsDraft, State: meta.State,
	}

	// Modern check-runs for the head SHA.
	var cr struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HeadSHA    string `json:"head_sha"`
		} `json:"check_runs"`
	}
	if err := ghJSON(&cr, "api", "--paginate",
		fmt.Sprintf("repos/%s/commits/%s/check-runs", repo, meta.HeadRefOid)); err != nil {
		return prState{}, err
	}
	for _, c := range cr.CheckRuns {
		st.Checks = append(st.Checks, checkRun{Name: c.Name, Status: c.Status, Conclusion: c.Conclusion, HeadSHA: c.HeadSHA})
	}
	// Legacy commit statuses (some required contexts are statuses, not check-runs).
	var sts struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"` // success | pending | failure | error
		} `json:"statuses"`
	}
	if err := ghJSON(&sts, "api", fmt.Sprintf("repos/%s/commits/%s/status", repo, meta.HeadRefOid)); err == nil {
		for _, s := range sts.Statuses {
			status, concl := "completed", s.State
			switch s.State {
			case "pending":
				status, concl = "in_progress", ""
			case "success":
				concl = "success"
			default:
				concl = "failure"
			}
			st.Checks = append(st.Checks, checkRun{Name: s.Context, Status: status, Conclusion: concl, HeadSHA: meta.HeadRefOid})
		}
	}
	return st, nil
}

// fetchRequiredChecks resolves the authoritative required-check set from branch
// protection. Returns known=false when unavailable (e.g. private repo on the
// free tier, where the protection API 403s) — the verifier then cannot prove
// completeness and must not emit MISSING from absence alone.
func fetchRequiredChecks(repo, base string) (names []string, known bool) {
	var rsc struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	}
	if err := ghJSON(&rsc, "api", fmt.Sprintf("repos/%s/branches/%s/protection/required_status_checks", repo, base)); err != nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, c := range rsc.Contexts {
		if !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	for _, c := range rsc.Checks {
		if !seen[c.Context] {
			seen[c.Context] = true
			names = append(names, c.Context)
		}
	}
	return names, true
}

func runMergeCheck(args []string) int {
	fs := flag.NewFlagSet("awg merge-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prNum := fs.Int("pr", 0, "PR number (required)")
	repo := fs.String("repo", "", "owner/repo (required)")
	expectBase := fs.String("expect-base", "", "required base branch (optional; verifies the PR targets it)")
	requiredCSV := fs.String("required", "", "comma-separated required check names (optional; overrides branch protection)")
	retries := fs.Int("retries", 5, "re-fetch attempts while GitHub mergeability is UNKNOWN (lazy computation)")
	asJSON := fs.Bool("json", false, "emit the full result as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg merge-check --pr <n> --repo <owner/repo> [flags]

Merge-authority verifier. Exits 0 ONLY for MERGE_AUTHORIZED; non-zero for every
blocked/unknown verdict. Never merges. Authority is GitHub's actual mergeable +
mergeStateStatus plus per-check conclusions tied to the current head SHA — never
a watcher/summary exit code.

Flags:
  --pr <n>            PR number (required)
  --repo <owner/repo> repository (required)
  --expect-base <b>   block unless the PR's base is exactly <b>
  --required <a,b>    authoritative required-check set (else branch protection;
                      if neither is available, completeness cannot be proven)
  --json              emit full JSON evidence
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *prNum == 0 || *repo == "" {
		fmt.Fprintln(os.Stderr, "merge-check: --pr and --repo are required")
		fs.Usage()
		return 2
	}

	attempts := *retries
	if attempts < 1 {
		attempts = 1
	}
	pr, err := fetchPRStateRetry(*prNum, *repo, attempts, 2*time.Second)
	if err != nil {
		// Could not read authoritative state → UNKNOWN, never authorize.
		r := mergeCheckResult{PR: *prNum, Repo: *repo, Verdict: verdictUnknownState, Reason: "could not read PR state: " + err.Error()}
		emitMergeCheck(r, *asJSON)
		return 1
	}

	cfg := mergeCheckConfig{ExpectedBase: *expectBase}
	if strings.TrimSpace(*requiredCSV) != "" {
		for _, n := range strings.Split(*requiredCSV, ",") {
			if n = strings.TrimSpace(n); n != "" {
				cfg.RequiredChecks = append(cfg.RequiredChecks, n)
			}
		}
		cfg.RequiredKnown = true
	} else if names, known := fetchRequiredChecks(*repo, pr.BaseRef); known {
		cfg.RequiredChecks, cfg.RequiredKnown = names, true
	}

	res := classifyMergeAuthority(pr, cfg)
	emitMergeCheck(res, *asJSON)
	return exitCodeForVerdict(res.Verdict)
}

// exitCodeForVerdict enforces the contract: exit 0 ONLY for MERGE_AUTHORIZED;
// every blocked/unknown verdict is non-zero.
func exitCodeForVerdict(v string) int {
	if v == verdictAuthorized {
		return 0
	}
	return 1
}

func emitMergeCheck(r mergeCheckResult, asJSON bool) {
	if asJSON {
		b, _ := json.MarshalIndent(r, "", "  ")
		fmt.Println(string(b))
		return
	}
	fmt.Printf("merge-check: PR #%d  %s\n", r.PR, r.Repo)
	fmt.Printf("  base:        %s", r.BaseRef)
	if r.ExpectedBase != "" {
		fmt.Printf("  (expected: %s)", r.ExpectedBase)
	}
	fmt.Printf("\n  head:        %s @ %s\n", r.HeadRef, shortSHA(r.HeadSHA))
	fmt.Printf("  mergeable:   %s / %s\n", r.Mergeable, r.MergeState)
	reqLabel := "required set: unknown (completeness NOT proven)"
	if r.RequiredKnown {
		reqLabel = fmt.Sprintf("required set: %d known", len(r.RequiredChecks))
	}
	fmt.Printf("  %s\n", reqLabel)
	for _, c := range r.Checks {
		tag := ""
		if c.Required {
			tag = " [required]"
		}
		fmt.Printf("    - %-40s %s%s\n", c.Name, strings.ToUpper(c.State), tag)
	}
	if len(r.MissingChecks) > 0 {
		fmt.Printf("  missing required: %s\n", strings.Join(r.MissingChecks, ", "))
	}
	if len(r.StaleChecks) > 0 {
		fmt.Printf("  stale: %s\n", strings.Join(r.StaleChecks, ", "))
	}
	fmt.Printf("  VERDICT: %s\n", r.Verdict)
	if r.Reason != "" {
		fmt.Printf("  reason:  %s\n", r.Reason)
	}
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
