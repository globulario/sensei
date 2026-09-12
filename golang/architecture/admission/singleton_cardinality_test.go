// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Family B contract cases, written against B-R1 (9134271) and the frozen repair
// contracts (45bc4ed §2).
//
//	B-INV  For a SINGLETON event type, every occurrence must satisfy that type's
//	       payload contract, and the occurrence COUNT must satisfy that type's
//	       protocol rule. There is no selection algorithm for a single-use fact.
//
//	count == 0                          genuinely unconsumed
//	count == 1 && valid && binding      consumed
//	count >  1                          INTEGRITY FAILURE
//	count == 1 && malformed/nonbinding  INTEGRITY FAILURE
//
// Family B's defining property: the history is COMPLETE, VALID and append-only.
// An integrity anchor would certify this chain and the resurrection still occurs.

// consumedChain builds a task with a real recorded consumption and returns the
// store, task dir and current head.
func consumedChain(t *testing.T) (*ledger.Store, string, string, closureprotocol.TaskBinding) {
	t.Helper()
	task := v2Task()
	store, dir, head := admissionLedgerStore(t, task)

	dec := admittedDecision(t)
	res, err := RecordAdmissionDecided(store, head, dec, task, ledgerProducedAt())
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}
	head = res.Entry.EntryDigestSHA256

	cons, err := ConsumeCapability(dec, task, v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	res, err = RecordAdmissionConsumed(store, head, cons, ledgerProducedAt())
	if err != nil {
		t.Fatalf("record consumption: %v", err)
	}
	return store, dir, res.Entry.EntryDigestSHA256, task
}

// B-N1. An artifact-less occurrence must not hide the real record. Nothing is
// deleted; the chain stays complete, valid and append-only -- Family B's defining
// property, and the reason an integrity anchor would certify it unchanged.
//
// Two halves, because the repair has two: the WRITER must refuse to construct it,
// and the READER must refuse it when it arrives by another route. B-Q2 makes the
// reader load-bearing precisely because imported, older-version, hand-appended and
// alternate-writer histories never pass through the writer at all.
func TestBNegative1_ArtifactlessOccurrenceDoesNotHideTheRecord(t *testing.T) {
	t.Run("the writer refuses to construct it", func(t *testing.T) {
		store, dir, head, task := consumedChain(t)
		before, err := LoadRecordedConsumption(dir)
		if err != nil {
			t.Fatalf("precondition: %v", err)
		}
		_, aerr := store.Append(context.Background(), ledger.AppendRequest{
			TaskID: task.ID, SessionID: task.SessionID, ExpectedHeadDigestSHA256: head,
			EventType: closureprotocol.LedgerEventAdmissionConsumed,
			Payload: ledger.TaskEventPayload{
				SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventAdmissionConsumed,
				TaskID: task.ID, SessionID: task.SessionID,
			},
			PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: ledgerProducedAt(),
		})
		if aerr == nil {
			t.Fatal("B-N1(writer): an artifact-less admission_consumed was appended")
		}
		t.Logf("writer refused: %v", aerr)

		// And the refusal happened BEFORE history changed.
		after, rerr := LoadRecordedConsumption(dir)
		if rerr != nil || after.CapabilityID != before.CapabilityID {
			t.Errorf("B-N1(writer): the refused append dirtied history: %q -> %q err=%v", before.CapabilityID, after.CapabilityID, rerr)
		}
	})

	t.Run("the reader refuses it when it arrives by another route", func(t *testing.T) {
		// A chain carrying exactly one admission_consumed whose payload declares no
		// artifacts -- the shape an import or an alternate writer can still produce.
		dir := t.TempDir()
		payloadPath := filepath.Join(dir, "bare-payload.yaml")
		if err := os.WriteFile(payloadPath, []byte("schema_version: \"1\"\nevent_type: admission_consumed\ntask_id: task.v2\nsession_id: session.v2\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		chain := ledger.VerifiedChain{
			TaskDir: dir,
			Entries: []ledger.VerifiedEntry{{
				Entry: closureprotocol.LedgerEntry{
					Sequence: 1, EventType: closureprotocol.LedgerEventAdmissionConsumed,
					Task: v2Task(),
				},
				PayloadPath: payloadPath,
			}},
		}
		var out closureprotocol.CapabilityConsumption
		found, err := singletonArtifactFromChain(dir, chain, closureprotocol.LedgerEventAdmissionConsumed, "capability_consumption", &out)
		t.Logf("reader: found=%v err=%v", found, err)
		if err == nil {
			t.Fatal("B-N1(reader): an artifact-less occurrence was not refused")
		}
		if !isIntegrityRefusal(err) {
			t.Errorf("B-N1(reader): must be a TYPED integrity refusal, distinguishable from absence; got %v", err)
		}
		if found {
			t.Error("B-N1(reader): reported found")
		}
	})
}

// B-N2. A structurally valid but UNRELATED second receipt cannot shadow the
// binding consumption.
//
// NOT THE BINDING AXIS. After B-Q1 strengthened the invariant this construction is
// rejected at CARDINALITY, before any cross-record relation is examined -- there is
// no candidate selection at all, so the unrelated receipt never gets the chance to
// be preferred. It still proves the original property; it proves it by the stronger
// later rule.
//
// The binding predicate itself is owned by tasksession.consumptionBinds and is
// deliberately NOT duplicated in this reader: one predicate, one authority.
func TestBNegative2_UnrelatedSecondReceiptCannotShadowTheConsumption(t *testing.T) {
	store, dir, head, task := consumedChain(t)
	before, err := LoadRecordedConsumption(dir)
	if err != nil {
		t.Fatalf("precondition: %v", err)
	}

	// A consumption receipt for a DIFFERENT capability and decision: structurally
	// valid, and bound to nothing in this task.
	foreign := before
	foreign.CapabilityID = "capability.someone_else"
	foreign.DecisionDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	ref, serr := store.StoreArtifactBytes(mustJSON(t, foreign), "application/json")
	if serr != nil {
		t.Fatal(serr)
	}
	if _, aerr := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: task.ID, SessionID: task.SessionID, ExpectedHeadDigestSHA256: head,
		EventType: closureprotocol.LedgerEventAdmissionConsumed,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventAdmissionConsumed,
			TaskID: task.ID, SessionID: task.SessionID,
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{"capability_consumption": ref},
		},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: ledgerProducedAt(),
	}); aerr != nil {
		t.Logf("the WRITER refused a non-binding second occurrence: %v", aerr)
		return
	}
	t.Log("the writer accepted a non-binding second admission_consumed")

	got, gerr := LoadRecordedConsumption(dir)
	t.Logf("reader: capability=%q err=%v", got.CapabilityID, gerr)
	if gerr == nil && got.CapabilityID == "capability.someone_else" {
		t.Error("B-N2: a non-binding occurrence SHADOWED the binding one; latest-wins selected a receipt bound to nothing in this task")
	}
	if gerr == nil && got.CapabilityID == before.CapabilityID {
		return // the binding record survived
	}
	if gerr != nil && !isIntegrityRefusal(gerr) {
		t.Errorf("B-N2: refused, but not as a typed integrity failure: %v", gerr)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// B-N4. A SECOND occurrence of a singleton fact is an integrity failure in itself.
// Consumption is a uniqueness predicate, not a replaceable projection: once one
// binding spend exists, a second cannot supersede it.
//
// Per the enforcement-boundary ruling the GENERIC ledger writer may represent this
// history -- it must, so readers can be proven to refuse it. The refusal is the
// READER's, and it is a property of the occurrence SET, not a choice among its
// members.
func TestBNegative4_SecondSingletonOccurrenceIsAnIntegrityFailure(t *testing.T) {
	store, dir, head, task := consumedChain(t)

	second, err := ConsumeCapability(admittedDecision(t), task, v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt)
	if err != nil {
		t.Fatalf("build second consumption: %v", err)
	}
	if _, err := RecordAdmissionConsumed(store, head, second, ledgerProducedAt()); err != nil {
		t.Fatalf("the generic ledger must be able to REPRESENT a contradictory history so readers can refuse it: %v", err)
	}

	rep, verr := store.Verify()
	t.Logf("chain: Valid=%v entries=%d err=%v", rep.Valid, rep.EntryCount, verr)
	if !rep.Valid {
		t.Fatal("B-N4 requires a STRUCTURALLY VALID chain; otherwise it proves Family A, not Family B")
	}

	got, gerr := LoadRecordedConsumption(dir)
	t.Logf("reader: capability=%q err=%v", got.CapabilityID, gerr)
	if gerr == nil {
		t.Fatal("B-N4: two occurrences of a single-use fact, and the reader SELECTED one. There must be no selection algorithm")
	}
	var ie *EventIntegrityError
	if !errors.As(gerr, &ie) || ie.Code != CodeMultipleOccurrences {
		t.Errorf("B-N4: want a typed %s refusal; got %v", CodeMultipleOccurrences, gerr)
	}
}

// B-N3. A record that is PRESENT but undecodable is never equivalent to absent.
func TestBNegative3_UndecodableRecordIsAnIntegrityFailureNotAbsence(t *testing.T) {
	_, dir, _, _ := consumedChain(t)

	arts, _ := filepath.Glob(filepath.Join(dir, "artifacts", "sha256", "*"))
	corrupted := ""
	for _, a := range arts {
		b, _ := os.ReadFile(a)
		if strings.Contains(string(b), "consumed_operation_ids") {
			if err := os.WriteFile(a, []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
			corrupted = filepath.Base(a)
			break
		}
	}
	if corrupted == "" {
		t.Skip("no consumption artifact found")
	}
	t.Logf("CORRUPTED %s", corrupted)

	_, err := LoadRecordedConsumption(dir)
	t.Logf("reader err=%v", err)
	if err == nil {
		t.Fatal("B-N3: an undecodable record loaded cleanly")
	}
	if !isIntegrityRefusal(err) {
		t.Errorf("B-N3: an undecodable record must be a TYPED integrity failure, distinguishable from absence; got %v", err)
	}
}

// B-P1. SUPERSEDING types must keep superseding. admission_decided may legitimately
// recur, and the later decision is the current one.
func TestBPositive1_SupersedingTypeStillSupersedes(t *testing.T) {
	task := v2Task()
	store, dir, head := admissionLedgerStore(t, task)

	first := admittedDecision(t)
	res, err := RecordAdmissionDecided(store, head, first, task, ledgerProducedAt())
	if err != nil {
		t.Fatal(err)
	}
	head = res.Entry.EntryDigestSHA256

	second := admittedDecision(t)
	second.DecisionID = "decision.superseding"
	if _, err := RecordAdmissionDecided(store, head, second, task, ledgerProducedAt()); err != nil {
		t.Fatalf("B-P1: a legitimate re-decision was refused: %v", err)
	}
	got, err := LoadRecordedDecision(dir)
	if err != nil {
		t.Fatalf("B-P1: %v", err)
	}
	if got.DecisionID != "decision.superseding" {
		t.Errorf("B-P1: the later decision must be current; got %q", got.DecisionID)
	}
}

// B-P2. A task with genuinely no consumption reads as unconsumed. This is the
// legitimate absence the count predicate protects.
func TestBPositive2_GenuinelyAbsentReadsAsAbsent(t *testing.T) {
	task := v2Task()
	store, dir, head := admissionLedgerStore(t, task)
	if _, err := RecordAdmissionDecided(store, head, admittedDecision(t), task, ledgerProducedAt()); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRecordedConsumption(dir)
	if err == nil {
		t.Fatal("B-P2: a task that never consumed reported a consumption")
	}
	if isIntegrityRefusal(err) {
		t.Errorf("B-P2: genuine absence must NOT be an integrity failure; got %v", err)
	}
	t.Logf("absence reported as: %v", err)
}

// isIntegrityRefusal requires the TYPED error, not a rendering of it.
//
// An early draft matched the substring "invalid", which a raw json decode error
// satisfies -- so B-N3 passed while the reader was returning `invalid character
// 'n' looking for beginning of object key string`. A decode failure that happens
// to READ like a refusal is not one a caller can act on. The contract requires
// the reader to make "damaged" DISTINGUISHABLE from "absent", and a predicate
// that cannot tell them apart cannot test for it.
//
// errors.As is the strongest available form: it survives wrapping, and it cannot
// be satisfied by wording.
func isIntegrityRefusal(err error) bool {
	var ie *EventIntegrityError
	return errors.As(err, &ie)
}

// B-N5. An occurrence of a RESERVED_UNPRODUCED type must never participate in an
// authority reduction.
//
// Four of the nineteen declared types have no producer at all (B-R1 §2.3). The
// refusal is deliberately narrower than "corrupt": the vocabulary RECOGNISES the
// token, so it is not unknown. What is missing is a ratified producer contract,
// and the refusal should say which of the two it is.
func TestBNegative5_UnratifiedEventIsRefusedNotInterpreted(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "unratified-payload.yaml")
	if err := os.WriteFile(payloadPath, []byte("schema_version: \"1\"\nevent_type: evidence_recorded\ntask_id: task.v2\nsession_id: session.v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chain := ledger.VerifiedChain{
		TaskDir: dir,
		Entries: []ledger.VerifiedEntry{{
			Entry: closureprotocol.LedgerEntry{
				Sequence: 1, EventType: closureprotocol.LedgerEventEvidenceRecorded, Task: v2Task(),
			},
			PayloadPath: payloadPath,
		}},
	}
	var out map[string]any
	found, err := singletonArtifactFromChain(dir, chain, closureprotocol.LedgerEventEvidenceRecorded, "whatever", &out)
	t.Logf("reader: found=%v err=%v", found, err)
	if err == nil {
		t.Fatal("B-N5: an unratified event type was interpreted rather than refused")
	}
	// TYPED, not textual. Matching the word "unratified" would also be satisfied by
	// an unrelated decoder failure that happened to mention it; the axis under test
	// is that the vocabulary RECOGNISES the token while the protocol has not
	// ratified its authority semantics, and only the code says that.
	var ie *EventIntegrityError
	if !errors.As(err, &ie) || ie.Code != CodeUnratifiedEvent {
		t.Errorf("B-N5: want a typed %s refusal; got %v", CodeUnratifiedEvent, err)
	}
	if found {
		t.Error("B-N5: reported found")
	}

	// A task with no such event at all is still plain absence.
	empty := ledger.VerifiedChain{TaskDir: dir}
	if f, e := singletonArtifactFromChain(dir, empty, closureprotocol.LedgerEventEvidenceRecorded, "whatever", &out); f || e != nil {
		t.Errorf("B-N5: absence of an unratified type must be absence, not a refusal: found=%v err=%v", f, e)
	}
}
