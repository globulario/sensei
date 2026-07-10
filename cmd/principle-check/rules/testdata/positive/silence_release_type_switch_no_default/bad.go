// Positive-control fixture for silence_release_type_switch_no_default.
// 3-arm type switch over release types with NO default case.
package badfix

import "github.com/globulario/sensei/cmd/principle-check/rules/testdata/positive/silence_release_type_switch_no_default/cluster_controllerpb"

func resolve(obj any) string {
	switch v := obj.(type) {
	case *cluster_controllerpb.ServiceRelease:
		return v.Name
	case *cluster_controllerpb.InfrastructureRelease:
		return v.Name
	case *cluster_controllerpb.ApplicationRelease:
		return v.Name
	}
	// BAD: no default — a future ComputeRelease silently yields ""
	return ""
}
