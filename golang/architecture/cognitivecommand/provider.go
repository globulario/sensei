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

const sessionOnlyContextReason = "command provider received the closed O2 session payload and digest references only; no external closure or repository content was resolved"

// Provider is a bounded interpretation/planning provider backed by one
// StructuredAgent.
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

	switch request.Operation {
	case providerport.OperationInterpretation:
		return p.interpretationResult(request, payload)
	case providerport.OperationPlanning:
		return p.planResult(request, payload)
	default:
		return terminalResult(request, providerport.OutcomeUnsupportedCapability, fmt.Sprintf("unsupported cognitive operation %q", request.Operation))
	}
}

func (p *Provider) interpretationResult(request providerport.Request, payload []byte) (providerport.Result, error) {
	if request.InterpretationPayload == nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "interpretation request has no session payload")
	}
	proposal, err := decodeInterpretationProposal(payload)
	if err != nil {
		return invalidResult(request, err)
	}
	limitations := append([]synthesis.Limitation{}, proposal.Limitations...)
	limitations = append(limitations, synthesis.Limitation{
		Source:   "cognitivecommand",
		Scope:    "interpretation-context",
		Reason:   sessionOnlyContextReason,
		Blocking: false,
	})
	interpretation := synthesis.NormalizeInterpretation(synthesis.Interpretation{
		SchemaVersion:               synthesis.InterpretationSchemaVersion,
		InterpretationID:            "interpretation." + requestPrefix(request),
		SessionDigestSHA256:         request.SessionDigestSHA256,
		GeneratedBy:                 synthesis.GeneratedBy,
		Objective:                   request.InterpretationPayload.Objective,
		ApplicableIntent:            proposal.ApplicableIntent,
		BindingInvariants:           proposal.BindingInvariants,
		RelevantContracts:          proposal.RelevantContracts,
		AuthorityBoundaries:        proposal.AuthorityBoundaries,
		KnownFailureModes:          proposal.KnownFailureModes,
		ForbiddenFixes:             proposal.ForbiddenFixes,
		RequiredProofObligations:   proposal.RequiredProofObligations,
		Assumptions:                proposal.Assumptions,
		UnresolvedQuestions:        proposal.UnresolvedQuestions,
		SourceReferences:           []synthesis.SourceReference{},
		Limitations:                limitations,
	})
	digest, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		return providerport.Result{}, fmt.Errorf("cognitivecommand: interpretation digest: %w", err)
	}
	interpretation.InterpretationDigestSHA256 = digest
	data, err := json.Marshal(interpretation)
	if err != nil {
		return providerport.Result{}, err
	}
	if err := synthesis.ValidateInterpretationSchema(data); err != nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "mapped interpretation failed O1 schema: "+err.Error())
	}
	return completedResult(request, digest, &interpretation, nil)
}

func (p *Provider) planResult(request providerport.Request, payload []byte) (providerport.Result, error) {
	if request.PlanningPayload == nil || request.ExpectedPlanGeneration == nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "planning request has no interpretation or expected plan generation")
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
	return completedResult(request, digest, nil, &plan)
}

func completedResult(request providerport.Request, payloadDigest string, interpretation *synthesis.Interpretation, plan *synthesis.Plan) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:         providerport.ResultSchemaVersion,
		RequestDigestSHA256:   request.RequestDigestSHA256,
		Operation:             request.Operation,
		TerminalOutcome:       providerport.OutcomeCompleted,
		PayloadDigestSHA256:   &payloadDigest,
		InterpretationPayload: interpretation,
		PlanningPayload:       plan,
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

func decodeInterpretationProposal(data []byte) (InterpretationProposal, error) {
	var proposal InterpretationProposal
	if err := decodeOne(data, &proposal); err != nil {
		return InterpretationProposal{}, err
	}
	if proposal.SchemaVersion != InterpretationProposalSchemaVersion {
		return InterpretationProposal{}, invalidOutput("interpretation schema_version %q", proposal.SchemaVersion)
	}
	proposal.ApplicableIntent = normalizeStrings(proposal.ApplicableIntent)
	proposal.BindingInvariants = normalizeStrings(proposal.BindingInvariants)
	proposal.RelevantContracts = normalizeStrings(proposal.RelevantContracts)
	proposal.AuthorityBoundaries = normalizeStrings(proposal.AuthorityBoundaries)
	proposal.KnownFailureModes = normalizeStrings(proposal.KnownFailureModes)
	proposal.ForbiddenFixes = normalizeStrings(proposal.ForbiddenFixes)
	proposal.RequiredProofObligations = normalizeStrings(proposal.RequiredProofObligations)
	proposal.Assumptions = normalizeStrings(proposal.Assumptions)
	proposal.UnresolvedQuestions = normalizeStrings(proposal.UnresolvedQuestions)
	if proposal.Limitations == nil {
		proposal.Limitations = []synthesis.Limitation{}
	}
	return proposal, nil
}

func decodePlanProposal(data []byte) (PlanProposal, error) {
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

func encodePrompt(request providerport.Request) ([]byte, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var instruction string
	switch request.Operation {
	case providerport.OperationInterpretation:
		instruction = "Return exactly one interpretation proposal JSON object. It must contain schema_version=sensei.cognitivecommand.interpretationproposal.v1 and all fields: applicable_intent, binding_invariants, relevant_contracts, authority_boundaries, known_failure_modes, forbidden_fixes, required_proof_obligations, assumptions, unresolved_questions, limitations. Do not include objective, identity, source references, provider identity, digests, repository paths, commands, or authority claims. Use empty arrays where evidence is unavailable."
	case providerport.OperationPlanning:
		instruction = "Return exactly one plan proposal JSON object. It must contain schema_version=sensei.cognitivecommand.planproposal.v1 and all fields: steps, assumptions, risks, stop_conditions. Every step must contain step_id, description, intended_files, intended_symbols, expected_evidence. Do not include identity, plan generation, provider identity, digests, commands, repository paths, or authority claims."
	default:
		return nil, fmt.Errorf("cognitivecommand: cannot prompt for operation %q", request.Operation)
	}
	var prompt bytes.Buffer
	prompt.WriteString("You are a bounded software interpretation and planning provider. You have no tools and no authority to inspect a repository, run commands, mutate files, admit changes, declare completion, or interact with GitHub.\n")
	prompt.WriteString(instruction)
	prompt.WriteString("\nBase every statement only on the closed O2 request below. Preserve uncertainty explicitly.\n\nO2_REQUEST_JSON\n")
	prompt.Write(requestJSON)
	prompt.WriteByte('\n')
	return prompt.Bytes(), nil
}

func normalizeOperations(in []providerport.Operation) ([]providerport.Operation, error) {
	set := map[providerport.Operation]struct{}{}
	for _, operation := range in {
		if operation != providerport.OperationInterpretation && operation != providerport.OperationPlanning {
			return nil, fmt.Errorf("cognitivecommand: unsupported configured operation %q", operation)
		}
		set[operation] = struct{}{}
	}
	if len(set) == 0 {
		return nil, errors.New("cognitivecommand: at least one interpretation or planning operation is required")
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
