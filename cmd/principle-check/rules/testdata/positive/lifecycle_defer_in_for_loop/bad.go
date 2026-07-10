// Positive-control fixture: this file MUST trigger the defer_in_for_loop
// ruleguard rule. If the rule reports zero findings here, the rule is
// broken/ineffective and its production "0 findings" is uncharted, not clean.
package defer_in_for_loop

import "os"

func leaky(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close() // BAD: defer accumulates across iterations
		_ = f
	}
	return nil
}
