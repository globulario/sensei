// SPDX-License-Identifier: Apache-2.0

package questiondisposition_test

import (
	"context"
	"sync"
	"testing"

	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// TestConcurrentRecordsRecordExactlyOnce: N goroutines racing to record the same
// prepared candidate produce exactly one durable event; every caller sees either
// recorded or replayed, never a double-append.
func TestConcurrentRecordsRecordExactlyOnce(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	const workers = 6
	var wg sync.WaitGroup
	results := make([]qd.RecordResult, workers)
	errs := make([]error, workers)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = qd.RecordDisposition(context.Background(),
				qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
		}(i)
	}
	wg.Wait()

	recorded := 0
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		switch results[i].Outcome {
		case qd.OutcomeRecorded:
			recorded++
		case qd.OutcomeReplayed, qd.OutcomeReconciled:
		default:
			t.Fatalf("worker %d unexpected outcome %s", i, results[i].Outcome)
		}
	}
	if recorded != 1 {
		t.Fatalf("recorded outcomes = %d, want exactly 1", recorded)
	}
	if n := countDispositionEvents(t, env.TaskDir); n != 1 {
		t.Fatalf("disposition events = %d, want 1", n)
	}
}
