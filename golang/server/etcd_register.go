// SPDX-License-Identifier: Apache-2.0

package main

// etcd_register.go — self-registration of the awareness-graph service with
// the Globular etcd service registry.
//
// At startup, serve() calls newEtcdRegistrar() to connect to the cluster
// etcd, then register() to write the service config + per-node instance
// keys. On shutdown, deregister() removes the instance key so the xDS
// watcher stops routing traffic to this endpoint.
//
// Registration is best-effort: if etcd is unreachable the server still
// starts and serves — it just won't be discovered via service discovery.
// The MCP tools will return DEGRADED in that case, which is the correct
// operator signal.
//
// etcd key contract (must match config.GetServicesConfigurationsByName):
//   /globular/services/{id}/config              — shared service metadata
//   /globular/services/{id}/instances/{macKey}  — per-node address + state
//
// The xDS watcher merges config + instance on each snapshot rebuild, so
// only the instance key needs to be updated on a per-node basis.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"gopkg.in/yaml.v3"
)

const (
	etcdEndpointsFile = "/var/lib/globular/config/etcd_endpoints"
	etcdYAMLFile      = "/var/lib/globular/config/etcd.yaml"
	clusterCAPath     = "/var/lib/globular/pki/ca.crt"
	clusterCertPath   = "/var/lib/globular/pki/issued/services/service.crt"
	clusterKeyPath    = "/var/lib/globular/pki/issued/services/service.key"

	globularServicesPrefix = "/globular/services/"
	etcdConfigLeaf         = "config"
	etcdInstancesLeaf      = "instances"
)

var errServiceConfigNotFound = errors.New("service config not found in etcd")

// etcdRegistrar holds the live etcd client and the keys this instance
// wrote, so deregister() knows exactly what to clean up.
type etcdRegistrar struct {
	client      *clientv3.Client
	instanceKey string
}

// newEtcdRegistrar opens a TLS-authenticated connection to the cluster etcd.
// It reads endpoints from /var/lib/globular/config/etcd_endpoints (written
// by the controller reconciliation) and falls back to parsing etcd.yaml.
func newEtcdRegistrar() (*etcdRegistrar, error) {
	endpoints := loadEtcdEndpoints()
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no etcd endpoints found (checked %s and %s)", etcdEndpointsFile, etcdYAMLFile)
	}

	tlsCfg, err := buildEtcdTLS()
	if err != nil {
		return nil, fmt.Errorf("etcd TLS config: %w", err)
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
		TLS:         tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("connect etcd %v: %w", endpoints, err)
	}

	return &etcdRegistrar{client: cli}, nil
}

// register writes the service config key and the per-node instance key to
// etcd. The config key carries shared service metadata; the instance key
// carries this node's address so the xDS watcher can build an Envoy cluster.
//
// The xDS watcher auto-generates a gRPC route for prefix
// "/awareness.AwarenessGraphService/" when it sees this registration.
func (r *etcdRegistrar) register(cfg serviceConfig, ip, mac string) error {
	// Shared service config — written once (idempotent, last-writer-wins).
	configPayload := map[string]interface{}{
		"Id":           cfg.ID,
		"Name":         cfg.Name,
		"Description":  cfg.Description,
		"Protocol":     cfg.Protocol,
		"Port":         cfg.Port,
		"Proxy":        cfg.Proxy,
		"Address":      fmt.Sprintf("%s:%d", ip, cfg.Port),
		"TLS":          cfg.TLS,
		"Keywords":     cfg.Keywords,
		"KeepAlive":    cfg.KeepAlive,
		"KeepUpToDate": cfg.KeepUpToDate,
		"Domain":       "globular.internal",
		"Mac":          mac,
		"PublisherId":  cfg.PublisherID,
		"Plaform":      "linux_amd64",
		"Version":      Version,
	}
	configBytes, err := json.MarshalIndent(configPayload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal service config: %w", err)
	}

	configKey := globularServicesPrefix + cfg.ID + "/" + etcdConfigLeaf
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if _, err := r.client.Put(ctx, configKey, string(configBytes)); err != nil {
		return fmt.Errorf("put service config: %w", err)
	}

	// Per-node instance — provides Address/Port for this node's endpoint.
	// Instance fields override config in the xDS merger (watcher.go:2238).
	instancePayload := map[string]interface{}{
		"Address":   fmt.Sprintf("%s:%d", ip, cfg.Port),
		"Port":      cfg.Port,
		"State":     "running",
		"Mac":       mac,
		"UpdatedAt": time.Now().Unix(),
	}
	instanceBytes, err := json.Marshal(instancePayload)
	if err != nil {
		return fmt.Errorf("marshal instance: %w", err)
	}

	macKey := strings.ReplaceAll(mac, ":", "_")
	instanceKey := globularServicesPrefix + cfg.ID + "/" + etcdInstancesLeaf + "/" + macKey
	ctx2, cancel2 := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel2()
	if _, err := r.client.Put(ctx2, instanceKey, string(instanceBytes)); err != nil {
		return fmt.Errorf("put instance: %w", err)
	}

	r.instanceKey = instanceKey
	return nil
}

// deregister removes the per-node instance key so the xDS watcher stops
// routing to this endpoint. The shared config key is left in place — it is
// service metadata, not node state, and is safe to leave for the next startup.
func (r *etcdRegistrar) deregister() {
	if r.client == nil || r.instanceKey == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_, _ = r.client.Delete(ctx, r.instanceKey)
	_ = r.client.Close()
}

// resolveAuthoritativeServicePort returns the service Port from the etcd
// authority key /globular/services/{serviceID}/config.
//
// The caller should treat errors as non-fatal fallback signals (for example:
// etcd temporarily unavailable during local dev), not as startup blockers.
func resolveAuthoritativeServicePort(serviceID string) (int, error) {
	r, err := newEtcdRegistrar()
	if err != nil {
		return 0, err
	}
	defer func() { _ = r.client.Close() }()

	key := globularServicesPrefix + serviceID + "/" + etcdConfigLeaf
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	resp, err := r.client.Get(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("get %s: %w", key, err)
	}
	if resp.Count == 0 || len(resp.Kvs) == 0 {
		return 0, errServiceConfigNotFound
	}
	port, err := parsePortFromServiceConfigJSON(resp.Kvs[0].Value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return port, nil
}

func parsePortFromServiceConfigJSON(data []byte) (int, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, err
	}
	v, ok := raw["Port"]
	if !ok {
		return 0, fmt.Errorf("missing Port")
	}
	switch t := v.(type) {
	case float64:
		port := int(t)
		if float64(port) != t {
			return 0, fmt.Errorf("port not integer: %v", t)
		}
		if port <= 0 || port > 65535 {
			return 0, fmt.Errorf("port out of range: %d", port)
		}
		return port, nil
	case string:
		port, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0, fmt.Errorf("invalid Port string %q: %w", t, err)
		}
		if port <= 0 || port > 65535 {
			return 0, fmt.Errorf("port out of range: %d", port)
		}
		return port, nil
	default:
		return 0, fmt.Errorf("unsupported Port type %T", v)
	}
}

// loadEtcdEndpoints reads the etcd endpoint list from disk.
//
// Priority:
//  1. /var/lib/globular/config/etcd_endpoints — written by the controller
//     reconciliation; one URL per line, e.g. "https://10.0.0.63:2379".
//  2. advertise-client-urls from /var/lib/globular/config/etcd.yaml.
//  3. Synthetic "https://{routeableIP}:2379" as last resort.
func loadEtcdEndpoints() []string {
	// 1. Endpoints file.
	if data, err := os.ReadFile(etcdEndpointsFile); err == nil {
		var eps []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				eps = append(eps, line)
			}
		}
		if len(eps) > 0 {
			return eps
		}
	}

	// 2. Parse etcd.yaml for advertise-client-urls.
	if data, err := os.ReadFile(etcdYAMLFile); err == nil {
		var cfg struct {
			AdvertiseClientURLs string `yaml:"advertise-client-urls"`
		}
		if yaml.Unmarshal(data, &cfg) == nil && cfg.AdvertiseClientURLs != "" {
			for _, u := range strings.Split(cfg.AdvertiseClientURLs, ",") {
				u = strings.TrimSpace(u)
				if u != "" {
					return []string{u}
				}
			}
		}
	}

	// 3. Last resort.
	if ip := getRoutableIPv4(); ip != "" {
		return []string{fmt.Sprintf("https://%s:2379", ip)}
	}
	return nil
}

// buildEtcdTLS returns a *tls.Config for etcd. etcd uses the cluster CA for
// server verification; client-cert-auth is false so the client cert is
// optional but presented when available.
func buildEtcdTLS() (*tls.Config, error) {
	caData, err := os.ReadFile(clusterCAPath)
	if err != nil {
		return nil, fmt.Errorf("read cluster CA %s: %w", clusterCAPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caData) {
		return nil, fmt.Errorf("parse cluster CA %s", clusterCAPath)
	}
	cfg := &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	// Client cert is optional (etcd has client-cert-auth: false).
	cert, err := tls.LoadX509KeyPair(clusterCertPath, clusterKeyPath)
	if err == nil {
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// getRoutableIPAndMAC returns the IPv4 address and hardware MAC of the
// first non-loopback, non-link-local network interface that is UP.
// Both are empty string if detection fails.
func getRoutableIPAndMAC() (ip, mac string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var candidate net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				candidate = v.IP
			case *net.IPAddr:
				candidate = v.IP
			}
			if candidate == nil || candidate.IsLoopback() || candidate.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := candidate.To4(); ip4 != nil {
				return ip4.String(), iface.HardwareAddr.String()
			}
		}
	}
	return "", ""
}

// getRoutableIPv4 returns just the routable IPv4 address (used when MAC is not needed).
func getRoutableIPv4() string {
	ip, _ := getRoutableIPAndMAC()
	return ip
}

// stateRootDir is the canonical Globular state directory.
const stateRootDir = "/var/lib/globular"

// pki returns a path under the canonical PKI directory.
func pki(rel string) string {
	return filepath.Join(stateRootDir, "pki", rel)
}
