// Package transport holds the wire limits both ends of the awareness-graph
// gRPC connection must agree on.
//
// It exists so the ceiling is stated ONCE. A server that accepts more than the
// client will send, or a client that will send more than the server accepts,
// fails as ResourceExhausted at the far end -- an error that reads like a
// backend problem and not like a limit nobody updated on both sides.
package transport

// MaxMessageBytes is the largest single gRPC message either end will send or
// receive.
//
// WHY THIS IS NOT 4 MiB. gRPC's default receive limit is 4 MiB. EditCheck sends
// a changed file's whole content in one unary request, so the default silently
// made "this file is large" indistinguishable from "this file could not be
// verified". A 5.6 MB run log in a repository's own evidence directory made an
// enforcing gate fail closed with CANNOT VERIFY -- the correct refusal under
// the capability it had, and the wrong capability.
//
// WHY THIS IS TECHNICAL DEBT, RECORDED AS SUCH. Raising a fixed ceiling does
// not remove the boundary, it moves it. Evidence larger than this will fail the
// same way, and the honest fix is size-aware chunked transport that carries a
// whole-object digest and length, so a verifier proves every chunk belongs to
// that exact object and a missing chunk stays CANNOT VERIFY. Until that exists,
// this constant is a stopgap and MUST NOT be read as a statement that 16 MiB is
// the largest evidence the system is willing to verify.
//
// What must never replace it is a pathname exemption. Skipping
// experiments/**/runs/*.log would turn "too large to verify" into "we decided
// this evidence does not need verifying", which is a far worse claim than the
// refusal it silences.
const MaxMessageBytes = 16 << 20 // 16 MiB
