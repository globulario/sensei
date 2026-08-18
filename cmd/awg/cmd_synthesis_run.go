// SPDX-License-Identifier: AGPL-3.0-only

// cmd_synthesis_run.go wires golang/architecture/synthesisdriver.Run to a
// CLI surface -- the gap docs/design/archer-integration-closure.md's
// "Current state vs. this contract" section names as gap #1 ("No CLI
// surface exists"). This command composes only existing owners; it invents
// no new architectural authority. It stops at candidate-ready-for-admission
// or a governed terminal/stopped/step-limit disposition and never touches
// admission or application (golang/architecture/admissioncomposition,
// golang/architecture/candidateapply) -- that stays a deliberate, separate
// human step via the existing admit-change/verify-admission commands.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/cognitivecommand"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver"
	"github.com/globulario/sensei/golang/architecture/synthesisdriver/fileinterpretation"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

func runSynthesisRun(args []string) int {
	fs := flag.NewFlagSet("sensei synthesis-run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	repoFlag := fs.String("repo", ".", "repository checkout")
	addr := fs.String("addr", defaultServiceAddr(), "Sensei gRPC server address")
	taskFlag := fs.String("task", "", "task directory (default: the active task from .sensei/tasks/active.yaml)")
	interpretationPath := fs.String("interpretation", "", "path to an authored synthesis.Interpretation JSON file (required)")
	interpretationChallengePath := fs.String("interpretation-challenge", "", "optional query-only interpretation challenge JSON; Go probes are executed against the bound checkout before O1 planning")
	objectiveFlag := fs.String("objective", "", "session objective (default: the task's own recorded description)")
	retryBudget := fs.Int("retry-budget", 0, "O1 Session.RetryBudget")
	replanBudget := fs.Int("replan-budget", 0, "O1 Session.ReplanBudget")
	maxSteps := fs.Int("max-steps", 20, "O7 step limit")
	agentFlag := fs.String("agent", "", "vendor CLI to drive: codex or claude (required)")
	agentCommand := fs.String("agent-command", "", "absolute path to the installed codex/claude binary (required; no PATH lookup)")
	agentWorkdir := fs.String("agent-workdir", "", "empty, absolute directory for the vendor subprocess (default: a fresh temp dir)")
	var agentEnv multiString
	fs.Var(&agentEnv, "agent-env", "environment variable NAME to allowlist through to the vendor CLI (repeatable)")
	gatePolicy := fs.String("gate-policy", "", "path to the O4 gate policy (default: <repo>/.sensei/gate-policy.yaml)")
	senseiExecutable := fs.String("sensei-executable", "", "absolute path to the sensei binary the gate evaluator invokes (default: the running binary)")
	candidateStoreDir := fs.String("candidate-store", "", "directory for sealed candidates (default: <taskDir>/synthesis-run/candidates)")
	evidenceStoreDir := fs.String("evidence-store", "", "directory for evaluator evidence (default: <taskDir>/synthesis-run/evidence)")
	deadlineMinutes := fs.Int("deadline-minutes", 10, "minutes from now used as the shared deadline for every policy")
	maxObservationCount := fs.Int("max-observation-count", 32, "provider observation count bound")
	maxObservationBytes := fs.Int("max-observation-bytes", 65536, "provider observation byte bound")
	maxSnapshotBytes := fs.Int("max-snapshot-bytes", 1<<20, "O3 generation snapshot byte bound")
	maxStdoutBytes := fs.Int64("max-stdout-bytes", 4<<20, "vendor subprocess stdout byte bound")
	maxStderrBytes := fs.Int64("max-stderr-bytes", 1<<20, "vendor subprocess stderr byte bound")
	maxStructuredPayloadBytes := fs.Int("max-structured-payload-bytes", 1<<20, "vendor subprocess structured-output byte bound")
	forceUnconverged := fs.Bool("force-unconverged", false, "proceed even though the task's control state has an active primary blocker")
	forceThinCoverage := fs.Bool("force-thin-coverage", false, "proceed even though workspace identity coverage is not sufficient (e.g. a freshly-onboarded benchmark checkout); refused if identity is incomplete for any other reason (revision unresolved, graph not authoritative)")
	format := fs.String("format", "text", "output format: text | json")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei synthesis-run --interpretation <path.json> --agent codex|claude --agent-command <abs path> [flags]

Drives one synchronous O1-O8 governed synthesis session
(golang/architecture/synthesisdriver.Run) against an already-prepared task:
interpretation -> planning -> O3 generation -> O4 evaluation, stopping at
candidate-ready-for-admission or a governed terminal/stopped/step-limit
disposition.

This command NEVER admits, applies, commits, pushes, or merges anything.
A sealed candidate is only ever a proposal on disk. It persists an
admission lineage bundle alongside the candidate; 'sensei synthesis-admit'
reads that bundle and derives the O5 admission request from it, and
'sensei admit-change' then decides that request. Both remain separate,
deliberate steps.

Requires a repository with served graph authority and an already-prepared
task (run 'sensei prepare-change' first) -- this command creates neither.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitInvalidInvocation
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: unexpected argument %q\n", fs.Arg(0))
		return exitInvalidInvocation
	}
	if strings.TrimSpace(*interpretationPath) == "" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-run: --interpretation is required")
		return exitInvalidInvocation
	}
	if *agentFlag != "codex" && *agentFlag != "claude" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-run: --agent must be \"codex\" or \"claude\"")
		return exitInvalidInvocation
	}
	if strings.TrimSpace(*agentCommand) == "" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-run: --agent-command is required")
		return exitInvalidInvocation
	}
	if !filepath.IsAbs(*agentCommand) {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: --agent-command must be an absolute path, got %q (no PATH lookup is performed -- pass \"$(command -v %s)\")\n", *agentCommand, *agentFlag)
		return exitResolutionFailure
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-run: --format must be \"text\" or \"json\"")
		return exitInvalidInvocation
	}

	absRepo, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: resolve --repo: %v\n", err)
		return exitResolutionFailure
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*deadlineMinutes+5)*time.Minute)
	defer cancel()

	// --- step 1: resolve the task directory ---
	taskDir := strings.TrimSpace(*taskFlag)
	if taskDir == "" {
		ptr, err := tasksession.LoadActivePointer(absRepo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: no active task and --task not given; run 'sensei prepare-change' first: %v\n", err)
			return exitResolutionFailure
		}
		taskDir = filepath.Dir(ptr.SessionPath)
	} else if !filepath.IsAbs(taskDir) {
		taskDir = filepath.Join(absRepo, taskDir)
	}

	// --- step 2: load the task session (self digest-verifying) ---
	taskSession, err := tasksession.LoadSession(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: load task session: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 3: refuse an unconverged task unless overridden, and resolve
	// the closure report and admission decision this task's control
	// readiness was itself derived from -- all three from
	// tasksession.ResolveControlAndClosure's single currentControlPaths
	// resolution, never independent calls or a fixed prepare-time path. A
	// concurrent `sensei advance-task` publishes a new generation as two
	// separate, non-atomic writes (control/latest.yaml, then
	// control/latest-generation.yaml); two independently-resolved reads can
	// observe the pointer move in between and bind readiness and closure
	// digest to a pair that never coexisted as one real generation. A live
	// review also found that reading the admission decision from a fixed
	// taskDir/admission/decision.yaml path (rather than this same
	// generation-scoped resolution) could check a stale, prepare-time
	// decision even after `sensei advance-task` recomputed a current one
	// declaring real proof obligations the stale decision never had. ---
	control, closureReport, taskDecision, err := tasksession.ResolveControlAndClosure(absRepo, taskDir, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: resolve task control state and closure report (run 'sensei advance-task' to converge closure first): %v\n", err)
		return exitResolutionFailure
	}
	// A stale binding is not necessarily represented by PrimaryBlocker; see
	// validateCurrentBinding's own doc comment. Refused unconditionally,
	// never something --force-unconverged is meant to bypass.
	if err := validateCurrentBinding(control); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitResolutionFailure
	}
	if control.PrimaryBlocker != nil && !*forceUnconverged {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: task has an active primary blocker (%s); pass --force-unconverged to proceed anyway\n", control.PrimaryBlocker.Statement)
		return exitResolutionFailure
	}

	// --- step 4: compose workspace identity via a live Metadata RPC ---
	identity, err := composeSynthesisRunIdentity(ctx, *addr, absRepo, taskDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: compose workspace identity: %v\n", err)
		return exitResolutionFailure
	}
	if identity.CompositionState != workspacecontract.CompositionComplete {
		if *forceThinCoverage && identityPartialOnlyForThinCoverage(identity) {
			fmt.Fprintln(os.Stderr, "sensei synthesis-run: WARNING: proceeding with --force-thin-coverage; workspace identity is partial (coverage is not sufficient) and this run's evidence will honestly carry that:")
			for _, l := range identity.Limitations {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", l.Scope, l.Reason)
			}
		} else {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: workspace identity is %s, not complete:\n", identity.CompositionState)
			for _, l := range identity.Limitations {
				fmt.Fprintf(os.Stderr, "  - %s: %s\n", l.Scope, l.Reason)
			}
			if identityPartialOnlyForThinCoverage(identity) {
				fmt.Fprintln(os.Stderr, "sensei synthesis-run: pass --force-thin-coverage to proceed anyway (e.g. a freshly-onboarded benchmark checkout)")
			}
			return exitResolutionFailure
		}
	}
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: digest workspace identity: %v\n", err)
		return exitResolutionFailure
	}
	if identity.Binding.Revision == nil {
		fmt.Fprintln(os.Stderr, "sensei synthesis-run: composed identity is complete but carries no revision")
		return exitResolutionFailure
	}
	baseRevision := *identity.Binding.Revision

	// --- step 5: closure digest, from the closure report step 3 already
	// resolved atomically alongside control readiness ---
	closureDigest, err := closureprotocol.SemanticDigest(closureReport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: digest closure report: %v\n", err)
		return exitResolutionFailure
	}

	// Optional Gate-1 challenge input contains queries only. It cannot carry
	// supported/contradicted/authority outcomes; the production authority
	// executes these probes against absRepo and recomputes the plan digest.
	var challengePlan interpretationclosure.ChallengePlan
	var challengePlanDigest string
	if challengePath := strings.TrimSpace(*interpretationChallengePath); challengePath != "" {
		if !filepath.IsAbs(challengePath) {
			challengePath = filepath.Join(absRepo, challengePath)
		}
		challengePlan, challengePlanDigest, err = interpretationclosure.LoadChallengePlan(challengePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: load --interpretation-challenge: %v\n", err)
			return exitResolutionFailure
		}
	}

	interpretationAuthority, err := synthesisdriver.NewClosureReportAuthority(synthesisdriver.ClosureReportAuthorityConfig{
		Report:                    closureReport,
		GoProbes:                  challengePlan.GoProbes,
		ChallengePlanDigestSHA256: challengePlanDigest,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct interpretation authority: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 6: file-backed interpretation provider, and the two
	// preconditions a real review found this command previously let slide
	// silently: the authored interpretation's objective must match the
	// session objective exactly (never two silently divergent identities
	// for what drove generation vs. what the receipt records), and the
	// authored interpretation must declare zero required proof obligations
	// (no production EvidenceResolver exists yet to bind any declared
	// obligation to a verified discharge digest, so a non-empty declaration
	// cannot proceed -- refusing beats silently discarding it). ---
	now := time.Now().UTC().Format(time.RFC3339)
	interpretationProvider, err := fileinterpretation.New(fileinterpretation.Config{
		Path:       *interpretationPath,
		ProviderID: "sensei-synthesis-run.o2.file",
		ObservedAt: now,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct interpretation provider: %v\n", err)
		return exitResolutionFailure
	}

	objective, err := resolveSynthesisRunObjective(*objectiveFlag, taskSession.TaskRequest.Description, interpretationProvider.Objective())
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitResolutionFailure
	}

	if err := validateNoRequiredProofObligations(interpretationProvider.RequiredProofObligations()); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitResolutionFailure
	}

	// The task's own admission decision -- already resolved above, from
	// the same generation snapshot as control/closure (never a separately
	// re-read, possibly stale, path) -- is authoritative. The
	// interpretation file's own (empty) declaration checked above is
	// necessary but not sufficient: it must never be read as clearing or
	// overriding obligations the decision already recorded.
	if err := validateNoDecisionProofObligations(taskDecision.ProofObligations); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 7: build the O1 Session and its SessionState ---
	deadline := time.Now().Add(time.Duration(*deadlineMinutes) * time.Minute).UTC().Format(time.RFC3339)

	session := synthesis.NormalizeSession(synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.synthesis-run." + taskSession.TaskID,
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              identity.Binding.RepositoryDomain,
		BaseRevision:                  baseRevision,
		WorkspaceIdentityDigestSHA256: identityDigest,
		// The same graph-.nt digest concept closureprotocol.GraphSnapshot
		// references -- a new mapping with no prior authored contract; see
		// the plan's "known limitations" note. No production code
		// constructed synthesis.Session before this command.
		GraphAuthorityDigestSHA256: taskSession.Binding.GraphDigestSHA256,
		TaskSessionDigestSHA256:    taskSession.SessionDigestSHA256,
		ClosureDigestSHA256:        closureDigest,
		// Empty here means exactly one thing: the accepted authored
		// interpretation validated above declared zero required proof
		// obligations. It does not mean, and must never be read as meaning,
		// "Sensei searched every authority surface and found none" -- no
		// such search happens. Task-level obligation discovery beyond the
		// authored interpretation is currently unavailable; see the step 6
		// refusal above for the non-empty case this command refuses to run.
		ProofObligationDigests: []string{},
		Objective:              objective,
		RetryBudget:            *retryBudget,
		ReplanBudget:           *replanBudget,
		CreatedAt:              time.Now().UTC().Format(time.RFC3339),
	})
	sessionDigest, err := synthesis.SessionDigest(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: digest session: %v\n", err)
		return exitResolutionFailure
	}
	session.SessionDigestSHA256 = sessionDigest
	initialState, err := synthesis.NewSessionState(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct session state: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 8: vendor subprocess agents (O3 generation, O8 planning) ---
	generationWorkdir, planningWorkdir, cleanupWorkdirs, err := resolveAgentWorkdirs(*agentWorkdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitResolutionFailure
	}
	defer cleanupWorkdirs()

	baseAgentConfig := agentcommand.CommandAgentConfig{
		Command:                   *agentCommand,
		EnvironmentAllowlist:      []string(agentEnv),
		MaxStdoutBytes:            *maxStdoutBytes,
		MaxStderrBytes:            *maxStderrBytes,
		MaxStructuredPayloadBytes: *maxStructuredPayloadBytes,
		MaxMutationPlanBytes:      *maxStructuredPayloadBytes,
	}

	genConfig := baseAgentConfig
	genConfig.WorkDir = generationWorkdir
	planConfig := baseAgentConfig
	planConfig.WorkDir = planningWorkdir

	var genAgent agentcommand.Agent
	var planAgent agentcommand.StructuredAgent
	switch *agentFlag {
	case "codex":
		genAgent, err = agentcommand.NewCodexAgent(genConfig)
		if err == nil {
			planAgent, err = agentcommand.NewCodexStructuredAgent(planConfig)
		}
	case "claude":
		genAgent, err = agentcommand.NewClaudeAgent(genConfig)
		if err == nil {
			planAgent, err = agentcommand.NewClaudeStructuredAgent(planConfig)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct %s agent: %v\n", *agentFlag, err)
		return exitResolutionFailure
	}

	generationFactory, err := agentcommand.NewFactory(agentcommand.Config{
		Agent:            genAgent,
		ProviderID:       "sensei-synthesis-run.o3." + *agentFlag,
		ProviderKind:     "agent-command",
		ModelIdentifier:  *agentFlag,
		ObservedAt:       now,
		ProducedAt:       now,
		MaxSnapshotBytes: *maxSnapshotBytes,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct generation factory: %v\n", err)
		return exitResolutionFailure
	}

	planningProvider, err := cognitivecommand.New(cognitivecommand.Config{
		Agent:               planAgent,
		ProviderID:          "sensei-synthesis-run.o8." + *agentFlag,
		ProviderKind:        "cognitive-command",
		ModelIdentifier:     *agentFlag,
		ObservedAt:          now,
		SupportedOperations: []providerport.Operation{providerport.OperationPlanning},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct planning provider: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 9: O4 engine, wired to the real `sensei gate` evaluator ---
	// NewFSCandidateArtifactStore and NewFSEvidenceSink both require an
	// absolute root; resolveStoreDir's own doc comment explains why a
	// caller-supplied relative value must be resolved here rather than
	// left to fail deep inside those constructors.
	*candidateStoreDir = resolveStoreDir(*candidateStoreDir, absRepo, filepath.Join(taskDir, "synthesis-run", "candidates"))
	*evidenceStoreDir = resolveStoreDir(*evidenceStoreDir, absRepo, filepath.Join(taskDir, "synthesis-run", "evidence"))
	if err := os.MkdirAll(*candidateStoreDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: create candidate store dir: %v\n", err)
		return exitResolutionFailure
	}
	if err := os.MkdirAll(*evidenceStoreDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: create evidence store dir: %v\n", err)
		return exitResolutionFailure
	}
	candidateStore, err := runnercomposition.NewFSCandidateArtifactStore(*candidateStoreDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct candidate store: %v\n", err)
		return exitResolutionFailure
	}
	evidenceSink, err := evaluatorcomposition.NewFSEvidenceSink(*evidenceStoreDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct evidence sink: %v\n", err)
		return exitResolutionFailure
	}
	materializer, err := evaluatorcomposition.NewCandidateMaterializer(identity.Binding.RepositoryDomain, absRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: construct candidate materializer: %v\n", err)
		return exitResolutionFailure
	}

	policyPath := strings.TrimSpace(*gatePolicy)
	if policyPath == "" {
		policyPath = filepath.Join(absRepo, ".sensei", "gate-policy.yaml")
	}
	if !filepath.IsAbs(policyPath) {
		policyPath, err = filepath.Abs(policyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: resolve --gate-policy: %v\n", err)
			return exitResolutionFailure
		}
	}
	senseiExe := strings.TrimSpace(*senseiExecutable)
	if senseiExe == "" {
		senseiExe, err = os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: resolve running sensei binary: %v\n", err)
			return exitResolutionFailure
		}
	}

	const gateEvaluatorID = "sensei.gate"
	// gateCheckID is SenseiGateEvaluator's own hardcoded CheckObservation.CheckID
	// (golang/architecture/evaluatorcomposition/sensei_gate.go's
	// SupportedCheckIDs/observation) -- a distinct string from EvaluatorID
	// above, discovered via a real dogfood run: RequiredCheckIDs must match
	// this check ID, not the evaluator ID, or a real passing gate result
	// (0 blocking findings) still gets misreported as "required check ...
	// is missing" and aborted.
	const gateCheckID = "sensei-gate"
	gateBinding := synthesisdriver.EvaluatorBinding{
		EvaluatorID: gateEvaluatorID,
		SurfaceMode: evaluatorcomposition.SurfaceModeGitDiff,
		New: func(surface evaluatorcomposition.EvaluatorSurface) (evaluatorcomposition.Evaluator, error) {
			return evaluatorcomposition.NewSenseiGateEvaluator(evaluatorcomposition.SenseiGateConfig{
				EvaluatorID:      gateEvaluatorID,
				EvaluatorVersion: "v1",
				SenseiExecutable: senseiExe,
				Address:          *addr,
				PolicyPath:       policyPath,
				TotalTimeout:     5 * time.Minute,
				RPCTimeout:       30 * time.Second,
			}, surface, evaluatorcomposition.OSCommandRunner{}, evidenceSink)
		},
	}

	engine := &synthesisdriver.O4Engine{
		Store:        candidateStore,
		Materializer: materializer,
		Evaluators:   []synthesisdriver.EvaluatorBinding{gateBinding},
		Now:          time.Now,
		PolicyFactory: synthesisdriver.EvaluationPolicyFactoryFunc(func(_ context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.EvaluationPolicy, error) {
			if handoff.RunnerReceipt.CandidateArtifactDigestSHA256 == nil {
				return evaluatorcomposition.EvaluationPolicy{}, errors.New("sensei synthesis-run: runner receipt carries no candidate artifact digest")
			}
			if handoff.Result.GenerationPayload == nil {
				return evaluatorcomposition.EvaluationPolicy{}, errors.New("sensei synthesis-run: generation result carries no attempt payload")
			}
			policy := evaluatorcomposition.EvaluationPolicy{
				SchemaVersion:                 evaluatorcomposition.EvaluationPolicySchemaVersion,
				PolicyID:                      "policy.synthesis-run." + taskSession.TaskID,
				SessionDigestSHA256:           state.Session.SessionDigestSHA256,
				AttemptDigestSHA256:           handoff.Result.GenerationPayload.AttemptDigestSHA256,
				CandidateArtifactDigestSHA256: *handoff.RunnerReceipt.CandidateArtifactDigestSHA256,
				Evaluators: []evaluatorcomposition.EvaluatorSpec{
					{EvaluatorID: gateEvaluatorID, Required: true},
				},
				DeadlineAt:       deadline,
				MaxEvidenceCount: *maxObservationCount,
				MaxEvidenceBytes: int64(*maxObservationBytes),
				RequiredCheckIDs: []string{gateCheckID},
				FailureClassRecommendations: []evaluatorcomposition.FailureClassRecommendation{
					// A blocking gate finding indicates a scope/architecture
					// violation, not a transient generation defect --
					// retrying would very likely just repeat it and burn
					// API credits, so this v1 default is conservative:
					// abort rather than retry/replan.
					{FailureClass: "sensei-gate-blocking-finding", Recommendation: synthesis.RecommendAbort},
					// FailureClassRequiredCheckUnsatisfied is always a live
					// possibility once RequiredCheckIDs names the gate's own
					// check below -- discovered via a real dogfood run
					// (composition failed with "has no precommitted
					// recommendation") when the gate reported a non-passing
					// check. Same conservative default as above.
					{FailureClass: evaluatorcomposition.FailureClassRequiredCheckUnsatisfied, Recommendation: synthesis.RecommendAbort},
					// The remaining two package-level failure classes are
					// live regardless of RequiredCheckIDs/Evaluators
					// configuration (evidence-completeness and limitation
					// blocking are evaluated unconditionally in Compose) --
					// precommitted here up front rather than discovered one
					// real run at a time, same conservative default.
					{FailureClass: evaluatorcomposition.FailureClassIncompleteObservation, Recommendation: synthesis.RecommendAbort},
					{FailureClass: evaluatorcomposition.FailureClassBlockingLimitation, Recommendation: synthesis.RecommendAbort},
				},
			}
			digest, err := evaluatorcomposition.EvaluationPolicyDigest(policy)
			if err != nil {
				return evaluatorcomposition.EvaluationPolicy{}, err
			}
			policy.PolicyDigestSHA256 = digest
			return policy, nil
		}),
	}

	// --- step 10: build Config and run ---
	config := synthesisdriver.Config{
		WorkspaceIdentity:       identity,
		RepositoryRoot:          absRepo,
		CandidateStore:          candidateStore,
		InterpretationProvider:  interpretationProvider,
		InterpretationAuthority: interpretationAuthority,
		PlanningProvider:        planningProvider,
		GenerationFactory:       generationFactory,
		EvaluationEngine:        engine,
		InterpretationPolicy: synthesisdriver.ProviderPolicy{
			DeadlineAt:          deadline,
			MaxObservationCount: *maxObservationCount,
			MaxObservationBytes: *maxObservationBytes,
		},
		PlanningPolicy: synthesisdriver.ProviderPolicy{
			DeadlineAt:          deadline,
			MaxObservationCount: *maxObservationCount,
			MaxObservationBytes: *maxObservationBytes,
		},
		GenerationPolicy: runnercomposition.RequestPolicy{
			DeadlineAt:          deadline,
			MaxObservationCount: *maxObservationCount,
			MaxObservationBytes: *maxObservationBytes,
		},
		MaxSteps: *maxSteps,
		Now:      time.Now,
	}

	result, err := synthesisdriver.Run(ctx, initialState, config)
	if err != nil {
		// Run() itself only returns a Go error for a malformed Config or a
		// caller-supplied SessionState it should never have been asked to
		// drive -- everything a governed session can legitimately end in
		// (a provider declining, a runner stopping, a terminal O1 failure,
		// the step limit) comes back as a typed Result/Disposition instead,
		// per driver.go's own contract. Reaching here means something is
		// wrong with this command's own construction, not with the run --
		// exitInternalDefect, not one of the resolution-failure exits above.
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: %v\n", err)
		return exitInternalDefect
	}

	lineagePath, err := persistAdmissionLineage(ctx, result, candidateStore, *candidateStoreDir,
		synthesisRunTaskBinding{
			TaskID:                       taskSession.TaskID,
			TaskControlStateDigestSHA256: taskcontrol.StateDigest(control),
			// Reuses the SAME closureDigest step 5 already computed and the
			// run itself was bound to -- recomputing it here could bind the
			// receipt to a different value than the session actually used.
			ClosureReportDigestSHA256: closureDigest,
		})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-run: persist admission lineage: %v\n", err)
		return exitInternalDefect
	}

	// If a candidate was sealed, the report below names CandidatePath and
	// LineagePath by joining *candidateStoreDir (a path STRING captured
	// before this potentially long-running O3/O4 work started) with a
	// filename. candidateStore/persistAdmissionLineage themselves always
	// write through the store's stable, rename-immune root, so the
	// content is safely wherever that root actually is -- but if another
	// process renamed or replaced --candidate-store's original directory
	// while this run was in progress, the STRING *candidateStoreDir no
	// longer names that directory, and reporting success at that path
	// would point automation at a missing or unrelated (possibly
	// attacker-controlled) location. Verify before reporting rather than
	// reporting a path that might be wrong.
	if result.Receipt.CandidateArtifactDigestSHA256 != nil {
		if err := candidateStore.VerifyRootIdentity(*candidateStoreDir); err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-run: candidate sealed, but its reported location cannot be trusted: %v\n", err)
			return exitInternalDefect
		}
	}

	printSynthesisRunResult(result, taskSession.TaskID, *candidateStoreDir, lineagePath, *maxSteps, *format)
	return exitCodeForDisposition(result.Receipt.Disposition)
}

// synthesisRunLineage is the complete O5 admission input chain a
// candidate-ready run produces: the full synthesis/runner/evaluation
// receipt documents (not merely their digests, which RunReceipt already
// carries) and the candidate's own identity. A real review found that
// without this, the sealed candidate artifact this command already
// persists separately (<candidateStoreDir>/<digest>.json) was reachable,
// but the receipt DOCUMENTS admissioncomposition.ComposeInput actually
// requires (SynthesisReceipt, RunnerReceipt, EvaluationReceipt -- full
// objects, not digests) existed only in-memory and vanished when this
// process exited, leaving no durable way for a caller directing the
// operator to `sensei admit-change` to actually construct that input.
//
// AdmissionTemplate (the fourth ComposeInput field) and BaseManifest are
// deliberately NOT persisted here: both are the admission step's own
// inputs to author/derive at admission time (an explicit request template
// a human or admit-change constructs, and a base-revision manifest more
// honestly computed fresh from git at admission time than frozen here) --
// persisting them would overstep this command's own stated boundary
// (stops at candidate-ready, never constructs or presumes admission
// inputs). `sensei synthesis-admit` (cmd_synthesis_admit.go) supplies both
// at the moment it composes, resolving the template from the task's
// current generation and re-reading the base tree from git.
type synthesisRunLineage struct {
	SchemaVersion                 string                                 `json:"schema_version"`
	CandidateArtifactDigestSHA256 string                                 `json:"candidate_artifact_digest_sha256"`
	CandidateArtifactPath         string                                 `json:"candidate_artifact_path"`
	SynthesisReceipt              synthesis.Receipt                      `json:"synthesis_receipt"`
	RunnerReceipt                 runnercomposition.RunnerReceipt        `json:"runner_receipt"`
	EvaluationReceipt             evaluatorcomposition.EvaluationReceipt `json:"evaluation_receipt"`
	TaskBinding                   synthesisRunTaskBinding                `json:"task_binding"`
}

// synthesisRunTaskBinding is what the later steps need in order to notice that
// the world moved after the candidate was sealed.
//
// Issue #149 hard law 10 requires refusing task, closure, and base-revision
// drift when a run is resumed. The resumption that actually happens today is
// `sensei synthesis-admit` and `sensei synthesis-apply` picking up a persisted
// bundle minutes or days later -- and base-revision drift was the only one of
// the three they could detect, because the bundle recorded the candidate's git
// base and nothing about the governance state the run was bound to.
//
// A candidate generated against one closure state and admitted against another
// is not the same proposal, even when every digest inside the bundle still
// verifies against itself: internal consistency says the bundle was not
// tampered with, and says nothing about whether it is still current.
type synthesisRunTaskBinding struct {
	TaskID                       string `json:"task_id"`
	TaskControlStateDigestSHA256 string `json:"task_control_state_digest_sha256"`
	ClosureReportDigestSHA256    string `json:"closure_report_digest_sha256"`
}

// v2 adds TaskBinding.
//
// A candidate sealed without a task binding can never be drift-checked
// afterwards, because the governance state it was generated under is not
// recoverable from anything else in the bundle.
//
// It is consumed by verifyTaskBindingUnchanged (synthesis_task_drift.go),
// which synthesis-admit and synthesis-apply both call before doing anything:
// #149 hard law 10, for the resumption that actually exists.
const synthesisRunLineageSchemaVersion = "sensei.synthesis-run.lineage.v2"

// persistAdmissionLineage writes the complete O5 input chain to
// <candidateStoreDir>/<candidate-digest>.lineage.json when (and only when)
// the run reached candidate-ready -- every other disposition has no
// sealed candidate, so there is no lineage to persist. Returns an empty
// path, nil error for every other disposition.
//
// The write itself goes through candidateStore.PutAuxiliaryFile, not a
// freshly re-opened path -- a live review found that opening a fresh
// os.Root(candidateStoreDir) here (as an earlier revision of this function
// did) could split from the directory candidateStore actually sealed the
// candidate artifact into, if --candidate-store's original path were
// renamed or replaced by something else during this run's (potentially
// long) generation/evaluation phases: the candidate would stay reachable
// in the original directory while lineage silently landed in whatever
// replacement now sits at the same path. Routing through candidateStore's
// own already-open root closes that gap the same way its Put/Get already
// do (see TestFSCandidateArtifactStorePutAndGetShareStableRootIdentityAcrossRename).
// candidateStoreDir is still threaded through separately, but only ever
// used to build the human-facing CandidateArtifactPath/return-path
// strings, never to perform the write itself.
func persistAdmissionLineage(ctx context.Context, result synthesisdriver.Result, candidateStore runnercomposition.CandidateArtifactStore, candidateStoreDir string, taskBinding synthesisRunTaskBinding) (string, error) {
	r := result.Receipt
	if r.Disposition != synthesisdriver.DispositionCandidateReady {
		return "", nil
	}
	if r.CandidateArtifactDigestSHA256 == nil {
		return "", errors.New("candidate-ready receipt carries no candidate artifact digest")
	}
	candidateDigest := *r.CandidateArtifactDigestSHA256

	if result.SessionState.Receipt == nil {
		return "", errors.New("candidate-ready result carries no synthesis.Receipt")
	}

	var runnerReceipt *runnercomposition.RunnerReceipt
	for _, handoff := range result.Trace.GenerationHandoffs {
		if handoff.RunnerReceipt.CandidateArtifactDigestSHA256 != nil && *handoff.RunnerReceipt.CandidateArtifactDigestSHA256 == candidateDigest {
			receipt := handoff.RunnerReceipt
			runnerReceipt = &receipt
			break
		}
	}
	if runnerReceipt == nil {
		return "", fmt.Errorf("no runner receipt in this run's trace matches candidate artifact digest %s", candidateDigest)
	}

	var evaluationReceipt *evaluatorcomposition.EvaluationReceipt
	for _, evalResult := range result.Trace.EvaluationResults {
		if evalResult.Receipt != nil && evalResult.Receipt.CandidateArtifactDigestSHA256 == candidateDigest {
			evaluationReceipt = evalResult.Receipt
			break
		}
	}
	if evaluationReceipt == nil {
		return "", fmt.Errorf("no evaluation receipt in this run's trace matches candidate artifact digest %s", candidateDigest)
	}

	lineage := synthesisRunLineage{
		SchemaVersion:                 synthesisRunLineageSchemaVersion,
		TaskBinding:                   taskBinding,
		CandidateArtifactDigestSHA256: candidateDigest,
		CandidateArtifactPath:         filepath.Join(candidateStoreDir, candidateDigest+".json"),
		SynthesisReceipt:              *result.SessionState.Receipt,
		RunnerReceipt:                 *runnerReceipt,
		EvaluationReceipt:             *evaluationReceipt,
	}
	data, err := json.MarshalIndent(lineage, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal admission lineage: %w", err)
	}
	filename := candidateDigest + ".lineage.json"
	if err := candidateStore.PutAuxiliaryFile(ctx, filename, data); err != nil {
		return "", fmt.Errorf("write admission lineage: %w", err)
	}
	return filepath.Join(candidateStoreDir, filename), nil
}

// Exit code contract. Every distinct outcome gets its own code so a caller
// scripting this command does not need to parse text or JSON just to tell
// "produced a candidate" apart from "the vendor CLI misbehaved" apart from
// "the task wasn't ready to run." 0 is reserved for the one outcome an
// automated caller can safely treat as "proceed": a sealed candidate ready
// for the separate, human admission step.
const (
	exitCandidateReady       = 0
	exitInvalidInvocation    = 2
	exitResolutionFailure    = 1
	exitGovernedTerminalStop = 3
	exitGovernedProviderStop = 4
	exitGovernedRunnerStop   = 5
	exitStepLimitReached     = 6
	exitInternalDefect       = 7
)

func exitCodeForDisposition(d synthesisdriver.Disposition) int {
	switch d {
	case synthesisdriver.DispositionCandidateReady:
		return exitCandidateReady
	case synthesisdriver.DispositionTerminalFailure:
		return exitGovernedTerminalStop
	case synthesisdriver.DispositionProviderStopped:
		return exitGovernedProviderStop
	case synthesisdriver.DispositionRunnerStopped:
		return exitGovernedRunnerStop
	case synthesisdriver.DispositionStepLimitReached:
		return exitStepLimitReached
	default:
		// synthesisdriver.Disposition is a closed vocabulary; reaching here
		// means a new disposition was added there without a matching case
		// here. Treat it as an internal defect (fail loud) rather than
		// silently returning 0/success for an outcome this command doesn't
		// actually recognize.
		return exitInternalDefect
	}
}

// resolveSynthesisRunObjective resolves the session objective (--objective,
// falling back to the task's own recorded description) and requires it to
// exactly match the authored interpretation's own objective (both
// whitespace-normalized). A real review found that nothing previously
// enforced this: the authored interpretation could silently drive
// generation under one objective while the session/receipt recorded a
// different one -- a split identity between what happened and what was
// claimed to have happened. On a mismatch, the caller must refuse before
// planning or generation ever runs, not merely warn.
func resolveSynthesisRunObjective(objectiveFlag, taskDescription, authoredObjective string) (string, error) {
	objective := strings.TrimSpace(objectiveFlag)
	source := "--objective"
	if objective == "" {
		objective = strings.TrimSpace(taskDescription)
		source = "the task's own recorded description"
	}
	authored := strings.TrimSpace(authoredObjective)
	if authored != objective {
		return "", fmt.Errorf("interpretation objective %q does not match the session objective %q (from %s); the authored interpretation must state exactly the objective this run will be driven under, or a split identity forms between what drove generation and what the receipt records",
			authored, objective, source)
	}
	return objective, nil
}

// validateNoRequiredProofObligations refuses an authored interpretation
// that declares any required proof obligation. No production
// EvidenceResolver exists yet to bind a declared obligation to a verified
// discharge digest, so synthesis.Session.ProofObligationDigests can only
// ever legitimately be constructed empty -- and only once this check has
// passed. That makes the empty slice an honest, bounded claim ("the
// accepted authored interpretation declared none"), never a silent
// substitute for verification Sensei cannot yet perform.
// validateCurrentBinding refuses a task whose binding is stale (repository
// revision, graph snapshot, or an out-of-envelope working tree change since
// 'sensei prepare-change') or whose admission decision refuses mutation
// capability. This is deliberately a separate, unconditional check from the
// PrimaryBlocker gate: a task that converged with zero closure blockers
// before its binding went stale has no blocker at all to classify as
// uncertifiable, so the PrimaryBlocker check alone -- even without
// --force-unconverged -- would let it through, while workspace identity is
// then composed from the *current* revision and the closure report/digest
// stay bound to the stale task-session's own recorded state. That
// combination can seal a candidate-ready receipt whose architectural
// closure was established for a repository state that no longer exists.
// Unlike an unconverged-but-acknowledged closure, a stale binding is not
// something --force-unconverged is meant to paper over: the only correct
// repair is to re-run 'sensei prepare-change'/'sensei advance-task' and
// re-establish a current binding. Mirrors tasksession.BuildTaskBriefing's
// own unconditional "BindingHealth != current" refusal.
func validateCurrentBinding(control taskcontrol.TaskControlState) error {
	if control.BindingHealth != "current" {
		return fmt.Errorf("task binding is stale (binding_health=%q); run 'sensei prepare-change' or 'sensei advance-task' to establish a current binding before synthesis", control.BindingHealth)
	}
	if control.Permission.Modify == admission.CapabilityRefused {
		return fmt.Errorf("task mutation capability is %q; repair the task binding or admission decision before synthesis", control.Permission.Modify)
	}
	return nil
}

func validateNoRequiredProofObligations(obligations []string) error {
	if len(obligations) == 0 {
		return nil
	}
	return fmt.Errorf("authored interpretation declares %d required proof obligation(s) (%s); no production EvidenceResolver exists yet to bind these to verified discharge digests, so this run cannot proceed -- author an interpretation whose required_proof_obligations is empty, or wait for O2 proof-obligation binding to land",
		len(obligations), strings.Join(obligations, "; "))
}

// validateNoDecisionProofObligations refuses when the task's own admission
// decision -- authoritative, already computed by `sensei prepare-change`,
// never something the caller-authored interpretation may erase or override
// -- declares any required proof obligation. Complements
// validateNoRequiredProofObligations: an interpretation's own empty
// declaration is necessary but not sufficient, since this is a real,
// already-wired obligation source independent of whatever the
// interpretation claims.
func validateNoDecisionProofObligations(obligations []admission.ProofReceipt) error {
	if len(obligations) == 0 {
		return nil
	}
	ids := make([]string, len(obligations))
	for i, o := range obligations {
		ids[i] = o.ID
	}
	return fmt.Errorf("task admission decision declares %d required proof obligation(s) (%s); no production EvidenceResolver exists yet to bind these to verified discharge digests, so this run cannot proceed -- the authored interpretation may not erase obligations already projected by 'sensei prepare-change'",
		len(obligations), strings.Join(ids, "; "))
}

// resolveAgentWorkdirs returns two distinct, empty, absolute directories for
// the O3 generation and O8 planning subprocesses -- they must never share
// one workdir, since validateEmptyAgentWorkDir requires emptiness both at
// construction and immediately before each call, and the driver can
// interleave planning and generation calls across phases. If base is empty,
// fresh temp dirs are created and the returned cleanup removes them.
// resolveStoreDir resolves a --candidate-store/--evidence-store flag
// value to the absolute path NewFSCandidateArtifactStore/NewFSEvidenceSink
// require. explicit == "" uses defaultDir (already absolute, since it is
// always built from the already-absolute taskDir). A non-empty explicit
// value that is relative is resolved against absRepo -- the same
// convention --task itself already uses -- rather than left to fail deep
// inside a store constructor with a "must be absolute" error after
// os.MkdirAll has already silently accepted and created the relative
// path.
func resolveStoreDir(explicit, absRepo, defaultDir string) string {
	if strings.TrimSpace(explicit) == "" {
		return defaultDir
	}
	if !filepath.IsAbs(explicit) {
		return filepath.Join(absRepo, explicit)
	}
	return explicit
}

func resolveAgentWorkdirs(base string) (generation, planning string, cleanup func(), err error) {
	if strings.TrimSpace(base) == "" {
		root, err := os.MkdirTemp("", "sensei-synthesis-run-*")
		if err != nil {
			return "", "", func() {}, fmt.Errorf("create agent workdir: %w", err)
		}
		gen := filepath.Join(root, "generation")
		plan := filepath.Join(root, "planning")
		if err := os.MkdirAll(gen, 0o755); err != nil {
			return "", "", func() {}, err
		}
		if err := os.MkdirAll(plan, 0o755); err != nil {
			return "", "", func() {}, err
		}
		return gen, plan, func() { _ = os.RemoveAll(root) }, nil
	}
	if !filepath.IsAbs(base) {
		return "", "", func() {}, fmt.Errorf("--agent-workdir must be absolute, got %q", base)
	}
	gen := filepath.Join(base, "generation")
	plan := filepath.Join(base, "planning")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		return "", "", func() {}, err
	}
	if err := os.MkdirAll(plan, 0o755); err != nil {
		return "", "", func() {}, err
	}
	return gen, plan, func() {}, nil
}

// synthesisRunReport is the CLI's own output envelope: the driver's
// RunReceipt verbatim, plus exactly the operator-facing context the receipt
// itself doesn't carry (task identity, exit code and its meaning, and what
// to do next) -- point 5 of the dogfood review: the output must name the
// candidate, evaluation result, stopping reason, task/session identity, and
// the explicit next permitted operation, for every disposition, not just
// candidate-ready.
type synthesisRunReport struct {
	Receipt         synthesisdriver.RunReceipt `json:"receipt"`
	TaskID          string                     `json:"task_id"`
	ConfiguredSteps int                        `json:"configured_max_steps"`
	ExitCode        int                        `json:"exit_code"`
	ExitMeaning     string                     `json:"exit_meaning"`
	// CandidatePath is the sealed candidate artifact's own file
	// (<candidateStoreDir>/<digest>.json), not the store directory -- a
	// live review found this previously named the directory while the
	// field name promised a path to the candidate itself, so JSON
	// automation opening the reported path received a directory instead
	// of the artifact.
	CandidatePath string `json:"candidate_path,omitempty"`
	// LineagePath names the durable file admit-change can load directly to
	// construct admissioncomposition.ComposeInput's SynthesisReceipt/
	// RunnerReceipt/EvaluationReceipt fields -- populated only when
	// Receipt.Disposition is candidate-ready (every other disposition has
	// no sealed candidate, so there is nothing to persist).
	LineagePath string `json:"lineage_path,omitempty"`
	NextStep    string `json:"next_step"`
	// Evaluations carries every O4 evaluator verdict this run produced --
	// the RunReceipt's evaluation_receipt_digests_sha256 is only a digest
	// list, which leaves an operator unable to see *why* a real evaluator
	// rejected a candidate without separately archived evidence. Omitted
	// when O4 never ran (e.g. a provider/runner stop before evaluation).
	Evaluations []*synthesis.Evaluation `json:"evaluations,omitempty"`
	// EvaluationReceipts carries every O4 EvaluationReceipt this run
	// produced. Disposition/FailureDetail are populated for every
	// disposition (unlike Evaluation, which is only non-nil for
	// DispositionEvaluated) -- this is the one place a short-circuit
	// (e.g. candidate-load-failure, invalid-output-terminated) explains
	// itself.
	EvaluationReceipts []*evaluatorcomposition.EvaluationReceipt `json:"evaluation_receipts,omitempty"`
}

// buildSynthesisRunReport assembles the CLI's output envelope from a
// driver Result -- pure and side-effect-free so its field derivations
// (particularly CandidatePath, a live review finding: it must be the
// sealed artifact's own file, not the candidate store directory) are
// directly testable without capturing stdout.
func buildSynthesisRunReport(result synthesisdriver.Result, taskID, candidateStoreDir, lineagePath string, configuredMaxSteps int) synthesisRunReport {
	r := result.Receipt
	exitCode := exitCodeForDisposition(r.Disposition)
	report := synthesisRunReport{
		Receipt:         r,
		TaskID:          taskID,
		ConfiguredSteps: configuredMaxSteps,
		ExitCode:        exitCode,
		ExitMeaning:     exitMeaning(exitCode),
		LineagePath:     lineagePath,
		NextStep:        nextStep(r.Disposition, r.CandidateArtifactDigestSHA256),
	}
	if r.CandidateArtifactDigestSHA256 != nil {
		report.CandidatePath = filepath.Join(candidateStoreDir, *r.CandidateArtifactDigestSHA256+".json")
	}
	for _, evalResult := range result.Trace.EvaluationResults {
		if evalResult.Evaluation != nil {
			report.Evaluations = append(report.Evaluations, evalResult.Evaluation)
		}
		if evalResult.Receipt != nil {
			report.EvaluationReceipts = append(report.EvaluationReceipts, evalResult.Receipt)
		}
	}
	return report
}

func printSynthesisRunResult(result synthesisdriver.Result, taskID, candidateStoreDir, lineagePath string, configuredMaxSteps int, format string) {
	r := result.Receipt
	report := buildSynthesisRunReport(result, taskID, candidateStoreDir, lineagePath, configuredMaxSteps)

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}

	fmt.Printf("task_id:      %s\n", report.TaskID)
	fmt.Printf("disposition:  %s\n", r.Disposition)
	fmt.Printf("final_phase:  %s\n", r.FinalPhase)
	fmt.Printf("step_count:   %d of %d configured\n", r.StepCount, report.ConfiguredSteps)
	fmt.Printf("o2_receipts:  %d (interpretation/planning provider calls)\n", len(r.O2ReceiptDigestsSHA256))
	fmt.Printf("o3_receipts:  %d (generation runner calls)\n", len(r.RunnerReceiptDigestsSHA256))
	fmt.Printf("o4_receipts:  %d (evaluation calls)\n", len(r.EvaluationReceiptDigestsSHA256))
	fmt.Printf("exit_code:    %d (%s)\n", report.ExitCode, report.ExitMeaning)
	fmt.Printf("detail:       %s\n", r.Detail)
	if r.CandidateArtifactDigestSHA256 != nil {
		fmt.Printf("candidate:    %s\n", *r.CandidateArtifactDigestSHA256)
		fmt.Printf("candidate_path: %s\n", report.CandidatePath)
		fmt.Printf("admission lineage: %s\n", report.LineagePath)
	}
	for i, eval := range report.Evaluations {
		fmt.Printf("evaluation[%d]: %s %s recommendation=%s\n", i, eval.EvaluatorKind, eval.EvaluatorVersion, eval.Recommendation)
		if len(eval.ClassifiedFailureReasons) > 0 {
			fmt.Printf("  failure reasons: %s\n", strings.Join(eval.ClassifiedFailureReasons, "; "))
		}
		for _, check := range eval.Checks {
			fmt.Printf("  check: %s status=%s %s\n", check.CheckID, check.Status, check.Detail)
		}
	}
	for i, receipt := range report.EvaluationReceipts {
		fmt.Printf("evaluation_receipt[%d]: disposition=%s %s\n", i, receipt.Disposition, receipt.FailureDetail)
	}
	fmt.Println()
	fmt.Println(report.NextStep)
}

// exitMeaning names each exit code's outcome category in one short phrase,
// matching the constants above.
func exitMeaning(code int) string {
	switch code {
	case exitCandidateReady:
		return "candidate ready for admission"
	case exitInvalidInvocation:
		return "invalid invocation"
	case exitResolutionFailure:
		return "resolution failure (could not even start the run)"
	case exitGovernedTerminalStop:
		return "governed terminal failure"
	case exitGovernedProviderStop:
		return "governed provider stop"
	case exitGovernedRunnerStop:
		return "governed runner stop"
	case exitStepLimitReached:
		return "step limit reached"
	default:
		return "internal defect"
	}
}

// nextStep names the one explicitly permitted next operation for a
// disposition. It never suggests admission, application, or any mutation --
// this command's own authority boundary -- and it never suggests retrying
// with different flags when the honest answer is "inspect what happened."
// nextStep builds the operator-facing guidance for a run's outcome.
// candidateArtifactDigestSHA256 is result.Receipt.CandidateArtifactDigestSHA256
// verbatim -- NEVER assumed nil for a non-candidate-ready disposition, for
// any disposition. A live review found that DispositionTerminalFailure's
// text unconditionally claimed "No candidate exists," directly
// contradicting the very same report's candidate_path field whenever a
// candidate actually was sealed: runnercomposition.Run seals a candidate
// into the store unconditionally on every O3-verified attempt (run.go's
// own "seal the candidate before the ephemeral buffer is destroyed" step),
// strictly BEFORE O4 evaluation ever runs -- so a candidate can be durably
// sealed and the run still end in DispositionTerminalFailure (O4
// recommends abort on THIS attempt), DispositionRunnerStopped (an EARLIER
// attempt in the same run's retry loop sealed a candidate, recommended
// retry, and a LATER attempt's O3 generation itself then failed), or even
// DispositionProviderStopped (an EARLIER attempt sealed a candidate,
// O4 recommended replan, and the REPLAN's own planning-provider call then
// failed -- PhaseReplan -> PhasePlanning is a second, later call to the
// same providers a fresh run's first PhasePlanning call also uses, so
// "provider stopped" does not mean "before PhaseAttempting could ever run
// even once" the way it first appears to). All three confirmed
// empirically via dedicated driver-level tests before this fix, not
// merely inferred from the disposition name -- there turned out to be no
// disposition, other than CandidateReady itself, that can safely assume a
// candidate never exists. The clause is therefore applied uniformly to
// every non-candidate-ready disposition.
func nextStep(d synthesisdriver.Disposition, candidateArtifactDigestSHA256 *string) string {
	candidateClause := "No candidate exists."
	if candidateArtifactDigestSHA256 != nil {
		candidateClause = "A candidate WAS sealed during this run (see candidate_path above), but it is not admission-ready: this run's own receipt did not reach candidate-ready, and no admission lineage was recorded for it."
	}
	switch d {
	case synthesisdriver.DispositionCandidateReady:
		return "Candidate sealed. Nothing has been applied. The admission lineage bundle (synthesis/runner/evaluation receipts) needed to construct admissioncomposition.ComposeInput is durably persisted at the path named above. Run `sensei synthesis-admit --lineage <path>` to derive the O5 admission request from it -- that command reads this bundle, re-reads the base tree from git, and derives the mutation scope from the sealed manifests. It decides nothing: `sensei admit-change` evaluates the request it produces, and remains a separate, deliberate step."
	case synthesisdriver.DispositionTerminalFailure:
		return fmt.Sprintf("O1 reached a governed terminal failure. %s Inspect the receipt detail and the task's control state before authoring a new interpretation or task.", candidateClause)
	case synthesisdriver.DispositionProviderStopped:
		return fmt.Sprintf("The interpretation or planning provider stopped. %s Inspect the receipt detail and the vendor CLI's own output; this is not a governance rejection of generated content.", candidateClause)
	case synthesisdriver.DispositionRunnerStopped:
		return fmt.Sprintf("O3 generation stopped on this attempt. %s Inspect the receipt detail; this may be a vendor CLI or workspace problem, not a content rejection.", candidateClause)
	case synthesisdriver.DispositionStepLimitReached:
		return fmt.Sprintf("The step limit was reached before reaching a terminal disposition. %s Re-run with a higher --max-steps only after understanding why this many steps were needed.", candidateClause)
	default:
		return "Unrecognized disposition. Treat this as an internal defect, not a governed outcome."
	}
}
