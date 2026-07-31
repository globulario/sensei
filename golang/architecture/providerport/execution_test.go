// SPDX-License-Identifier: AGPL-3.0-only

// execution_test.go proves Run (execution.go), the O2 bounded execution
// boundary introduced as the checkpoint after the operation-discriminated
// payload repair on PR #124 head ce00379d2fe1d1ef1b0f927a4e518d7c526f9cee:
// Provider.Describe -> Provider.Execute, bounded by the request's
// precommitted deadline/observation limits, producing exactly one terminal
// Result and Receipt regardless of how execution ends. No O1 mapping is
// exercised here -- these tests only prove the execution boundary itself.
package providerport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeProvider is a test double: every behavior is supplied by the test
// via closures, never a real model/process/network call.
type fakeProvider struct {
	capabilities Capabilities
	describeErr  error
	executeFn    func(ctx context.Context, request Request, obs Observer) (Result, error)

	mu           sync.Mutex
	executeCalls int
}

func (p *fakeProvider) Describe(ctx context.Context) (Capabilities, error) {
	return p.capabilities, p.describeErr
}

func (p *fakeProvider) Execute(ctx context.Context, request Request, obs Observer) (Result, error) {
	p.mu.Lock()
	p.executeCalls++
	p.mu.Unlock()
	return p.executeFn(ctx, request, obs)
}

func (p *fakeProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executeCalls
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// withFreshDeadline returns a copy of request with DeadlineAt set to a real
// near-future time and RequestDigestSHA256 recomputed to match. The fixture
// builders' baked-in DeadlineAt is a fixed calendar timestamp (matching the
// rest of this package's fixtures) that is not guaranteed to be in the
// future relative to real wall-clock time -- Run is the first code in this
// package to actually context.WithDeadline against DeadlineAt, so any test
// that expects Run to execute normally (rather than immediately observing
// an already-elapsed deadline) must use a request built with this helper.
func withFreshDeadline(t *testing.T, request Request, d time.Duration) Request {
	t.Helper()
	request.DeadlineAt = time.Now().Add(d).UTC().Format(time.RFC3339)
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	return request
}

// --- happy path ---

func TestRunCompletedProducesRealResultAndReceipt(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			if err := obs.Observe("planning started"); err != nil {
				t.Errorf("unexpected Observe error: %v", err)
			}
			return fixturePlanningResult(t, req.RequestDigestSHA256, plan), nil
		},
	}

	result, batch, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeCompleted {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeCompleted)
	}
	if result.PlanningPayload == nil || result.PlanningPayload.PlanDigestSHA256 != plan.PlanDigestSHA256 {
		t.Error("result does not carry the real candidate plan")
	}
	if len(batch.Observations) != 1 || batch.Observations[0].Detail != "planning started" {
		t.Errorf("batch = %+v, want exactly one 'planning started' observation", batch)
	}
	if receipt.TerminalOutcome != OutcomeCompleted {
		t.Errorf("receipt.TerminalOutcome = %s, want %s", receipt.TerminalOutcome, OutcomeCompleted)
	}
	if receipt.ResultDigestSHA256 != result.ResultDigestSHA256 {
		t.Error("receipt does not reference the result's real digest")
	}
	if receipt.PayloadDigestSHA256 == nil || *receipt.PayloadDigestSHA256 != *result.PayloadDigestSHA256 {
		t.Error("receipt does not reference the result's real payload digest")
	}
	if receipt.ObservationBatchDigestSHA256 == nil || *receipt.ObservationBatchDigestSHA256 != batch.ObservationBatchDigestSHA256 {
		t.Error("receipt does not reference the observation batch's real digest")
	}
	if err := validateDocument(ValidateReceiptSchema, receipt); err != nil {
		t.Errorf("receipt failed schema validation: %v", err)
	}
	if got, wantErr := ReceiptDigest(receipt); wantErr != nil || got != receipt.ReceiptDigestSHA256 {
		t.Errorf("receipt digest not self-consistent: declared %q, computed %q (err %v)", receipt.ReceiptDigestSHA256, got, wantErr)
	}
}

// --- unsupported capability: short-circuits before Execute is ever called ---

func TestRunUnsupportedCapabilityNeverCallsExecute(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)

	capabilities := fixtureCapabilities(t)
	capabilities.SupportedOperations = []Operation{OperationInterpretation}
	digest, err := CapabilitiesDigest(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	capabilities.CapabilitiesDigestSHA256 = digest

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			t.Fatal("Execute must not be called when the provider does not claim support for the operation")
			return Result{}, nil
		},
	}

	result, _, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeUnsupportedCapability {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeUnsupportedCapability)
	}
	if receipt.TerminalOutcome != OutcomeUnsupportedCapability {
		t.Errorf("receipt.TerminalOutcome = %s, want %s", receipt.TerminalOutcome, OutcomeUnsupportedCapability)
	}
	if provider.calls() != 0 {
		t.Errorf("Execute was called %d times, want 0", provider.calls())
	}
}

// --- execute Go error maps to unavailable ---

func TestRunExecuteErrorMapsToUnavailable(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			return Result{}, errors.New("connection refused")
		},
	}

	result, _, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeUnavailable {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeUnavailable)
	}
	if receipt.ResultDigestSHA256 != result.ResultDigestSHA256 {
		t.Error("a typed failure must not lose its Result-envelope evidence on the receipt")
	}
}

// --- cancellation and timeout ---

func TestRunContextCancelledMapsToCancelled(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	capabilities := fixtureCapabilities(t)

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			close(started)
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
	}

	go func() {
		<-started
		cancel()
	}()

	result, _, receipt, err := Run(ctx, provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeCancelled {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeCancelled)
	}
	if receipt.TerminalOutcome != OutcomeCancelled {
		t.Errorf("receipt.TerminalOutcome = %s, want %s", receipt.TerminalOutcome, OutcomeCancelled)
	}
}

func TestRunDeadlineElapsedMapsToTimedOut(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := fixturePlanningRequest(t, session, interpretation)
	request.DeadlineAt = time.Now().Add(20 * time.Millisecond).UTC().Format(time.RFC3339)
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			<-ctx.Done()
			return Result{}, ctx.Err()
		},
	}

	result, _, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeTimedOut {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeTimedOut)
	}
	if receipt.TerminalOutcome != OutcomeTimedOut {
		t.Errorf("receipt.TerminalOutcome = %s, want %s", receipt.TerminalOutcome, OutcomeTimedOut)
	}
}

// TestRunRaceProducesExactlyOneTerminalOutcome stresses a provider that
// finishes at almost exactly the same moment its deadline elapses, many
// times, proving Run never returns zero or two outcomes -- exactly one,
// every time.
func TestRunRaceProducesExactlyOneTerminalOutcome(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)

	for i := 0; i < 200; i++ {
		request := fixturePlanningRequest(t, session, interpretation)
		request.DeadlineAt = time.Now().Add(2 * time.Millisecond).UTC().Format(time.RFC3339)
		digest, err := RequestDigest(request)
		if err != nil {
			t.Fatal(err)
		}
		request.RequestDigestSHA256 = digest
		capabilities := fixtureCapabilities(t)

		provider := &fakeProvider{
			capabilities: capabilities,
			executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
				select {
				case <-ctx.Done():
					return Result{}, ctx.Err()
				case <-time.After(time.Duration(i%3) * time.Millisecond):
					return fixturePlanningResult(t, req.RequestDigestSHA256, plan), nil
				}
			},
		}

		result, _, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		validOutcomes := map[TerminalOutcome]bool{OutcomeCompleted: true, OutcomeTimedOut: true, OutcomeCancelled: true}
		if !validOutcomes[result.TerminalOutcome] {
			t.Fatalf("iteration %d: unexpected TerminalOutcome %s", i, result.TerminalOutcome)
		}
		if receipt.TerminalOutcome != result.TerminalOutcome {
			t.Fatalf("iteration %d: receipt outcome %s != result outcome %s", i, receipt.TerminalOutcome, result.TerminalOutcome)
		}
		if receipt.ResultDigestSHA256 != result.ResultDigestSHA256 {
			t.Fatalf("iteration %d: receipt does not reference the result actually returned", i)
		}
	}
}

// --- observation limits cannot be enlarged by the provider ---

func TestRunObservationCountLimitCannotBeEnlarged(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	request.MaxObservationCount = 2
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	capabilities := fixtureCapabilities(t)

	rejectedCount := 0
	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			for i := 0; i < 5; i++ {
				if err := obs.Observe(fmt.Sprintf("observation %d", i)); err != nil {
					rejectedCount++
				}
			}
			return fixturePlanningResult(t, req.RequestDigestSHA256, plan), nil
		},
	}

	_, batch, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch.Observations) != 2 {
		t.Errorf("batch has %d observations, want exactly the precommitted limit of 2", len(batch.Observations))
	}
	if rejectedCount != 3 {
		t.Errorf("rejectedCount = %d, want 3 (5 attempts - 2 allowed)", rejectedCount)
	}
}

func TestRunObservationByteLimitCannotBeEnlarged(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	request.MaxObservationCount = 100
	request.MaxObservationBytes = 10
	digest, err := RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = digest
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			if err := obs.Observe("12345"); err != nil {
				t.Errorf("first 5-byte observation should fit under the 10-byte limit: %v", err)
			}
			if err := obs.Observe("12345"); err != nil {
				t.Errorf("second 5-byte observation should exactly fill the 10-byte limit: %v", err)
			}
			if err := obs.Observe("x"); err == nil {
				t.Error("expected the third observation to be rejected for exceeding the byte limit")
			}
			return fixturePlanningResult(t, req.RequestDigestSHA256, plan), nil
		},
	}

	_, batch, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch.Observations) != 2 {
		t.Errorf("batch has %d observations, want 2", len(batch.Observations))
	}
}

// --- Result digest-integrity: a provider-declared digest is never trusted
// unchecked ---

func TestRunRejectsResultWithMismatchedDeclaredDigest(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			result := fixturePlanningResult(t, req.RequestDigestSHA256, plan)
			result.ResultDigestSHA256 = zeroDigest // declared digest no longer matches content
			return result, nil
		},
	}

	result, _, receipt, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeInvalidOutput {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeInvalidOutput)
	}
	if receipt.TerminalOutcome != OutcomeInvalidOutput {
		t.Errorf("receipt.TerminalOutcome = %s, want %s", receipt.TerminalOutcome, OutcomeInvalidOutput)
	}
	// The synthesized failure Result must still be self-consistent.
	if got, err := ResultDigest(result); err != nil || got != result.ResultDigestSHA256 {
		t.Errorf("synthesized invalid-output result is not itself digest-consistent: declared %q, computed %q (err %v)", result.ResultDigestSHA256, got, err)
	}
}

func TestRunRejectsResultWithMismatchedOperation(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	plan := fixtureSynthesisPlan(t, interpretation.InterpretationDigestSHA256)
	attempt := fixtureSynthesisAttempt(t, plan.PlanDigestSHA256, plan.PlanGeneration)
	request := withFreshDeadline(t, fixturePlanningRequest(t, session, interpretation), 5*time.Second)
	capabilities := fixtureCapabilities(t)

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			// Returns a generation-shaped Result for a planning request.
			return fixtureGenerationResult(t, req.RequestDigestSHA256, attempt), nil
		},
	}

	result, _, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TerminalOutcome != OutcomeInvalidOutput {
		t.Errorf("TerminalOutcome = %s, want %s", result.TerminalOutcome, OutcomeInvalidOutput)
	}
}

// --- malformed inputs never reach a receipt at all ---

func TestRunRejectsMalformedRequestWithoutBuildingAReceipt(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := fixturePlanningRequest(t, session, interpretation)
	request.MaxObservationCount = -1 // schema-invalid: minimum 0

	provider := &fakeProvider{capabilities: fixtureCapabilities(t)}

	_, _, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err == nil {
		t.Fatal("expected a malformed request to be rejected before any receipt is built")
	}
	if provider.calls() != 0 {
		t.Errorf("Execute was called %d times, want 0", provider.calls())
	}
}

func TestRunRejectsCapabilitiesWithMismatchedDeclaredDigest(t *testing.T) {
	session := fixtureSynthesisSession(t)
	interpretation := fixtureSynthesisInterpretation(t, session.SessionDigestSHA256)
	request := fixturePlanningRequest(t, session, interpretation)

	capabilities := fixtureCapabilities(t)
	capabilities.CapabilitiesDigestSHA256 = zeroDigest // declared digest no longer matches content

	provider := &fakeProvider{
		capabilities: capabilities,
		executeFn: func(ctx context.Context, req Request, obs Observer) (Result, error) {
			t.Fatal("Execute must not be called when Capabilities fails its own digest-integrity check")
			return Result{}, nil
		},
	}

	_, _, _, err := Run(context.Background(), provider, request, fixedClock(time.Now()))
	if err == nil {
		t.Fatal("expected self-inconsistent capabilities to be rejected before any receipt is built")
	}
}
