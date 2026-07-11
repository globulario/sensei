// SPDX-License-Identifier: AGPL-3.0-only

package main

import "github.com/globulario/sensei/golang/netcfg"

// Endpoint defaults for the awg/sensei CLI. These wrap the shared netcfg
// package so every subcommand's --addr / --oxigraph-url / --oxigraph-bind flag
// default honors the same environment variables (AWG_ADDR, AWG_OXIGRAPH_URL,
// AWG_OXIGRAPH_BIND). An explicit flag still overrides the environment.
func defaultServiceAddr() string      { return netcfg.ServiceAddr() }
func defaultServiceListen() string    { return netcfg.ServiceListenAddr() }
func defaultOxigraphBind() string     { return netcfg.OxigraphBind() }
func defaultOxigraphQueryURL() string { return netcfg.OxigraphQueryURL() }
func defaultOxigraphStoreURL() string { return netcfg.OxigraphStoreURL() }
