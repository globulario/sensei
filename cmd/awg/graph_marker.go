// SPDX-License-Identifier: AGPL-3.0-only

package main

import "github.com/globulario/sensei/golang/seedmeta"

func defaultRuntimeMarkerFile() (string, error) {
	root, err := resolveProjectRoot("")
	if err != nil {
		return "", err
	}
	return seedmeta.RuntimeMarkerPath(root), nil
}
