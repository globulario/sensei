// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

var resolveRPC = func(ctx context.Context, addr, class, id, domain string) (*awarenesspb.ResolveResponse, error) {
	c, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Resolve(ctx, class, id, domain)
}

func runResolve(args []string) int {
	fs := flag.NewFlagSet("awg resolve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	domain := fs.String("domain", "", "optional domain/repo scope; a node outside this scope resolves to not-found")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg resolve <class> <id>

Fetches a single awareness node by class + bare id.

Classes: Invariant, FailureMode, IncidentPattern, Intent,
         ForbiddenFix, Test, SourceFile, Symbol, CodeSymbol,
         MetaPrinciple, Component, Boundary, Contract, Decision, Evidence,
         DesignPattern, ImplementationPattern, PatternMisuse

Examples:
  awg resolve Invariant reconcile.dep_block_records_must_be_cleared
  awg resolve FailureMode service.runtime_identity_unproven

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "awg resolve: requires exactly 2 args: <class> <id>")
		return 2
	}
	class := fs.Arg(0)
	id := fs.Arg(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := resolveRPC(ctx, *addr, class, id, *domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg resolve: %s\n", formatReadSurfaceError("resolve", err))
		return 1
	}

	if !resp.GetFound() {
		printGraphAuthority(resp.GetAuthority())
		fmt.Printf("not found: %s:%s\n", class, id)
		return 2
	}

	if *asJSON {
		return emitProtoJSON(resp)
	}

	n := resp.GetNode()
	if n == nil {
		printGraphAuthority(resp.GetAuthority())
		fmt.Println("(empty node)")
		return 0
	}
	printGraphAuthority(resp.GetAuthority())
	fmt.Printf("%s:%s\n", n.GetClass(), n.GetId())
	if v := n.GetLabel(); v != "" {
		fmt.Printf("  Label:       %s\n", v)
	}
	if v := n.GetSeverity(); v != "" {
		fmt.Printf("  Severity:    %s\n", v)
	}
	if v := n.GetStatus(); v != "" {
		fmt.Printf("  Status:      %s\n", v)
	}
	if v := n.GetDescription(); v != "" {
		fmt.Printf("  Description: %s\n", strings.TrimSpace(v))
	}
	if k := n.GetUmlKind(); k != "" {
		uml := "UML " + k
		if s := n.GetUmlStereotype(); s != "" {
			uml += " «" + s + "»"
		}
		if vw := n.GetUmlView(); vw != "" {
			uml += " [" + vw + " view]"
		}
		fmt.Printf("  UML:         %s\n", uml)
	}
	if v := n.GetIri(); v != "" {
		fmt.Printf("  IRI:         %s\n", v)
	}
	if a := n.GetAnchor(); a != nil {
		if src := a.GetSourceYaml(); src != "" {
			fmt.Printf("  Source YAML: %s\n", src)
		}
		if f := a.GetFile(); f != "" {
			loc := f
			if a.GetLineStart() != 0 {
				loc = fmt.Sprintf("%s:%d", f, a.GetLineStart())
				if a.GetLineEnd() != 0 && a.GetLineEnd() != a.GetLineStart() {
					loc += fmt.Sprintf("-%d", a.GetLineEnd())
				}
			}
			fmt.Printf("  Code anchor: %s\n", loc)
		}
		if sym := a.GetSymbol(); sym != "" {
			fmt.Printf("  Symbol:      %s\n", sym)
		}
	}
	if rel := n.GetRelatedIds(); len(rel) > 0 {
		fmt.Println("  Related:")
		for _, rid := range rel {
			fmt.Printf("    - %s\n", rid)
		}
	}
	return 0
}
