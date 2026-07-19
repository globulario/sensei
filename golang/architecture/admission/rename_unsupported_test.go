// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// v1 cannot admit or verify a repository rename. DecideAdmission refuses a plan
// that carries one, and VerifyScope refuses an observed Git rename, naming both
// endpoints and never producing a verified receipt.

func TestDecideAdmissionRejectsRename(t *testing.T) {
	req, res := v2Fixture(t)
	req.ChangePlan.Operations[0].Kind = closureprotocol.OperationRename
	if _, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt); err == nil ||
		!strings.Contains(err.Error(), "protocol.rename_requires_explicit_source_and_destination") {
		t.Fatalf("expected DecideAdmission to refuse rename, got %v", err)
	}
}

func TestVerifyScopeRefusesObservedRename(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.Files = []ObservedFile{{
		ChangeType: string(closureprotocol.OperationRename),
		Path:       "internal/owner.go",
		FromPath:   "owner.go",
		ToPath:     "internal/owner.go",
	}}
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) {
		t.Fatal("a rename must never scope-verify")
	}
	if !hasViolation(v, "scope.operation.rename_unsupported") {
		t.Fatalf("expected scope.operation.rename_unsupported, got %+v", v.Violations)
	}
	// The violation detail must preserve both endpoints, not drop the source.
	var detail string
	for _, viol := range v.Violations {
		if viol.Code == "scope.operation.rename_unsupported" {
			detail = viol.Detail
		}
	}
	if !strings.Contains(detail, "owner.go") || !strings.Contains(detail, "internal/owner.go") {
		t.Fatalf("rename violation must name both endpoints, got %q", detail)
	}
}
