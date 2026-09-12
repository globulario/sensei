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

// InjectHeadWriteFaults makes the next n HEAD writes fail. Compiled only under the
// sensei_faultinject tag; it does not exist in the production ledger API.
func InjectHeadWriteFaults(n int) { pendingHeadWriteFaults = n }

// HeadPublicationAttempts is the bound Store.Append retries one HEAD publication
// to. Injecting exactly this many faults exhausts it; one fewer is absorbed.
func HeadPublicationAttempts() int { return headPublicationAttempts }

// PendingHeadWriteFaults reports faults armed but not yet consumed, so a test can
// prove its faults actually fired rather than merely that they were armed.
func PendingHeadWriteFaults() int { return pendingHeadWriteFaults }

// headWriteFault returns an injected error for each pending fault, then nil.
func headWriteFault() error {
	if pendingHeadWriteFaults > 0 {
		pendingHeadWriteFaults--
		return errors.New("injected head write fault")
	}
	return nil
}

// headPublicationFaults is the armable per-store publication seam: each HEAD
// publication attempt Store.Append makes consumes one error, in order.
type headPublicationFaults struct {
	errs []error
}

func (f *headPublicationFaults) next() error {
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

// WithHeadPublicationFault makes the next HEAD publication attempt on THIS store
// fail once, with the supplied error. Compiled only under the sensei_faultinject
// tag; it does not exist in the production ledger API.
//
// One fault is absorbed by Append's bounded retry; see WithHeadPublicationFaults
// to exhaust it.
func WithHeadPublicationFault(err error) StoreOption {
	return WithHeadPublicationFaults(err)
}

// WithHeadPublicationFaults makes the next len(errs) HEAD publication attempts on
// THIS store fail, in order. It is instance-scoped -- it lives on one Store, never
// in package state -- so two tests cannot reach each other. Compiled only under
// the sensei_faultinject tag; it does not exist in the production ledger API.
func WithHeadPublicationFaults(errs ...error) StoreOption {
	return func(s *Store) { s.headFaults.errs = append(s.headFaults.errs, errs...) }
}
