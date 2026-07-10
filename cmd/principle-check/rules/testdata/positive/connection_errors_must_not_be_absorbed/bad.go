// Positive-control fixture for connection_errors_must_not_be_absorbed.
// net.Dial / net.DialTimeout / tls.Dial / gocql.CreateSession err absorbed
// (returned nil for the err position).
package badfix

import (
	"crypto/tls"
	"net"
)

func dialAbsorbed(addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, nil // BAD: dial err absorbed
	}
	return conn, nil
}

func dialTimeoutAbsorbed(addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 0)
	if err != nil {
		return nil, nil // BAD: dial err absorbed
	}
	return conn, nil
}

func tlsDialAbsorbed(addr string) (net.Conn, error) {
	conn, err := tls.Dial("tcp", addr, nil)
	if err != nil {
		return nil, nil // BAD: tls handshake err absorbed
	}
	return conn, nil
}

// gocql-like stub: method name CreateSession() matches the no-type-filter rule.
type clusterConfig struct{}

func (c *clusterConfig) CreateSession() (*session, error) { return nil, nil }

type session struct{}

func scyllaSessionAbsorbed(c *clusterConfig) (*session, error) {
	sess, err := c.CreateSession()
	if err != nil {
		return nil, nil // BAD: scylla session err absorbed
	}
	return sess, nil
}
