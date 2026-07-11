// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/netcfg"
)

const (
	defaultServiceName  = "globular.awareness_graph.AwarenessGraph"
	defaultServicePort  = netcfg.DefaultServicePort
	defaultServiceProxy = netcfg.DefaultProxyPort
)

// serviceConfig is the install/runtime metadata used for Globular-style service wiring.
type serviceConfig struct {
	ID           string   `json:"Id"`
	Name         string   `json:"Name"`
	Description  string   `json:"Description"`
	Keywords     []string `json:"Keywords"`
	Protocol     string   `json:"Protocol"`
	Port         int      `json:"Port"`
	Proxy        int      `json:"Proxy"`
	PublisherID  string   `json:"PublisherId"`
	KeepAlive    bool     `json:"KeepAlive"`
	KeepUpToDate bool     `json:"KeepUpToDate"`
	Dependencies []string `json:"Dependencies"`
	Permissions  []string `json:"Permissions"`
	TLS          bool     `json:"TLS"`

	OxigraphQueryURL     string `json:"OxigraphQueryURL"`
	RequireStore         bool   `json:"RequireStore"`
	StartupHealthTimeout string `json:"StartupHealthTimeout"`
}

func defaultServiceConfig() serviceConfig {
	return serviceConfig{
		ID:          "awareness-graph",
		Name:        defaultServiceName,
		Description: "Awareness graph service for Resolve, Impact, Briefing, and constrained Query over compiled RDF awareness context.",
		Keywords: []string{
			"awareness", "rdf", "graph", "briefing", "impact", "intent", "agent",
		},
		Protocol:             "grpc",
		Port:                 defaultServicePort,
		Proxy:                defaultServiceProxy,
		PublisherID:          "localhost",
		KeepAlive:            true,
		KeepUpToDate:         true,
		Dependencies:         []string{"oxigraph-backend"},
		Permissions:          []string{},
		TLS:                  true,
		OxigraphQueryURL:     netcfg.OxigraphQueryURL(),
		RequireStore:         false,
		StartupHealthTimeout: startupHealthTimeout.String(),
	}
}

func (c serviceConfig) validate() error {
	if c.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if c.Protocol == "" {
		return fmt.Errorf("protocol is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port out of range: %d", c.Port)
	}
	if c.Proxy <= 0 || c.Proxy > 65535 {
		return fmt.Errorf("proxy out of range: %d", c.Proxy)
	}
	if c.OxigraphQueryURL == "" {
		return fmt.Errorf("oxigraph query url is required")
	}
	return nil
}

func loadServiceConfig(path string) (serviceConfig, error) {
	cfg := defaultServiceConfig()
	if path == "" {
		return cfg, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.validate()
}
