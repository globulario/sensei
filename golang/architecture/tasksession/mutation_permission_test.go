// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

// The three healthy-binding cases G3 was measured on, pinned.
//
// Each drives a REAL governed chain -- Prepare, DecideAdmission,
// RecordAdmissionDecided, ConsumeCapability, RecordAdmissionConsumed -- and
// asserts that the ledger-derived answer and every public reader agree. The
// binding is asserted healthy first, because a stale binding refuses everything
// and would make all three pass for the wrong reason.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// readers collects what every public projection says about mutation permission.
type readers struct {
	control  string
	rebuild  string
	briefing string
	ledger   bool // governance granted a new capability
}

func readAll(t *testing.T, repo, taskDir string) readers {
	t.Helper()
	var out readers
	gov, err := governanceDisposition(taskDir, time.Now().UTC(), nil)
	if err != nil {
		t.Fatalf("governanceDisposition: %v", err)
	}
	out.ledger = gov.GrantModify

	st, _, err := ControlStatus(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ControlStatus: %v", err)
	}
	out.control = st.Permission.Modify

	rs, _, _, err := ResolveControlAndClosure(repo, taskDir, false)
	if err != nil {
		t.Fatalf("ResolveControlAndClosure: %v", err)
	}
	out.rebuild = rs.Permission.Modify

	br, err := BuildTaskBriefing(repo, taskDir, "gin.go", false)
	if err != nil {
		t.Fatalf("BuildTaskBriefing: %v", err)
	}
	out.briefing = br.Modify
	return out
}

func assertBindingHealthy(t *testing.T, repo, taskDir string) {
	t.Helper()
	session, _, err := loadSessionForControl(filepath.Join(taskDir, "session.yaml"))
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if errs := verifySession(repo, taskDir, session, nil); len(errs) != 0 {
		t.Fatalf("binding is not healthy (%v); a stale binding refuses everything and would make this pass for the wrong reason", errs)
	}
}

func assertAllReadersSay(t *testing.T, r readers, want string, why string) {
	t.Helper()
	for name, got := range map[string]string{"ControlStatus": r.control, "ResolveControlAndClosure": r.rebuild, "BuildTaskBriefing": r.briefing} {
		if got != want {
			t.Fatalf("%s reports modify=%q, want %q: %s", name, got, want, why)
		}
	}
}

// G3-1. Authority is resolved but no typed decision binds, so the ledger grants
// nothing -- while the decision FILE says admitted. Every public reader must
// withhold. This is the over-authorization direction.
func TestNoTypedDecisionMeansNoReaderGrantsMutation(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	assertBindingHealthy(t, repo, taskDir)

	// A real, digest-valid positive decision file that no chain entry references.
	dpath := filepath.Join(taskDir, "admission", "decision.yaml")
	dec, err := admission.LoadDecision(dpath)
	if err != nil {
		t.Fatalf("load decision: %v", err)
	}
	dec.Decision = admission.CapabilityAdmitted
	dec.MutationCapability = admission.CapabilityAdmitted
	if err := admission.WriteCanonicalDecision(dpath, dec); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	r := readAll(t, repo, taskDir)
	if r.ledger {
		t.Fatal("governance granted mutation with no admission_decided on the chain")
	}
	assertAllReadersSay(t, r, admission.CapabilityWaiting,
		"the decision file is not an authority; only the ledger grants a capability")
}

// G3-2. A typed decision binds and its capability is unconsumed, so the ledger
// grants mutation -- while the decision FILE still says waiting. Every public
// reader must report the ledger-derived grant. This is the under-reporting
// direction, and it is the case a file-first reader gets wrong in the other way.
func TestTypedGrantIsReportedByEveryReader(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)
	assertBindingHealthy(t, repo, taskDir)

	if fileDec, err := admission.LoadDecision(filepath.Join(taskDir, "admission", "decision.yaml")); err == nil {
		if fileDec.MutationCapability == admission.CapabilityAdmitted {
			t.Fatal("fixture is not exercising the disagreement: the file already says admitted")
		}
	}

	r := readAll(t, repo, taskDir)
	if !r.ledger {
		t.Fatal("governance withheld mutation despite a recorded admission_decided with an unconsumed capability")
	}
	assertAllReadersSay(t, r, admission.CapabilityAdmitted,
		"a recorded typed grant must reach every reader, whatever the file says")
}

// G3-3. After consumption the answer is "no NEW consumable capability". It must
// NOT be Refused: that would assert the application already made was
// unauthorized, which is a different and false claim.
func TestAfterConsumptionReadersReportNoNewCapabilityRatherThanRefusal(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)
	assertBindingHealthy(t, repo, taskDir)

	r := readAll(t, repo, taskDir)
	if r.ledger {
		t.Fatal("governance still grants a capability after admission_consumed")
	}
	assertAllReadersSay(t, r, admission.CapabilityWaiting,
		"a consumed capability is spent, not repudiated; Refused would deny an authorized application")
	for name, got := range map[string]string{"ControlStatus": r.control, "ResolveControlAndClosure": r.rebuild, "BuildTaskBriefing": r.briefing} {
		if got == admission.CapabilityRefused {
			t.Fatalf("%s reports Refused after consumption, claiming an authorized application was not authorized", name)
		}
	}
}

// A persisted projection must never re-assert a grant the ledger has withdrawn.
// This is the measured production defect: seven control/latest.yaml files
// asserting modify=admitted for chains carrying no authority resolution.
func TestAStalePersistedProjectionCannotReAssertAGrant(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	before := readAll(t, repo, taskDir)
	if !before.ledger || before.control != admission.CapabilityAdmitted {
		t.Fatalf("fixture did not reach a granted state: %+v", before)
	}
	// Freeze that granted projection, then spend the capability.
	cachePath := filepath.Join(taskDir, "control", "latest.yaml")
	granted, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)
	if err := os.WriteFile(cachePath, granted, 0o644); err != nil {
		t.Fatalf("restore cache: %v", err)
	}

	after := readAll(t, repo, taskDir)
	if after.control == admission.CapabilityAdmitted {
		t.Fatal("a restored pre-consumption cache re-asserted the spent grant through ControlStatus")
	}
	assertAllReadersSay(t, after, admission.CapabilityWaiting,
		"the cache may carry the projection but never the permission")
}
