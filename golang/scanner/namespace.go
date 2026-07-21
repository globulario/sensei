// SPDX-License-Identifier: AGPL-3.0-only

package scanner

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Namespace is one entry in namespaces.yaml.
type Namespace struct {
	ID          string   `yaml:"id"`
	Label       string   `yaml:"label"`
	Owns        []string `yaml:"owns"`
	Description string   `yaml:"description"`
}

// Registry holds the validated namespace table loaded from namespaces.yaml.
type Registry struct {
	byID map[string]*Namespace
	all  []*Namespace
}

// LoadRegistry parses namespaces.yaml and returns a Registry.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read namespace registry %s: %w", path, err)
	}
	var raw struct {
		Namespaces []Namespace `yaml:"namespaces"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse namespace registry %s: %w", path, err)
	}
	r := &Registry{byID: make(map[string]*Namespace)}
	for i := range raw.Namespaces {
		ns := &raw.Namespaces[i]
		if ns.ID == "" {
			return nil, fmt.Errorf("namespace entry at index %d missing id", i)
		}
		if _, dup := r.byID[ns.ID]; dup {
			return nil, fmt.Errorf("duplicate namespace id %q", ns.ID)
		}
		r.byID[ns.ID] = ns
		r.all = append(r.all, ns)
	}
	return r, nil
}

// Has reports whether ns is a known namespace ID.
func (r *Registry) Has(ns string) bool {
	_, ok := r.byID[ns]
	return ok
}

// All returns all registered namespaces.
func (r *Registry) All() []*Namespace {
	return r.all
}

// NamespaceForPath returns the namespace ID whose owns list contains the
// given repo-relative path (or a prefix of it). Returns "" if not found.
func (r *Registry) NamespaceForPath(repoRelPath string) string {
	repoRelPath = strings.TrimPrefix(repoRelPath, "./")
	for _, ns := range r.all {
		for _, own := range ns.Owns {
			own = strings.TrimSuffix(own, "/")
			if repoRelPath == own || strings.HasPrefix(repoRelPath, own+"/") {
				return ns.ID
			}
		}
	}
	return ""
}
