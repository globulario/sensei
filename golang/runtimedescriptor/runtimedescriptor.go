// SPDX-License-Identifier: AGPL-3.0-only

// Package runtimedescriptor lets a `sensei serve` invocation prove that an
// already-listening Oxigraph or awareness-graph process is compatible with
// the current invocation's execution habitat, before silently reusing it
// (see docs/design/serve-runtime-compatibility.md, issue #118).
//
// A descriptor is keyed by LISTEN ADDRESS + process Kind, not by checkout —
// a new invocation has no other way to discover what is already running on
// a given address. It is written atomically (mirroring
// golang/seedmeta.WriteMarkerFile's temp-file-then-rename pattern) to a
// machine-global location, since the resource being claimed (a TCP port) is
// itself machine-global.
//
// Reads always re-verify PID-liveness and treat a dead-PID or corrupt
// descriptor as absent (ErrAbsent), self-healing by deleting the stale file
// — this is the only approach that survives a killed/crashed process or a
// host reboot without a heavier IPC/registry mechanism.
package runtimedescriptor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kind identifies which kind of process a Descriptor describes.
type Kind string

const (
	KindOxigraph       Kind = "oxigraph"
	KindAwarenessGraph Kind = "awareness-graph"
)

// Descriptor is the compatibility fingerprint one `sensei serve` invocation
// writes for a process it starts, and a later invocation reads to decide
// whether an occupied listen address is safe to reuse.
//
// Only the fields relevant to Kind are populated by convention; field
// comparisons are performed by cmd/awg/serve_compatibility.go, not by this
// package.
type Descriptor struct {
	Kind             Kind   `json:"kind"`
	PID              int    `json:"pid"`
	ListenAddr       string `json:"listen_addr"`
	OxigraphQueryURL string `json:"oxigraph_query_url,omitempty"`
	GraphMarkerFile  string `json:"graph_marker_file,omitempty"`
	RepoRoot         string `json:"repo_root,omitempty"`
	RepoDomain       string `json:"repo_domain,omitempty"`
	// HomeDomain and AwarenessDir are BEHAVIORAL settings, not just identity:
	// a different home-domain attributes untagged nodes differently, and a
	// non-empty AwarenessDir means the Propose RPC write path is enabled.
	// Both must be part of the exact-match fingerprint — a service reused
	// under a silently different behavioral configuration is exactly the
	// "approximate similarity" contract §3.4 forbids.
	HomeDomain    string `json:"home_domain,omitempty"`
	AwarenessDir  string `json:"awareness_dir,omitempty"`
	DataDir       string `json:"data_dir,omitempty"`
	StartedAtUnix int64  `json:"started_at_unix"`
	SenseiVersion string `json:"sensei_version,omitempty"`
}

// ErrAbsent reports that no live, readable descriptor exists for the given
// kind/address — either none was ever written, the file could not be
// parsed, or the PID it names is no longer running. Callers must treat this
// identically to "no descriptor": an occupied listener with no provable
// descriptor is NOT safe to reuse (unidentifiable occupancy is incompatible
// occupancy).
var ErrAbsent = errors.New("runtime descriptor absent")

// Write atomically persists d to the path derived from d.Kind and
// d.ListenAddr. d.PID and d.ListenAddr must be set.
func Write(d Descriptor) error {
	if d.PID <= 0 {
		return fmt.Errorf("runtime descriptor: PID must be set")
	}
	if strings.TrimSpace(d.ListenAddr) == "" {
		return fmt.Errorf("runtime descriptor: ListenAddr must be set")
	}
	path, err := pathFor(d.Kind, d.ListenAddr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir runtime descriptor dir: %w", err)
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal runtime descriptor: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write runtime descriptor temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename runtime descriptor file: %w", err)
	}
	return nil
}

// Read loads the descriptor for kind/listenAddr. It returns ErrAbsent if no
// file exists, the file cannot be parsed, or the PID it names is no longer
// alive — in the last two cases it also best-effort deletes the stale file.
func Read(kind Kind, listenAddr string) (Descriptor, error) {
	path, err := pathFor(kind, listenAddr)
	if err != nil {
		return Descriptor{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Descriptor{}, ErrAbsent
		}
		return Descriptor{}, fmt.Errorf("read runtime descriptor: %w", err)
	}
	var d Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		_ = os.Remove(path)
		return Descriptor{}, ErrAbsent
	}
	if !isProcessAlive(d.PID) {
		_ = os.Remove(path)
		return Descriptor{}, ErrAbsent
	}
	return d, nil
}

// Remove deletes the descriptor file for kind/listenAddr, if any. A missing
// file is not an error.
func Remove(kind Kind, listenAddr string) error {
	path, err := pathFor(kind, listenAddr)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// pathFor derives the descriptor file path from kind and listenAddr alone —
// deliberately NOT from any checkout root, so a new invocation can find
// what's already running on an address without first knowing which checkout
// started it.
func pathFor(kind Kind, listenAddr string) (string, error) {
	if strings.TrimSpace(listenAddr) == "" {
		return "", fmt.Errorf("runtime descriptor: listen address must not be empty")
	}
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, sanitizeAddr(string(kind)+"_"+listenAddr)+".json"), nil
}

// baseDir is the machine-global directory runtime descriptors live under —
// the resource being claimed (a TCP port) is itself machine-global, so a
// descriptor cannot live inside any one checkout's state directory.
func baseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "sensei", "runtime"), nil
}

// sanitizeAddr replaces filesystem-hostile characters in an address string
// so it can be used as a single path component (":" is invalid in a
// Windows filename outside the drive-letter position).
func sanitizeAddr(s string) string {
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_")
	return r.Replace(s)
}
