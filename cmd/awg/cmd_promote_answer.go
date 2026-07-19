// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qp "github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/propose"
)

// promoteOutput is the stable machine render of the promotion owner's typed
// result. The CLI is a control panel: it renders only owner-returned facts and
// computes no authority, digest, or status of its own. correctness_certified is
// ALWAYS false; promotion never certifies.
type promoteOutput struct {
	Outcome                       string `json:"outcome" yaml:"outcome"`
	PromotionLineageID            string `json:"promotion_lineage_id,omitempty" yaml:"promotion_lineage_id,omitempty"`
	ReceiptDigestSHA256           string `json:"receipt_digest_sha256,omitempty" yaml:"receipt_digest_sha256,omitempty"`
	CommittedCausalIdentitySHA256 string `json:"committed_causal_identity_sha256,omitempty" yaml:"committed_causal_identity_sha256,omitempty"`
	// PromotionDirectory / ReceiptPath are non-authoritative location hints only.
	PromotionDirectory   string `json:"promotion_directory,omitempty" yaml:"promotion_directory,omitempty"`
	ReceiptPath          string `json:"receipt_path,omitempty" yaml:"receipt_path,omitempty"`
	Detail               string `json:"detail,omitempty" yaml:"detail,omitempty"`
	CorrectnessCertified bool   `json:"correctness_certified" yaml:"correctness_certified"`
}

// runPromoteAnswer is the thin adapter over questionpromotion.Promote. It parses
// inputs, constructs a PromoteRequest, calls the owner, and renders the typed
// result. It holds NO promotion policy, eligibility, authority, mutation, journal,
// graph, receipt, or recovery logic, and performs no direct governed writes.
func runPromoteAnswer(args []string) int {
	var repoRoot, taskDir, identityRoot, domain, dispositionReceipt, proposalJSON, format string
	var kind, id, title, description, severity, contract, scopeDomain string
	var sourceFiles, relatedInvariants, relatedFailures, requiredTests, scopeFiles multiString

	fs := flag.NewFlagSet("sensei promote-answer", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&identityRoot, "identity-root", "", "promotion actor identity store (default: <repo>/.sensei/identity)")
	fs.StringVar(&domain, "domain", "", "repository domain")
	fs.StringVar(&dispositionReceipt, "disposition-receipt", "", "exact QuestionDispositionReceipt digest to promote")
	fs.StringVar(&proposalJSON, "proposal-json", "", "read the governed record proposal from this JSON file (a propose.Request)")
	fs.StringVar(&kind, "kind", "", "governed record kind: invariant | failure_mode | forbidden_fix | required_test | decision")
	fs.StringVar(&id, "id", "", "explicit canonical record id (optional)")
	fs.StringVar(&title, "title", "", "record title")
	fs.StringVar(&description, "description", "", "record description")
	fs.StringVar(&severity, "severity", "", "record severity (where applicable)")
	fs.StringVar(&contract, "contract", "", "the contract the record captures")
	fs.Var(&sourceFiles, "source-file", "source file the record anchors to (repeatable)")
	fs.Var(&relatedInvariants, "related-invariant", "related invariant id (repeatable)")
	fs.Var(&relatedFailures, "related-failure", "related failure_mode id (repeatable)")
	fs.Var(&requiredTests, "required-test", "required test ref (repeatable)")
	fs.StringVar(&scopeDomain, "scope-domain", "", "bounded effective scope domain (may not broaden the disposition)")
	fs.Var(&scopeFiles, "scope-file", "bounded effective scope file (repeatable)")
	fs.StringVar(&format, "format", "text", "output format: text | json | yaml")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "promote-answer:", err)
		return 2
	}
	if strings.TrimSpace(identityRoot) == "" {
		identityRoot = filepath.Join(repoRoot, ".sensei", "identity")
	}

	// Build the proposed governed record (JSON base, flags override set fields).
	var proposal propose.Request
	if strings.TrimSpace(proposalJSON) != "" {
		raw, rerr := os.ReadFile(proposalJSON)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "promote-answer:", rerr)
			return 2
		}
		if jerr := json.Unmarshal(raw, &proposal); jerr != nil {
			fmt.Fprintln(os.Stderr, "promote-answer: parse proposal json:", jerr)
			return 2
		}
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "kind":
			proposal.Kind = kind
		case "id":
			proposal.ID = id
		case "title":
			proposal.Title = title
		case "description":
			proposal.Description = description
		case "severity":
			proposal.Severity = severity
		case "contract":
			proposal.Contract = contract
		case "source-file":
			proposal.SourceFiles = append(proposal.SourceFiles, sourceFiles...)
		case "related-invariant":
			proposal.RelatedInvariants = append(proposal.RelatedInvariants, relatedInvariants...)
		case "related-failure":
			proposal.RelatedFailures = append(proposal.RelatedFailures, relatedFailures...)
		case "required-test":
			proposal.RequiredTests = append(proposal.RequiredTests, requiredTests...)
		}
	})
	if strings.TrimSpace(proposal.Domain) == "" {
		proposal.Domain = domain
	}

	res, err := qp.Promote(context.Background(), qp.PromoteRequest{
		RepositoryRoot:                         repoRoot,
		TaskDirectory:                          dir,
		RepositoryDomain:                       domain,
		IdentityRoot:                           identityRoot,
		QuestionDispositionReceiptDigestSHA256: dispositionReceipt,
		Proposal:                               proposal,
		EffectiveScopeDomain:                   scopeDomain,
		EffectiveScopeFiles:                    scopeFiles,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "promote-answer:", err)
		return 2
	}

	out := promoteOutput{
		Outcome:                       string(res.Outcome),
		PromotionLineageID:            res.PromotionLineageID,
		ReceiptDigestSHA256:           res.ReceiptDigestSHA256,
		CommittedCausalIdentitySHA256: res.CommittedCausalIdentitySHA256,
		Detail:                        res.Detail,
		CorrectnessCertified:          false,
	}
	if res.PromotionLineageID != "" {
		out.PromotionDirectory = filepath.ToSlash(filepath.Join(".sensei", "project", "promotions", res.PromotionLineageID))
		out.ReceiptPath = filepath.ToSlash(filepath.Join(out.PromotionDirectory, "receipt.json"))
	}

	if strings.TrimSpace(format) == "" || format == "text" {
		renderPromoteHuman(out)
	} else if perr := printValue(out, format); perr != nil {
		fmt.Fprintln(os.Stderr, "promote-answer:", perr)
		return 2
	}
	return promoteExitCode(res.Outcome)
}

func renderPromoteHuman(o promoteOutput) {
	fmt.Printf("outcome:  %s\n", o.Outcome)
	if o.PromotionLineageID != "" {
		fmt.Printf("lineage:  %s\n", short(o.PromotionLineageID))
	}
	if o.ReceiptDigestSHA256 != "" {
		fmt.Printf("receipt:  %s\n", short(o.ReceiptDigestSHA256))
	}
	if o.CommittedCausalIdentitySHA256 != "" {
		fmt.Printf("commit:   %s\n", short(o.CommittedCausalIdentitySHA256))
	}
	if o.PromotionDirectory != "" {
		fmt.Printf("dir:      %s (location hint)\n", o.PromotionDirectory)
	}
	if o.Detail != "" {
		fmt.Printf("detail:   %s\n", o.Detail)
	}
	fmt.Printf("correctness_certified: %v\n", o.CorrectnessCertified)
}

// promoteExitCode maps the owner outcome to a coarse, stable shell code. The JSON
// outcome remains the canonical semantic result.
func promoteExitCode(o qp.Outcome) int {
	switch o {
	case qp.OutcomeCommitted, qp.OutcomeExactReplay:
		return 0
	case qp.OutcomeIneligibleDisposition, qp.OutcomeStaleInput, qp.OutcomeAuthorityRefusal,
		qp.OutcomeScopeRefusal, qp.OutcomeContradiction:
		return 3 // typed refusal
	case qp.OutcomeIncompleteAtSource, qp.OutcomeIncompleteAtGraph, qp.OutcomeIncompleteAtCommit:
		return 4 // incomplete recovery state
	case qp.OutcomeManifestCASFailure, qp.OutcomeGraphVerificationFailure, qp.OutcomeTamperedJournal:
		return 5 // integrity failure
	default:
		return 2 // usage / internal
	}
}
