// SPDX-License-Identifier: AGPL-3.0-only

package commandprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

var _ providerport.Provider = (*Adapter)(nil)

// Describe returns a detached copy of the deterministic capability snapshot.
func (a *Adapter) Describe(ctx context.Context) (providerport.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return providerport.Capabilities{}, err
	}
	capabilities := a.capabilities
	capabilities.SupportedOperations = append(
		[]providerport.Operation(nil),
		capabilities.SupportedOperations...,
	)
	return capabilities, nil
}

// Execute writes exactly one closed O2 request to stdin and accepts exactly one
// closed O2 result from stdout. Protocol violations are converted into typed
// invalid-output Result data. A Go error is reserved for infrastructure failure
// where the command could not produce a trustworthy result envelope at all.
func (a *Adapter) Execute(
	ctx context.Context,
	request providerport.Request,
	observer providerport.Observer,
) (providerport.Result, error) {
	if !supports(a.config.SupportedOperations, request.Operation) {
		return terminalResult(
			request,
			providerport.OutcomeUnsupportedCapability,
			fmt.Sprintf("configured command does not support operation %q", request.Operation),
		)
	}

	stdin, err := encodeRequest(request)
	if err != nil {
		return providerport.Result{}, err
	}

	command := exec.Command(a.config.Command, a.config.Args...)
	command.Dir = a.config.WorkDir
	command.Env = allowedEnvironment(a.config.EnvironmentAllowlist)
	configureProcessGroup(command)
	command.Stdin = bytes.NewReader(stdin)

	stdout := newLimitedBuffer(a.config.MaxStdoutBytes)
	stderr := newObservationWriter(a.config.MaxStderrBytes, observer)
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		return providerport.Result{}, fmt.Errorf(
			"commandprovider: start %q: %w",
			a.config.Command,
			err,
		)
	}
	waitErr := waitProcess(ctx, command)
	stderr.finish()

	if err := ctx.Err(); err != nil {
		return providerport.Result{}, err
	}
	if waitErr != nil {
		return providerport.Result{}, fmt.Errorf("commandprovider: command failed: %w", waitErr)
	}
	if stdout.exceeded() {
		return terminalResult(
			request,
			providerport.OutcomeInvalidOutput,
			fmt.Sprintf("stdout exceeded configured limit of %d bytes", a.config.MaxStdoutBytes),
		)
	}
	if stderr.exceeded() && observer != nil {
		_ = observer.Observe(fmt.Sprintf(
			"stderr truncated at configured limit of %d bytes",
			a.config.MaxStderrBytes,
		))
	}

	result, detail := decodeSingleResult(stdout.bytes(), request)
	if detail != "" {
		return terminalResult(request, providerport.OutcomeInvalidOutput, detail)
	}
	return result, nil
}

func encodeRequest(request providerport.Request) ([]byte, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("commandprovider: encode request: %w", err)
	}
	if err := providerport.ValidateRequestSchema(data); err != nil {
		return nil, fmt.Errorf("commandprovider: request failed schema validation: %w", err)
	}
	digest, err := providerport.RequestDigest(request)
	if err != nil {
		return nil, fmt.Errorf("commandprovider: compute request digest: %w", err)
	}
	if digest != request.RequestDigestSHA256 {
		return nil, fmt.Errorf(
			"commandprovider: request declares digest %q but computed %q",
			request.RequestDigestSHA256,
			digest,
		)
	}
	return append(data, '\n'), nil
}

func decodeSingleResult(data []byte, request providerport.Request) (providerport.Result, string) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var result providerport.Result
	if err := decoder.Decode(&result); err != nil {
		return providerport.Result{}, "decode result: " + err.Error()
	}

	var extra any
	switch err := decoder.Decode(&extra); {
	case err == io.EOF:
		// Exactly one document.
	case err == nil:
		return providerport.Result{}, "stdout contained multiple JSON documents"
	default:
		return providerport.Result{}, "stdout contained trailing non-whitespace: " + err.Error()
	}

	if err := providerport.ValidateResultSchema(data); err != nil {
		return providerport.Result{}, "result failed schema validation: " + err.Error()
	}
	if result.RequestDigestSHA256 != request.RequestDigestSHA256 {
		return providerport.Result{}, "result request_digest_sha256 does not match the request"
	}
	if result.Operation != request.Operation {
		return providerport.Result{}, "result operation does not match the request"
	}

	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, "compute result digest: " + err.Error()
	}
	if digest != result.ResultDigestSHA256 {
		return providerport.Result{}, fmt.Sprintf(
			"result declares digest %q but computed %q",
			result.ResultDigestSHA256,
			digest,
		)
	}
	if detail := validateCompletedPayload(result); detail != "" {
		return providerport.Result{}, detail
	}
	return result, ""
}

func validateCompletedPayload(result providerport.Result) string {
	if result.TerminalOutcome != providerport.OutcomeCompleted {
		return ""
	}
	if result.PayloadDigestSHA256 == nil {
		return "completed result has no payload_digest_sha256"
	}

	var declared string
	var computed string
	var err error

	switch result.Operation {
	case providerport.OperationInterpretation:
		if result.InterpretationPayload == nil {
			return "completed interpretation result has no interpretation payload"
		}
		declared = result.InterpretationPayload.InterpretationDigestSHA256
		computed, err = synthesis.InterpretationDigest(*result.InterpretationPayload)
	case providerport.OperationPlanning:
		if result.PlanningPayload == nil {
			return "completed planning result has no planning payload"
		}
		declared = result.PlanningPayload.PlanDigestSHA256
		computed, err = synthesis.PlanDigest(*result.PlanningPayload)
	case providerport.OperationGeneration:
		if result.GenerationPayload == nil {
			return "completed generation result has no generation payload"
		}
		declared = result.GenerationPayload.AttemptDigestSHA256
		computed, err = synthesis.AttemptDigest(*result.GenerationPayload)
	case providerport.OperationEvaluationObservation:
		if result.EvaluationObservationPayload == nil {
			return "completed evaluation-observation result has no evaluation payload"
		}
		declared = result.EvaluationObservationPayload.EvaluationDigestSHA256
		computed, err = synthesis.EvaluationDigest(*result.EvaluationObservationPayload)
	default:
		return fmt.Sprintf("completed result has unknown operation %q", result.Operation)
	}
	if err != nil {
		return "compute completed payload digest: " + err.Error()
	}
	if declared != computed {
		return fmt.Sprintf(
			"embedded payload declares digest %q but computed %q",
			declared,
			computed,
		)
	}
	if *result.PayloadDigestSHA256 != computed {
		return "result payload_digest_sha256 does not match the embedded payload"
	}
	return ""
}

func terminalResult(
	request providerport.Request,
	outcome providerport.TerminalOutcome,
	detail string,
) (providerport.Result, error) {
	result := providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     outcome,
		Detail:              detail,
	}
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, fmt.Errorf(
			"commandprovider: compute terminal result digest: %w",
			err,
		)
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}
