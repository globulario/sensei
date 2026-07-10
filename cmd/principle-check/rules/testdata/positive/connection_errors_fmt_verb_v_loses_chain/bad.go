// Positive-control fixture for connection_errors_fmt_verb_v_loses_chain.
// fmt.Errorf("...: %v", err) — wraps with %v instead of %w, losing the chain.
package badfix

import "fmt"

func wrap(err error) error {
	return fmt.Errorf("cannot resolve ScyllaDB hosts from etcd: %v", err) // BAD: %v
}
