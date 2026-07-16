// SPDX-License-Identifier: Apache-2.0

package prereview

import (
	"bytes"
	"encoding/json"
)

// RenderJSON renders the full report as deterministic, indented JSON — the
// machine-readable artifact. The model contains no map fields, so output is
// byte-identical across runs. HTML escaping is disabled for readable digests
// and a trailing newline is emitted.
func RenderJSON(r PreReviewReport) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
