// Stub proto-like release types so the fixture's type switch over
// *cluster_controllerpb.{Service,Infrastructure,Application}Release resolves.
package cluster_controllerpb

type ServiceRelease struct{ Name string }

type InfrastructureRelease struct{ Name string }

type ApplicationRelease struct{ Name string }
