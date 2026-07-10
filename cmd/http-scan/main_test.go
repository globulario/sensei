// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScan_ExtractsRoutes covers: mux.Handle + HandleFunc detection, the
// handler expression, subtree (/x vs /x/) staying DISTINCT, read/write
// inference, and test-file exclusion.
func TestScan_ExtractsRoutes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "config", "config.go"), `package config
func Mount(mux M, d Deps) {
	mux.Handle("/api/save-config", d.SaveConfig)
	mux.Handle("/config", d.GetConfig)
	mux.Handle("/config/", d.GetServiceConfig)
	mux.HandleFunc("/serve", serve)
}
`)
	// a test file must NOT contribute routes
	write(t, filepath.Join(root, "config", "config_test.go"), `package config
func TestX(t T) { mux.Handle("/leaked", d.Leaked) }
`)

	routes, err := scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := map[string]route{}
	for _, r := range routes {
		got[r.path] = r
	}
	for _, p := range []string{"/api/save-config", "/config", "/config/", "/serve"} {
		if _, ok := got[p]; !ok {
			t.Errorf("missing route %q", p)
		}
	}
	if _, leaked := got["/leaked"]; leaked {
		t.Error("route from _test.go leaked into the scan")
	}
	if got["/api/save-config"].handler != "d.SaveConfig" {
		t.Errorf("handler = %q, want d.SaveConfig", got["/api/save-config"].handler)
	}

	// /config and /config/ are DIFFERENT handlers — their ids must not collide.
	out, err := render(routes)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(out)
	for _, id := range []string{
		"id: contract.http.api_save_config",
		"id: contract.http.config",
		"id: contract.http.config_subtree",
		"id: contract.http.serve",
	} {
		if !strings.Contains(s, id) {
			t.Errorf("rendered output missing %q", id)
		}
	}
	// save-config is a write; config (get) is a read.
	if readOrWrite("/api/save-config") != "write" {
		t.Error("save-config should classify as write")
	}
	if readOrWrite("/api/get-config") != "read" {
		t.Error("get-config should classify as read")
	}
}

func TestSlug_SubtreeDistinct(t *testing.T) {
	if a, b := slug("/config"), slug("/config/"); a == b {
		t.Errorf("slug collision: /config and /config/ both -> %q", a)
	}
	if got := slug("/api/save-config"); got != "api_save_config" {
		t.Errorf("slug(/api/save-config) = %q, want api_save_config", got)
	}
}
