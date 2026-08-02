// SPDX-License-Identifier: AGPL-3.0-only

package commandprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

const (
	helperEnabled = "SENSEI_COMMANDPROVIDER_HELPER"
	helperMode    = "SENSEI_COMMANDPROVIDER_MODE"
	helperMarker  = "SENSEI_COMMANDPROVIDER_MARKER"
)

type collectingObserver struct {
	details []string
}

func (o *collectingObserver) Observe(detail string) error {
	o.details = append(o.details, detail)
	return nil
}

func TestCommandProviderHelperProcess(t *testing.T) {
	if os.Getenv(helperEnabled) != "1" {
		return
	}

	var request providerport.Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		fmt.Fprintln(os.Stderr, "decode request:", err)
		os.Exit(2)
	}

	switch os.Getenv(helperMode) {
	case "valid":
		writeUnavailable(request, "ok")
	case "stderr":
		fmt.Fprintln(os.Stderr, "provider diagnostic")
		writeUnavailable(request, "done")
	case "environment":
		writeUnavailable(
			request,
			os.Getenv("COMMANDPROVIDER_ALLOWED")+"|"+os.Getenv("COMMANDPROVIDER_DENIED"),
		)
	case "arguments":
		writeUnavailable(request, strings.Join(os.Args, "|"))
	case "malformed":
		fmt.Print("{")
	case "multiple":
		writeUnavailable(request, "first")
		fmt.Print("{}")
	case "unknown-field":
		result := unavailableResult(request, "unknown")
		data, _ := json.Marshal(result)
		var document map[string]any
		_ = json.Unmarshal(data, &document)
		document["provider_command"] = "not-authority"
		_ = json.NewEncoder(os.Stdout).Encode(document)
	case "oversized":
		fmt.Print(strings.Repeat("x", 8192))
	case "digest-mismatch":
		result := unavailableResult(request, "bad digest")
		result.ResultDigestSHA256 = strings.Repeat("0", 64)
		_ = json.NewEncoder(os.Stdout).Encode(result)
	case "request-mismatch":
		result := unavailableResult(request, "wrong request")
		result.RequestDigestSHA256 = strings.Repeat("f", 64)
		digest, _ := providerport.ResultDigest(result)
		result.ResultDigestSHA256 = digest
		_ = json.NewEncoder(os.Stdout).Encode(result)
	case "operation-mismatch":
		result := unavailableResult(request, "wrong operation")
		result.Operation = providerport.OperationPlanning
		digest, _ := providerport.ResultDigest(result)
		result.ResultDigestSHA256 = digest
		_ = json.NewEncoder(os.Stdout).Encode(result)
	case "sleep-tree":
		child := exec.Command(os.Args[0], "-test.run=TestCommandProviderHelperProcess")
		child.Env = append(
			os.Environ(),
			helperEnabled+"=1",
			helperMode+"=delayed-marker",
		)
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start child:", err)
			os.Exit(3)
		}
		time.Sleep(10 * time.Second)
	case "delayed-marker":
		time.Sleep(500 * time.Millisecond)
		_ = os.WriteFile(os.Getenv(helperMarker), []byte("survived"), 0o600)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(4)
	}
	os.Exit(0)
}

func unavailableResult(request providerport.Request, detail string) providerport.Result {
	result := providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     providerport.OutcomeUnavailable,
		Detail:              detail,
	}
	digest, _ := providerport.ResultDigest(result)
	result.ResultDigestSHA256 = digest
	return result
}

func writeUnavailable(request providerport.Request, detail string) {
	_ = json.NewEncoder(os.Stdout).Encode(unavailableResult(request, detail))
}

func testAdapter(t *testing.T, mode string, extraAllowlist ...string) *Adapter {
	return testAdapterWithArgs(t, mode, nil, extraAllowlist...)
}

func testAdapterWithArgs(t *testing.T, mode string, extraArgs []string, extraAllowlist ...string) *Adapter {
	t.Helper()
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnabled, "1")
	t.Setenv(helperMode, mode)

	allowlist := []string{helperEnabled, helperMode, helperMarker}
	allowlist = append(allowlist, extraAllowlist...)
	if runtime.GOOS == "windows" {
		allowlist = append(allowlist, "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT")
	}

	adapter, err := New(Config{
		ProviderID:           "test.command",
		ObservedAt:           "2026-08-02T00:00:00Z",
		Command:              command,
		Args:                 append([]string{"-test.run=TestCommandProviderHelperProcess"}, extraArgs...),
		WorkDir:              t.TempDir(),
		EnvironmentAllowlist: allowlist,
		SupportedOperations:  []providerport.Operation{providerport.OperationInterpretation},
		MaxStdoutBytes:       4096,
		MaxStderrBytes:       4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func requestFixture(t *testing.T) providerport.Request {
	t.Helper()
	shaA := strings.Repeat("a", 64)
	shaB := strings.Repeat("b", 64)
	shaC := strings.Repeat("c", 64)
	shaD := strings.Repeat("d", 64)

	session := synthesis.NormalizeSession(synthesis.Session{
		SchemaVersion:                 synthesis.SessionSchemaVersion,
		SessionID:                     "session.commandprovider.test",
		GeneratedBy:                   synthesis.GeneratedBy,
		RepositoryDomain:              "github.com/globulario/sensei",
		BaseRevision:                  strings.Repeat("1", 40),
		WorkspaceIdentityDigestSHA256: shaA,
		GraphAuthorityDigestSHA256:    shaB,
		TaskSessionDigestSHA256:       shaC,
		ClosureDigestSHA256:           shaD,
		ProofObligationDigests:        []string{},
		Objective:                     "exercise the command provider adapter",
		RetryBudget:                   1,
		ReplanBudget:                  1,
		CreatedAt:                     "2026-08-02T00:00:00Z",
	})
	sessionDigest, err := synthesis.SessionDigest(session)
	if err != nil {
		t.Fatal(err)
	}
	session.SessionDigestSHA256 = sessionDigest

	request := providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:                providerport.RequestSchemaVersion,
		RequestID:                    "request.commandprovider.test",
		Operation:                    providerport.OperationInterpretation,
		SessionDigestSHA256:          sessionDigest,
		RepositoryDomain:             session.RepositoryDomain,
		BaseRevision:                 session.BaseRevision,
		ParentArtifactDigestSHA256:   sessionDigest,
		ExpectedPlanGeneration:       nil,
		ExpectedAttemptNumber:        nil,
		DeadlineAt:                   time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		MaxObservationCount:          8,
		MaxObservationBytes:          4096,
		InterpretationPayload:        &session,
		PlanningPayload:              nil,
		GenerationPayload:            nil,
		EvaluationObservationPayload: nil,
	})
	requestDigest, err := providerport.RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = requestDigest
	return request
}

func TestDescribeIsDeterministicAndDetached(t *testing.T) {
	adapter := testAdapter(t, "valid")
	first, err := adapter.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.SupportedOperations[0] = providerport.OperationPlanning
	second, err := adapter.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.SupportedOperations[0] != providerport.OperationInterpretation {
		t.Fatal("Describe leaked mutable capability state")
	}
	computed, err := providerport.CapabilitiesDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if computed != second.CapabilitiesDigestSHA256 {
		t.Fatal("Describe returned a capability snapshot with a stale digest")
	}
}

func TestExecuteRejectsClosedProtocolViolationsAsTypedInvalidOutput(t *testing.T) {
	tests := []struct {
		mode   string
		detail string
	}{
		{mode: "malformed", detail: "decode result"},
		{mode: "multiple", detail: "multiple JSON documents"},
		{mode: "unknown-field", detail: "unknown field"},
		{mode: "oversized", detail: "stdout exceeded"},
		{mode: "digest-mismatch", detail: "declares digest"},
		{mode: "request-mismatch", detail: "request_digest_sha256"},
		{mode: "operation-mismatch", detail: "operation does not match"},
	}

	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			adapter := testAdapter(t, test.mode)
			result, err := adapter.Execute(
				context.Background(),
				requestFixture(t),
				&collectingObserver{},
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.TerminalOutcome != providerport.OutcomeInvalidOutput {
				t.Fatalf("terminal outcome = %q", result.TerminalOutcome)
			}
			if !strings.Contains(result.Detail, test.detail) {
				t.Fatalf("detail = %q, want substring %q", result.Detail, test.detail)
			}
		})
	}
}

func TestStderrIsBoundedObservationEvidenceOnly(t *testing.T) {
	adapter := testAdapter(t, "stderr")
	observer := &collectingObserver{}
	result, err := adapter.Execute(context.Background(), requestFixture(t), observer)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeUnavailable {
		t.Fatalf("terminal outcome = %q", result.TerminalOutcome)
	}
	if len(observer.details) != 1 || observer.details[0] != "provider diagnostic" {
		t.Fatalf("observations = %#v", observer.details)
	}
}

func TestEnvironmentIsExplicitlyAllowlisted(t *testing.T) {
	t.Setenv("COMMANDPROVIDER_ALLOWED", "visible")
	t.Setenv("COMMANDPROVIDER_DENIED", "secret")
	adapter := testAdapter(t, "environment", "COMMANDPROVIDER_ALLOWED")
	result, err := adapter.Execute(
		context.Background(),
		requestFixture(t),
		&collectingObserver{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Detail != "visible|" {
		t.Fatalf("detail = %q", result.Detail)
	}
}

func TestArgumentsArePassedWithoutShellInterpolation(t *testing.T) {
	adapter := testAdapterWithArgs(
		t,
		"arguments",
		[]string{"$(printf injected)", ";touch /tmp/commandprovider-forbidden"},
	)
	result, err := adapter.Execute(
		context.Background(),
		requestFixture(t),
		&collectingObserver{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Detail, "$(printf injected)") ||
		!strings.Contains(result.Detail, ";touch /tmp/commandprovider-forbidden") {
		t.Fatalf("argv was not preserved literally: %q", result.Detail)
	}
}

func TestUnsupportedOperationIsTypedData(t *testing.T) {
	adapter := testAdapter(t, "valid")
	request := requestFixture(t)
	request.Operation = providerport.OperationPlanning
	request.InterpretationPayload = nil
	requestDigest, err := providerport.RequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	request.RequestDigestSHA256 = requestDigest

	result, err := adapter.Execute(context.Background(), request, &collectingObserver{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeUnsupportedCapability {
		t.Fatalf("terminal outcome = %q", result.TerminalOutcome)
	}
}

func TestProviderPortRunComposesWithoutGrantingResultAuthority(t *testing.T) {
	adapter := testAdapter(t, "valid")
	request := requestFixture(t)
	result, _, receipt, err := providerport.Run(
		context.Background(),
		adapter,
		request,
		func() time.Time { return time.Unix(100, 0).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeUnavailable {
		t.Fatalf("terminal outcome = %q", result.TerminalOutcome)
	}
	if receipt.ResultDigestSHA256 != result.ResultDigestSHA256 {
		t.Fatal("providerport receipt did not bind the exact command result")
	}
}

func TestCancellationTerminatesCompleteProcessTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	t.Setenv(helperMarker, marker)
	adapter := testAdapter(t, "sleep-tree")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := adapter.Execute(ctx, requestFixture(t), &collectingObserver{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}

	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descendant process escaped cancellation: %v", err)
	}
}

func TestNewRejectsImplicitOrUnboundedConfiguration(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("empty configuration must fail")
	}

	_, err = New(Config{
		ProviderID:          "relative.command",
		ObservedAt:          "2026-08-02T00:00:00Z",
		Command:             "codex",
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
		MaxStdoutBytes:      1,
		MaxStderrBytes:      1,
	})
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("relative command error = %v", err)
	}

	command, absErr := filepath.Abs(os.Args[0])
	if absErr != nil {
		t.Fatal(absErr)
	}
	_, err = New(Config{
		ProviderID:          "invalid.time",
		ObservedAt:          "not-a-time",
		Command:             command,
		SupportedOperations: []providerport.Operation{providerport.OperationInterpretation},
		MaxStdoutBytes:      1,
		MaxStderrBytes:      1,
	})
	if err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("invalid observed-at error = %v", err)
	}
}
