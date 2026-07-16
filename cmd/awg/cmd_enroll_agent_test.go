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
	if id.AuthenticationReceiptDigestSHA256 == "" || len(id.RoleAttestationReceiptDigests) == 0 {
		t.Fatalf("manifest missing receipt digests: %+v", id)
	}
}
