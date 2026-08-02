// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

var _ providerport.Provider = (*Provider)(nil)

// Provider is a bounded planning provider backed by one StructuredAgent. It
// consumes an already-grounded O1 Interpretation and cannot create one.
type Provider struct {
	config       Config
	capabilities providerport.Capabilities
}

func New(config Config) (*Provider, error) {
	config.ProviderID = strings.TrimSpace(config.ProviderID)
	config.ProviderKind = strings.TrimSpace(config.ProviderKind)
	config.ModelIdentifier = strings.TrimSpace(config.ModelIdentifier)
	config.ObservedAt = strings.TrimSpace(config.ObservedAt)
	if config.Agent == nil {
		return nil, errors.New("cognitivecommand: structured agent is required")
	}
	if config.ProviderID == "" {
		return nil, errors.New("cognitivecommand: provider id is required")
	}
	if config.ProviderKind == "" {
		config.ProviderKind = "cognitive-command"
	}
	if _, err := time.Parse(time.RFC3339, config.ObservedAt); err != nil {
		return nil, fmt.Errorf("cognitivecommand: observed_at must be RFC3339: %w", err)
	}
	operations, err := normalizeOperations(config.SupportedOperations)
	if err != nil {
		return nil, err
	}
	config.SupportedOperations = operations
	capabilities := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:      config.ProviderID,
			ProviderKind:    config.ProviderKind,
			ModelIdentifier: config.ModelIdentifier,
			ObservedAt:      config.ObservedAt,
		},
		SupportedOperations: append([]providerport.Operation{}, operations...),
	}
	digest, err := providerport.CapabilitiesDigest(capabilities)
	if err != nil {
		return nil, fmt.Errorf("cognitivecommand: compute capabilities digest: %w", err)
	}
	capabilities.CapabilitiesDigestSHA256 = digest
	return &Provider{config: config, capabilities: capabilities}, nil
}

func (p *Provider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return providerport.Capabilities{}, err
	}
	out := p.capabilities
	out.SupportedOperations = append([]providerport.Operation{}, out.SupportedOperations...)
	return out, nil
}

func (p *Provider) Execute(ctx context.Context, request providerport.Request, observer providerport.Observer) (providerport.Result, error) {
	if !supports(p.config.SupportedOperations, request.Operation) {
		return terminalResult(request, providerport.OutcomeUnsupportedCapability, fmt.Sprintf("cognitive command does not support operation %q", request.Operation))
	}
	if request.Operation != providerport.OperationPlanning {
		return terminalResult(request, providerport.OutcomeUnsupportedCapability, "cognitive command is planning-only; interpretation requires governed, digest-bound source evidence")
	}
	prompt, err := encodePrompt(request)
	if err != nil {
		return providerport.Result{}, err
	}
	payload, err := p.config.Agent.Complete(ctx, prompt, observer)
	if err != nil {
		var invalid *agentcommand.InvalidOutputError
		if errors.As(err, &invalid) {
			return terminalResult(request, providerport.OutcomeInvalidOutput, invalid.Detail)
		}
		return providerport.Result{}, fmt.Errorf("cognitivecommand: execute structured agent: %w", err)
	}
	return p.planResult(request, payload)
}

func (p *Provider) planResult(request providerport.Request, payload []byte) (providerport.Result, error) {
	if request.PlanningPayload == nil || request.ExpectedPlanGeneration == nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "planning request has no grounded interpretation or expected plan generation")
	}
	proposal, err := decodePlanProposal(payload)
	if err != nil {
		return invalidResult(request, err)
	}
	plan := synthesis.NormalizePlan(synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan." + requestPrefix(request),
		InterpretationDigestSHA256: request.PlanningPayload.InterpretationDigestSHA256,
		PlanGeneration:             *request.ExpectedPlanGeneration,
		Steps:                      proposal.Steps,
		Assumptions:                proposal.Assumptions,
		Risks:                      proposal.Risks,
		StopConditions:             proposal.StopConditions,
		ProviderObservation:        p.capabilities.ProviderObservation,
	})
	digest, err := synthesis.PlanDigest(plan)
	if err != nil {
		return providerport.Result{}, fmt.Errorf("cognitivecommand: plan digest: %w", err)
	}
	plan.PlanDigestSHA256 = digest
	data, err := json.Marshal(plan)
	if err != nil {
		return providerport.Result{}, err
	}
	if err := synthesis.ValidatePlanSchema(data); err != nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "mapped plan failed O1 schema: "+err.Error())
	}
	return completedResult(request, digest, &plan)
}

func completedResult(request providerport.Request, payloadDigest string, plan *synthesis.Plan) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     providerport.OutcomeCompleted,
		PayloadDigestSHA256: &payloadDigest,
		PlanningPayload:     plan,
	})
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}

func invalidResult(request providerport.Request, err error) (providerport.Result, error) {
	var invalid *agentcommand.InvalidOutputError
	if errors.As(err, &invalid) {
		return terminalResult(request, providerport.OutcomeInvalidOutput, invalid.Detail)
	}
	return providerport.Result{}, err
}

func terminalResult(request providerport.Request, outcome providerport.TerminalOutcome, detail string) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     outcome,
		Detail:              strings.TrimSpace(detail),
	})
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}

func decodePlanProposal(data []byte) (PlanProposal, error) {
	if err := rejectDuplicateObjectKeys(data); err != nil {
		return PlanProposal{}, err
	}
	if err := ValidatePlanProposalSchema(data); err != nil {
		return PlanProposal{}, invalidOutput("plan proposal schema: %v", err)
	}
	var proposal PlanProposal
	if err := decodeOne(data, &proposal); err != nil {
		return PlanProposal{}, err
	}
	if proposal.SchemaVersion != PlanProposalSchemaVersion {
		return PlanProposal{}, invalidOutput("plan schema_version %q", proposal.SchemaVersion)
	}
	if proposal.Steps == nil {
		proposal.Steps = []synthesis.PlanStep{}
	}
	for i := range proposal.Steps {
		proposal.Steps[i].IntendedFiles = normalizeStrings(proposal.Steps[i].IntendedFiles)
		proposal.Steps[i].IntendedSymbols = normalizeStrings(proposal.Steps[i].IntendedSymbols)
		proposal.Steps[i].ExpectedEvidence = normalizeStrings(proposal.Steps[i].ExpectedEvidence)
	}
	proposal.Assumptions = normalizeStrings(proposal.Assumptions)
	proposal.Risks = normalizeStrings(proposal.Risks)
	proposal.StopConditions = normalizeStrings(proposal.StopConditions)
	return proposal, nil
}

func decodeOne(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalidOutput("decode proposal: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return invalidOutput("proposal contained multiple JSON documents")
		}
		return invalidOutput("proposal contained trailing data: %v", err)
	}
	return nil
}

func rejectDuplicateObjectKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$" ); err != nil {
		return invalidOutput("ambiguous proposal JSON: %v", err)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key at %s is not a string", path)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate object key %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected delimiter %q at %s", delimiter, path)
	}
}

func encodePrompt(request providerport.Request) ([]byte, error) {
	if request.Operation != providerport.OperationPlanning {
		return nil, fmt.Errorf("cognitivecommand: cannot prompt for operation %q", request.Operation)
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	schemaJSON, err := PlanProposalSchemaBytes()
	if err != nil {
		return nil, fmt.Errorf("cognitivecommand: load plan proposal schema: %w", err)
	}
	var prompt bytes.Buffer
	prompt.WriteString("You are a bounded software planning provider. You have no tools and no authority to inspect a repository, run commands, mutate files, admit changes, declare completion, or interact with GitHub.\n")
	prompt.WriteString("Return exactly one JSON object conforming to the supplied closed schema. Use only the grounded O1 Interpretation embedded in the O2 request. Preserve uncertainty explicitly. intended_files may contain repository-relative logical file references already justified by that Interpretation; never emit an absolute path, repository root, worktree path, command, identity, digest, provider identity, plan generation, or authority claim.\n\nPROPOSAL_JSON_SCHEMA\n")
	prompt.Write(schemaJSON)
	prompt.WriteString("\n\nO2_REQUEST_JSON\n")
	prompt.Write(requestJSON)
	prompt.WriteByte('\n')
	return prompt.Bytes(), nil
}

func normalizeOperations(in []providerport.Operation) ([]providerport.Operation, error) {
	set := map[providerport.Operation]struct{}{}
	for _, operation := range in {
		if operation != providerport.OperationPlanning {
			return nil, fmt.Errorf("cognitivecommand: unsupported configured operation %q; O8 is planning-only until interpretation has a governed source resolver", operation)
		}
		set[operation] = struct{}{}
	}
	if len(set) == 0 {
		return nil, errors.New("cognitivecommand: planning operation is required")
	}
	out := make([]providerport.Operation, 0, len(set))
	for operation := range set {
		out = append(out, operation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func supports(operations []providerport.Operation, wanted providerport.Operation) bool {
	for _, operation := range operations {
		if operation == wanted {
			return true
		}
	}
	return false
}

func normalizeStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func invalidOutput(format string, args ...any) error {
	return &agentcommand.InvalidOutputError{Detail: fmt.Sprintf(format, args...)}
}

func requestPrefix(request providerport.Request) string {
	if len(request.RequestDigestSHA256) >= 16 {
		return request.RequestDigestSHA256[:16]
	}
	return "invalid-request"
}
