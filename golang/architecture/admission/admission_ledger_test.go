// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

func ledgerProducedAt() time.Time { return time.Unix(0, 0).UTC() }

// admissionLedgerStore builds a fresh task ledger with the real payload
// validator and a task_prepared genesis event, returning the store, its task
// directory, and its head digest.
func admissionLedgerStore(t *testing.T, task closureprotocol.TaskBinding) (*ledger.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	store := ledger.NewStore(dir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	genesis, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: "",
		EventType:                closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventTaskPrepared,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       "test",
		ProducedAt:       ledgerProducedAt(),
	})
	if err != nil {
		t.Fatalf("genesis append: %v", err)
	}
	return store, dir, genesis.Entry.EntryDigestSHA256
}

func TestAdmissionLedgerRecordsChain(t *testing.T) {
	task := v2Task()
	store, _, head := admissionLedgerStore(t, task)

	exp, observed := scopeFixture(t)
	decision := exp.Decision
	consumption := exp.Consumption
	scope, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatalf("VerifyScope: %v", err)
	}

	decided, err := RecordAdmissionDecided(store, head, decision, task, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAdmissionDecided: %v", err)
	}
	if decided.Entry.EventType != closureprotocol.LedgerEventAdmissionDecided {
		t.Fatalf("unexpected event type %q", decided.Entry.EventType)
	}
	consumed, err := RecordAdmissionConsumed(store, decided.Entry.EntryDigestSHA256, consumption, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAdmissionConsumed: %v", err)
	}
	verified, err := RecordScopeVerified(store, consumed.Entry.EntryDigestSHA256, task, scope, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordScopeVerified: %v", err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Valid {
		t.Fatalf("ledger chain is not valid: %+v", report)
	}
	if report.EntryCount != 4 { // genesis + decided + consumed + verified
		t.Fatalf("expected 4 chained entries, got %d", report.EntryCount)
	}
	if report.HeadDigestSHA256 != verified.Entry.EntryDigestSHA256 {
		t.Fatalf("head digest does not point at the last recorded event")
	}
	if verified.Entry.Sequence != 4 {
		t.Fatalf("expected scope_verified at sequence 4, got %d", verified.Entry.Sequence)
	}
}

func TestAdmissionLedgerRejectsStaleHead(t *testing.T) {
	task := v2Task()
	store, _, head := admissionLedgerStore(t, task)
	exp, _ := scopeFixture(t)
	if _, err := RecordAdmissionDecided(store, head, exp.Decision, task, ledgerProducedAt()); err != nil {
		t.Fatalf("first record: %v", err)
	}
	// Re-using the genesis head (not the new head) must be rejected as stale —
	// the append-only chain cannot be forked.
	if _, err := RecordAdmissionConsumed(store, head, exp.Consumption, ledgerProducedAt()); err == nil {
		t.Fatal("expected stale-head rejection when reusing an outdated head")
	}
}
