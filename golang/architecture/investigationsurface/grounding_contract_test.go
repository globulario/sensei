// SPDX-License-Identifier: AGPL-3.0-only

package investigationsurface

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestRunArchitectureRefusesPreCompositionCandidates(t *testing.T) {
	how := validSurfaceDocument()
	why := how
	how.CandidateClaims = []architecture.Claim{{ID: "claim.preexisting"}}
	grounding := GroundingFromDocuments(how, why)
	_, err := RunArchitecture(ArchitectureRequest{How: how, Why: why, Grounding: grounding})
	if err == nil || !strings.Contains(err.Error(), "must not contain pre-composition candidates or questions") {
		t.Fatalf("pre-composition candidate was not refused: %v", err)
	}
}
