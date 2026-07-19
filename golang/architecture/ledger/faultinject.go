// SPDX-License-Identifier: Apache-2.0

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

// InjectHeadWriteFaults makes the next n HEAD writes fail. Compiled only under the
// sensei_faultinject tag; it does not exist in the production ledger API.
func InjectHeadWriteFaults(n int) { pendingHeadWriteFaults = n }

// headWriteFault returns an injected error for each pending fault, then nil.
func headWriteFault() error {
	if pendingHeadWriteFaults > 0 {
		pendingHeadWriteFaults--
		return errors.New("injected head write fault")
	}
	return nil
}
