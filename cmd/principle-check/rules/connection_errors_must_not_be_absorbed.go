// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.connection_errors
// @awareness file_role=ruleguard_rules_for_meta_connection_errors_must_not_be_absorbed
// @awareness enforces=globular.platform:invariant.meta.connection_errors_must_not_be_absorbed

// Ruleguard rules for the meta-principle
//
//	meta.connection_errors_must_not_be_absorbed
//
// The principle: a TLS/auth/dial/network error must not be silently
// downgraded to a generic timeout or swallowed entirely. The error CLASS
// (the typed sentinel) must reach the caller so downstream code can
// branch on the real cause.
//
// Detection strategy — purely structural; ruleguard does not have
// interprocedural dataflow. Each rule pairs:
//
//	(1) a connection-class function call binding err, AND
//	(2) an err != nil branch that returns nil for the error position.
//
// "Returns nil for err" is the absorption shape. Wrapping with
// fmt.Errorf("...: %w", err) is NOT absorption — the err class is
// preserved via %w. Returning a freshly-constructed error without
// wrapping IS absorption.
//
// The rules are intentionally narrow: false positives are worse than
// missed findings here. Operators rerun the scanner after each fix.
//
// Build tag `ignore` keeps this file out of `go build ./...`; ruleguard
// reads it as a rule source, not as a Go source file in the module.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// netDialAbsorbed catches the canonical net.Dial / net.DialTimeout /
// (*net.Dialer).Dial idiom where the returned err is then swallowed.
//
//	conn, err := net.Dial(...)
//	if err != nil {
//	    return nil, nil               // ← absorption
//	}
//
//	conn, err := net.Dial(...)
//	if err != nil {
//	    return nil                    // ← absorption (single return)
//	}
func netDialAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := net.Dial($*_); if $err != nil { $*_; return $_, nil }`,
		`$_, $err := net.DialTimeout($*_); if $err != nil { $*_; return $_, nil }`).
		Report(`net.Dial err absorbed (returned nil for err position); preserve the dial-error class via fmt.Errorf("...: %w", err) or a typed sentinel — see meta.connection_errors_must_not_be_absorbed`)
}

// netDialerAbsorbed catches the receiver form (*net.Dialer).Dial.
// Separate rule because the Where filter binds a metavariable that only
// exists in this Match pattern.
func netDialerAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := $d.Dial($*_); if $err != nil { $*_; return $_, nil }`).
		Where(m["d"].Type.Is(`*net.Dialer`)).
		Report(`(*net.Dialer).Dial err absorbed; preserve via fmt.Errorf("...: %w", err) or typed sentinel — see meta.connection_errors_must_not_be_absorbed`)
}

// tlsDialAbsorbed catches tls.Dial / tls.DialWithDialer absorption.
// TLS handshake errors are especially load-bearing: callers branch on
// "is this a cert problem, an auth problem, or a network problem?"
// Collapsing into a generic timeout destroys the signal.
func tlsDialAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := tls.Dial($*_); if $err != nil { $*_; return $_, nil }`,
		`$_, $err := tls.DialWithDialer($*_); if $err != nil { $*_; return $_, nil }`).
		Report(`tls.Dial err absorbed; TLS handshake errors carry cert/auth/network class — preserve via fmt.Errorf("...: %w", err) or branch on typed sentinels`)
}

// grpcDialAbsorbed catches grpc.Dial / grpc.DialContext absorption.
// gRPC dial errors usually wrap a connection class plus a service-
// discovery class — both must survive to the caller.
func grpcDialAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := grpc.Dial($*_); if $err != nil { $*_; return $_, nil }`,
		`$_, $err := grpc.DialContext($*_); if $err != nil { $*_; return $_, nil }`,
		`$_, $err := grpc.NewClient($*_); if $err != nil { $*_; return $_, nil }`).
		Report(`grpc.Dial err absorbed; gRPC dial errors carry status codes — preserve via fmt.Errorf("...: %w", err)`)
}

// etcdClientAbsorbed catches clientv3.New absorption. Etcd connection
// errors carry endpoint, auth, and TLS information; absorbing them
// leaves the caller blind to why etcd is unreachable.
func etcdClientAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := clientv3.New($*_); if $err != nil { $*_; return $_, nil }`).
		Report(`etcd clientv3.New err absorbed; etcd connect errors carry endpoint/auth/TLS class — preserve via fmt.Errorf("...: %w", err)`)
}

// gocqlSessionAbsorbed catches Scylla session creation absorption.
// Scylla session errors are particularly important — auth/TLS/quorum
// failure shapes must reach the caller for incident triage.
//
// We match structurally without a type filter — gocql isn't in stdlib
// so ruleguard's type-name resolution can't bind *gocql.ClusterConfig
// at rule-compile time. The CreateSession method name is distinctive
// enough that false positives are unlikely.
func gocqlSessionAbsorbed(m dsl.Matcher) {
	m.Match(`$_, $err := $_.CreateSession(); if $err != nil { $*_; return $_, nil }`).
		Report(`possible gocql/Scylla session err absorbed; if this is gocql.CreateSession, preserve via fmt.Errorf("...: %w", err) — Scylla session errors carry auth/TLS/quorum class`)
}
