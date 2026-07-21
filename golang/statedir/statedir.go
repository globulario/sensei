// SPDX-License-Identifier: AGPL-3.0-only

// Package statedir resolves the per-repository state directory that Sensei
// writes local, gitignored runtime state into: the project config, the managed
// governance store, the graph-authority marker, and the gate policy.
//
// The directory was named ".awg" while the project was called Awareness Graph.
// After the rename to Sensei, new repositories use ".sensei"; repositories that
// were initialized under the old name keep working because the resolver falls
// back to a pre-existing ".awg" directory. This lets a single release rename the
// command without stranding any repo that already has local state on disk.
package statedir

import (
	"os"
	"path/filepath"
)

const (
	// DefaultName is the state directory created for new repositories.
	DefaultName = ".sensei"
	// LegacyName is the pre-rename state directory, still honored on read so
	// repositories initialized before the Sensei rename keep working.
	LegacyName = ".awg"
)

// Name returns the active state-directory name for root. It prefers the modern
// ".sensei" directory, falls back to a pre-existing legacy ".awg" directory, and
// otherwise defaults to ".sensei" so fresh repositories are created under the new
// name. An existing ".awg" repo keeps writing into ".awg" (no split-brain state);
// migration to ".sensei" happens only when a ".sensei" directory is present.
func Name(root string) string {
	if root == "" {
		return DefaultName
	}
	if isDir(filepath.Join(root, DefaultName)) {
		return DefaultName
	}
	if isDir(filepath.Join(root, LegacyName)) {
		return LegacyName
	}
	return DefaultName
}

// Path joins the resolved state directory of root with the given sub-path
// elements. With an empty root the returned path is relative to the working
// directory (e.g. ".sensei/graph-authority.json").
func Path(root string, sub ...string) string {
	return filepath.Join(append([]string{root, Name(root)}, sub...)...)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
