// Positive-control fixture for code_mirror_external_enumeration.
// A map[string]bool literal mirroring an external set of infrastructure names.
package badfix

func classifyPackage(name string) string {
	infraNames := map[string]bool{ // BAD: hand-authored mirror of packages/specs/*.yaml
		"etcd": true, "minio": true, "envoy": true,
		"scylladb": true, "keepalived": true,
	}
	if infraNames[name] {
		return "infrastructure"
	}
	return "service"
}
