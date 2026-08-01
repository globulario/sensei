// SPDX-License-Identifier: AGPL-3.0-only

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
//	sensei complete-task                    Delegate terminal completion to the Phase-8 owner (thin invocation surface)
//	sensei inspect-terminal                 Reconstruct a task's honest terminal state (read-only surface)
//	sensei recover-projections              Rebuild stale/missing derived projections from a valid conjunction
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

	os.Exit(dispatch(os.Args[1], os.Args[2:]))
}

// dispatch resolves one Sensei subcommand to its exit code. Extracted
// from main so tests can re-exec the compiled test binary as a real
// "sensei" process (env-var-gated) without a separate `go build`.
func dispatch(cmd string, args []string) int {
	switch cmd {

	case "demo":
		return runDemo(args)
	case "init":
		return runInit(args)
	case "bootstrap":
		return runBootstrap(args)
	case "import":
		return runImport(args)
	case "onboard":
		return runOnboard(args)
	case "build":
		return runBuild(args)
	case "scip-ingest":
		return runScipIngest(args)
	case "protection-status":
		return runProtectionStatus(args)
	case "protection-check":
		return runProtectionCheck(args)
	case "repo-domain":
		return runRepoDomain(args)
	case "serve":
		return runServe(args)
	case "briefing":
		return runBriefing(args)
	case "impact":
		return runImpact(args)
	case "preflight":
		return runPreflight(args)
	case "contract-assess":
		return runContractAssess(args)
	case "contract-bootstrap":
		return runContractBootstrap(args)
	case "edit-check":
		return runEditCheck(args)
	case "edit-guard":
		return runEditGuard(args)
	case "edit-brief":
		return runEditBrief(args)
	case "rigor":
		return runRigor(args)
	case "gate":
		return runGate(args)
	case "produce-change-binding":
		return runProduceChangeBinding(args)
	case "evidence":
		return runEvidence(args)
	case "resolve":
		return runResolve(args)
	case "query":
		return runQuery(args)
	case "metadata":
		return runMetadata(args)
	case "domains":
		return runDomains(args)
	case "governance":
		return runGovernance(args)
	case "check":
		return runCheck(args)
	case "validate":
		return runValidate(args)
	case "audit":
		return runAudit(args)
	case "merge-check":
		return runMergeCheck(args)
	case "runtime-adapter":
		return runRuntimeAdapter(args)
	case "runtime-snapshot":
		return runRuntimeSnapshot(args)
	case "cluster-diagnose":
		return runClusterDiagnose(args)
	case "runtime-repair-report":
		return runRuntimeRepairReport(args)
	case "runtime-gate":
		return runRuntimeGate(args)
	case "runtime-candidate":
		return runRuntimeCandidate(args)
	case "suggest-realizations":
		return runSuggestRealizations(args)
	case "promote-realization":
		return runPromoteRealization(args)
	case "review-realization":
		return runReviewRealization(args)
	case "repo-eval":
		return runRepoEval(args)
	case "architecture-extract":
		return runArchitectureExtract(args)
	case "dashboard-projection":
		return runDashboardProjection(args)
	case "benchmark-brief":
		return runBenchmarkBrief(args)
	case "benchmark-judge":
		return runBenchmarkJudge(args)
	case "benchmark-score":
		return runBenchmarkScore(args)
	case "benchmark-retry":
		return runBenchmarkRetry(args)
	case "benchmark-event-meta":
		return runBenchmarkEventMeta(args)
	case "benchmark-freeze":
		return runBenchmarkFreezeExternal(args)
	case "benchmark-reconstruct":
		return runBenchmarkReconstruct(args)
	case "benchmark-evaluate":
		return runBenchmarkEvaluateExternal(args)
	case "benchmark-status":
		return runBenchmarkStatusExternal(args)
	case "certify":
		return runCertify(args)
	case "certify-change":
		return runCertifyChange(args)
	case "complete-task":
		return runCompleteTask(args)
	case "inspect-terminal":
		return runInspectTerminal(args)
	case "recover-projections":
		return runRecoverProjections(args)
	case "extract-authority":
		return runExtractAuthority(args)
	case "extract-proof-obligations":
		return runExtractProofObligations(args)
	case "extract-invariants":
		return runExtractInvariants(args)
	case "infer-claims":
		return runInferClaims(args)
	case "maintain-claims":
		return runMaintainClaims(args)
	case "assess-planes":
		return runAssessPlanes(args)
	case "assess-closure":
		return runAssessClosure(args)
	case "generate-questions":
		return runGenerateQuestions(args)
	case "record-answer":
		return runRecordAnswer(args)
	case "adjudicate-answer":
		return runAdjudicateAnswer(args)
	case "plan-probes":
		return runPlanProbes(args)
	case "record-probe-result":
		return runRecordProbeResult(args)
	case "advance-convergence":
		return runAdvanceConvergence(args)
	case "convergence-status":
		return runConvergenceStatus(args)
	case "bootstrap-direction-digest":
		return runBootstrapDirectionDigest(args)
	case "enroll-agent":
		return runEnrollAgent(args)
	case "authority-resolve":
		return runAuthorityResolve(args)
	case "consume-admission":
		return runConsumeAdmission(args)
	case "admit-change":
		return dispatchAdmitChange(args)
	case "verify-admission":
		return dispatchVerifyAdmission(args)
	case "admission-status":
		return runAdmissionStatus(args)
	case "advance-result":
		return runAdvanceResult(args)
	case "disposition-question":
		return runDispositionQuestion(args)
	case "promote-answer":
		return runPromoteAnswer(args)
	case "prepare-change":
		return runPrepareChange(args)
	case "task-status":
		return runTaskStatus(args)
	case "advance-task":
		return runAdvanceTask(args)
	case "task-briefing":
		return runTaskBriefing(args)
	case "task-ledger":
		return runTaskLedger(args)
	case "proof-plan":
		return runProofPlan(args)
	case "repair-plan":
		return runRepairPlan(args)
	case "seed-status":
		return runSeedStatus(args)
	case "reconcile":
		return runReconcile(args)
	case "draft-candidate":
		return runDraftCandidate(args)
	case "impact-gate":
		return runImpactGate(args)
	case "repair-report":
		return runRepairReport(args)
	case "repair-gate":
		return runRepairGate(args)
	case "seed-freshness":
		return runSeedFreshness(args)
	case "rebuild":
		return runRebuild(args)
	case "learn":
		return runLearn(args)
	case "lifecycle":
		return runLifecycle(args)
	case "promote":
		return runPromote(args)
	case "propose":
		return runPropose(args)
	case "feedback-check":
		return runFeedbackCheck(args)
	case "ingest":
		return runIngest(args)
	case "skill-ingest":
		return runSkillIngest(args)
	case "pattern-check":
		return runPatternCheck(args)
	case "source-check":
		return runSourceCheck(args)
	case "visual-audit":
		return runVisualAudit(args)
	case "cold-bootstrap":
		return runColdBootstrap(args)
	case "validate-draft":
		return runValidateDraft(args)
	case "intent-mine":
		return runIntentMine(args)
	case "corpus":
		return runCorpus(args)
	case "version":
		fmt.Println(Version)
		return 0
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "sensei: unknown command %q\n\n", cmd)
		printUsage()
		return 2
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
  dashboard-projection Build a sensei.dashboard.projection.v1 document from the authored corpus
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
  evidence                   Aggregate ledger evidence or snapshot/import/inspect Phase 10 evidence
  investigate                Run or inspect deterministic Phase 10 investigations
  candidates                 List, show, or record governed review of investigation candidates

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
  complete-task  Delegate terminal completion to the Phase-8 owner (thin invocation surface)
  inspect-terminal Reconstruct a task's honest terminal state (read-only surface)
  recover-projections Rebuild stale/missing derived projections from a valid conjunction
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
