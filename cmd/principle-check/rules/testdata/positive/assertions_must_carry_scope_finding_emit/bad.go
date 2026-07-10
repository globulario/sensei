// Positive-control fixture for assertions_must_carry_scope_finding_emit.
// emitClusterEvent called with ONLY an event name (no payload/scope).
package badfix

type server struct{}

func (s *server) emitClusterEvent(name string) {}

func (s *server) run() {
	s.emitClusterEvent("node_unreachable") // BAD: no scope payload
}
