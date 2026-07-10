// SPDX-License-Identifier: AGPL-3.0-only

// Command awg is the standalone Sensei CLI.
//
// Sensei makes architectural intent queryable at the point of edit,
// preventing the slow drift that kills codebases.
//
// Usage:
//
//	awg init                             Scaffold awareness for a new project
//	awg build                            Compile YAML sources and load into store
//	awg serve                            Start the gRPC awareness server
//	awg briefing --file <path>           Query the graph for a file
//	awg impact --file <path>             Structured knowledge nodes for a file
//	awg preflight --file <path>          Risk classification before editing
//	awg contract-assess                  Report contract-gate outcome from explicit evidence
//	awg contract-bootstrap               Build a proposed repair-contract bootstrap
//	awg resolve <class> <id>             Fetch a single node by class + id
//	awg query --mode <mode>              Structured browse of the graph
//	awg metadata                         Graph-level coverage and freshness
//	awg governance status                Show local managed-governance state
//	awg check                            Validate YAML sources without building
//	awg validate                         Deep structural check of YAML sources
//	awg audit                            Self-audit for drift, gaps, inconsistencies
//	awg repo-eval                        Evidence-based repository quality evaluation
//	awg benchmark-brief                  Local repair envelope for benchmark/PR fixing
//	awg benchmark-judge                  Local post-patch contract/test judge
//	awg benchmark-score                  Standard brief->judge benchmark workflow
//	awg benchmark-retry                  Benchmark retry-plan controller
//	awg benchmark-event-meta             Read orchestration metadata from learning events
//	awg certify                          Local governance certification over authored event metadata
//	awg extract-authority                Extract candidate authority surfaces from code
//	awg extract-proof-obligations        Generate proof obligations from authority surfaces
//	awg proof-plan                       Show required proof before a repair can be promoted
//	awg repair-plan                      Build an authoritative governed repair plan
//	awg seed-status                      Check generated/committed/live seed authority alignment
//	awg repair-report                    Emit a governed post-edit repair report artifact
//	awg repair-gate                      CI-friendly governed repair verdict
//	awg repo-eval fix                    Safe evidence-backed metadata repair
//	awg repo-eval draft-upgrade          Draft review-only governance candidates from repo-eval
//	awg rebuild                          Rebuild awareness.nt from YAML sources
//	awg promote <id>                     Promote a candidate to canonical YAML
//	awg propose --kind <kind> ...        Append one typed feedback entry (scar) and rebuild
//	awg feedback-check                   Warn when a fix added durable knowledge but no graph feedback
//	awg ingest --from-file <path>        Feed new knowledge into the graph
//	awg skill-ingest <skill-pack-root>   Generate review-only candidates from external skills
//	awg pattern-check <file>...          Check files against pattern recipes
//	awg version                          Print version and exit
//
// See https://github.com/globulario/sensei for documentation.
package main

import (
	"fmt"
	"os"
)

var Version = "0.0.1-dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		os.Exit(runInit(args))
	case "bootstrap":
		os.Exit(runBootstrap(args))
	case "onboard":
		os.Exit(runOnboard(args))
	case "build":
		os.Exit(runBuild(args))
	case "scip-ingest":
		os.Exit(runScipIngest(args))
	case "serve":
		os.Exit(runServe(args))
	case "briefing":
		os.Exit(runBriefing(args))
	case "impact":
		os.Exit(runImpact(args))
	case "preflight":
		os.Exit(runPreflight(args))
	case "contract-assess":
		os.Exit(runContractAssess(args))
	case "contract-bootstrap":
		os.Exit(runContractBootstrap(args))
	case "edit-check":
		os.Exit(runEditCheck(args))
	case "edit-guard":
		os.Exit(runEditGuard(args))
	case "gate":
		os.Exit(runGate(args))
	case "evidence":
		os.Exit(runEvidence(args))
	case "resolve":
		os.Exit(runResolve(args))
	case "query":
		os.Exit(runQuery(args))
	case "metadata":
		os.Exit(runMetadata(args))
	case "governance":
		os.Exit(runGovernance(args))
	case "check":
		os.Exit(runCheck(args))
	case "validate":
		os.Exit(runValidate(args))
	case "audit":
		os.Exit(runAudit(args))
	case "merge-check":
		os.Exit(runMergeCheck(args))
	case "runtime-adapter":
		os.Exit(runRuntimeAdapter(args))
	case "runtime-snapshot":
		os.Exit(runRuntimeSnapshot(args))
	case "cluster-diagnose":
		os.Exit(runClusterDiagnose(args))
	case "runtime-repair-report":
		os.Exit(runRuntimeRepairReport(args))
	case "runtime-gate":
		os.Exit(runRuntimeGate(args))
	case "runtime-candidate":
		os.Exit(runRuntimeCandidate(args))
	case "suggest-realizations":
		os.Exit(runSuggestRealizations(args))
	case "promote-realization":
		os.Exit(runPromoteRealization(args))
	case "review-realization":
		os.Exit(runReviewRealization(args))
	case "repo-eval":
		os.Exit(runRepoEval(args))
	case "benchmark-brief":
		os.Exit(runBenchmarkBrief(args))
	case "benchmark-judge":
		os.Exit(runBenchmarkJudge(args))
	case "benchmark-score":
		os.Exit(runBenchmarkScore(args))
	case "benchmark-retry":
		os.Exit(runBenchmarkRetry(args))
	case "benchmark-event-meta":
		os.Exit(runBenchmarkEventMeta(args))
	case "certify":
		os.Exit(runCertify(args))
	case "extract-authority":
		os.Exit(runExtractAuthority(args))
	case "extract-proof-obligations":
		os.Exit(runExtractProofObligations(args))
	case "proof-plan":
		os.Exit(runProofPlan(args))
	case "repair-plan":
		os.Exit(runRepairPlan(args))
	case "seed-status":
		os.Exit(runSeedStatus(args))
	case "reconcile":
		os.Exit(runReconcile(args))
	case "draft-candidate":
		os.Exit(runDraftCandidate(args))
	case "impact-gate":
		os.Exit(runImpactGate(args))
	case "repair-report":
		os.Exit(runRepairReport(args))
	case "repair-gate":
		os.Exit(runRepairGate(args))
	case "seed-freshness":
		os.Exit(runSeedFreshness(args))
	case "rebuild":
		os.Exit(runRebuild(args))
	case "learn":
		os.Exit(runLearn(args))
	case "lifecycle":
		os.Exit(runLifecycle(args))
	case "promote":
		os.Exit(runPromote(args))
	case "propose":
		os.Exit(runPropose(args))
	case "feedback-check":
		os.Exit(runFeedbackCheck(args))
	case "ingest":
		os.Exit(runIngest(args))
	case "skill-ingest":
		os.Exit(runSkillIngest(args))
	case "pattern-check":
		os.Exit(runPatternCheck(args))
	case "source-check":
		os.Exit(runSourceCheck(args))
	case "visual-audit":
		os.Exit(runVisualAudit(args))
	case "cold-bootstrap":
		os.Exit(runColdBootstrap(args))
	case "validate-draft":
		os.Exit(runValidateDraft(args))
	case "intent-mine":
		os.Exit(runIntentMine(args))
	case "corpus":
		os.Exit(runCorpus(args))
	case "version":
		fmt.Println(Version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "awg: unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `Sensei — architectural awareness for any codebase

Usage: awg <command> [flags]

Query commands (require a running Sensei server):
  briefing       Query the graph for a file or task
  impact         Get structured knowledge nodes for a file
  preflight      Risk classification before editing a file
  contract-assess Report-only contract synthesis assessment from explicit evidence
  contract-bootstrap Build a proposed repair-contract bootstrap from issue/tests/Sensei
  edit-check     Warn (advisory) if a proposed edit violates repo-scoped rules
  gate           Hard gate over a git diff (--enforce to block; --event-log to record outcomes)
  evidence       Aggregate the gate/guard outcome ledger ("caught N incidents across M repos")
  resolve        Fetch a single awareness node by class + id
  query          Structured browse (by_file | by_id | by_class | related)
  metadata       Show graph-level coverage and freshness
  governance     Verify/activate/status for local managed-governance packs
  pattern-check  Check files against ImplementationPattern recipes
  source-check   Scan source files for structural pattern violations
  visual-audit   Screenshot routes and compare against golden images

Local commands (no server required):
  init           Scaffold awareness for a new project
  bootstrap      Initialize Sensei for an existing repo (deterministic extraction + optional history)
  build          Compile YAML sources and load into the store
  serve          Start the gRPC awareness server
  check          Validate YAML sources without building
  validate       Deep structural check (dangling refs, missing files, dup IDs)
  audit          Self-audit for drift, gaps, and inconsistencies
  merge-check    Verify a PR is merge-authorized (per-check + mergeability; never merges)
  runtime-adapter validate  Validate a runtime-adapter/v1 manifest (lanes->platform mapping)
  runtime-snapshot validate Validate a runtime-evidence/v1 snapshot (schema only; Phase 1)
  cluster-diagnose Typed runtime verdict from a snapshot (blocked_by_quorum, converged, ...)
  runtime-repair-report Validate a runtime repair (before/action/after; valid_runtime_repair or honest block)
  runtime-gate     Fail-closed CI/operator gate over a runtime verdict (no implicit green)
  runtime-candidate Turn a recurring runtime verdict into a governance CANDIDATE (review-gated; never auto-enforced)
  repo-eval      Evidence-based repository quality evaluation and safe metadata repair
                 subcommands: fix | draft-upgrade
  benchmark-brief Build a compact local repair envelope for benchmark/PR fixing
  benchmark-judge Judge a patch envelope for contract/test discipline
  benchmark-score Standard brief->judge benchmark workflow and combined score
  benchmark-retry Build a reusable benchmark retry plan from run evidence
  benchmark-event-meta Read small orchestration metadata from benchmark learning events
  certify        Evaluate a repair claim/promotion verdict from authored event metadata
  extract-authority Extract candidate authority surfaces from Go code
  extract-proof-obligations Generate proof obligations from authority surfaces
  proof-plan     Show the required proof/forbidden-move checklist before editing
  repair-plan    Build an authoritative governed repair plan
  seed-status    Check generated/committed/live seed authority alignment
  reconcile      Diff the live store against the committed seed; name store-only orphans
  draft-candidate Draft an incident/finding/scar into a review-queue candidate (WB-2)
  impact-gate    Changed files -> protecting invariants' required_tests; fail-closed verify (CG-5)
  repair-report  Emit the governed post-edit repair report artifact
  repair-gate    Fail-closed CI verdict from repair classification or artifact
  rebuild        Rebuild awareness.nt from YAML sources across repos
  promote        Promote a candidate into canonical awareness YAML
  propose        Append one typed feedback entry (failure_mode/invariant/required_test/forbidden_fix), rebuild + reload, stage (never commit)
  feedback-check Warn when a session fixed a durable error class but added no graph feedback (Stop-hook backing)
  ingest         Feed new knowledge into the graph
  skill-ingest   Generate review-only ImplementationPattern candidates from external SKILL.md files

Other:
  version        Print version and exit

Run "awg <command> --help" for details on each command.
`)
}
