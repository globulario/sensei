// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/identity"
)

// runEnrollAgent materializes a locally-trusted agent identity for admission v2.
// It is the explicit, human-invoked trust act: task preparation only consumes an
// enrolled identity and never mints one itself.
//
// Local enrollment is confined to a single governed repository-repair identity.
// It always mints the governed local issuer and repository-repair role and can
// neither select nor mint an arbitrary privileged issuer or role: a non-governed
// override is refused rather than honored.
func runEnrollAgent(args []string) int {
	var repoRoot, principal, issuer, role, format string
	var validFor time.Duration
	fs := flag.NewFlagSet("sensei enroll-agent", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&principal, "principal", "", "agent principal id (default "+identity.DefaultPrincipalID+")")
	fs.StringVar(&issuer, "issuer", "", "reserved: enrollment always uses the governed issuer "+identity.DefaultIssuer)
	fs.StringVar(&role, "role", "", "reserved: enrollment always attests the governed role "+identity.DefaultRoleID)
	fs.DurationVar(&validFor, "valid-for", 0, "receipt validity duration (0 = no expiry)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if v := strings.TrimSpace(issuer); v != "" && v != identity.DefaultIssuer {
		fmt.Fprintf(os.Stderr, "enroll-agent: local enrollment uses only the governed issuer %q; %q is refused\n", identity.DefaultIssuer, v)
		return 1
	}
	if v := strings.TrimSpace(role); v != "" && v != identity.DefaultRoleID {
		fmt.Fprintf(os.Stderr, "enroll-agent: local enrollment attests only the governed role %q; %q is refused\n", identity.DefaultRoleID, v)
		return 1
	}
	id, err := identity.Enroll(identity.EnrollOptions{
		Root:        identity.Root(repoRoot),
		PrincipalID: principal,
		Issuer:      identity.DefaultIssuer,
		Roles:       []string{identity.DefaultRoleID},
		Now:         nowUTC(),
		ValidFor:    validFor,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "enroll-agent:", err)
		return 1
	}
	if err := printValue(id, format); err != nil {
		return 1
	}
	return 0
}
