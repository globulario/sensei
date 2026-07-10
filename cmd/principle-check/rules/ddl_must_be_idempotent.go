// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.ddl_must_be_idempotent
// @awareness file_role=ruleguard_rules_for_meta_idempotence_is_a_requirement_not_a_quality

// Ruleguard rules for the per-instance invariant
//
//	ddl_statements_must_use_if_not_exists
//
// (parent meta.idempotence_is_a_requirement_not_a_quality)
//
// The bug shape: a CREATE KEYSPACE / CREATE TABLE / CREATE INDEX
// statement in source code that does not include `IF NOT EXISTS`.
// On every restart or replay (workflow resume, schema migration
// coordinator retry, recovery path), a bare CREATE fails the second
// time and is propagated as a fatal error. With IF NOT EXISTS the
// statement is idempotent — it succeeds on first run and is a no-op
// on subsequent ones.
//
// Today's sweep: every DDL site I could find already uses
// IF NOT EXISTS. The rule is a pure regression detector — any NEW
// DDL that lands without it fails the scan loudly.
//
// Canonical good shape:
//
//	`CREATE KEYSPACE IF NOT EXISTS keyspace_name WITH ...`
//	`CREATE TABLE IF NOT EXISTS keyspace.table_name (...)`
//	`CREATE INDEX IF NOT EXISTS index_name ON table (col)`
//
// Bug shape:
//
//	`CREATE KEYSPACE keyspace_name WITH ...`
//
// ruleguard sees source code STRINGS, so we match string literals
// at any position (function arg, raw string assignment, switch case
// value) whose contents begin with a CREATE statement missing the
// IF NOT EXISTS clause.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// ddlMissingIfNotExists catches CREATE statements in string literals
// that lack IF NOT EXISTS. Matches three flavours: KEYSPACE, TABLE,
// INDEX. ruleguard matches the literal text of a string; we use a
// regex test on the matched literal's Text.Value to detect the
// missing IF NOT EXISTS clause.
//
// The regex demands:
//
//	^\s*CREATE\s+(KEYSPACE|TABLE|INDEX|MATERIALIZED VIEW)\s+
//
// followed by anything OTHER than IF NOT EXISTS as the first token.
// To express "not IF NOT EXISTS" we negate by requiring an
// identifier directly after the kind keyword.
func ddlMissingIfNotExists(m dsl.Matcher) {
	// ruleguard rejects `$x` alone as "too general". We need more
	// specific positional patterns. The shapes that actually appear
	// in our codebase: string literal passed to a gocql/sql Query
	// method, string literal assigned to a const, string literal
	// in a Sprintf/format call. Match those three positions.
	m.Match(
		// session.Query(`CREATE ...`).Exec()  (gocql)
		`$_.Query($x).$_`,
		// session.ExecuteBatch(... Query($x) ...)  (gocql)
		`$_.Query($x)`,
		// db.Exec(`CREATE ...`)  (database/sql)
		`$_.Exec($x)`,
		`$_.Exec($x, $*_)`,
		// const $name = `CREATE ...`
		`const $name = $x`,
		// var $name = `CREATE ...`
		`var $name = $x`,
		// fmt.Sprintf(`CREATE ...`, args...)
		`fmt.Sprintf($x, $*_)`,
		// $name := `CREATE ...` followed by use
		`$name := $x`,
	).
		Where(
			m["x"].Const &&
				m["x"].Type.Is("string") &&
				m["x"].Text.Matches(`(?i)\bCREATE\s+(KEYSPACE|TABLE|INDEX|MATERIALIZED\s+VIEW)\b`) &&
				!m["x"].Text.Matches(`(?i)IF\s+NOT\s+EXISTS`),
		).
		Report(`DDL string contains a CREATE statement without IF NOT EXISTS — replay or workflow resume will fail. Add IF NOT EXISTS to make the statement idempotent. See meta.idempotence_is_a_requirement_not_a_quality`)
}
