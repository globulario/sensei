// SPDX-License-Identifier: Apache-2.0

package importgraph

import (
	"path/filepath"
	"strings"
	"testing"
)

// TS/JS parser fixtures — all fictional (acme.test monorepo), no real-project paths.

// jsoncTSConfig is a deliberately JSONC tsconfig (comments + trailing comma) with
// a path alias, to prove both alias resolution and JSONC tolerance.
const jsoncTSConfig = `{
  // acme monorepo config
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@billing/*": ["packages/billing/src/*"], /* the billing package */
    },
  },
}`

// TestScan_TS_AliasAndExternals is the core case: a path-alias import that
// crosses components → one depends_on; relative same-component ignored; external
// listed; node builtins ignored; require + dynamic import extracted and deduped;
// non-literal dynamic import skipped; index.ts entrypoint → service.
func TestScan_TS_AliasAndExternals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), jsoncTSConfig)
	writeFile(t, filepath.Join(root, "packages", "billing", "src", "api.ts"), "export const api = 1;\n")
	writeFile(t, filepath.Join(root, "packages", "app", "src", "helper.ts"), "export const h = 1;\n")
	writeFile(t, filepath.Join(root, "packages", "app", "src", "index.ts"), `
import { api } from '@billing/api';
import './helper';
import * as _ from 'lodash';
import * as fs from 'fs';
import pathx from 'node:path';
const u = require('@billing/api');
const lazy = () => import('@billing/api');
const bad = (name: string) => import(`+"`./${name}`"+`);
export { api, u, lazy, bad, _, fs, pathx };
`)

	doc, err := Scan(root, "typescript", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.packages.app")
	if app == nil {
		t.Fatalf("missing component.packages.app; got %d components", len(doc.Components))
	}
	if app.Kind != "service" {
		t.Errorf("app kind = %q, want service (has index.ts)", app.Kind)
	}
	if got := app.DependsOn; len(got) != 1 || got[0] != "component.packages.billing" {
		t.Errorf("app depends_on = %v, want [component.packages.billing] (alias resolved + deduped across import/require/dynamic)", got)
	}
	if !contains(app.ExternalImports, "lodash") {
		t.Errorf("external_imports = %v, want it to include lodash", app.ExternalImports)
	}
	if contains(app.ExternalImports, "fs") || contains(app.ExternalImports, "node:path") {
		t.Errorf("node builtins leaked into external_imports: %v", app.ExternalImports)
	}
	if !contains(app.SourceFiles, "packages/app/src/index.ts") || !contains(app.SourceFiles, "packages/app/src/helper.ts") {
		t.Errorf("app source_files missing index.ts/helper.ts: %v", app.SourceFiles)
	}
	if findComp(doc, "component.packages.billing") == nil {
		t.Error("missing component.packages.billing (api.ts should be scanned)")
	}
}

// TestScan_TS_UnresolvedAliasSafe — an alias that matches a paths key but is not
// on disk resolves to unresolved: no crash, no edge, not an external import.
func TestScan_TS_UnresolvedAliasSafe(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), jsoncTSConfig)
	writeFile(t, filepath.Join(root, "packages", "app", "src", "a.ts"),
		"import { x } from '@billing/missing';\nexport { x };\n")

	doc, err := Scan(root, "typescript", Config{})
	if err != nil {
		t.Fatalf("Scan must not fail on unresolved imports: %v", err)
	}
	app := findComp(doc, "component.packages.app")
	if app == nil {
		t.Fatal("missing component.packages.app")
	}
	if len(app.DependsOn) != 0 {
		t.Errorf("unresolved alias produced an edge: %v", app.DependsOn)
	}
	if contains(app.ExternalImports, "@billing/missing") {
		t.Errorf("unresolved alias leaked into external_imports: %v", app.ExternalImports)
	}
}

// TestScan_TS_ClassifierUpgrade — a typescript classifier rule upgrades a bare
// specifier into a semantic edge; a go rule is ignored during a TS scan.
func TestScan_TS_ClassifierUpgrade(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "app", "src", "a.ts"),
		"import { c } from '@svc/billing';\nexport { c };\n")

	cfg := Config{Classifiers: []Rule{
		{ID: "go_noise", Language: "go", Match: `.*billing.*`, Edge: "reads_from", Target: "component.go_billing"},
		{ID: "ts_svc", Language: "typescript", Match: `^@svc/([a-z]+)$`, Edge: "reads_from", Target: "component.$1"},
	}}
	doc, err := Scan(root, "typescript", cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.packages.app")
	if app == nil {
		t.Fatal("missing component.packages.app")
	}
	if !contains(app.ReadsFrom, "component.billing") {
		t.Errorf("reads_from = %v, want component.billing (ts classifier upgrade)", app.ReadsFrom)
	}
	if contains(app.ReadsFrom, "component.go_billing") {
		t.Errorf("go classifier leaked into a typescript scan: %v", app.ReadsFrom)
	}
	if contains(app.ExternalImports, "@svc/billing") {
		t.Errorf("classified import should not also be external: %v", app.ExternalImports)
	}
}

// TestScan_TS_TestAndDeclExcluded — *.test.ts and *.d.ts contribute nothing.
func TestScan_TS_TestAndDeclExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "app", "src", "real.ts"), "export const r = 1;\n")
	writeFile(t, filepath.Join(root, "packages", "app", "src", "real.test.ts"),
		"import 'jest';\nimport '@somewhere/secret';\n")
	writeFile(t, filepath.Join(root, "packages", "app", "src", "types.d.ts"), "export declare const t: number;\n")

	doc, err := Scan(root, "typescript", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.packages.app")
	if app == nil {
		t.Fatal("missing component.packages.app")
	}
	for _, f := range app.SourceFiles {
		if filepath.Ext(f) == ".ts" && (f == "packages/app/src/real.test.ts" || f == "packages/app/src/types.d.ts") {
			t.Errorf("excluded file leaked into source_files: %s", f)
		}
	}
	if contains(app.ExternalImports, "jest") || contains(app.ExternalImports, "@somewhere/secret") {
		t.Errorf("test-file imports leaked: %v", app.ExternalImports)
	}
}

// TestScan_TS_Determinism — repeated Scan+Render is byte-identical; header names TS.
func TestScan_TS_Determinism(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "tsconfig.json"), jsoncTSConfig)
	writeFile(t, filepath.Join(root, "packages", "billing", "src", "api.ts"), "export const a = 1;\n")
	writeFile(t, filepath.Join(root, "packages", "app", "src", "index.ts"), "import '@billing/api';\n")

	render := func() []byte {
		doc, err := Scan(root, "typescript", Config{})
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		out, err := Render(doc, "typescript")
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		return out
	}
	first := render()
	if string(first) != string(render()) {
		t.Error("Scan+Render is not deterministic")
	}
	if !strings.Contains(string(first), "-lang typescript") {
		t.Error("render header should name the typescript language")
	}
}

// writeWorkspaceFixture lays out a fictional pnpm/npm monorepo: one shared
// package @acme/ui and an app that imports it by workspace name + a subpath +
// a non-workspace bare package. configFiles supplies the workspace config.
func writeWorkspaceFixture(t *testing.T, root string, configFiles map[string]string) {
	t.Helper()
	for p, content := range configFiles {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), content)
	}
	writeFile(t, filepath.Join(root, "packages", "ui", "package.json"), `{"name":"@acme/ui"}`)
	writeFile(t, filepath.Join(root, "packages", "ui", "src", "index.ts"), "export const x = 1;\n")
	writeFile(t, filepath.Join(root, "apps", "web", "package.json"), `{"name":"@acme/web"}`)
	writeFile(t, filepath.Join(root, "apps", "web", "src", "main.ts"), `
import { x } from '@acme/ui';
import '@acme/ui/button';
import * as React from 'react';
`)
}

func assertWorkspaceResolved(t *testing.T, root string) {
	t.Helper()
	doc, err := Scan(root, "typescript", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.apps.web")
	if app == nil {
		t.Fatalf("missing component.apps.web; got %d components", len(doc.Components))
	}
	if got := app.DependsOn; len(got) != 1 || got[0] != "component.packages.ui" {
		t.Errorf("apps/web depends_on = %v, want [component.packages.ui] (workspace pkg + subpath resolved)", got)
	}
	if !contains(app.ExternalImports, "react") {
		t.Errorf("react should remain external; external_imports = %v", app.ExternalImports)
	}
	for _, leak := range []string{"@acme/ui", "@acme/ui/button"} {
		if contains(app.ExternalImports, leak) {
			t.Errorf("workspace import %q leaked into external_imports: %v", leak, app.ExternalImports)
		}
	}
}

// TestScan_TS_WorkspacePnpm — pnpm-workspace.yaml resolves @acme/ui internally.
func TestScan_TS_WorkspacePnpm(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFixture(t, root, map[string]string{
		"pnpm-workspace.yaml": "packages:\n  - 'apps/*'\n  - 'packages/*'\n",
	})
	assertWorkspaceResolved(t, root)
}

// TestScan_TS_WorkspaceNpmArray — package.json "workspaces": [...] array form.
func TestScan_TS_WorkspaceNpmArray(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFixture(t, root, map[string]string{
		"package.json": `{"name":"root","workspaces":["apps/*","packages/*"]}`,
	})
	assertWorkspaceResolved(t, root)
}

// TestScan_TS_WorkspaceNpmObject — package.json "workspaces": {packages:[...]} form.
func TestScan_TS_WorkspaceNpmObject(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFixture(t, root, map[string]string{
		"package.json": `{"name":"root","workspaces":{"packages":["apps/*","packages/*"]}}`,
	})
	assertWorkspaceResolved(t, root)
}

// TestScan_TS_NoWorkspaceConfig — without a workspace config, a bare scoped
// import stays external (regression: the standalone editor case is unaffected).
func TestScan_TS_NoWorkspaceConfig(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "ui", "package.json"), `{"name":"@acme/ui"}`)
	writeFile(t, filepath.Join(root, "packages", "ui", "src", "index.ts"), "export const x = 1;\n")
	writeFile(t, filepath.Join(root, "apps", "web", "src", "main.ts"), "import { x } from '@acme/ui';\n")

	doc, err := Scan(root, "typescript", Config{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	app := findComp(doc, "component.apps.web")
	if app == nil {
		t.Fatal("missing component.apps.web")
	}
	if len(app.DependsOn) != 0 {
		t.Errorf("no workspace config → no internal edge; got depends_on = %v", app.DependsOn)
	}
	if !contains(app.ExternalImports, "@acme/ui") {
		t.Errorf("without workspace config @acme/ui should be external; got %v", app.ExternalImports)
	}
}
