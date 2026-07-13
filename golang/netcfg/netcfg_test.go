// SPDX-License-Identifier: Apache-2.0

package netcfg

import "testing"

func TestServiceAddrDefaultAndEnv(t *testing.T) {
	t.Setenv(EnvServiceAddr, "")
	t.Setenv(EnvLegacyServiceAddr, "")
	if got := ServiceAddr(); got != DefaultServiceHostPort {
		t.Fatalf("ServiceAddr default = %q, want %q", got, DefaultServiceHostPort)
	}
	if got := ServiceListenAddr(); got != DefaultServiceListen {
		t.Fatalf("ServiceListenAddr default = %q, want %q", got, DefaultServiceListen)
	}

	t.Setenv(EnvServiceAddr, "example:20120")
	if got := ServiceAddr(); got != "example:20120" {
		t.Fatalf("ServiceAddr env = %q, want example:20120", got)
	}
	if got := ServiceListenAddr(); got != "example:20120" {
		t.Fatalf("ServiceListenAddr env = %q, want example:20120", got)
	}
}

func TestServiceAddrLegacyFallback(t *testing.T) {
	t.Setenv(EnvServiceAddr, "")
	t.Setenv(EnvLegacyServiceAddr, "legacy:20120")
	if got := ServiceAddr(); got != "legacy:20120" {
		t.Fatalf("ServiceAddr legacy env = %q, want legacy:20120", got)
	}

	t.Setenv(EnvServiceAddr, "sensei:20120")
	if got := ServiceAddr(); got != "sensei:20120" {
		t.Fatalf("ServiceAddr should prefer %s over %s, got %q", EnvServiceAddr, EnvLegacyServiceAddr, got)
	}
}

func TestOxigraphBind(t *testing.T) {
	t.Setenv(EnvOxigraphBind, "")
	t.Setenv(EnvLegacyOxigraphBind, "")
	if got := OxigraphBind(); got != DefaultOxigraphBind {
		t.Fatalf("OxigraphBind default = %q, want %q", got, DefaultOxigraphBind)
	}
	t.Setenv(EnvOxigraphBind, "0.0.0.0:8878")
	if got := OxigraphBind(); got != "0.0.0.0:8878" {
		t.Fatalf("OxigraphBind env = %q, want 0.0.0.0:8878", got)
	}
}

func TestOxigraphURLs(t *testing.T) {
	cases := []struct {
		name      string
		env       string
		wantQuery string
		wantStore string
	}{
		{"default", "", "http://localhost:7878/query", "http://localhost:7878/store?default"},
		{"base only", "http://host:9999", "http://host:9999/query", "http://host:9999/store?default"},
		{"trailing slash", "http://host:9999/", "http://host:9999/query", "http://host:9999/store?default"},
		{"query suffix", "http://host:9999/query", "http://host:9999/query", "http://host:9999/store?default"},
		{"store suffix", "http://host:9999/store?default", "http://host:9999/query", "http://host:9999/store?default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvOxigraphURL, c.env)
			t.Setenv(EnvLegacyOxigraphURL, "")
			if got := OxigraphQueryURL(); got != c.wantQuery {
				t.Errorf("OxigraphQueryURL() = %q, want %q", got, c.wantQuery)
			}
			if got := OxigraphStoreURL(); got != c.wantStore {
				t.Errorf("OxigraphStoreURL() = %q, want %q", got, c.wantStore)
			}
		})
	}
}
