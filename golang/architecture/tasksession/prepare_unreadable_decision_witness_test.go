// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// Witness for Antigravity chunk prod-b P2 on ede08ebe. resultFromSession publishes
// PrepareResult.Modify as CURRENT permission and says it must come from the sole owner. When the
// current decision cannot be loaded, the err==nil guard skips the owner entirely and the session's
// HISTORICAL MutationCapability and NextActions are published instead.
func TestUnreadableDecisionDoesNotPublishTheHistoricalGrant(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	sess, _, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	// The historical record a stale session can carry.
	sess.MutationCapability = admission.CapabilityAdmitted
	sess.NextActions = []NextAction{{Action: NextPerformEdit, Reference: sess.TaskID}}

	// Positive control: with a readable decision the owner answers.
	if _, err := loadCurrentAdmissionDecision(taskDir); err != nil {
		t.Fatalf("precondition: the current decision is not loadable before damage: %v", err)
	}

	// Damage exactly the file the production loader resolves.
	paths, _, err := currentControlPaths(taskDir)
	if err != nil {
		t.Fatalf("control paths: %v", err)
	}
	decisionPath := currentDecisionPath(taskDir, paths.Results)
	if _, err := os.Stat(decisionPath); err != nil {
		t.Fatalf("precondition: resolved decision path %s does not exist, so damaging it proves nothing: %v", decisionPath, err)
	}
	if err := os.WriteFile(decisionPath, []byte("{{{ not a decision"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCurrentAdmissionDecision(taskDir); err == nil {
		t.Fatal("precondition: damaging the resolved decision did not make it unloadable")
	}

	res := resultFromSession(repo, taskDir, sess, "")
	if res.Modify == admission.CapabilityAdmitted {
		t.Errorf("an unreadable current decision published the historical Modify=%q as current permission", res.Modify)
	}
	if res.Next.Action == NextPerformEdit {
		t.Errorf("an unreadable current decision published the historical Next=%q", res.Next.Action)
	}
}
