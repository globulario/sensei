// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/probeexec"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"gopkg.in/yaml.v3"
)

type taskFileFlags []tasksession.FileOperation

func (f *taskFileFlags) String() string {
	var parts []string
	for _, op := range *f {
		parts = append(parts, op.Operation+":"+op.Path)
	}
	return strings.Join(parts, ",")
}

func (f *taskFileFlags) Set(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return fmt.Errorf("empty file scope")
	}
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("file scope must be operation:path")
	}
	*f = append(*f, tasksession.FileOperation{Operation: strings.TrimSpace(parts[0]), Path: strings.TrimSpace(parts[1])})
	return nil
}

func runPrepareChange(args []string) int {
	fs := flag.NewFlagSet("sensei prepare-change", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var files taskFileFlags
	var opts tasksession.PrepareOptions
	var format string
	var noActive bool
	fs.StringVar(&opts.RepoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&opts.RepositoryDomain, "repo-domain", "", "repository domain, e.g. github.com/org/repo")
	fs.StringVar(&opts.Description, "description", "", "bounded user task description")
	fs.StringVar(&opts.Mode, "mode", "", "inspect or modify")
	fs.StringVar(&opts.TaskClass, "task-class", "", "stable task class")
	fs.StringVar(&opts.RiskClass, "risk-class", "", "closure risk class")
	fs.StringVar(&opts.DirectionRequirement, "direction", "", "direction requirement: preserve|evolve|migrate|not_applicable|unknown")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit graph snapshot N-Triples file")
	fs.StringVar(&opts.Claims, "claims", "", "architecture_claims YAML override (default: <repo>/.sensei/project/claims.yaml)")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "optional architecture_dialogue YAML")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "optional architecture_evidence_state YAML")
	fs.StringVar(&opts.DirectionBootstrapAuthorization, "bootstrap-direction-authorization", "", "path to an independently created bootstrap direction authorization YAML")
	fs.StringVar(&opts.QuestionCreatedAt, "question-created-at", "", "RFC3339 timestamp for deterministic generated questions")
	fs.StringVar(&opts.RequestedBy, "requested-by", "coding_agent", "requester recorded in task artifacts")
	fs.Var(&files, "file", "exact scope file as operation:path; operation is read or modify (repeatable)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.BoolVar(&noActive, "no-active", false, "do not update .sensei/tasks/active.yaml")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei prepare-change --repo-domain <domain> --description <text> --mode inspect|modify --task-class <class> --risk-class <risk> --direction <direction> --graph-nt <awareness.nt> --file modify:path [flags]

Creates or replays one deterministic active architectural task session.
It runs at most one convergence iteration, evaluates admission, writes task
workspace receipts under .sensei/tasks/<task-id>/, and never edits source,
executes tests/probes, promotes knowledge, mutates the graph, or changes proto.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	opts.Files = files
	opts.SetActive = !noActive
	res, err := tasksession.Prepare(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei prepare-change: %v\n", err)
		return 1
	}
	if err := printPrepareChange(res, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei prepare-change: %v\n", err)
		return 2
	}
	return 0
}

func runTaskStatus(args []string) int {
	fs := flag.NewFlagSet("sensei task-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts tasksession.StatusOptions
	var format string
	var compact, verbose, asJSON, asYAML bool
	fs.StringVar(&opts.RepoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&opts.TaskDir, "task", "", "task directory; defaults to active task")
	fs.BoolVar(&opts.Active, "active", false, "read .sensei/tasks/active.yaml")
	fs.BoolVar(&opts.Verify, "verify", false, "verify pointer, session digest, graph digest, revision, and artifact references")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.BoolVar(&compact, "compact", false, "print the bounded task-control projection")
	fs.BoolVar(&verbose, "verbose", false, "include the full task-control projection")
	fs.BoolVar(&asJSON, "json", false, "output stable JSON")
	fs.BoolVar(&asYAML, "yaml", false, "output stable YAML")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei task-status [--active|--task <dir>] [--verify]

Reads one task session and prints the current operational next action.
This command is read-only.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	if asJSON {
		format = "json"
	}
	if asYAML {
		format = "yaml"
	}
	if compact || verbose {
		control, taskDir, err := tasksession.ControlStatus(opts.RepoRoot, opts.TaskDir, opts.Active)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei task-status: %v\n", err)
			return 1
		}
		if err := printTaskControl(control, taskDir, format, verbose); err != nil {
			fmt.Fprintf(os.Stderr, "sensei task-status: %v\n", err)
			return 2
		}
		return 0
	}
	res, err := tasksession.Status(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-status: %v\n", err)
		return 1
	}
	// Read-only: surface the ONE canonical completion projection (Phase 9.1). This
	// consumes the completion owner's reconstruction; it re-derives nothing and
	// mutates nothing. A task without a completion-relevant ledger simply yields the
	// projection's `unsupported`/`not_completed` state.
	env := completionProjectionEnvelope(opts)
	if err := printTaskStatus(res, env, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-status: %v\n", err)
		return 2
	}
	return 0
}

// completionProjectionEnvelope resolves the same task directory tasksession.Status
// uses, then builds the canonical completion projection wrapped in a typed
// availability envelope. It NEVER omits: a resolution or owner failure becomes an
// explicit `unavailable` envelope with a typed class/detail, never silence, and never
// a fabricated terminal state. It never fails the status command and mutates nothing.
func completionProjectionEnvelope(opts tasksession.StatusOptions) completion.CompletionProjectionEnvelope {
	abs, err := filepath.Abs(strings.TrimSpace(opts.RepoRoot))
	if err != nil {
		return completion.UnavailableTaskDirectoryEnvelope("repository path: " + err.Error())
	}
	taskDir := strings.TrimSpace(opts.TaskDir)
	if opts.Active || taskDir == "" {
		p, perr := tasksession.LoadActivePointer(abs)
		if perr != nil {
			return completion.UnavailableTaskDirectoryEnvelope("active task pointer: " + perr.Error())
		}
		taskDir = filepath.Dir(filepath.Join(abs, filepath.FromSlash(p.SessionPath)))
	}
	if strings.TrimSpace(taskDir) == "" {
		return completion.UnavailableTaskDirectoryEnvelope("no task directory resolved")
	}
	env := completion.BuildCompletionProjectionEnvelope(context.Background(), completion.Request{RepositoryRoot: abs, TaskDirectory: taskDir})
	// The task-status path receives only a validated envelope.
	if completion.ValidateCompletionEnvelope(env) != nil {
		return completion.UnavailableProjectionOwnerEnvelope("internal: invalid completion envelope")
	}
	return env
}

func runAdvanceTask(args []string) int {
	fs := flag.NewFlagSet("sensei advance-task", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var opts tasksession.AdvanceTaskOptions
	var format string
	var active bool
	var maxProbes, maxFiles int
	var maxBytes int64
	fs.StringVar(&opts.RepoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&opts.TaskDir, "task", "", "task directory; defaults to active task")
	fs.BoolVar(&active, "active", false, "resolve .sensei/tasks/active.yaml")
	fs.StringVar(&opts.ObservedAt, "observed-at", "", "explicit RFC3339 observation time")
	fs.IntVar(&maxProbes, "max-probes", 32, "maximum probes executed in this invocation")
	fs.IntVar(&maxFiles, "max-files", 128, "maximum distinct files read")
	fs.Int64Var(&maxBytes, "max-bytes", 16<<20, "maximum total bytes read")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei advance-task [--active|--task <dir>] [flags]

Executes only eligible static_read probes, records exact receipts, advances
convergence exactly once, re-evaluates admission, and atomically publishes one
task-control generation. It never edits source or executes tests or commands.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}
	opts.Active = active || opts.TaskDir == ""
	opts.Budget = probeexec.Budget{MaxProbes: maxProbes, MaxFiles: maxFiles, MaxBytes: maxBytes}
	opts.LockWait = 2 * time.Second
	res, err := tasksession.AdvanceTask(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei advance-task: %v\n", err)
		return 1
	}
	if err := printAdvanceTask(res, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei advance-task: %v\n", err)
		return 2
	}
	return 0
}

func runTaskBriefing(args []string) int {
	fs := flag.NewFlagSet("sensei task-briefing", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repoRoot, taskDir, file, format string
	var active bool
	fs.StringVar(&repoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&taskDir, "task", "", "task directory; defaults to active task")
	fs.BoolVar(&active, "active", false, "resolve .sensei/tasks/active.yaml")
	fs.StringVar(&file, "file", "", "repository-relative file")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: sensei task-briefing --file <path> [--active|--task <dir>] [--format text|yaml|json]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || strings.TrimSpace(file) == "" {
		fs.Usage()
		return 2
	}
	brief, err := tasksession.BuildTaskBriefing(repoRoot, taskDir, file, active || taskDir == "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-briefing: %v\n", err)
		return 1
	}
	if err := printTaskBriefing(brief, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-briefing: %v\n", err)
		return 2
	}
	return 0
}

func printTaskBriefing(brief tasksession.TaskBriefing, format string) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		fmt.Printf("File: %s\n", brief.File)
		if brief.Component != "" {
			fmt.Printf("Component: %s\n", brief.Component)
		}
		fmt.Printf("Task: %s\n\nInspect: %s\nModify: %s\n", brief.TaskID, brief.Inspect, brief.Modify)
		fmt.Printf("\nRelevant architecture: %d claims, %d Test-backed, %d failure/forbidden constraints\n", brief.RelevantClaimCount, brief.TestBackedCount, len(brief.FailureModes))
		for _, claim := range brief.RelevantClaims {
			fmt.Printf("  - %s: %s [%s/%s]\n", claim.ID, claim.Statement, claim.Plane, claim.Status)
		}
		if brief.PrimaryBlocker != nil {
			fmt.Printf("\nBlocking:\n  %s [%s]\n", brief.PrimaryBlocker.Statement, brief.PrimaryBlocker.ID)
		} else {
			fmt.Println("\nBlocking: none")
		}
		if brief.PrimaryQuestion != nil {
			fmt.Printf("\nArchitect question:\n  %s [%s]\n", brief.PrimaryQuestion.QuestionText, brief.PrimaryQuestion.ID)
		}
		fmt.Printf("\nNext:\n  %s", brief.PrimaryNextAction.Kind)
		if brief.PrimaryNextAction.TargetID != "" {
			fmt.Printf(" %s", brief.PrimaryNextAction.TargetID)
		}
		fmt.Println()
		if brief.AdditionalBlockers+brief.AdditionalQuestions+brief.AdditionalProbes > 0 {
			fmt.Printf("\nExpanded detail: %d additional root blockers, %d additional architect questions, %d additional eligible probes\n", brief.AdditionalBlockers, brief.AdditionalQuestions, brief.AdditionalProbes)
		}
		return nil
	case "yaml", "yml":
		data, err := yaml.Marshal(map[string]tasksession.TaskBriefing{"architecture_task_briefing": brief})
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(map[string]tasksession.TaskBriefing{"architecture_task_briefing": brief}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

func printAdvanceTask(res tasksession.AdvanceTaskResult, format string) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		fmt.Printf("Disposition: %s\n", res.Disposition)
		return printTaskControl(res.Control, res.TaskDir, "text", false)
	case "yaml", "yml":
		data, err := yaml.Marshal(map[string]tasksession.AdvanceTaskResult{"architecture_advance_task": res})
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(map[string]tasksession.AdvanceTaskResult{"architecture_advance_task": res}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

func printTaskControl(state taskcontrol.TaskControlState, taskDir, format string, verbose bool) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		fmt.Printf("Task: %s\n", state.TaskID)
		fmt.Printf("Binding: %s @ %s (graph %s)\n", state.Binding.RepositoryDomain, shortValue(state.Binding.Revision), shortValue(state.Binding.GraphDigestSHA256))
		fmt.Printf("Inspect: %s\nModify: %s\n", state.Permission.Inspect, state.Permission.Modify)
		if state.PrimaryBlocker != nil {
			fmt.Printf("Blocking: %s [%s]\n", state.PrimaryBlocker.Statement, state.PrimaryBlocker.ID)
		} else {
			fmt.Println("Blocking: none")
		}
		fmt.Printf("Automatic evidence: %d completed, %d inconclusive, %d eligible, %d failed/rejected\n", state.Evidence.Completed, state.Evidence.Inconclusive, state.Evidence.Eligible, state.Evidence.Failed+state.Evidence.Rejected)
		if state.PrimaryQuestion != nil {
			fmt.Printf("Architect question: %s [%s]\n", state.PrimaryQuestion.QuestionText, state.PrimaryQuestion.ID)
		}
		fmt.Printf("Next: %s", state.NextAction.Kind)
		if state.NextAction.TargetID != "" {
			fmt.Printf(" %s", state.NextAction.TargetID)
		}
		fmt.Println()
		if len(state.Limitations) > 0 {
			fmt.Printf("Limitations: %s\n", strings.Join(state.Limitations, "; "))
		}
		if verbose {
			fmt.Printf("Full counts: %d blockers, %d questions, %d probes, %d groups\n", len(state.Blockers), len(state.Questions), len(state.Probes), len(state.Groups))
			for _, blocker := range state.Blockers {
				fmt.Printf("Blocker %s: %s group=%s load_bearing=%t\n", blocker.ID, blocker.Disposition, blocker.GroupID, blocker.LoadBearing)
			}
			for _, question := range state.Questions {
				fmt.Printf("Question %s: %s actor=%s group=%s\n", question.ID, question.ResolutionClass, question.RequiredActor, question.GroupID)
			}
			for _, evidenceProbe := range state.Probes {
				fmt.Printf("Probe %s: %s kind=%s\n", evidenceProbe.ID, evidenceProbe.Disposition, evidenceProbe.Kind)
			}
			for _, group := range state.Groups {
				fmt.Printf("Group %s: root=%s dependents=%d\n", group.ID, group.RootBlockerID, len(group.DependentBlockerIDs))
			}
		}
		_ = taskDir
		return nil
	case "yaml", "yml":
		data, err := taskcontrol.MarshalYAML(state)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(taskcontrol.Envelope{TaskControl: state}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

func shortValue(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func printPrepareChange(res tasksession.PrepareResult, format string) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		fmt.Printf("Task: %s\n", res.TaskID)
		fmt.Printf("Graph: %s\n", res.GraphState)
		fmt.Printf("Closure: %s\n", res.Closure)
		fmt.Printf("Convergence: %s\n", res.Convergence)
		fmt.Printf("Inspect: %s\n", res.Inspect)
		fmt.Printf("Modify: %s\n", res.Modify)
		if len(res.WaitingOn) > 0 {
			fmt.Printf("Waiting on: %s\n", strings.Join(res.WaitingOn, ", "))
		} else {
			fmt.Println("Waiting on: none")
		}
		fmt.Println("Envelope:")
		if len(res.ReadEnvelope) > 0 {
			fmt.Println("  read:")
			for _, p := range res.ReadEnvelope {
				fmt.Printf("    - %s\n", p)
			}
		}
		if len(res.ModifyEnvelope) > 0 {
			fmt.Println("  modify:")
			for _, p := range res.ModifyEnvelope {
				fmt.Printf("    - %s\n", p)
			}
		}
		fmt.Printf("Next: %s", res.Next.Action)
		if res.Next.Reference != "" {
			fmt.Printf(" %s", res.Next.Reference)
		}
		fmt.Println()
		return nil
	case "yaml", "yml":
		data, err := yaml.Marshal(map[string]tasksession.PrepareResult{"architecture_prepare_change": res})
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(map[string]tasksession.PrepareResult{"architecture_prepare_change": res}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

func printTaskStatus(res tasksession.StatusResult, env completion.CompletionProjectionEnvelope, format string) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		fmt.Printf("Task: %s\n", res.TaskID)
		fmt.Printf("Phase: %s\n", res.Phase)
		fmt.Printf("Status: %s\n", res.Status)
		fmt.Printf("Closure: %s\n", res.Closure)
		fmt.Printf("Convergence: %s\n", res.Convergence)
		fmt.Printf("Admission: %s\n", res.Admission)
		if len(res.WaitingOn) > 0 {
			fmt.Printf("Waiting on: %s\n", strings.Join(res.WaitingOn, ", "))
		} else {
			fmt.Println("Waiting on: none")
		}
		fmt.Printf("Next: %s", res.Next.Action)
		if res.Next.Reference != "" {
			fmt.Printf(" %s", res.Next.Reference)
		}
		fmt.Println()
		if res.Verified {
			fmt.Println("Verified: true")
		} else if len(res.VerifyErrors) > 0 {
			fmt.Printf("Verified: false (%s)\n", strings.Join(res.VerifyErrors, "; "))
		}
		// The single canonical completion mapping — always shown, never omitted; an
		// unavailable projection is reported explicitly, not as silence.
		fmt.Printf("Completion: %s\n", env.Summary())
		return nil
	case "yaml", "yml":
		data, err := yaml.Marshal(taskStatusEnvelope(res, env))
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(taskStatusEnvelope(res, env), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("--format must be text, yaml, or json")
	}
}

// taskStatusEnvelope carries the task status plus the canonical, non-authoritative
// completion projection ENVELOPE under a distinct key. The envelope is always present
// (available or unavailable), so structured output never silently omits completion.
func taskStatusEnvelope(res tasksession.StatusResult, env completion.CompletionProjectionEnvelope) map[string]any {
	return map[string]any{
		"architecture_task_status": res,
		"completion_projection":    env,
	}
}
