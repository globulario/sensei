// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// InterpretationAuthorityRequest is the exact evidence-binding surface an
// interpretation authority owner receives. The candidate interpretation is
// already detached and O2-validated, but still advisory. RepositoryRoot is a
// capability location, never identity: authoritative identity comes from the
// session's repository revision and graph-authority digest.
type InterpretationAuthorityRequest struct {
	RepositoryRoot string
	Session        synthesis.Session
	Interpretation synthesis.Interpretation
}

// InterpretationAuthority owns the pre-synthesis promotion decision. O2 is
// intentionally absent from this interface: a provider may propose an
// interpretation, but cannot certify its own proposal. Implementations are
// responsible for collecting/recomputing the evidence represented by the
// returned receipt. The driver independently verifies receipt binding and
// policy before promotion.
type InterpretationAuthority interface {
	Assess(ctx context.Context, request InterpretationAuthorityRequest) (interpretationclosure.Receipt, error)
}

// InterpretationAuthorityFunc is the function adapter for tests and concrete
// composition roots. It does not weaken the boundary: Config still requires
// an explicit authority capability distinct from InterpretationProvider.
type InterpretationAuthorityFunc func(context.Context, InterpretationAuthorityRequest) (interpretationclosure.Receipt, error)

func (f InterpretationAuthorityFunc) Assess(ctx context.Context, request InterpretationAuthorityRequest) (interpretationclosure.Receipt, error) {
	if f == nil {
		return interpretationclosure.Receipt{}, errors.New("synthesisdriver: nil interpretation authority")
	}
	return f(ctx, request)
}
