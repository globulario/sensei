// SPDX-License-Identifier: Apache-2.0

package prereview

import "gopkg.in/yaml.v3"

// RenderYAML renders the full report as deterministic YAML. Struct field order
// is fixed and the model has no map fields, so output is byte-identical across
// runs.
func RenderYAML(r PreReviewReport) ([]byte, error) {
	return yaml.Marshal(r)
}
