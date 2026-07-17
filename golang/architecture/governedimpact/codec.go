// SPDX-License-Identifier: Apache-2.0

package governedimpact

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// MarshalCanonicalReport renders a validated impact report to deterministic
// canonical JSON. The same report always produces identical bytes, and the full
// base and result manifests plus exact changed record ids are preserved.
func MarshalCanonicalReport(report Report) ([]byte, error) {
	if err := ValidateReport(report); err != nil {
		return nil, err
	}
	return closureprotocol.CanonicalJSON(report)
}

// ParseReport strictly decodes canonical impact-report bytes: unknown fields and
// trailing data are rejected, and ValidateReport runs before returning, so a
// stored report re-validates on reload.
func ParseReport(data []byte) (Report, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rep Report
	if err := dec.Decode(&rep); err != nil {
		return Report{}, newErr(CodeInvalidReport, "decode: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Report{}, newErr(CodeInvalidReport, "trailing content after report")
	}
	if err := ValidateReport(rep); err != nil {
		return Report{}, err
	}
	return rep, nil
}
