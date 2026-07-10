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
