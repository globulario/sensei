#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-only
//
// Cross-platform test runner. `node --test out/*.test.js` relies on POSIX shell
// glob expansion, which PowerShell on Windows does NOT perform (node then gets a
// literal `out/*.test.js` and fails). This lists the compiled top-level test
// files in Node — identical semantics on Ubuntu and Windows — and runs them.

import { readdirSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import * as path from 'node:path';

const outDir = 'out';
const files = readdirSync(outDir)
  .filter((f) => f.endsWith('.test.js'))
  .sort()
  .map((f) => path.join(outDir, f));

if (files.length === 0) {
  console.error(`no compiled tests found in ${outDir}/ (did compile run?)`);
  process.exit(1);
}

const res = spawnSync(process.execPath, ['--test', ...files], { stdio: 'inherit' });
process.exit(res.status == null ? 1 : res.status);
