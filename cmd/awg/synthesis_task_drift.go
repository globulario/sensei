// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/taskcontrol"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// verifyTaskBindingUnchanged implements issue #149 hard law 10 for the
// resumption that actually exists.
//
// `sensei synthesis-run` drives one synchronous session, so there is no
// mid-session resume to refuse. What genuinely resumes is the pair of commands
// that pick up a persisted bundle afterwards -- synthesis-admit and
// synthesis-apply, minutes or days later -- and until the bundle carried a task
// binding they could detect only base-revision drift, because that was the one
// binding recorded.
//
// Task and closure drift are the more dangerous half, precisely because no
// digest inside the bundle can reveal them. A bundle whose internal chain
// verifies perfectly says only that nobody tampered with it. It says nothing
// about whether `sensei advance-task` has since published a new generation,
// recomputing the closure state and the admission decision's proof obligations.
// A candidate generated under one closure state and admitted under another is
// not the same proposal, and every receipt in the chain would still check out.
//
// The refusal is deliberately not overridable. A flag here would be a way to
// admit a stale proposal while producing a receipt that looks current, which is
// worse than having no check at all: it would launder the staleness through the
// governance surface rather than merely failing to catch it.
func verifyTaskBindingUnchanged(absRepo, taskFlag string, binding synthesisRunTaskBinding, artifactSessionDigest string) error {
	if strings.TrimSpace(binding.TaskID) == "" {
		return fmt.Errorf("this lineage bundle records no task binding, so task and closure drift cannot be checked; re-run 'sensei synthesis-run' to produce a bundle that can be")
	}
	// Tie the binding to the one document the store verifies independently.
	//
	// The bundle is a plain auxiliary file; anyone who can write it can rewrite
	// the three drift fields to the task's current values and walk past this
	// check while the candidate's own digest chain still validates. This does
	// not make the bundle tamper-proof -- nothing signs it -- but it refuses a
	// binding lifted from a different run, and a candidate swapped underneath
	// a binding. The residual exposure is real and is recorded on #149 rather
	// than papered over.
	if binding.SessionDigestSHA256 != artifactSessionDigest {
		return fmt.Errorf("lineage/candidate session mismatch: the task binding records session %s but the sealed candidate is from session %s -- this bundle and this candidate are not from the same run",
			short(binding.SessionDigestSHA256), short(artifactSessionDigest))
	}

	taskDir, err := resolveTaskDirForBundle(absRepo, taskFlag)
	if err != nil {
		return err
	}

	session, err := tasksession.LoadSession(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		return fmt.Errorf("load the task this candidate was generated under: %w", err)
	}
	if session.TaskID != binding.TaskID {
		return fmt.Errorf("task drift: the candidate was generated under task %s, but %s holds task %s",
			binding.TaskID, taskDir, session.TaskID)
	}

	// Resolved through the SAME single generation resolution synthesis-run
	// used, never independent reads: a concurrent `sensei advance-task`
	// publishes control and closure as two non-atomic writes, and two separate
	// reads can observe a pair that never coexisted as one real generation.
	control, closureReport, _, err := tasksession.ResolveControlAndClosure(absRepo, taskDir, false)
	if err != nil {
		return fmt.Errorf("resolve the task's current control and closure state: %w", err)
	}

	if got := taskcontrol.StateDigest(control); got != binding.TaskControlStateDigestSHA256 {
		return fmt.Errorf("task control drift: the candidate was generated against control state %s, but the task's current state is %s -- 'sensei advance-task' has run since, so the proof obligations this candidate was admitted under may no longer be the current ones",
			short(binding.TaskControlStateDigestSHA256), short(got))
	}

	closureDigest, err := closureprotocol.SemanticDigest(closureReport)
	if err != nil {
		return fmt.Errorf("digest the task's current closure report: %w", err)
	}
	if closureDigest != binding.ClosureReportDigestSHA256 {
		return fmt.Errorf("closure drift: the candidate was generated against closure state %s, but the task's current closure state is %s",
			short(binding.ClosureReportDigestSHA256), short(closureDigest))
	}
	return nil
}

// resolveTaskDirForBundle mirrors synthesis-run's own task resolution so all
// three commands agree on which task they are talking about.
func resolveTaskDirForBundle(absRepo, taskFlag string) (string, error) {
	taskDir := strings.TrimSpace(taskFlag)
	if taskDir == "" {
		ptr, err := tasksession.LoadActivePointer(absRepo)
		if err != nil {
			return "", fmt.Errorf("no active task and --task not given, so the candidate's task binding cannot be checked: %w", err)
		}
		// The active pointer records a REPO-RELATIVE session path. Callers
		// that happen to run from the repository root get away with using it
		// directly; these commands take --repo and must not, or they resolve
		// the task against the current working directory instead of the
		// repository the candidate belongs to.
		taskDir = filepath.Dir(ptr.SessionPath)
	}
	if !filepath.IsAbs(taskDir) {
		taskDir = filepath.Join(absRepo, taskDir)
	}
	return taskDir, nil
}
