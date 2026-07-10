// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultServiceConfig_Valid(t *testing.T) {
	cfg := defaultServiceConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	if cfg.Name != defaultServiceName {
		t.Fatalf("Name=%q, want %q", cfg.Name, defaultServiceName)
	}
	if cfg.Port != defaultServicePort || cfg.Proxy != defaultServiceProxy {
		t.Fatalf("Port/Proxy=%d/%d, want %d/%d", cfg.Port, cfg.Proxy, defaultServicePort, defaultServiceProxy)
	}
}

func TestLoadServiceConfig_DefaultWhenPathEmpty(t *testing.T) {
	cfg, err := loadServiceConfig("")
	if err != nil {
		t.Fatalf("loadServiceConfig empty path: %v", err)
	}
	if cfg.Name == "" || cfg.OxigraphQueryURL == "" {
		t.Fatalf("unexpected empty required defaults: %+v", cfg)
	}
}

func TestLoadServiceConfig_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "awareness-config.json")
	content := `{"Name":"awareness.AwarenessGraphService","Protocol":"grpc","Port":12000,"Proxy":12001,"OxigraphQueryURL":"http://localhost:7878/query"}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := loadServiceConfig(p)
	if err != nil {
		t.Fatalf("loadServiceConfig file: %v", err)
	}
	if cfg.Port != 12000 || cfg.Proxy != 12001 {
		t.Fatalf("Port/Proxy=%d/%d, want 12000/12001", cfg.Port, cfg.Proxy)
	}
}
