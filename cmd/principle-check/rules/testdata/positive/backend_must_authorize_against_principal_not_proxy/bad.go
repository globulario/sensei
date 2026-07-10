// Positive-control fixture for backend_must_authorize_against_principal_not_proxy.
// Server handler extracts peer.FromContext and authorizes against it
// (confused deputy) — returns status.Error(codes.PermissionDenied, ...).
package badfix

import "context"

// --- local stubs to resolve the matched identifiers ---

type peerInfo struct{ Addr string }

type peerPkg struct{}

func (peerPkg) FromContext(ctx context.Context) (*peerInfo, bool) {
	return &peerInfo{}, true
}

var peer peerPkg

type codesPkg struct{ PermissionDenied int }

var codes = codesPkg{PermissionDenied: 7}

type statusPkg struct{}

func (statusPkg) Error(code int, msg string) error { return nil }

var status statusPkg

func isAllowed(p *peerInfo) bool { return false }

// --- the bug shape ---

func handle(ctx context.Context) (any, error) {
	p, ok := peer.FromContext(ctx)
	_ = ok
	if !isAllowed(p) {
		return nil, status.Error(codes.PermissionDenied, "denied") // BAD: authorize against peer
	}
	return nil, nil
}
