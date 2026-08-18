// SPDX-License-Identifier: AGPL-3.0-only

// Command smokeplan emits one valid agent mutation plan for the
// synthesis-run CLI smoke.
//
// It exists so the smoke does not reimplement the plan digest. That digest is
// closureprotocol.SemanticDigest over the normalized plan, not a hash of the
// bytes, and a shell or Python approximation of it would drift from the real
// definition silently -- producing a fixture the code rejects for a reason
// unrelated to whatever the smoke meant to test, or worse, one it accepts
// while the real rule has moved on. Calling the exported function is the only
// way the fixture stays correct by construction.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/agentcommand"
	"github.com/globulario/sensei/golang/architecture/cognitivecommand"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// The plan proposal is cognitivecommand.PlanProposal itself, for the same
// reason the mutation plan below is agentcommand's own type: a hand-written
// mirror of a schema drifts from it silently, and the failure surfaces inside
// the run as "invalid-output" -- indistinguishable from a provider fault.
func emitPlanProposal(path, summary, profile string) {
	proposal := cognitivecommand.PlanProposal{
		SchemaVersion: cognitivecommand.PlanProposalSchemaVersion,
		Steps: []synthesis.PlanStep{{
			StepID:           "step-1",
			Description:      summary,
			IntendedFiles:    []string{path},
			IntendedSymbols:  []string{},
			ExpectedEvidence: []string{"the file exists with the intended content"},
		}},
		Assumptions:    []string{"the smoke's synthetic scope is the only surface touched"},
		Risks:          []string{"none: this plan writes one file in a candidate workspace"},
		StopConditions: []string{"the single intended file has been written"},
	}
	body, err := json.Marshal(proposal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smokeplan: marshal proposal: %v\n", err)
		os.Exit(1)
	}
	writeFramed(body, profile, "")
}

// writeFramed applies the vendor envelope: codex takes the JSON directly,
// claude wraps it in {"result": "..."}. The corruption modes live here too,
// because each one is a defect in the FRAMING rather than in the artifact --
// a second document after the first, or two "result" keys in one object.
func writeFramed(body []byte, profile, corrupt string) {
	switch profile {
	case "codex":
		os.Stdout.Write(body)
	case "claude":
		env, err := json.Marshal(struct {
			Result string `json:"result"`
		}{Result: string(body)})
		if err != nil {
			fmt.Fprintf(os.Stderr, "smokeplan: envelope: %v\n", err)
			os.Exit(1)
		}
		os.Stdout.Write(env)
		if corrupt == "duplicate-key" {
			// Two "result" keys in one object. JSON decoders silently take the
			// last, so a vendor that emitted both would have one of its answers
			// discarded without anyone noticing.
			os.Stdout.Write([]byte("\x00"))
		}
	default:
		fmt.Fprintf(os.Stderr, "smokeplan: unknown profile %q\n", profile)
		os.Exit(2)
	}
	if corrupt == "trailing" {
		// A second JSON document after the first. Accepting it would mean the
		// command silently chose one of two answers.
		os.Stdout.Write([]byte("\n{\"result\":\"second document\"}\n"))
	}
	os.Stdout.Write([]byte("\n"))
}

func main() {
	path := flag.String("path", "", "candidate-relative file the plan writes (required)")
	content := flag.String("content", "", "file content the plan writes")
	contentFile := flag.String("content-file", "", "read the file content from this path instead of --content")
	summary := flag.String("summary", "smoke: single write operation", "plan summary")
	profile := flag.String("profile", "codex", "vendor profile to frame the output for: codex | claude")
	corrupt := flag.String("corrupt", "", "deliberately break the fixture: digest | trailing | duplicate-key")
	kind := flag.String("kind", "mutation-plan", "artifact to emit: mutation-plan (O3 generation) | plan-proposal (O8 planning)")
	flag.Parse()

	// Modifying an existing file means restating its whole content, because a
	// mutation operation carries content and not a patch. Passing a repository
	// file through --content would mean quoting it into a shell argument.
	if *contentFile != "" {
		data, err := os.ReadFile(*contentFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "smokeplan: read --content-file: %v\n", err)
			os.Exit(1)
		}
		*content = string(data)
	}

	if *path == "" {
		fmt.Fprintln(os.Stderr, "smokeplan: --path is required")
		os.Exit(2)
	}

	// The pipeline calls the agent TWICE with different contracts: O8 planning
	// wants a plan proposal, O3 generation wants a mutation plan. A fixture that
	// always emits one of them stops the run at the other with "invalid-output",
	// which looks like a provider fault rather than a fixture that answered the
	// wrong question.
	if *kind == "plan-proposal" {
		emitPlanProposal(*path, *summary, *profile)
		return
	}
	if *kind != "mutation-plan" {
		fmt.Fprintf(os.Stderr, "smokeplan: unknown --kind %q\n", *kind)
		os.Exit(2)
	}

	plan := agentcommand.MutationPlan{
		SchemaVersion: agentcommand.MutationPlanSchemaVersion,
		Summary:       *summary,
		Operations: []agentcommand.MutationOperation{{
			OperationID: "op-1",
			Kind:        "write",
			Path:        *path,
			Content:     []byte(*content),
			// For kind "write" the schema pins mode to "": mode is set-mode's field.
			Mode: "",
		}},
	}
	// An agent emits a PROPOSAL, not a canonical plan: schema_version, summary
	// and operations, with NO digest. Sensei computes the digest itself
	// (MutationPlanDigest) precisely so a provider cannot declare its own
	// plan's identity, and the O3 decoder uses DisallowUnknownFields to enforce
	// that -- an emitted mutation_plan_digest_sha256 is rejected as an unknown
	// field. The v1 schema requires the digest because it describes the
	// CANONICAL plan Sensei produces, not the proposal an agent submits; the
	// two are different artifacts and validating a proposal against the
	// canonical schema is a category error this fixture made first time round.
	type proposal struct {
		SchemaVersion string                           `json:"schema_version"`
		Summary       string                           `json:"summary"`
		Operations    []agentcommand.MutationOperation `json:"operations"`
		// Populated ONLY by --corrupt digest, to prove Sensei refuses a provider
		// that tries to declare its own plan digest.
		Digest string `json:"mutation_plan_digest_sha256,omitempty"`
	}
	out := proposal{SchemaVersion: plan.SchemaVersion, Summary: plan.Summary, Operations: plan.Operations}
	if *corrupt == "digest" {
		out.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	body, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smokeplan: marshal: %v\n", err)
		os.Exit(1)
	}

	writeFramed(body, *profile, *corrupt)
}
