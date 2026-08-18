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
)

func main() {
	path := flag.String("path", "", "candidate-relative file the plan writes (required)")
	content := flag.String("content", "", "file content the plan writes")
	summary := flag.String("summary", "smoke: single write operation", "plan summary")
	profile := flag.String("profile", "codex", "vendor profile to frame the output for: codex | claude")
	corrupt := flag.String("corrupt", "", "deliberately break the fixture: digest | trailing | duplicate-key")
	flag.Parse()

	if *path == "" {
		fmt.Fprintln(os.Stderr, "smokeplan: --path is required")
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
	digest, err := agentcommand.MutationPlanDigest(plan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smokeplan: digest: %v\n", err)
		os.Exit(1)
	}
	plan.MutationPlanDigestSHA256 = digest
	if *corrupt == "digest" {
		// A plan whose declared digest does not match its content. The agent
		// layer must reject this rather than trust the declaration.
		plan.MutationPlanDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	}

	body, err := json.Marshal(plan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smokeplan: marshal: %v\n", err)
		os.Exit(1)
	}

	switch *profile {
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
		if *corrupt == "duplicate-key" {
			// Two "result" keys in one object. JSON decoders silently take the
			// last, so a vendor that emitted both would have one of its answers
			// discarded without anyone noticing.
			os.Stdout.Write([]byte("\x00"))
		}
	default:
		fmt.Fprintf(os.Stderr, "smokeplan: unknown profile %q\n", *profile)
		os.Exit(2)
	}
	if *corrupt == "trailing" {
		// A second JSON document after the first. Accepting it would mean the
		// command silently chose one of two answers.
		os.Stdout.Write([]byte("\n{\"result\":\"second document\"}\n"))
	}
	os.Stdout.Write([]byte("\n"))
}
