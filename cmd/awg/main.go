// SPDX-License-Identifier: Apache-2.0

// Command sensei is the standalone Sensei CLI.
//
// Sensei makes architectural intent queryable at the point of edit,
// preventing the slow drift that kills codebases.
//
// The binary was named "awg" (Awareness Graph) before the rename; it is still
// installed as a deprecated alias for one release. Invoking it as "awg" prints
// a deprecation notice and otherwise behaves identically.
//
// Usage:
//
//	sensei demo                             One command: stand up a graph and return a briefing
//	sensei init                             Scaffold awareness for a new project
//	sensei build                            Compile YAML sources and load into store
//	sensei serve                            Start the gRPC awareness server
//	sensei briefing --file <path>           Query the graph for a file
//	sensei impact --file <path>             Structured knowledge nodes for a file
//	sensei preflight --file <path>          Risk classification before editing
//	sensei contract-assess                  Report contract-gate outcome from explicit evidence
//	sensei contract-bootstrap               Build a proposed repair-contract bootstrap
//	sensei resolve <class> <id>             Fetch a single node by class + id
//	sensei query --mode <mode>              Structured browse of the graph
//	sensei metadata                         Graph-level coverage and freshness
//	sensei domains                          List selectable graph domains
//	sensei governance status                Show local managed-governance state
//	sensei check                            Validate YAML sources without building
//	sensei validate                         Deep structural check of YAML sources
//	sensei audit                            Self-audit for drift, gaps, inconsistencies
//	sensei repo-eval                        Evidence-based repository quality evaluation
//	sensei benchmark-brief                  Local repair envelope for benchmark/PR fixing
//	sensei benchmark-judge                  Local post-patch contract/test judge
//	sensei benchmark-score                  Standard brief->judge benchmark workflow
//	sensei benchmark-retry                  Benchmark retry-plan controller
//	sensei benchmark-event-meta             Read orchestration metadata from learning events
//	sensei benchmark-freeze                 Freeze external cold-start benchmark workspace
//	sensei benchmark-reconstruct            Reconstruct bounded benchmark state
//	sensei benchmark-evaluate               Evaluate external benchmark receipts
//	sensei benchmark-status                 Inspect external benchmark state
//	sensei certify                          Legacy benchmark certification adapter (not architectural closure)
//	sensei certify-change                   Architectural-closure certification over a verified task ledger
//	sensei extract-authority                Extract candidate authority surfaces from code
//	sensei extract-proof-obligations        Generate proof obligations from authority surfaces
//	sensei infer-claims                     Derive offline ArchitectureClaim candidates from facts
//	sensei maintain-claims                  Recalculate offline ArchitectureClaim status
//	sensei assess-planes                    Verify ArchitectureClaim architectural-plane basis offline
//	sensei generate-questions               Generate offline OpenQuestion candidates from closure blockers
//	sensei record-answer                    Record an exact architect answer offline
//	sensei adjudicate-answer                Adjudicate a recorded architect answer offline
//	sensei plan-probes                      Generate offline EvidenceProbe plans
//	sensei record-probe-result              Record an externally executed probe result offline
//	sensei advance-convergence              Advance one offline convergence session iteration
//	sensei convergence-status               Inspect an offline convergence session
//	sensei bootstrap-direction-digest       Compute canonical digest for a bootstrap direction authorization
//	sensei admit-change                     Evaluate bounded agent admission
//	sensei verify-admission                 Verify a diff stayed inside admission scope
//	sensei admission-status                 Inspect admission receipts
//	sensei prepare-change                   Create or refresh one active task session
//	sensei task-status                      Inspect an active task session
//	sensei advance-task                     Run safe evidence and advance one task iteration
//	sensei task-briefing                    Show bounded file context for an active task
//	sensei proof-plan                       Show required proof before a repair can be promoted
//	sensei repair-plan                      Build an authoritative governed repair plan
//	sensei seed-status                      Check generated/committed/live seed authority alignment
//	sensei repair-report                    Emit a governed post-edit repair report artifact
//	sensei repair-gate                      CI-friendly governed repair verdict
//	sensei repo-eval fix                    Safe evidence-backed metadata repair
//	sensei repo-eval draft-upgrade          Draft review-only governance candidates from repo-eval
//	sensei rebuild                          Rebuild awareness.nt from YAML sources
//	sensei promote <id>                     Promote a candidate to canonical YAML
//	sensei propose --kind <kind> ...        Append one typed feedback entry (scar) and rebuild
//	sensei feedback-check                   Warn when a fix added durable knowledge but no graph feedback
//	sensei ingest --from-file <path>        Feed new knowledge into the graph
//	sensei skill-ingest <skill-pack-root>   Generate review-only candidates from external skills
//	sensei pattern-check <file>...          Check files against pattern recipes
//	sensei version                          Print version and exit
//
// See https://github.com/globulario/sensei for documentation.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var Version = "0.0.1-dev"

// warnIfLegacyAlias prints a deprecation notice when the binary is invoked
// under its pre-rename name ("awg"). The alias is kept for one release so CI
// scripts and muscle memory don't break; the notice goes to stderr so it never
// pollutes stdout pipelines.
func warnIfLegacyAlias() {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if base == "awg" {
		fmt.Fprintln(os.Stderr, "warning: 'awg' is deprecated and will be removed in a future release; use 'sensei' instead")
	}
}

func main() {
	warnIfLegacyAlias()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "demo":
		os.Exit(runDemo(args))
	case "init":
		os.Exit(runInit(args))
	case "bootstrap":
		os.Exit(runBootstrap(args))
	case "import":
		os.Exit(runImport(args))
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
	case "edit-brief":
		os.Exit(runEditBrief(args))
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
	case "domains":
		os.Exit(runDomains(args))
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
	case "architecture-extract":
		os.Exit(runArchitectureExtract(args))
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
	case "benchmark-freeze":
		os.Exit(runBenchmarkFreezeExternal(args))
	case "benchmark-reconstruct":
		os.Exit(runBenchmarkReconstruct(args))
	case "benchmark-evaluate":
		os.Exit(runBenchmarkEvaluateExternal(args))
	case "benchmark-status":
		os.Exit(runBenchmarkStatusExternal(args))
	case "certify":
		os.Exit(runCertify(args))
	case "certify-change":
		os.Exit(runCertifyChange(args))
	case "extract-authority":
		os.Exit(runExtractAuthority(args))
	case "extract-proof-obligations":
		os.Exit(runExtractProofObligations(args))
	case "extract-invariants":
		os.Exit(runExtractInvariants(args))
	case "infer-claims":
		os.Exit(runInferClaims(args))
	case "maintain-claims":
		os.Exit(runMaintainClaims(args))
	case "assess-planes":
		os.Exit(runAssessPlanes(args))
	case "assess-closure":
		os.Exit(runAssessClosure(args))
	case "generate-questions":
		os.Exit(runGenerateQuestions(args))
	case "record-answer":
		os.Exit(runRecordAnswer(args))
	case "adjudicate-answer":
		os.Exit(runAdjudicateAnswer(args))
	case "plan-probes":
		os.Exit(runPlanProbes(args))
	case "record-probe-result":
		os.Exit(runRecordProbeResult(args))
	case "advance-convergence":
		os.Exit(runAdvanceConvergence(args))
	case "convergence-status":
		os.Exit(runConvergenceStatus(args))
	case "bootstrap-direction-digest":
		os.Exit(runBootstrapDirectionDigest(args))
	case "enroll-agent":
		os.Exit(runEnrollAgent(args))
	case "authority-resolve":
		os.Exit(runAuthorityResolve(args))
	case "consume-admission":
		os.Exit(runConsumeAdmission(args))
	case "admit-change":
		os.Exit(dispatchAdmitChange(args))
	case "verify-admission":
		os.Exit(dispatchVerifyAdmission(args))
	case "admission-status":
		os.Exit(runAdmissionStatus(args))
	case "advance-result":
		os.Exit(runAdvanceResult(args))
	case "disposition-question":
		os.Exit(runDispositionQuestion(args))
	case "prepare-change":
		os.Exit(runPrepareChange(args))
	case "task-status":
		os.Exit(runTaskStatus(args))
	case "advance-task":
		os.Exit(runAdvanceTask(args))
	case "task-briefing":
		os.Exit(runTaskBriefing(args))
	case "task-ledger":
		os.Exit(runTaskLedger(args))
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
		fmt.Fprintf(os.Stderr, "sensei: unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `Sensei — architectural awareness for any codebase

Usage: sensei <command> [flags]

Onboard or refresh a repo:
  init           Scaffold awareness for a new project
  import         Learn a repo in one command; --refresh re-extracts an existing checkout
  bootstrap      Advanced extractor stage: deterministic structure + optional history
  build          Compile YAML sources and load into the store
  rebuild        Rebuild self-only awareness.nt (--combined includes paired repo)
  serve          Start the gRPC awareness server
  demo           Stand up a private graph and return one real briefing

Query before editing:
  preflight      Risk classification before editing a file or task
  briefing       Query the graph for a file or task
  edit-brief     Claude Code PreToolUse push: hand the agent a file briefing
  impact         Get structured knowledge nodes for a file
  resolve        Fetch a single awareness node by class + id
  query          Structured browse (by_file | by_id | by_class | related)
  metadata       Show graph-level coverage and freshness
  domains        List selectable graph domains from Metadata

Record or promote a lesson:
  propose        Append one typed feedback entry, rebuild + reload, stage
  feedback-check Warn when a durable fix added no graph feedback
  promote        Promote a candidate into canonical awareness YAML
  ingest         Feed new knowledge into the graph
  skill-ingest   Generate review-only ImplementationPattern candidates from SKILL.md files
  intent-mine    Mine and ground architectural-intent candidates
  cold-bootstrap Advanced miner: history/review candidates
  corpus         Review/hold/never classification for finding reports

Gate or validate a change:
  gate           Hard gate over a git diff (--enforce to block)
  impact-gate    Changed files -> protecting invariants' required_tests
  repair-gate    Fail-closed CI verdict from repair classification or artifact
  runtime-gate   Fail-closed CI/operator gate over a runtime verdict
  contract-assess Report-only contract synthesis assessment
  contract-bootstrap Build a proposed repair-contract bootstrap
  architecture-extract Layer repository evidence into observed/inferred/governed contracts
  check          Validate YAML sources without building
  validate       Deep structural check (dangling refs, missing files, dup IDs)
  validate-draft Validate draft candidates before promotion
  audit          Self-audit for drift, gaps, and inconsistencies
  repo-eval      Evidence-based repository quality evaluation (fix | draft-upgrade)
  merge-check    Verify a PR is merge-authorized; never merges
  edit-check     Warn if a proposed edit violates repo-scoped rules
  pattern-check  Check files against ImplementationPattern recipes
  source-check   Scan source files for structural pattern violations
  visual-audit   Screenshot routes and compare against golden images

Runtime, recovery, and provenance:
  runtime-adapter validate   Validate a runtime-adapter/v1 manifest
  runtime-snapshot validate  Validate a runtime-evidence/v1 snapshot
  cluster-diagnose           Typed runtime verdict from a snapshot
  runtime-repair-report      Validate a runtime repair claim
  runtime-candidate          Turn a recurring runtime verdict into a candidate
  reconcile                  Diff live store against committed seed
  seed-status                Check generated/committed/live seed authority alignment
  governance                 Verify/activate/status for managed-governance packs
  evidence                   Aggregate the gate/guard outcome ledger

Repair and evaluation helpers:
  proof-plan     Show required proof/forbidden-move checklist before editing
  repair-plan    Build an authoritative governed repair plan
  repair-report  Emit the governed post-edit repair report artifact
  draft-candidate Draft an incident/finding/scar into a review-queue candidate
  benchmark-brief Build a compact repair envelope for benchmark/PR fixing
  benchmark-judge Judge a patch envelope for contract/test discipline
  benchmark-score Standard brief->judge benchmark workflow and combined score
  benchmark-retry Build a reusable benchmark retry plan from run evidence
  benchmark-event-meta Read orchestration metadata from benchmark learning events
  benchmark-freeze Freeze an external cold-start benchmark workspace
  benchmark-reconstruct Reconstruct bounded benchmark state from a blind workspace
  benchmark-evaluate Reveal oracle receipts and produce a categorical report
  benchmark-status Print compact external benchmark state
  certify        Legacy benchmark repair-claim verdict (not architectural closure)
  certify-change Architectural-closure certification over a verified task ledger
  extract-invariants Extract normalized facts and review-only invariant candidates
  infer-claims   Derive offline ArchitectureClaim candidates from normalized facts
  maintain-claims Recalculate offline ArchitectureClaim status from explicit proof
  assess-planes  Verify ArchitectureClaim architectural-plane basis offline
  assess-closure Evaluate bounded architectural closure from explicit artifacts
  generate-questions Generate offline OpenQuestion candidates from closure blockers
  record-answer  Record an exact architect answer offline
  adjudicate-answer Adjudicate a recorded architect answer offline
  plan-probes    Generate offline EvidenceProbe plans from evidence questions
  record-probe-result Record an externally executed probe result offline
  advance-convergence Advance one offline convergence session iteration
  convergence-status Inspect an offline convergence session bundle
  bootstrap-direction-digest Compute canonical digest for a bootstrap direction authorization
  admit-change   Evaluate bounded agent admission from a convergence bundle
  verify-admission Verify a working-tree diff against an admission envelope
  admission-status Inspect admission and scope-verification receipts
  prepare-change Create or refresh one active architectural task session
  task-status    Inspect an active architectural task session
  advance-task   Execute safe static evidence and advance one task iteration
  task-briefing  Show bounded file context for an active task
  task-ledger    Verify, import, and rebuild append-only task ledgers
  extract-authority Extract candidate authority surfaces from Go code
  extract-proof-obligations Generate proof obligations from authority surfaces

Other:
  version        Print version and exit

Run "sensei <command> --help" for details on each command.
`)
}
