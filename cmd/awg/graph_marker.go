// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/globulario/sensei/golang/seedmeta"

func defaultRuntimeMarkerFile() (string, error) {
	root, err := resolveProjectRoot("")
	if err != nil {
		return "", err
	}
	return seedmeta.RuntimeMarkerPath(root), nil
}

func runtimeMarkerFileForRoot(root string) (string, error) {
	if root != "" {
		return seedmeta.RuntimeMarkerPath(root), nil
	}
	return defaultRuntimeMarkerFile()
}
