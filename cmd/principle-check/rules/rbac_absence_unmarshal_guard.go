// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.rbac_absence_guard
// @awareness file_role=ruleguard_rules_for_meta_absence_scope_must_be_explicit
// @awareness enforces=globular.platform:invariant.rbac.getitem_consumers_must_handle_empty_payload

// Ruleguard rules for the per-instance invariant
//
//	rbac.getitem_consumers_must_handle_empty_payload
//
// (parent meta.absence_scope_must_be_explicit)
//
// The bug shape this catches is the 2026-05-21 regression (commit
// 0d009a4f). srv.getItem returns (nil, nil) when no row exists for
// the key — the absence is legitimate. Callers that pipe the (nil)
// bytes directly into json.Unmarshal get "unexpected end of JSON
// input" and bubble the decode error to the RPC caller as Internal.
// The bug class is "absence treated as malformed" — the empty payload
// IS the answer, not a parse failure.
//
// The fix in 0d009a4f added `if len(data) == 0 { return <empty> }`
// between getItem and json.Unmarshal at the 12 known call sites
// (rbac_actions.go, rbac_index.go, rbac_sharing.go,
// rbac_permissions.go, rbac_role_bindings.go).
//
// The rule matches the BUG sequence — getItem → err check →
// Unmarshal directly, without a len guard between. Correctly-fixed
// sites have an extra `if len(data) == 0 { ... }` statement that
// breaks the three-statement match.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// getItemUnmarshalWithoutLenGuard catches the regression shape of
// 2026-05-21 commit 0d009a4f: a sequence of (1) getItem call, (2) err
// check, (3) json.Unmarshal — with no len(data) == 0 guard inserted
// between (2) and (3). The properly-guarded form has four statements
// (getItem → err check → len guard → Unmarshal) and does not match.
//
// We match three flavors of the err-check return shape because
// different RPCs return different ProtoBuf response types.
func getItemUnmarshalWithoutLenGuard(m dsl.Matcher) {
	// `$*_` inside the err-handling block matches any sequence of
	// statements (nested returns, string-matching, etc.) so the rule
	// covers err-blocks with detection logic, not just bare returns.
	m.Match(
		`$data, $err := $srv.getItem($_); if $err != nil { $*_ }; json.Unmarshal($data, $_)`,
		`$data, $err := $srv.getItem($_); if $err != nil { $*_ }; if err := json.Unmarshal($data, $_); err != nil { $*_ }`,
	).Report(`getItem → err check → json.Unmarshal with no "if len(data) == 0" guard between — empty payload from getItem is legitimate absence (srv.getItem returns (nil, nil) for missing keys); json.Unmarshal on nil bytes returns "unexpected end of JSON input" and leaks decode errors to RPC callers. Add: if len($data) == 0 { return <empty result> }. See meta.absence_scope_must_be_explicit + commit 0d009a4f for the 2026-05-21 regression`)
}
