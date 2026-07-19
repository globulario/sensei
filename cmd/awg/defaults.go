// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/globulario/sensei/golang/netcfg"

// Endpoint defaults for the Sensei CLI. These wrap the shared netcfg
// package so every subcommand's --addr / --oxigraph-url / --oxigraph-bind flag
// default honors the same environment variables (SENSEI_ADDR,
// SENSEI_OXIGRAPH_URL, SENSEI_OXIGRAPH_BIND), with legacy AWG_* fallbacks. An
// explicit flag still overrides the environment.
func defaultServiceAddr() string      { return netcfg.ServiceAddr() }
func defaultServiceListen() string    { return netcfg.ServiceListenAddr() }
func defaultOxigraphBind() string     { return netcfg.OxigraphBind() }
func defaultOxigraphQueryURL() string { return netcfg.OxigraphQueryURL() }
func defaultOxigraphStoreURL() string { return netcfg.OxigraphStoreURL() }
