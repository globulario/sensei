// SPDX-License-Identifier: AGPL-3.0-only

//go:build !sensei_faultinject

package ledger

// headWriteFault is the production HEAD-write seam: always nil, so writeHead can
// never be induced to fail through any exported API or process-global state. The
// only alternative definition lives in faultinject.go, compiled solely under the
// sensei_faultinject build tag for deterministic durable-entry tests; it does not
// ship in any normal build.
func headWriteFault() error { return nil }

// headPublicationFaults is the production per-store publication seam: an empty
// struct with no state and no option that sets it, so publishHead can never be
// induced to fail through the ledger API. Its armable definition, and the
// WithHeadPublicationFault(s) options, live only in faultinject.go.
type headPublicationFaults struct{}

func (*headPublicationFaults) next() error { return nil }
