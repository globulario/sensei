// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Run is the O2 bounded execution boundary: it calls a Provider's Describe
// and Execute under the request's precommitted deadline and observation
// limits, and guarantees exactly one terminal outcome and exactly one
// Receipt regardless of how execution ends -- a clean completion, a
// Provider-reported Go error, the caller's ctx being cancelled, or the
// request's deadline elapsing. No O1 mapping happens here: the returned
// Result stays a distinct, untrusted document (hard law 3) until a later,
// separately bounded checkpoint's pure mapper accepts it into
// golang/architecture/synthesis.
//
// Run recomputes and verifies every digest it references before trusting
// it -- Capabilities' and Result's declared digests, never a Provider's
// unchecked word for them (hard law 5). A Result that fails that check, or
// whose Operation/RequestDigestSHA256 does not match request, is treated as
// OutcomeInvalidOutput rather than trusted.
//
// now supplies the wall-clock reading for StartedAt/CompletedAt/observation
// timestamps -- injected so tests are deterministic; production callers
// pass time.Now.
//
// A non-nil error return means no Receipt could be built at all -- request
// itself was malformed, its deadline_at could not be parsed, Describe
// failed, or the returned Capabilities failed its own digest-integrity
// check. Every other outcome (unsupported capability, execute error,
// cancellation, timeout, invalid output, or a clean completion) returns a
// nil error alongside a fully populated Result/ObservationBatch/Receipt.
func Run(ctx context.Context, provider Provider, request Request, now func() time.Time) (Result, ObservationBatch, Receipt, error) {
	if err := validateDocument(ValidateRequestSchema, request); err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: request failed schema validation: %w", err)
	}
	deadline, err := time.Parse(time.RFC3339, request.DeadlineAt)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: request deadline_at is not RFC3339: %w", err)
	}

	execCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	capabilities, err := provider.Describe(execCtx)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: describe failed: %w", err)
	}
	if err := validateDocument(ValidateCapabilitiesSchema, capabilities); err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: capabilities failed schema validation: %w", err)
	}
	capDigest, err := CapabilitiesDigest(capabilities)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: compute capabilities digest: %w", err)
	}
	if capDigest != capabilities.CapabilitiesDigestSHA256 {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: capabilities declares digest %q but its actual computed digest is %q", capabilities.CapabilitiesDigestSHA256, capDigest)
	}

	startedAt := now()

	if !supportsOperation(capabilities, request.Operation) {
		batch, err := emptyBatch(request.RequestDigestSHA256)
		if err != nil {
			return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: build empty observation batch: %w", err)
		}
		return finish(request, capabilities, OutcomeUnsupportedCapability,
			fmt.Sprintf("provider does not claim support for operation %q", request.Operation),
			batch, startedAt, now())
	}

	observer := newBoundedObserver(request.MaxObservationCount, request.MaxObservationBytes, now)

	type executed struct {
		result Result
		err    error
	}
	winner := make(chan executed, 1)
	go func() {
		res, err := provider.Execute(execCtx, request, observer)
		select {
		case winner <- executed{res, err}:
		default:
		}
	}()

	select {
	case w := <-winner:
		return conclude(request, capabilities, w.result, w.err, observer, startedAt, now())
	case <-execCtx.Done():
		batch, batchErr := observer.batch(request.RequestDigestSHA256)
		if batchErr != nil {
			return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: build observation batch: %w", batchErr)
		}
		if ctx.Err() != nil {
			return finish(request, capabilities, OutcomeCancelled, "context cancelled before the provider returned", batch, startedAt, now())
		}
		return finish(request, capabilities, OutcomeTimedOut, "request deadline elapsed before the provider returned", batch, startedAt, now())
	}
}

// conclude handles a Provider.Execute call that returned on its own --
// successfully or with a Go error -- and decides the terminal outcome.
//
// A well-behaved Provider that itself observes ctx being done (per its
// contract obligation to return promptly) reports that as its error --
// execErr may therefore already BE a context.DeadlineExceeded/
// context.Canceled, not just local/infrastructure failure. Mapping that to
// OutcomeTimedOut/OutcomeCancelled here, the same as when Run's own select
// observes execCtx.Done() directly, keeps the outcome for one underlying
// cause consistent regardless of which side of that inherent race "wins" --
// only a genuinely unrelated Go error maps to OutcomeUnavailable.
func conclude(request Request, capabilities Capabilities, result Result, execErr error, observer *boundedObserver, startedAt, completedAt time.Time) (Result, ObservationBatch, Receipt, error) {
	batch, err := observer.batch(request.RequestDigestSHA256)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: build observation batch: %w", err)
	}

	if execErr != nil {
		if errors.Is(execErr, context.DeadlineExceeded) {
			return finish(request, capabilities, OutcomeTimedOut, "request deadline elapsed before the provider returned", batch, startedAt, completedAt)
		}
		if errors.Is(execErr, context.Canceled) {
			return finish(request, capabilities, OutcomeCancelled, "context cancelled before the provider returned", batch, startedAt, completedAt)
		}
		return finish(request, capabilities, OutcomeUnavailable, "execute failed: "+execErr.Error(), batch, startedAt, completedAt)
	}
	if result.RequestDigestSHA256 != request.RequestDigestSHA256 || result.Operation != request.Operation {
		return finish(request, capabilities, OutcomeInvalidOutput, "result operation or request reference does not match the request", batch, startedAt, completedAt)
	}
	if err := validateDocument(ValidateResultSchema, result); err != nil {
		return finish(request, capabilities, OutcomeInvalidOutput, "result failed schema validation: "+err.Error(), batch, startedAt, completedAt)
	}
	digest, err := ResultDigest(result)
	if err != nil {
		return finish(request, capabilities, OutcomeInvalidOutput, "compute result digest: "+err.Error(), batch, startedAt, completedAt)
	}
	if digest != result.ResultDigestSHA256 {
		return finish(request, capabilities, OutcomeInvalidOutput,
			fmt.Sprintf("result declares digest %q but its actual computed digest is %q", result.ResultDigestSHA256, digest),
			batch, startedAt, completedAt)
	}

	receipt, err := buildReceipt(request, capabilities, result, batch, startedAt, completedAt)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: build receipt: %w", err)
	}
	return result, batch, receipt, nil
}

// finish synthesizes a payload-free Result for a terminal outcome Run
// itself decided (unsupported capability, cancelled, timed out, unavailable,
// or invalid output), computes its real digest, and builds the
// corresponding Receipt.
func finish(request Request, capabilities Capabilities, outcome TerminalOutcome, detail string, batch ObservationBatch, startedAt, completedAt time.Time) (Result, ObservationBatch, Receipt, error) {
	result := Result{
		SchemaVersion:       ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     outcome,
		Detail:              detail,
	}
	digest, err := ResultDigest(result)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: compute synthesized result digest: %w", err)
	}
	result.ResultDigestSHA256 = digest

	receipt, err := buildReceipt(request, capabilities, result, batch, startedAt, completedAt)
	if err != nil {
		return Result{}, ObservationBatch{}, Receipt{}, fmt.Errorf("providerport: build receipt: %w", err)
	}
	return result, batch, receipt, nil
}

// buildReceipt binds request, capabilities, result, and batch into a
// Receipt and computes its real digest. ResultDigestSHA256 is always
// populated (result is always a real, digest-verified envelope by the time
// this is called, whether the outcome was a clean completion or a
// Run-synthesized failure) -- PayloadDigestSHA256/
// ObservationBatchDigestSHA256 stay nil exactly when there is nothing to
// reference.
func buildReceipt(request Request, capabilities Capabilities, result Result, batch ObservationBatch, startedAt, completedAt time.Time) (Receipt, error) {
	var payloadDigest *string
	if result.PayloadDigestSHA256 != nil {
		d := *result.PayloadDigestSHA256
		payloadDigest = &d
	}
	var batchDigest *string
	if len(batch.Observations) > 0 {
		d := batch.ObservationBatchDigestSHA256
		batchDigest = &d
	}

	r := Receipt{
		SchemaVersion:                ReceiptSchemaVersion,
		ReceiptID:                    "receipt." + request.RequestID,
		RequestDigestSHA256:          request.RequestDigestSHA256,
		CapabilitiesDigestSHA256:     capabilities.CapabilitiesDigestSHA256,
		ProviderObservation:          capabilities.ProviderObservation,
		TerminalOutcome:              result.TerminalOutcome,
		ResultDigestSHA256:           result.ResultDigestSHA256,
		PayloadDigestSHA256:          payloadDigest,
		ObservationBatchDigestSHA256: batchDigest,
		StartedAt:                    startedAt.UTC().Format(time.RFC3339),
		CompletedAt:                  completedAt.UTC().Format(time.RFC3339),
	}
	digest, err := ReceiptDigest(r)
	if err != nil {
		return Receipt{}, err
	}
	r.ReceiptDigestSHA256 = digest
	return NormalizeReceipt(r), nil
}

func supportsOperation(c Capabilities, op Operation) bool {
	for _, supported := range c.SupportedOperations {
		if supported == op {
			return true
		}
	}
	return false
}

func emptyBatch(requestDigest string) (ObservationBatch, error) {
	b := NormalizeObservationBatch(ObservationBatch{
		SchemaVersion:       ObservationBatchSchemaVersion,
		RequestDigestSHA256: requestDigest,
	})
	digest, err := ObservationBatchDigest(b)
	if err != nil {
		return ObservationBatch{}, err
	}
	b.ObservationBatchDigestSHA256 = digest
	return b, nil
}

// validateDocument marshals doc to JSON and runs it through validate,
// mirroring golang/architecture/synthesis's own unexported helper of the
// same shape and purpose (not reusable directly -- different package).
func validateDocument(validate func([]byte) error, doc any) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return validate(data)
}

// boundedObserver is the Observer Run gives to Provider.Execute. It
// enforces the request's precommitted MaxObservationCount/MaxObservationBytes
// -- once either limit is reached, Observe rejects every further call, and
// nothing a Provider does through this interface can enlarge either limit.
// Safe for concurrent use.
type boundedObserver struct {
	mu           sync.Mutex
	maxCount     int
	maxBytes     int
	byteCount    int
	observations []Observation
	now          func() time.Time
}

func newBoundedObserver(maxCount, maxBytes int, now func() time.Time) *boundedObserver {
	return &boundedObserver{maxCount: maxCount, maxBytes: maxBytes, now: now}
}

func (o *boundedObserver) Observe(detail string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.observations) >= o.maxCount {
		return fmt.Errorf("providerport: observation count limit (%d) exceeded", o.maxCount)
	}
	if o.byteCount+len(detail) > o.maxBytes {
		return fmt.Errorf("providerport: observation byte limit (%d) exceeded", o.maxBytes)
	}
	o.byteCount += len(detail)
	o.observations = append(o.observations, Observation{
		SequenceNumber: len(o.observations) + 1,
		Detail:         detail,
		ObservedAt:     o.now().UTC().Format(time.RFC3339),
	})
	return nil
}

// batch returns the ordered observations gathered so far as a real,
// digested ObservationBatch.
func (o *boundedObserver) batch(requestDigest string) (ObservationBatch, error) {
	o.mu.Lock()
	observations := append([]Observation{}, o.observations...)
	o.mu.Unlock()

	b := NormalizeObservationBatch(ObservationBatch{
		SchemaVersion:       ObservationBatchSchemaVersion,
		RequestDigestSHA256: requestDigest,
		Observations:        observations,
	})
	digest, err := ObservationBatchDigest(b)
	if err != nil {
		return ObservationBatch{}, err
	}
	b.ObservationBatchDigestSHA256 = digest
	return b, nil
}
