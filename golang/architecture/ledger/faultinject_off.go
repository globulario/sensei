// SPDX-License-Identifier: Apache-2.0

//go:build !sensei_faultinject

package ledger

// headWriteFault is the production HEAD-write seam: always nil, so writeHead can
// never be induced to fail through any exported API or process-global state. The
// only alternative definition lives in faultinject.go, compiled solely under the
// sensei_faultinject build tag for deterministic durable-entry tests; it does not
// ship in any normal build.
func headWriteFault() error { return nil }
