// Positive-control fixture for silence_panic_swallowed_silently.
// defer func(){ _ = recover() }() — panic silently swallowed.
package badfix

func worker() {
	defer func() { _ = recover() }() // BAD: silent recover
	panic("boom")
}
