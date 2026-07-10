// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.errorf_v_loses_chain
// @awareness file_role=ruleguard_rules_for_meta_connection_errors_must_not_be_absorbed
// @awareness enforces=globular.platform:invariant.errorf_must_use_w_verb_to_preserve_err_chain

// Ruleguard rules for the per-instance invariant
//
//	errorf_must_use_w_verb_to_preserve_err_chain
//
// (parent meta.connection_errors_must_not_be_absorbed)
//
// The bug shape: `fmt.Errorf("...: %v", err)` formats the error's
// string representation but DOES NOT wrap it. Callers up the chain
// cannot use `errors.Is(err, context.DeadlineExceeded)`,
// `errors.Is(err, clientv3.ErrTimeout)`, `errors.As(err, &netErr)`,
// or any typed-error inspection — the err class is collapsed to a
// generic string.
//
// The fix is `%w` which produces identical formatted output but
// preserves the chain so errors.Is/As/Unwrap can traverse it.
//
// This is a strict subset of meta.connection_errors_must_not_be_absorbed:
// the absorption happens at the wrapping boundary, not at the
// return-nil boundary the earlier rule catches.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// errorfWithVerbVLosesChain catches `fmt.Errorf("...: %v", err)` and
// `fmt.Errorf("... %v", err)` shapes — wrapping an err with %v
// instead of %w. The metavar $err is constrained to look like a Go
// err variable so we don't false-positive on legitimate Sprintf-
// style formatting where the trailing arg happens to be unrelated.
func errorfWithVerbVLosesChain(m dsl.Matcher) {
	// The Text of $fmt is the literal source including the surrounding
	// quotes — e.g. `"cannot resolve ScyllaDB hosts from etcd: %v"`. We
	// match any format string ending in `%v"` (the closing quote is
	// part of the Text). $err is constrained to look like a Go err
	// variable so we don't false-positive on Sprintf-style logging.
	// Match fmt.Errorf with a string-literal format ending in `%v"`
	// (the source text of the literal includes its surrounding double
	// quotes). $err is constrained to look like an err variable to
	// avoid false positives on Sprintf-style logging.
	m.Match(`fmt.Errorf($fmt, $err)`).
		Where(
			m["fmt"].Text.Matches("%v\"$") &&
				m["err"].Text.Matches(`^(err|.*Err)$`),
		).
		Report(`fmt.Errorf wraps err with %v — loses the err chain so callers can't use errors.Is/As. Change %v to %w. See meta.connection_errors_must_not_be_absorbed`)
}
