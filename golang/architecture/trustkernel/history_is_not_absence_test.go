// SPDX-License-Identifier: AGPL-3.0-only

// Package trustkernel is a test-only LEAF over the packages that decide authority.
//
// golang/architecture/ledger answers "is this history intact" and
// golang/architecture/admission answers "was this capability spent". Each already
// carries its own repairs and each is guarded ONLY inside its own package suite, so a
// change that satisfies one package's tests while breaking a property that spans both
// has nothing to fail. This surface asserts the invariants TOGETHER, from outside.
//
// It is a LEAF on purpose: it may import kernel packages, and no package may import it
// (TestNoPackageImportsTheTrustKernel). A guard the guarded code can reach is a guard
// the guarded code can weaken.
//
// It asserts; it does not repair. If a witness here shows a kernel package does not
// hold its invariant, that is a finding for the architect, not a patch to ledger or
// admission made from this file.
package trustkernel

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

const (
	fixtureTaskID    = "task.trustkernel"
	fixtureSessionID = "session.trustkernel"
	fixtureProducer  = "trustkernel.test"

	// absenceReport is the phrase a reader uses when an event of a type was never
	// recorded. Damage must never be reported with it: that conflation is the whole
	// of #353 and #354.
	absenceReport = "no admission_consumed event found in task ledger"
)

// taskEventValidator is the payload validator the admission loaders themselves install,
// so a chain built here is verified under the same contract the governance path uses.
func taskEventValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

// governedChain builds a REAL chain of n hash-linked entries through the exported
// ledger API -- appended, verified and HEAD-published exactly as the governance path
// writes them. Nothing here is hand-assembled: a fixture that fabricates its own
// entries would witness the fixture, not the ledger.
func governedChain(t *testing.T, n int) string {
	t.Helper()
	taskDir := t.TempDir()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskEventValidator))
	head := ""
	for i := 0; i < n; i++ {
		res, err := store.Append(context.Background(), ledger.AppendRequest{
			TaskID:                   fixtureTaskID,
			SessionID:                fixtureSessionID,
			ExpectedHeadDigestSHA256: head,
			EventType:                closureprotocol.LedgerEventTaskPrepared,
			// A DISTINCT payload per entry, so each entry has its own content-addressed
			// payload artifact and deleting an entry genuinely orphans something.
			Payload: ledger.TaskEventPayload{
				SchemaVersion: ledger.EventPayloadSchemaVersion,
				EventType:     closureprotocol.LedgerEventTaskPrepared,
				TaskID:        fixtureTaskID,
				SessionID:     fixtureSessionID,
				Status:        fmt.Sprintf("step-%d", i),
			},
			PayloadMediaType: "application/yaml",
			ProducerID:       fixtureProducer,
			ProducedAt:       time.Date(2026, 9, 16, 10, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("build fixture chain, append %d: %v", i, err)
		}
		head = res.Entry.EntryDigestSHA256
	}
	return taskDir
}

// entryFiles lists a task's chain entry files, lowest sequence first.
func entryFiles(t *testing.T, taskDir string) []string {
	t.Helper()
	dirEntries, err := os.ReadDir(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatalf("read ledger dir: %v", err)
	}
	var files []string
	for _, e := range dirEntries {
		if e.IsDir() || e.Name() == "HEAD.yaml" || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		files = append(files, filepath.Join(taskDir, "ledger", e.Name()))
	}
	sort.Strings(files)
	return files
}

// corruptEntry overwrites a chain entry with bytes that are not a ledger entry at all.
func corruptEntry(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("{{ not: [an entry"), 0o644); err != nil {
		t.Fatalf("corrupt %s: %v", filepath.Base(path), err)
	}
}

// verifyWithoutPanicking runs the verifier and converts a panic into a NAMED test
// failure.
//
// A panic is not a verdict. #356: one corrupt entry took a task's whole governance path
// down by panic instead of returning the typed refusal the design calls for, and a
// fail-closed system that panics has no verdict to fail closed with. The recover is here
// so the witness reports THAT, rather than a bare stack trace from somewhere in ledger.
func verifyWithoutPanicking(t *testing.T, taskDir string) (report ledger.VerificationReport, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("#356: verification PANICKED instead of returning a verdict (%v); "+
				"a fail-closed reader that panics has no verdict to fail closed with", r)
		}
	}()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskEventValidator))
	return store.Verify()
}

func errorCodes(report ledger.VerificationReport) []string {
	codes := make([]string, 0, len(report.Errors))
	for _, e := range report.Errors {
		codes = append(codes, e.Code)
	}
	return codes
}

func hasErrorCode(report ledger.VerificationReport, code string) bool {
	for _, e := range report.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

func hasErrorCodePrefix(report ledger.VerificationReport, prefix string) bool {
	for _, e := range report.Errors {
		if strings.HasPrefix(e.Code, prefix) {
			return true
		}
	}
	return false
}

// damageShape is one way a history is destroyed or corrupted, together with the verdict
// the reader owes for it.
type damageShape struct {
	name   string
	defect string
	apply  func(t *testing.T, taskDir string)

	// wantErrorCode is the exact typed code that must NAME this damage. When set, the
	// report must also be invalid.
	wantErrorCode string
	// wantErrorCodePrefix is the family of codes that may name this damage, for shapes
	// whose exact code depends on how the bytes were mangled.
	wantErrorCodePrefix string

	// wantOrphansNameTheDamage is set for the one shape the VERIFIER alone cannot call
	// invalid. See the comment on the removed-directory case.
	wantOrphansNameTheDamage bool

	// admissionMayReportAbsence records, honestly, the one shape for which the admission
	// reader still answers "no such event". See the removed-directory case.
	admissionMayReportAbsence bool
}

// DESTROYED OR CORRUPT HISTORY IS NEVER ABSENCE.
//
// Two defects, one class. Removing a task's ledger directory made listLedgerEntryFiles
// return (nil, nil), so a destroyed history verified as VALID with zero entries (#353).
// Deleting the highest-sequence entry left a valid PREFIX -- every sequence link and
// previous-digest check satisfied -- whose only witness was HEAD, recorded as a warning
// that report.Valid ignores and no governance caller inspects (#352). Either way a
// reader saw genuine ABSENCE of an event and could reconstruct authority that event had
// already spent.
//
// The class is asserted over a TABLE rather than one example, because a single example
// proves only that one deletion is caught. The shapes differ in which guard has to fire:
// the HEAD/chain direction check, the per-entry checks, the orphan scan.
func TestDestroyedOrCorruptHistoryIsNeverAbsence(t *testing.T) {
	shapes := []damageShape{
		{
			name:   "the highest-sequence entry is deleted",
			defect: "#352: a truncated chain is a valid PREFIX; HEAD is the only witness it was longer",
			apply: func(t *testing.T, taskDir string) {
				files := entryFiles(t, taskDir)
				if err := os.Remove(files[len(files)-1]); err != nil {
					t.Fatalf("truncate chain: %v", err)
				}
			},
			wantErrorCode: "ledger.head_not_in_chain",
		},
		{
			// THE VERIFIER CANNOT CALL THIS INVALID, AND THAT IS DELIBERATE.
			//
			// `rm -rf <taskDir>/ledger` leaves a task indistinguishable from one that has no
			// chain yet, and a task with no chain yet MUST verify or no task could ever be
			// created. The destroyed-vs-fresh discrimination therefore lives in tasksession,
			// which can see the task's own state; the verifier cannot.
			//
			// What the verifier still owes is that the report is not SILENT. The payload
			// artifacts of the destroyed entries survive the deletion, and the orphan scan is
			// the only thing at this seam that says they are there with nothing referencing
			// them. That naming is what this shape asserts.
			name:   "the whole ledger directory is removed",
			defect: "#353: listLedgerEntryFiles returned (nil, nil), so destroyed history verified VALID with zero entries",
			apply: func(t *testing.T, taskDir string) {
				if err := os.RemoveAll(filepath.Join(taskDir, "ledger")); err != nil {
					t.Fatalf("destroy ledger: %v", err)
				}
			},
			wantOrphansNameTheDamage:  true,
			admissionMayReportAbsence: true,
		},
		{
			name:   "the FIRST entry is corrupted",
			defect: "#356: the first entry unreadable left out.Entries empty and indexing it panicked the verifier",
			apply: func(t *testing.T, taskDir string) {
				corruptEntry(t, entryFiles(t, taskDir)[0])
			},
			wantErrorCodePrefix: "ledger.entry_",
		},
		{
			name:   "a MIDDLE entry is corrupted",
			defect: "#356: a skipped entry makes the loaded slice diverge from the file listing",
			apply: func(t *testing.T, taskDir string) {
				files := entryFiles(t, taskDir)
				corruptEntry(t, files[len(files)/2])
			},
			wantErrorCodePrefix: "ledger.entry_",
		},
		{
			name:   "the LAST entry is corrupted",
			defect: "#356: the entry HEAD names is present but unreadable -- damage, not staleness",
			apply: func(t *testing.T, taskDir string) {
				files := entryFiles(t, taskDir)
				corruptEntry(t, files[len(files)-1])
			},
			wantErrorCodePrefix: "ledger.entry_",
		},
	}

	// A table that emptied itself would make every assertion below vacuous and the suite
	// would still report PASS.
	if len(shapes) == 0 {
		t.Fatal("the damage table is empty: this class asserts nothing")
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			taskDir := governedChain(t, 4)
			shape.apply(t, taskDir)

			report, err := verifyWithoutPanicking(t, taskDir)
			if err != nil {
				// An error return is itself a typed verdict; nothing further is owed.
				return
			}

			switch {
			case shape.wantErrorCode != "":
				if report.Valid {
					t.Errorf("%s\ndamaged history reported VALID (entries=%d warnings=%+v); "+
						"a reader may now reconstruct authority a deleted entry had already spent",
						shape.defect, report.EntryCount, report.Warnings)
				}
				if !hasErrorCode(report, shape.wantErrorCode) {
					t.Errorf("%s\nno error NAMES the damage as %q; got errors=%v warnings=%+v",
						shape.defect, shape.wantErrorCode, errorCodes(report), report.Warnings)
				}
			case shape.wantErrorCodePrefix != "":
				if report.Valid {
					t.Errorf("%s\ncorrupt history reported VALID (entries=%d); the damage is unnamed",
						shape.defect, report.EntryCount)
				}
				if !hasErrorCodePrefix(report, shape.wantErrorCodePrefix) {
					t.Errorf("%s\nno error in the %q family names the corrupted entry; got errors=%v",
						shape.defect, shape.wantErrorCodePrefix, errorCodes(report))
				}
			case shape.wantOrphansNameTheDamage:
				if len(report.OrphanArtifacts) == 0 {
					t.Errorf("%s\nthe report is SILENT about a destroyed history: valid=%v entries=%d "+
						"errors=%v orphan_artifacts=none. The surviving payloads of the deleted entries "+
						"are the only witness at this seam that anything was there",
						shape.defect, report.Valid, report.EntryCount, errorCodes(report))
				}
			default:
				t.Fatalf("damage shape %q declares no expected verdict", shape.name)
			}

			// THE CROSS-PACKAGE HALF, and the reason this surface is not two package suites.
			// Whatever the ledger decided, the admission reader must not turn damage into
			// "no such event" -- absence is what re-grants a spent capability.
			_, consumptionErr := admission.LoadRecordedConsumption(taskDir)
			if shape.admissionMayReportAbsence {
				return
			}
			if consumptionErr == nil {
				t.Fatalf("%s\nthe admission reader accepted a damaged history without error", shape.defect)
			}
			if strings.Contains(consumptionErr.Error(), absenceReport) {
				t.Errorf("%s\nthe admission reader reported ABSENCE for a DAMAGED history: %v",
					shape.defect, consumptionErr)
			}
		})
	}
}

// THE NEGATIVE CONTROL FOR THE HISTORY CLASS.
//
// Every refusal above is satisfiable by a reader that refuses everything. An intact
// chain must still verify, with its entries counted and nothing orphaned.
func TestAnIntactChainStillVerifiesValid(t *testing.T) {
	taskDir := governedChain(t, 4)

	report, err := verifyWithoutPanicking(t, taskDir)
	if err != nil {
		t.Fatalf("an intact chain was refused: %v", err)
	}
	if !report.Valid {
		t.Fatalf("an intact chain was reported INVALID: %v", errorCodes(report))
	}
	if report.EntryCount != 4 {
		t.Errorf("an intact 4-entry chain verified %d entries", report.EntryCount)
	}
	if len(report.OrphanArtifacts) != 0 {
		t.Errorf("an intact chain reported orphan artifacts %v; the damage signal fires on undamaged history",
			report.OrphanArtifacts)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("an intact chain carried warnings %+v", report.Warnings)
	}
}

// malformedShape is one way authority evidence is malformed, with the reader that must
// refuse it.
type malformedShape struct {
	name   string
	defect string
	// build damages a chain and returns a reader's answer: whether the artifact was
	// reported PRESENT, and the refusal.
	build func(t *testing.T, taskDir string) (found bool, err error)
	// forbiddenPhrase is the absence wording this damage must never be reported with.
	forbiddenPhrase string
}

// MALFORMED AUTHORITY EVIDENCE FAILS CLOSED WITH A TYPED REFUSAL AND NEVER DOWNGRADES
// TO A WEAKER PATH.
//
// latestArtifactFromChain returned (false, nil) -- "no such event" -- when the latest
// event of a type lacked a REQUIRED artifact (#354). That conflation let an appended
// artifact-less event SHADOW the real record below it: append rights alone, no deletion,
// and a chain that stays complete and valid throughout.
//
// Two damage shapes, because the two halves fail at different gates: an event type whose
// contract DECLARES a required artifact is refused when the chain is verified, while an
// event whose required artifacts are known only to its loader gets that far and must be
// refused there.
func TestMalformedAuthorityEvidenceFailsClosedWithATypedRefusal(t *testing.T) {
	shapes := []malformedShape{
		{
			name:   "an admission_consumed event carrying no capability_consumption artifact",
			defect: "#354: an artifact-less event of an existing type was read as 'no such event'",
			build: func(t *testing.T, taskDir string) (bool, error) {
				appendArtifactLessEvent(t, taskDir, closureprotocol.LedgerEventAdmissionConsumed, "damaged")
				var out closureprotocol.CapabilityConsumption
				return admission.LoadLatestArtifactOptional(taskDir,
					closureprotocol.LedgerEventAdmissionConsumed, "capability_consumption", &out)
			},
			forbiddenPhrase: absenceReport,
		},
		{
			// THE SHADOWING SHAPE. A REAL record is recorded first, then an artifact-less
			// event of the same type is appended ON TOP. Nothing is deleted and nothing is
			// forged. The reader scans backwards, stops at the latest event of the type, and
			// must refuse -- it may neither report absence nor keep scanning back to the
			// earlier record, which would let a later record be silently overridden by an
			// earlier one: the same defect pointed the other way.
			name:   "a real consumption shadowed by a later artifact-less one",
			defect: "#354: the latest event of a type shadowed the real record below it",
			build: func(t *testing.T, taskDir string) (bool, error) {
				recordRealConsumption(t, taskDir)
				appendArtifactLessEvent(t, taskDir, closureprotocol.LedgerEventAdmissionConsumed, "shadow")
				got, err := admission.LoadRecordedConsumption(taskDir)
				if err == nil {
					t.Errorf("the shadowed read SUCCEEDED, returning capability_id=%q from a record "+
						"the latest event of its type does not carry", got.CapabilityID)
				}
				return err == nil, err
			},
			forbiddenPhrase: absenceReport,
		},
		{
			// The authority_resolved bundle's four artifacts are required by its LOADER, not
			// by the event contract table, so an artifact-less authority_resolved event
			// verifies clean and reaches the loader. It must still refuse rather than fall
			// back to the complete record underneath it.
			name:   "an authority_resolved bundle shadowed by a later artifact-less event",
			defect: "#354: a shadowing event must not downgrade the reader to the record below",
			build: func(t *testing.T, taskDir string) (bool, error) {
				recordRealAuthority(t, taskDir)
				if _, err := admission.LoadRecordedAuthority(taskDir); err != nil {
					t.Fatalf("fixture: the intact authority bundle did not load: %v", err)
				}
				appendArtifactLessEvent(t, taskDir, closureprotocol.LedgerEventAuthorityResolved, "shadow")
				_, err := admission.LoadRecordedAuthority(taskDir)
				return err == nil, err
			},
			forbiddenPhrase: "no authority_resolved event found in task ledger",
		},
	}

	if len(shapes) == 0 {
		t.Fatal("the malformed-evidence table is empty: this class asserts nothing")
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			taskDir := governedChain(t, 2)

			found, err := shape.build(t, taskDir)

			if found {
				t.Fatalf("%s\nmalformed evidence was reported as PRESENT", shape.defect)
			}
			if err == nil {
				t.Fatalf("%s\nmalformed evidence yielded (false, nil): a DAMAGED record reported as an "+
					"ABSENT one, which is exactly the conflation that lets a shadowing append erase a "+
					"record it has no rights to delete", shape.defect)
			}
			if strings.Contains(err.Error(), shape.forbiddenPhrase) {
				t.Errorf("%s\ndamage was refused AS ABSENCE (%q): %v", shape.defect, shape.forbiddenPhrase, err)
			}
		})
	}
}

// THE NEGATIVE CONTROL FOR THE EVIDENCE CLASS.
//
// The refusals above are all satisfiable by a loader that refuses every record. A
// well-formed consumption must read back the value that was actually recorded -- not
// merely "some record loaded".
func TestAWellFormedRecordReadsBackItsRealValue(t *testing.T) {
	taskDir := governedChain(t, 2)
	recordRealConsumption(t, taskDir)

	got, err := admission.LoadRecordedConsumption(taskDir)
	if err != nil {
		t.Fatalf("a well-formed consumption was refused: %v", err)
	}
	want := realConsumption()
	if got.CapabilityID != want.CapabilityID {
		t.Errorf("capability_id read back as %q, recorded as %q", got.CapabilityID, want.CapabilityID)
	}
	if got.DecisionDigestSHA256 != want.DecisionDigestSHA256 {
		t.Errorf("decision_digest_sha256 read back as %q, recorded as %q",
			got.DecisionDigestSHA256, want.DecisionDigestSHA256)
	}
	if len(got.ConsumedOperationIDs) != len(want.ConsumedOperationIDs) {
		t.Fatalf("consumed_operation_ids read back as %v, recorded as %v",
			got.ConsumedOperationIDs, want.ConsumedOperationIDs)
	}
	for i := range want.ConsumedOperationIDs {
		if got.ConsumedOperationIDs[i] != want.ConsumedOperationIDs[i] {
			t.Errorf("consumed_operation_ids read back as %v, recorded as %v",
				got.ConsumedOperationIDs, want.ConsumedOperationIDs)
			break
		}
	}
}

func fixtureTask() closureprotocol.TaskBinding {
	return closureprotocol.TaskBinding{ID: fixtureTaskID, SessionID: fixtureSessionID}
}

func realConsumption() closureprotocol.CapabilityConsumption {
	return closureprotocol.CapabilityConsumption{
		CapabilityID:         "capability.trustkernel.1",
		Task:                 fixtureTask(),
		ConsumedOperationIDs: []string{"operation.trustkernel.1"},
		ConsumedAt:           "2026-09-16T11:00:00Z",
		DecisionDigestSHA256: "1d1d3cbd4f5b7fbf3a1ea0e3e0f0a4cbb4a51d0b6f2f4a7cf0b3f1b0d0a9c8e7",
	}
}

func currentHead(t *testing.T, taskDir string) string {
	t.Helper()
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("read ledger head: %v", err)
	}
	return head
}

// recordRealConsumption appends a genuine admission_consumed event through admission's own
// recorder, artifact and all.
func recordRealConsumption(t *testing.T, taskDir string) {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskEventValidator))
	if _, err := admission.RecordAdmissionConsumed(store, currentHead(t, taskDir), realConsumption(),
		time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("record admission_consumed: %v", err)
	}
}

// recordRealAuthority appends a genuine authority_resolved bundle through admission's own
// recorder.
func recordRealAuthority(t *testing.T, taskDir string) {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskEventValidator))
	if _, err := admission.RecordAuthorityResolved(store, currentHead(t, taskDir), fixtureTask(),
		closureprotocol.AuthorityResolution{}, closureprotocol.ActorBinding{},
		closureprotocol.ChangePlan{}, closureprotocol.BaseBinding{}, nil,
		time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("record authority_resolved: %v", err)
	}
}

// appendArtifactLessEvent appends a well-formed, correctly hash-linked event of eventType
// that carries NO artifacts.
//
// It is APPEND-ONLY: nothing is deleted, nothing is rewritten, no digest is forged. That
// is the point -- #354 needed no more rights than any producer already has. The store is
// built without a payload validator because that is the world the defect came from: the
// event contract did not yet declare the artifact required, so an event of this shape
// validated, appended, and became the latest event of its type.
func appendArtifactLessEvent(t *testing.T, taskDir string, eventType closureprotocol.LedgerEventType, marker string) {
	t.Helper()
	store := ledger.NewStore(taskDir)
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   fixtureTaskID,
		SessionID:                fixtureSessionID,
		ExpectedHeadDigestSHA256: currentHead(t, taskDir),
		EventType:                eventType,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     eventType,
			TaskID:        fixtureTaskID,
			SessionID:     fixtureSessionID,
			Status:        marker,
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       fixtureProducer,
		ProducedAt:       time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("append artifact-less %s event: %v", eventType, err)
	}
}

// THE LEAF LAW.
//
// This surface may import the packages that decide authority; none of them may import
// it. A guard the guarded code can reach is a guard the guarded code can weaken -- and
// an import cycle would force the assertions to move INTO the package they check, where
// they would be one refactor away from being adjusted to match the behaviour they exist
// to constrain.
func TestNoPackageImportsTheTrustKernel(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the trustkernel package directory")
	}
	selfDir := filepath.Dir(thisFile)
	repoRoot := filepath.Clean(filepath.Join(selfDir, "..", "..", ".."))
	const selfImportPath = "github.com/globulario/sensei/golang/architecture/trustkernel"

	fset := token.NewFileSet()
	var offenders []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return fs.SkipDir
			}
			if path == selfDir {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// A file this walk cannot parse is not evidence of an import; the compiler
			// owns that failure, not this witness.
			return nil
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == selfImportPath {
				rel, relErr := filepath.Rel(repoRoot, path)
				if relErr != nil {
					rel = path
				}
				offenders = append(offenders, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("the trust kernel surface is imported by %v; it must stay a LEAF, or the code it "+
			"constrains can reach the assertions that constrain it", offenders)
	}
}
