// Positive-control fixture for minio_commodity_no_hard_dependency.
// A primary service registering MinIO as a stop-mode health dependency:
//
//	dephealth.Dep("minio", ...)
//
// The rule MUST flag this. Real code has zero such sites (rbac's was removed),
// so a clean production scan is attested clean, not silently uncovered.
package badfix

type dep struct{}

type dephealthPkg struct{}

func (dephealthPkg) Dep(name string, ping func() error) dep { return dep{} }

var dephealth dephealthPkg

func registerDeps() {
	var deps []dep
	// BAD: MinIO gated as a hard health dependency — commodity treated as a pillar.
	deps = append(deps, dephealth.Dep("minio", func() error { return nil }))
	_ = deps
}
