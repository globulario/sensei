// SPDX-License-Identifier: Apache-2.0

// Package orders is a tiny fixture service. Money is modelled as integer
// minor units (cents) and protected by the invariant
// money.amounts_are_integer_minor_units.
package orders

// Money is an amount in integer minor units (cents). Never float — float
// arithmetic on money silently loses cents to rounding.
type Money int64 // cents

type LineItem struct {
	Name      string
	UnitCents Money
	Qty       int64
}

// OrderTotal sums the line items exactly, in cents.
func OrderTotal(items []LineItem) Money {
	var total Money
	for _, it := range items {
		total += it.UnitCents * Money(it.Qty)
	}
	return total
}

// DiscountedTotal applies a percent discount in integer minor units with an
// explicit, deterministic rounding rule (floor of the discount). This is the
// AWG-guided shape: the briefing for this file surfaces the critical invariant
// "money is integer cents; never float" before the edit, steering away from the
// obvious `float64(total) * 0.9`.
func DiscountedTotal(items []LineItem, percentOff int64) Money {
	total := OrderTotal(items)
	return total - (total*Money(percentOff))/100
}
