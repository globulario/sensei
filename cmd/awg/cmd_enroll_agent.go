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
func runEnrollAgent(args []string) int {
	var repoRoot, principal, issuer, role, format string
	var validFor time.Duration
	fs := flag.NewFlagSet("sensei enroll-agent", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&principal, "principal", "", "agent principal id (default "+identity.DefaultPrincipalID+")")
	fs.StringVar(&issuer, "issuer", "", "trusted issuer (default "+identity.DefaultIssuer+")")
	fs.StringVar(&role, "role", "", "role id to attest (default "+identity.DefaultRoleID+")")
	fs.DurationVar(&validFor, "valid-for", 0, "receipt validity duration (0 = no expiry)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	var roles []string
	if strings.TrimSpace(role) != "" {
		roles = []string{role}
	}
	id, err := identity.Enroll(identity.EnrollOptions{
		Root:        identity.Root(repoRoot),
		PrincipalID: principal,
		Issuer:      issuer,
		Roles:       roles,
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
