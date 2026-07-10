// Positive-control fixture for physical_clock_cross_node_comparison.
// time.Since(time.Unix(...)) on a remote node's heartbeat timestamp.
package badfix

import "time"

func isNodeStale(remoteHeartbeatUnix int64) bool {
	elapsed := time.Since(time.Unix(remoteHeartbeatUnix, 0)) // BAD: cross-node clock comparison
	return elapsed > 30*time.Second
}
