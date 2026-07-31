// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import "github.com/globulario/sensei/golang/architecture/synthesis"

// NormalizeCapabilities returns a copy of c with every slice field non-nil.
func NormalizeCapabilities(c Capabilities) Capabilities {
	c.SupportedOperations = normalizeOperations(c.SupportedOperations)
	return c
}

// NormalizeRequest returns r unchanged -- Request has no slice fields to
// normalize.
func NormalizeRequest(r Request) Request { return r }

// NormalizeResult returns r unchanged -- Result has no slice fields to
// normalize.
func NormalizeResult(r Result) Result { return r }

// NormalizeObservationBatch returns a copy of b with every slice field
// non-nil.
func NormalizeObservationBatch(b ObservationBatch) ObservationBatch {
	if b.Observations == nil {
		b.Observations = []Observation{}
	}
	return b
}

// NormalizeReceipt returns a copy of r with every slice field non-nil.
func NormalizeReceipt(r Receipt) Receipt {
	if r.Limitations == nil {
		r.Limitations = []synthesis.Limitation{}
	}
	return r
}

func normalizeOperations(in []Operation) []Operation {
	if in == nil {
		return []Operation{}
	}
	return in
}
