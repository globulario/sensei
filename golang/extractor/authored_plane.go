// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/rdf"
)

func emitGovernedArchitecturalPlane(e *rdf.Emitter, subj, class, id, status, explicitPlane string, supersededBy []string) error {
	explicitPlane = strings.TrimSpace(explicitPlane)
	if explicitPlane == "" {
		return nil
	}
	if err := plane.ValidateGovernedPlaneAnnotation(class, status, explicitPlane, supersededBy); err != nil {
		return fmt.Errorf("%s %s architectural_plane: %w", class, id, err)
	}
	e.Triple(subj, rdf.IRI(rdf.PropArchitecturalPlane), rdf.Lit(explicitPlane))
	return nil
}
