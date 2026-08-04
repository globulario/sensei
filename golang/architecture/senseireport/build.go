// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// maxRenderedFindings bounds the top-level Findings list to what materially
// affects a reader; Summary's counts remain the true totals regardless of
// this cap, per the design doc's "do not dump the entire graph" rule.
const maxRenderedFindings = 10

// Build assembles a Report from this repository's real, on-disk Sensei
// state. It never invents a value: every field either comes from an
// explicit read, or is left at its honest zero value with a Limitations
// entry naming why. Build is a pure function of on-disk state -- no clock,
// no injected time -- so identical repository state always produces an
// identical Report.
func Build(repoRoot string) (Report, error) {
	var r Report
	r.SchemaVersion = SchemaVersion
	r.GeneratedBy = "sensei report"
	r.Reproduction = Reproduction{Commands: []string{"sensei report", "sensei report --check"}}
	// Repository-wide verification is always NOT_RUN in this schema
	// version -- see RepositoryWideVerificationNotRun's doc comment. No
	// code path below ever changes this.
	r.Verification.RepositoryWideVerification = RepositoryWideVerificationNotRun

	key, err := moduleKey(repoRoot)
	if err != nil {
		return Report{}, fmt.Errorf("resolve module: %w", err)
	}
	r.Identity.Repository = Repository{Key: key, DisplayName: key}

	commit, status, _ := architecture.ResolveRevision(repoRoot, true)
	r.Identity.EvaluatedCommit = commit
	r.Identity.EvaluatedCommitStatus = status

	digest, err := ContentDigest(repoRoot, []string{".sensei/", ".git/"}, []string{"SENSEI.md", "SENSEI.report.json"})
	if err != nil {
		return Report{}, fmt.Errorf("content digest: %w", err)
	}
	r.Identity.EvaluatedContentDigestSHA256 = digest
	// Freshly built: by construction, the report a caller is about to
	// write matches the content it was just derived from. sensei report
	// --check is what re-derives this against a COMMITTED report.json and
	// can find STALE; Build itself only ever reports CURRENT.
	r.Verification.ReportFreshness = FreshnessCurrent

	if err := buildCurrentWork(repoRoot, &r); err != nil {
		return Report{}, fmt.Errorf("current work: %w", err)
	}

	mem, err := Candidates(repoRoot)
	if err != nil {
		return Report{}, fmt.Errorf("candidates: %w", err)
	}
	r.Memory = mem
	r.Summary.CandidatesAwaitingReview = mem.CandidatesAwaitingReview

	return r, nil
}

// buildCurrentWork populates Report.CurrentWork, Report.Findings, and
// Report.Summary's finding counts, and the task-scoped portion of
// Report.Verification, from the active task's control state (if any).
// CurrentWork.Disposition is task-scoped ONLY -- it must never be read as
// a repository-wide claim; see Verification.RepositoryWideVerification for
// that separate, deliberately unconditional field.
func buildCurrentWork(repoRoot string, r *Report) error {
	state, taskDir, err := tasksession.ControlStatus(repoRoot, "", true)
	if err != nil {
		if os.IsNotExist(err) {
			r.CurrentWork = CurrentWork{Active: false, Note: "no active task"}
			return nil
		}
		return err
	}

	cw := CurrentWork{
		Active: true,
		TaskID: state.TaskID,
		Scope:  sortedUnique(append([]string{}, state.Permission.ExactScope...)),
	}
	cw.Authority = fmt.Sprintf("inspect: %s, modify: %s", state.Permission.Inspect, state.Permission.Modify)
	cw.RemainingBlockers = state.Summary.ActiveRootBlockers
	if state.PrimaryBlocker != nil {
		cw.PrimaryBlocker = state.PrimaryBlocker.Statement
	}

	// A missing/unreadable task-request.yaml is not fatal to the report;
	// Title simply stays empty rather than being fabricated.
	if req, reqErr := tasksession.LoadTaskRequest(taskDir); reqErr == nil {
		cw.Title = req.Description
	}

	modifyAdmitted := state.Permission.Modify == admission.DecisionAdmitted ||
		state.Permission.Modify == admission.DecisionAdmittedWithConditions
	if !modifyAdmitted {
		cw.Disposition = DispositionBlocked
	} else {
		ready, readyErr := completion.AssessReadiness(context.Background(), completion.Request{
			RepositoryRoot: repoRoot,
			TaskDirectory:  taskDir,
		})
		switch {
		case readyErr != nil:
			cw.Disposition = DispositionUnverified
			r.Limitations = append(r.Limitations, "task readiness could not be assessed: "+readyErr.Error())
		case ready.Readiness == completion.ReadinessReady:
			cw.Disposition = DispositionVerified
			r.Verification.TaskReadiness = "ready"
		default:
			cw.Disposition = DispositionIncomplete
			r.Verification.TaskReadiness = string(ready.Readiness)
		}
		if readyErr == nil {
			for _, ob := range ready.Obligations {
				r.Verification.ObligationsTotal++
				if ob.State == completion.EvidenceSatisfied {
					r.Verification.ObligationsSatisfied++
				}
			}
		}
	}

	r.CurrentWork = cw

	blockers := append([]taskcontrol.ClassifiedBlocker{}, state.Blockers...)
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].ID < blockers[j].ID })
	for _, b := range blockers {
		kind := "advisory"
		if b.LoadBearing {
			kind = "blocking"
			r.Summary.BlockingFindings++
		} else {
			r.Summary.AdvisoryFindings++
		}
		if len(r.Findings) < maxRenderedFindings {
			r.Findings = append(r.Findings, Finding{ID: b.ID, Kind: kind, Statement: b.Statement, Files: b.Files})
		}
	}
	if len(blockers) > len(r.Findings) {
		r.Limitations = append(r.Limitations, fmt.Sprintf("%d additional finding(s) not shown (see full task control state)", len(blockers)-len(r.Findings)))
	}

	return nil
}

// moduleKey reads the module path directly from go.mod -- the same
// identity Go tooling itself uses, never re-derived or guessed via git
// remote heuristics.
func moduleKey(repoRoot string) (string, error) {
	f, err := os.Open(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no module line found in %s", filepath.Join(repoRoot, "go.mod"))
}
