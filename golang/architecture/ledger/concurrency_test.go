// SPDX-License-Identifier: Apache-2.0

package ledger

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestConcurrentAppendAllowsExactlyOneWriterPerHead(t *testing.T) {
	taskDir := t.TempDir()
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "prepared"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	head := report.HeadDigestSHA256

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, errs[i] = store.Append(context.Background(), AppendRequest{
				TaskID: "task.example", SessionID: "session.example", ExpectedHeadDigestSHA256: head,
				EventType:        closureprotocol.LedgerEventClosureAssessed,
				Payload:          testPayload{SchemaVersion: "1", Message: "closure"},
				PayloadMediaType: "application/yaml", ProducerID: "sensei.test", ProducedAt: time.Date(2026, 7, 15, 12, 5, i, 0, time.UTC),
			})
		}()
	}
	wg.Wait()

	successes := 0
	stales := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		var stale ErrStaleHead
		if errorAs(err, &stale) {
			stales++
			continue
		}
		t.Fatalf("unexpected append error: %v", err)
	}
	if successes != 1 || stales != 1 {
		t.Fatalf("successes=%d stales=%d", successes, stales)
	}
	report, err = store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.EntryCount != 2 {
		t.Fatalf("unexpected chain after concurrent append: %+v", report)
	}
}
