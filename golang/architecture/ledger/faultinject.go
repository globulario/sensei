// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package ledger

import "errors"

// This file is compiled ONLY under the sensei_faultinject build tag. It is absent
// from every normal build (`go build`, `go test` without the tag), so neither the
// exported ledger API nor any process-global state can toggle HEAD-write failures
// in production. It exists exclusively so deterministic tests of the durable-entry
// / HEAD-failure recovery path can inject a post-commit HEAD-write fault.

// pendingHeadWriteFaults counts HEAD writes that must fail. It is mutated only by
// InjectHeadWriteFaults, from serial tests running under the build tag.
var pendingHeadWriteFaults int

// consumedHeadWriteFaults counts faults that actually fired. A fault-injection
// test whose fault never fires passes while proving nothing, so the tests assert
// on this rather than trusting that arming an injector had an effect.
var consumedHeadWriteFaults int

// InjectHeadWriteFaults makes the next n HEAD writes fail. Compiled only under the
// sensei_faultinject tag; it does not exist in the production ledger API.
func InjectHeadWriteFaults(n int) { pendingHeadWriteFaults = n }

// headWriteFault returns an injected error for each pending fault, then nil.
func headWriteFault() error {
	if pendingHeadWriteFaults > 0 {
		pendingHeadWriteFaults--
		consumedHeadWriteFaults++
		return errors.New("injected head write fault")
	}
	return nil
}

// ClearHeadWriteFaults disarms any pending faults and resets the consumed count.
// The counters are process-global, so a test that arms faults it does not consume
// would poison the next one.
func ClearHeadWriteFaults() { pendingHeadWriteFaults, consumedHeadWriteFaults = 0, 0 }

// ConsumedHeadWriteFaults reports how many armed faults actually fired.
//
// This is what a test must assert on. Asserting that nothing remains PENDING is
// trivially true when nothing was armed, so it cannot tell "the fault fired" from
// "the fault was never armed" -- and the second is exactly how a fault-injection
// test comes to pass while exercising the ordinary path.
func ConsumedHeadWriteFaults() int { return consumedHeadWriteFaults }
