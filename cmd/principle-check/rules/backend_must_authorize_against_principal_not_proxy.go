// SPDX-License-Identifier: Apache-2.0

//go:build ignore
// +build ignore

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.rules.backend_must_authorize_against_principal_not_proxy
// @awareness file_role=ruleguard_rules_for_meta_principal_must_survive_proxy_chains

// Ruleguard rules for the per-instance invariant
//
//	backend_must_check_originating_principal_not_only_peer_cert
//
// (parent meta.principal_must_survive_proxy_chains)
//
// The bug shape (confused deputy, Hardy 1988): a gRPC server that
// extracts the peer mTLS certificate to identify the caller — and
// authorizes against that peer identity. When a gateway or sidecar
// proxies on behalf of a user, the peer cert is the GATEWAY's, not
// the user's. Authorizing against the peer grants the gateway's
// scope to anyone the gateway accepts.
//
// The principle says the originating PRINCIPAL must survive the
// proxy chain — via JWT claims, signed RPC metadata, or an mTLS
// chain whose terminal subject is the principal. The peer identity
// is metadata about the path the request took, not authority for
// the request.
//
// Canonical good shape:
//
//  1. Extract the peer identity (for transport-level allowlisting).
//  2. Extract the principal from JWT claims (for authorization).
//  3. Authorize against the PRINCIPAL, not the peer.
//
// Bug shape:
//
//	func (s *server) Foo(ctx context.Context, req *FooReq) (*FooResp, error) {
//	    p, ok := peer.FromContext(ctx)
//	    if !ok { return nil, status.Error(codes.Unauthenticated, "") }
//	    // Use p.AuthInfo.State.PeerCertificates[0].Subject as the
//	    // authorization subject — confused deputy.
//	    if !isAllowed(p) { return nil, status.Error(codes.PermissionDenied, "") }
//	    ...
//	}
//
// The narrow shape this rule catches: server methods that extract
// peer.FromContext AND authorize against the peer-derived identity
// WITHOUT also extracting the JWT principal. ruleguard cannot easily
// detect the negative ("doesn't also extract JWT"), so this rule
// flags the peer-cert extraction at the authorization-decision
// point — handlers that genuinely don't need authorization (raw
// health probes, certain interceptor-internal helpers) will be
// false positives that need exception_files entries.
//
// Today's sweep: starting-point detector. Real findings need
// review to confirm whether the handler is authorizing or just
// logging / transport-allowlisting.
package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// peerFromContextInAuthorizationDecision catches gRPC server
// methods that call peer.FromContext(ctx) AND make an
// authorization decision (return PermissionDenied / Unauthenticated)
// referencing the peer-derived value. The pattern is suspicious;
// not always a bug.
func peerFromContextInAuthorizationDecision(m dsl.Matcher) {
	m.Match(
		`$p, $_ := peer.FromContext($_); $*_; if $cond { return $_, status.Error(codes.PermissionDenied, $_) }`,
		`$p, $_ := peer.FromContext($_); $*_; if $cond { return nil, status.Error(codes.PermissionDenied, $_) }`,
	).
		Report(`server handler extracts peer.FromContext() and uses it in an authorization decision — confused-deputy risk if the peer is a gateway/sidecar proxying on behalf of a user. Verify authorization is against the originating PRINCIPAL (JWT claims, signed metadata) not the peer. See meta.principal_must_survive_proxy_chains`)
}
