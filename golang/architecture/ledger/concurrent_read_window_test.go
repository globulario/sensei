package ledger

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// A CONCURRENT APPEND IS NOT EVIDENCE LOSS.
//
// Both completeness comparisons ask whether the entries fall SHORT of another
// record. Reading that record AFTER listing the entries let an append land in
// between, so a reader compared a stale entry list against a fresh claim and
// reported ledger.history_truncated / ledger.head_leads_entries -- damage where
// there was only concurrency. Measured at 89 and 70 spurious reads in ~1.2s.
//
// This test asserts the absence of transient invalid reads. It is deliberately
// one-sided: it can fail only when the defect is present.
func TestConcurrentAppendIsNeverReportedAsEvidenceLoss(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, ".sensei", "tasks", "task.stress")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.stress", SessionID: "s", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "seed"},
		PayloadMediaType: "application/yaml", ProducerID: "t", ProducedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	seen := map[string]int{}
	var mu sync.Mutex

	for r := 0; r < 6; r++ { // readers
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				rep, err := store.Verify()
				if err == nil && !rep.Valid {
					mu.Lock()
					for _, e := range rep.Errors {
						seen[e.Code]++
					}
					mu.Unlock()
				}
			}
		}()
	}
	for w := 0; w < 4; w++ { // writers
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				rep, err := store.Verify()
				if err != nil {
					continue
				}
				_, _ = store.Append(context.Background(), AppendRequest{
					TaskID: "task.stress", SessionID: "s", ExpectedHeadDigestSHA256: rep.HeadDigestSHA256,
					EventType:        closureprotocol.LedgerEventTaskPrepared,
					Payload:          testPayload{SchemaVersion: "1", Message: "e"},
					PayloadMediaType: "application/yaml", ProducerID: "t", ProducedAt: time.Now().UTC(),
				})
			}
		}(w)
	}
	time.Sleep(1200 * time.Millisecond)
	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Log("no invalid reads observed")
		return
	}
	for code, n := range seen {
		t.Errorf("TRANSIENT INVALID READ: %s x%d", code, n)
	}
}
