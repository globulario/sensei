// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/derive"
)

// runDerive attempts one typed proposition against pinned project state and
// prints the receipt.
//
// This is the surface a governed run reaches for. A closure round can propose a
// proposition and say which package to look in; it cannot supply the bytes, the
// derivation, or the outcome. Exit status carries the answer so a caller does
// not have to parse prose:
//
//	0  DERIVED       Sensei computed the proposition from pinned source
//	1  NOT_DERIVED   Sensei computed it and found a counterexample
//	3  UNKNOWN       no registered derivation applies, or inputs unreadable
//
// UNKNOWN is deliberately distinct from NOT_DERIVED. "Nobody taught me to
// compute this" and "I checked and it is false" are different findings, and a
// caller that collapsed them would treat ignorance as refutation.
func runDerive(args []string) int {
	fs := flag.NewFlagSet("sensei derive", flag.ContinueOnError)
	repoRoot := fs.String("repo-root", ".", "repository checkout to read from")
	repo := fs.String("repo", "", "repository domain recorded in the receipt")
	revision := fs.String("revision", "HEAD", "revision to pin; resolved to a full commit id")
	kind := fs.String("kind", string(derive.KindFieldAccessUnderLock), "proposition family")
	dir := fs.String("dir", "", "package directory, repository-relative")
	typeName := fs.String("type", "", "struct type")
	field := fs.String("field", "", "field claimed to be protected")
	lock := fs.String("lock", "", "field holding the lock")
	asJSON := fs.Bool("json", false, "print the receipt as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Attempt one typed architectural proposition against pinned project state.

The proposer chooses the proposition and where to look. Sensei reads the pinned
source itself, runs a registered derivation, and reports what it computed. No
part of the answer comes from the caller.

A proposition is typed — entities and a relation — and carries no prose. A claim
that cannot be expressed that way cannot be attempted, and the answer is UNKNOWN
rather than a weaker kind of yes.

Exit status:
  0  DERIVED        computed from pinned source
  1  NOT_DERIVED    computed, and a counterexample was found
  3  UNKNOWN        no registered derivation applies, or inputs unreadable
  2  usage error

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*dir) == "" || strings.TrimSpace(*typeName) == "" ||
		strings.TrimSpace(*field) == "" || strings.TrimSpace(*lock) == "" {
		fmt.Fprintln(os.Stderr, "error: --dir, --type, --field and --lock are all required")
		return 2
	}
	domain := strings.TrimSpace(*repo)
	if domain == "" {
		domain = strings.TrimSpace(*repoRoot)
	}

	ctx := context.Background()
	src, err := derive.NewGitSource(ctx, *repoRoot, domain, *revision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei derive: %v\n", err)
		return 3
	}
	p := derive.Proposition{
		Kind: derive.Kind(strings.TrimSpace(*kind)), Dir: strings.TrimSpace(*dir),
		Type: strings.TrimSpace(*typeName), Field: strings.TrimSpace(*field),
		Lock: strings.TrimSpace(*lock),
	}
	receipt, established := derive.Derive(src, p, time.Now())

	if *asJSON {
		b, _ := json.MarshalIndent(receipt, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("proposition: %s\n", p)
		fmt.Printf("derivation:  %s/%s\n", receipt.DerivationID, receipt.DerivationVersion)
		fmt.Printf("pinned at:   %s of %s\n", receipt.Commit, receipt.Repository)
		fmt.Printf("read:        %s\n", strings.Join(receipt.Inputs, ", "))
		fmt.Printf("result:      %s\n", receipt.Outcome)
		fmt.Printf("detail:      %s\n", receipt.Detail)
		if len(receipt.CompletenessScope) > 0 {
			fmt.Printf("unobserved:  %s\n", strings.Join(receipt.CompletenessScope, "; "))
		}
		if established != nil {
			fmt.Printf("\nestablished, scoped:\n  %s\n", established.Scope())
		}
	}
	switch receipt.Outcome {
	case derive.Derived:
		return 0
	case derive.NotDerived:
		return 1
	default:
		return 3
	}
}
