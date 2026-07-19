// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	cbp "github.com/globulario/sensei/golang/architecture/changebindingproducer"
)

// runProduceChangeBinding is Phase 9.4c Checkpoint 2: the authoritative GitHub-side
// producer CLI. It reads event identity from the GitHub Actions environment, the checkout
// state from Git (read-only), and an EXPLICIT task + completion-result identity from flags,
// then constructs, self-validates, and publishes a completion.change_task_binding/v1
// artifact plus a typed audit record. It does NOT consume the binding for enforcement (that
// is Checkpoint 3) and never touches the 9.4b decision. Exit 0 on a produced+published
// binding; non-zero (with a typed audit) on any producer failure.
func runProduceChangeBinding(args []string) int {
	fs := flag.NewFlagSet("produce-change-binding", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "checked-out repository root")
	taskDir := fs.String("task-dir", "", "explicit task directory")
	taskID := fs.String("task-id", "", "explicit canonical task id")
	sessionID := fs.String("session-id", "", "explicit task session id")
	completionDigest := fs.String("completion-digest", "", "the exact completion-result digest being evaluated")
	completionTask := fs.String("completion-task", "", "the task id the completion result was produced for")
	completionSession := fs.String("completion-session", "", "the session id the completion result was produced for")
	issuer := fs.String("issuer", "sensei.ci", "authoritative issuer identity")
	tool := fs.String("tool", "sensei", "producer tool identity")
	toolVersion := fs.String("tool-version", Version, "producer tool version")
	checkoutProv := fs.String("checkout", "actions_checkout_v4", "checkout provenance descriptor")
	output := fs.String("output", "", "path to write the published binding artifact (required)")
	auditPath := fs.String("audit", "", "path to write the typed audit record (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "produce-change-binding: --output is required")
		return 2
	}

	// Authoritative event identity (GitHub Actions env + event payload). Never inferred.
	ev, evf := cbp.ExtractGitHubEvent(os.Getenv, os.ReadFile)
	if evf != cbp.FailNone {
		return emitProduceFailure(*auditPath, cbp.ProduceResult{Failure: evf, Audit: cbp.AuditRecord{SchemaVersion: cbp.AuditSchemaVersion, Outcome: "failed", Failure: evf, Reason: string(evf)}})
	}
	// Read-only checkout state.
	co, cerr := cbp.ReadCheckout(*repoRoot)
	if cerr != nil {
		f := cbp.FailCheckoutHeadMismatch
		return emitProduceFailure(*auditPath, cbp.ProduceResult{Failure: f, Audit: cbp.AuditRecord{SchemaVersion: cbp.AuditSchemaVersion, Outcome: "failed", Failure: f, Reason: "checkout_unreadable"}})
	}

	in := cbp.ProducerInput{
		EventSource:                  ev.EventSource,
		RepositoryProvider:           ev.RepositoryProvider,
		RepositoryIdentity:           ev.RepositoryIdentity,
		ChangeProvider:               ev.ChangeProvider,
		ChangeID:                     ev.ChangeID,
		BaseSHA:                      ev.BaseSHA,
		HeadSHA:                      ev.HeadSHA,
		CheckoutRepositoryIdentity:   co.RepositoryIdentity,
		CheckoutHeadSHA:              co.HeadSHA,
		TaskDirectory:                *taskDir,
		TaskID:                       *taskID,
		TaskSessionID:                *sessionID,
		CompletionResultDigestSHA256: *completionDigest,
		CompletionResultTaskID:       *completionTask,
		CompletionResultSessionID:    *completionSession,
		Issuer:                       *issuer,
		Checkout:                     *checkoutProv,
		Tool:                         *tool,
		ToolVersion:                  *toolVersion,
	}

	r := cbp.Produce(in, cbp.DefaultGitHubAuthority())
	if !r.OK() {
		return emitProduceFailure(*auditPath, r)
	}
	if pf := cbp.Publish(*r.Binding, *output); pf != cbp.FailNone {
		r.Failure = pf
		r.Audit.Outcome = "failed"
		r.Audit.Failure = pf
		r.Audit.Reason = string(pf)
		return emitProduceFailure(*auditPath, r)
	}
	r.Audit.Published = true
	writeAudit(*auditPath, r.Audit)
	fmt.Printf("produced change_task_binding/v1: %s\n", r.Binding.DigestSHA256)
	return 0
}

func emitProduceFailure(auditPath string, r cbp.ProduceResult) int {
	writeAudit(auditPath, r.Audit)
	fmt.Fprintf(os.Stderr, "produce-change-binding: %s\n", r.Failure)
	return 1
}

func writeAudit(path string, a cbp.AuditRecord) {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, string(data))
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}
