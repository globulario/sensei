// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func findConsistencyCandidate(cands []consistencyCandidate, kind, idContains string) *consistencyCandidate {
	for i := range cands {
		if cands[i].Kind == kind && strings.Contains(cands[i].ID, idContains) {
			return &cands[i]
		}
	}
	return nil
}

func TestConsistencyCheckIndexShape_DivergentShapeFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tree.go"), `package tree

type node struct {
	children []*node
}

func (n *node) getValue() *node {
	return n.children[len(n.children)-1]
}

func (n *node) findCaseInsensitivePathRec() *node {
	return n.children[0]
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	c := findConsistencyCandidate(got, "divergent_index_shape", "n.children")
	if c == nil {
		t.Fatalf("expected a divergent_index_shape candidate for n.children, got %+v", got)
	}
	if c.Status != "candidate" || c.Assertion != "inferred" {
		t.Errorf("wrong status/assertion: %+v", c)
	}
	if len(c.Evidence) != 2 {
		t.Errorf("expected 2 evidence lines (one per conflicting site), got %v", c.Evidence)
	}
	foundFirst, foundLast := false, false
	for _, e := range c.Evidence {
		if strings.Contains(e, "[0]") && strings.Contains(e, "findCaseInsensitivePathRec") {
			foundFirst = true
		}
		if strings.Contains(e, "[len-1]") && strings.Contains(e, "getValue") {
			foundLast = true
		}
	}
	if !foundFirst || !foundLast {
		t.Errorf("evidence missing expected shape/func attribution: %v", c.Evidence)
	}
}

func TestConsistencyCheckIndexShape_SingleShapeNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "list.go"), `package list

type node struct {
	children []*node
}

func (n *node) first() *node {
	return n.children[0]
}

func (n *node) firstAgain() *node {
	return n.children[0]
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "divergent_index_shape", "n.children"); c != nil {
		t.Errorf("expected no candidate when every accessor agrees on shape, got %+v", c)
	}
}

func TestConsistencyCheckAsymmetricCall_RunFamilyFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Engine struct{}

func (e *Engine) updateRouteTrees() {}

func (e *Engine) Run() error {
	e.updateRouteTrees()
	return nil
}

func (e *Engine) RunTLS() error {
	return nil
}

func (e *Engine) RunUnix() error {
	return nil
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees")
	if c == nil {
		t.Fatalf("expected an asymmetric_setup_call candidate for updateRouteTrees, got %+v", got)
	}
	if !strings.Contains(c.Description, "Run") || !strings.Contains(c.Description, "RunTLS") {
		t.Errorf("description should name both the caller and a non-caller: %q", c.Description)
	}
}

func TestConsistencyCheckAsymmetricCall_SymmetricNotFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Engine struct{}

func (e *Engine) updateRouteTrees() {}

func (e *Engine) Run() error {
	e.updateRouteTrees()
	return nil
}

func (e *Engine) RunTLS() error {
	e.updateRouteTrees()
	return nil
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees"); c != nil {
		t.Errorf("expected no candidate when every peer calls the helper, got %+v", c)
	}
}

func TestConsistencyCheckAsymmetricCall_SignatureFamilyBridgesUnnamedPeers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "group.go"), `package group

type RouterGroup struct{}

func (g *RouterGroup) prepare(path string) string { return path }

func (g *RouterGroup) POST(path string) string {
	return g.prepare(path)
}

func (g *RouterGroup) GET(path string) string {
	return path
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	// POST and GET share no name prefix at all; they can only be judged peers
	// via identical (string) -> (string) signature clustering.
	c := findConsistencyCandidate(got, "asymmetric_setup_call", "prepare")
	if c == nil {
		t.Fatalf("expected signature-based family clustering to flag POST/GET asymmetry over prepare, got %+v", got)
	}
}

func TestConsistencyCheckAsymmetricCall_DispatchIdiomClosesGap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Server struct{ Handler *Engine }

func (s *Server) ListenAndServe() error { return nil }

type Engine struct{}

func (e *Engine) updateRouteTrees() {}

func (e *Engine) Handler() *Engine { return e }

func (e *Engine) ServeHTTP() {
	e.updateRouteTrees()
}

func (e *Engine) Run() error {
	server := &Server{Handler: e.Handler()}
	return server.ListenAndServe()
}

func (e *Engine) RunTLS() error {
	server := &Server{Handler: e.Handler()}
	return server.ListenAndServe()
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	// Neither Run nor RunTLS calls updateRouteTrees directly, but both hand
	// Handler() to a Serve-shaped call, and ServeHTTP (reached via that
	// synthetic edge) calls it — so this must NOT be flagged.
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees"); c != nil {
		t.Errorf("expected the dispatch-idiom bridge to close the gap (both peers reach ServeHTTP), got %+v", c)
	}
}

func TestConsistencyCheckAsymmetricCall_DispatchIdiomStillCatchesRealAsymmetry(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Server struct{ Handler *Engine }

func (s *Server) ListenAndServe() error { return nil }

type Engine struct{}

func (e *Engine) updateRouteTrees() {}

func (e *Engine) Handler() *Engine { return e }

func (e *Engine) ServeHTTP() {}

func (e *Engine) Run() error {
	e.updateRouteTrees()
	server := &Server{Handler: e.Handler()}
	return server.ListenAndServe()
}

func (e *Engine) RunTLS() error {
	server := &Server{Handler: e.Handler()}
	return server.ListenAndServe()
}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	// ServeHTTP itself does NOT call updateRouteTrees here (bug still
	// present), so reaching ServeHTTP via the dispatch idiom must not
	// manufacture a false symmetry: RunTLS still doesn't reach it.
	c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees")
	if c == nil {
		t.Fatalf("expected updateRouteTrees asymmetry to still be caught when ServeHTTP itself doesn't call it, got %+v", got)
	}
}

func TestConsistencyCheckBuildConstraints_IgnoreMutuallyExclusiveFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "active.go"), `package sample

type node struct { children []*node }
func (n *node) active() *node { return n.children[0] }
`)
	writeFile(t, filepath.Join(root, "ignored.go"), `//go:build ignore

package sample

type ignoredNode struct { children []*ignoredNode }
func (n *ignoredNode) ignored() *ignoredNode { return n.children[len(n.children)-1] }
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "divergent_index_shape", "children"); c != nil {
		t.Fatalf("build-incompatible file contributed evidence: %+v", c)
	}
}

func TestConsistencyCheckDispatchIdiom_HandlerConstructionWithoutServeDoesNotBridge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "engine.go"), `package engine

type Server struct{ Handler *Engine }
type Engine struct{}
func (e *Engine) updateRouteTrees() {}
func (e *Engine) ServeHTTP() { e.updateRouteTrees() }
func (e *Engine) Run() error { e.updateRouteTrees(); return nil }
func (e *Engine) RunTLS() error { _ = &Server{Handler: e}; return nil }
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "updateroutetrees"); c == nil {
		t.Fatalf("handler construction without a serve call suppressed the real asymmetry: %+v", got)
	}
}

func TestConsistencyCheckAsymmetricCall_NestedPrefixFamiliesAreUnified(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "store.go"), `package store

type Store struct{}
func (s *Store) prepare() {}
func (s *Store) GetUser() { s.prepare() }
func (s *Store) GetUserByID() {}
func (s *Store) GetUserByName() {}
`)
	got, err := extractConsistencyCandidates(root)
	if err != nil {
		t.Fatalf("extractConsistencyCandidates: %v", err)
	}
	if c := findConsistencyCandidate(got, "asymmetric_setup_call", "prepare"); c == nil {
		t.Fatalf("nested Get.User and Get.User.By prefix families were not unified: %+v", got)
	}
}
