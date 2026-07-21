// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Note on output_config: we intentionally do NOT pass a json_schema. The
// Messages API's constrained decoding requires strict objects (every field
// enumerated + additionalProperties:false); a fully-typed candidate object (13
// fields) blows the grammar-compilation budget ("Grammar compilation timed
// out"), and a loose object is rejected ("additionalProperties must be false").
// So the candidate shape is specified in the PROMPT (mirroring cmd/awg's
// candidateSchemaSection) and parsed here; propose.Validate is the real gate —
// the LLM is a pure proposer, AWG remains the sole validator. coldsource must
// NOT import golang/propose (cmd depends on both — that would cycle), so the
// caller unmarshals the returned array into []propose.Request.

// DraftOnboardingCandidates asks the LLM to propose a starter set of awareness
// candidates from the onboarding brief and returns the raw JSON array bytes (the
// candidate objects). The caller unmarshals into []propose.Request and validates
// each with the same contract-first checks the Propose RPC / `awg onboard import`
// use — there is no relaxed path for the model. It fails clearly on client or
// parse errors and never fabricates candidates.
func DraftOnboardingCandidates(ctx context.Context, client LLMClient, brief string, max int) ([]byte, error) {
	if client == nil {
		return nil, fmt.Errorf("onboard drafter: no LLM client")
	}
	if max <= 0 {
		max = 15
	}
	system := fmt.Sprintf(`You propose a STARTER set of awareness rules for a repository from the brief below.
Ground every rule in the brief's architecture and high-risk paths — never invent files, symbols, or facts.
Favor invariants (rules that must hold) and the failure_modes they guard, prioritizing the high-risk paths.
Every entry MUST be contract-first: an invariant needs >=1 source_files plus a related_failure / forbidden_fix / required_test; a failure_mode needs a related_invariant (or contract) and evidence (or required_test).

Return ONLY a JSON object of the form {"candidates": [ ... ]} with at most %d entries and NO prose.
Each candidate is an object; include only the fields that apply:
  "kind" (one of: invariant, failure_mode, forbidden_fix, required_test, contract_unknown),
  "title", "description", "severity" (critical|high|warning),
  "source_files": [], "related_invariants": [], "related_failures": [],
  "required_tests": [], "forbidden_fixes": [], "evidence": [], "contract", "proposed_contract".`, max)

	reply, err := client.Complete(ctx, LLMRequest{
		System:    system,
		User:      brief,
		MaxTokens: 8192,
	})
	if err != nil {
		return nil, err
	}

	clean := strings.TrimSpace(stripCodeFence(reply))
	// Some backends (notably the claude-cli path, which cannot enforce
	// output_config) may return a bare array rather than the wrapper object.
	// Accept both so --drafter llm and --drafter claude-cli behave identically.
	var arr []byte
	if strings.HasPrefix(clean, "[") {
		arr = []byte(clean)
	} else {
		var wrapper struct {
			Candidates json.RawMessage `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(clean), &wrapper); err != nil {
			return nil, fmt.Errorf("onboard drafter: parse model output: %w", err)
		}
		arr = []byte(strings.TrimSpace(string(wrapper.Candidates)))
	}
	switch string(arr) {
	case "", "[]", "null":
		return nil, fmt.Errorf("onboard drafter: model returned no candidates")
	}
	return arr, nil
}
