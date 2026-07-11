// SPDX-License-Identifier: AGPL-3.0-only

// Package netcfg is the single source of truth for the default network
// endpoints used across the Sensei stack: the awareness-graph gRPC service and
// the Oxigraph SPARQL backend. Change a port here and every binary — the CLI,
// the server, and the MCP bridge — picks it up.
//
// Every default is overridable at runtime through an environment variable so
// that "change the port once" holds without recompiling:
//
//	AWG_ADDR           gRPC service address        (default localhost:10120 / :10120 to listen)
//	AWG_OXIGRAPH_URL   Oxigraph HTTP endpoint base  (default http://localhost:7878)
//	AWG_OXIGRAPH_BIND  Oxigraph listen address      (default 127.0.0.1:7878)
//
// An explicit --addr / --oxigraph-url / --oxigraph-bind flag still wins over
// the environment, because these functions only supply the flag default.
package netcfg

import (
	"os"
	"strings"
)

const (
	// DefaultServicePort is the awareness-graph gRPC port.
	DefaultServicePort = 10120
	// DefaultProxyPort is the Globular-style gRPC-web proxy port.
	DefaultProxyPort = 10121

	// DefaultServiceHostPort is the client-facing gRPC dial target.
	DefaultServiceHostPort = "localhost:10120"
	// DefaultServiceListen is the server-side listen address (all interfaces).
	DefaultServiceListen = ":10120"

	// DefaultOxigraphBind is the Oxigraph listen address.
	DefaultOxigraphBind = "127.0.0.1:7878"
	// DefaultOxigraphBase is the Oxigraph HTTP base URL (no path).
	DefaultOxigraphBase = "http://localhost:7878"

	// EnvServiceAddr overrides the gRPC service address.
	EnvServiceAddr = "AWG_ADDR"
	// EnvOxigraphURL overrides the Oxigraph HTTP endpoint (base or full path).
	EnvOxigraphURL = "AWG_OXIGRAPH_URL"
	// EnvOxigraphBind overrides the Oxigraph listen address.
	EnvOxigraphBind = "AWG_OXIGRAPH_BIND"
)

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

// ServiceAddr is the gRPC address clients should dial: $AWG_ADDR or
// localhost:10120.
func ServiceAddr() string {
	if v := env(EnvServiceAddr); v != "" {
		return v
	}
	return DefaultServiceHostPort
}

// ServiceListenAddr is the address the server binds: $AWG_ADDR or :10120.
func ServiceListenAddr() string {
	if v := env(EnvServiceAddr); v != "" {
		return v
	}
	return DefaultServiceListen
}

// OxigraphBind is the Oxigraph listen address: $AWG_OXIGRAPH_BIND or
// 127.0.0.1:7878.
func OxigraphBind() string {
	if v := env(EnvOxigraphBind); v != "" {
		return v
	}
	return DefaultOxigraphBind
}

// OxigraphBase returns the scheme://host:port of the Oxigraph endpoint with any
// well-known path/query suffix stripped, honoring $AWG_OXIGRAPH_URL. Callers
// should use OxigraphQueryURL / OxigraphStoreURL rather than this directly.
func OxigraphBase() string {
	raw := env(EnvOxigraphURL)
	if raw == "" {
		return DefaultOxigraphBase
	}
	raw = strings.TrimRight(raw, "/")
	for _, suffix := range []string{"/store?default", "/store", "/query", "/update"} {
		if strings.HasSuffix(raw, suffix) {
			raw = strings.TrimSuffix(raw, suffix)
			break
		}
	}
	return strings.TrimRight(raw, "/")
}

// OxigraphQueryURL is the SPARQL query endpoint: <base>/query.
func OxigraphQueryURL() string { return OxigraphBase() + "/query" }

// OxigraphStoreURL is the SPARQL Graph Store Protocol endpoint:
// <base>/store?default.
func OxigraphStoreURL() string { return OxigraphBase() + "/store?default" }
