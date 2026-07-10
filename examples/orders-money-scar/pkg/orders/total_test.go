// SPDX-License-Identifier: Apache-2.0

package orders

import "testing"

// naiveFloatDiscount is the fix an agent writes WITHOUT the AWG briefing — the
// obvious, plausible, and wrong one: money routed through float64.
func naiveFloatDiscount(items []LineItem, percentOff int64) Money {
	total := OrderTotal(items)
	return Money(float64(total) * (1 - float64(percentOff)/100))
}

// TestOrderTotal_DiscountStaysExactInteger is the required test named by the
// invariant. It proves the scar: on a $9.99 order the unsafe float path yields
// 899 cents while the AWG-guided integer path yields the exact 900 — a silent
// one-cent divergence that is real money across orders.
func TestOrderTotal_DiscountStaysExactInteger(t *testing.T) {
	items := []LineItem{{Name: "widget", UnitCents: 999, Qty: 1}} // $9.99

	safe := DiscountedTotal(items, 10)     // AWG-guided integer path
	naive := naiveFloatDiscount(items, 10) // blind float path

	if safe != 900 {
		t.Errorf("integer discount = %d cents, want 900", safe)
	}
	if naive == safe {
		t.Errorf("expected the float path to diverge from the integer path (scar not reproduced)")
	} else {
		t.Logf("SCAR PROVEN — float discount = %d cents vs exact integer = %d (silent %d-cent divergence)",
			naive, safe, safe-naive)
	}
}
