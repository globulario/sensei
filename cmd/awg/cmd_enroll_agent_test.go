// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/identity"
)

func TestEnrollAgentCommandWritesManifest(t *testing.T) {
	repo := t.TempDir()
	if code := runEnrollAgent([]string{"--repo", repo, "--format", "yaml"}); code != 0 {
		t.Fatalf("enroll-agent exit = %d, want 0", code)
	}
	id, ok, err := identity.LoadManifest(identity.Root(repo))
	if err != nil || !ok {
		t.Fatalf("manifest not written: ok=%v err=%v", ok, err)
	}
	if id.PrincipalID != identity.DefaultPrincipalID || id.Issuer != identity.DefaultIssuer {
		t.Fatalf("unexpected manifest identity: %+v", id)
	}
	// Enrollment mints exactly the one governed repository-repair role.
	if len(id.Roles) != 1 || id.Roles[0] != identity.DefaultRoleID {
		t.Fatalf("unexpected roles: %+v", id.Roles)
	}
	if id.AuthenticationReceiptDigestSHA256 == "" || len(id.RoleAttestationReceiptDigests) == 0 {
		t.Fatalf("manifest missing receipt digests: %+v", id)
	}
}

// TestEnrollAgentRefusesArbitraryRoleOrIssuer proves local enrollment cannot
// select or mint an arbitrary privileged role or ungoverned issuer: a
// non-governed override is refused and writes no identity (regression #10).
func TestEnrollAgentRefusesArbitraryRoleOrIssuer(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"custom role", []string{"--role", "role.repository_admin"}},
		{"custom issuer", []string{"--issuer", "attacker.local"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			args := append([]string{"--repo", repo}, tc.args...)
			if code := runEnrollAgent(args); code == 0 {
				t.Fatalf("enroll-agent accepted %s; want refusal", tc.name)
			}
			if _, ok, _ := identity.LoadManifest(identity.Root(repo)); ok {
				t.Fatalf("refused %s but wrote an identity manifest", tc.name)
			}
		})
	}
}

// TestEnrollAgentAcceptsGovernedOverride proves passing the governed issuer/role
// explicitly is honored — confinement refuses only non-governed values.
func TestEnrollAgentAcceptsGovernedOverride(t *testing.T) {
	repo := t.TempDir()
	code := runEnrollAgent([]string{"--repo", repo, "--issuer", identity.DefaultIssuer, "--role", identity.DefaultRoleID})
	if code != 0 {
		t.Fatalf("enroll-agent exit = %d, want 0", code)
	}
	id, ok, err := identity.LoadManifest(identity.Root(repo))
	if err != nil || !ok {
		t.Fatalf("manifest not written: ok=%v err=%v", ok, err)
	}
	if id.Issuer != identity.DefaultIssuer || len(id.Roles) != 1 || id.Roles[0] != identity.DefaultRoleID {
		t.Fatalf("unexpected governed identity: %+v", id)
	}
}
