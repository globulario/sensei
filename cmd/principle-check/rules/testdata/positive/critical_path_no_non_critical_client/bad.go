// Positive-control fixture for critical_path_no_non_critical_client.
// repository_client.<method>(...) used in a (notionally critical-path) file.
package badfix

type repoClient struct{}

func (repoClient) ListArtifacts() ([]string, error) { return nil, nil }

var repository_client repoClient

func heartbeat() {
	_, _ = repository_client.ListArtifacts() // BAD: non-critical dep in critical path
}
