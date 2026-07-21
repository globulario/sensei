#!/usr/bin/env node
// SPDX-License-Identifier: AGPL-3.0-only
//
// Deterministic packaged-artifact validation for the Phase 9.5 Checkpoint-6
// closure. Given a built .vsix it asserts the SHIPPED contents — not the source
// tree — describe the same AGPL-licensed extension the manifest claims:
//
//   * extension/package.json  license === "AGPL-3.0-only", a valid version, and
//     no residual "Apache" string in the extension's OWN manifest;
//   * extension/LICENSE.txt    is the AGPL text, never Apache;
//   * the control-panel runtime assets + vendored proto are present.
//
// The "no Apache" checks are scoped to the extension's own files. The genuine
// third-party licenses under node_modules/** are legitimate and never inspected.
//
// Pure Node (a minimal zip central-directory reader + zlib) so it runs
// identically on Ubuntu and Windows with no external unzip tool.

import { readFileSync } from 'node:fs';
import { inflateRawSync } from 'node:zlib';

const vsixPath = process.argv[2];
if (!vsixPath) {
  console.error('usage: node verify-vsix.mjs <path-to.vsix> [expectedVersion]');
  process.exit(2);
}
const expectedVersion = process.argv[3] || '';

// ---- minimal zip reader ----------------------------------------------------
const buf = readFileSync(vsixPath);

function findEOCD(b) {
  // End Of Central Directory signature 0x06054b50, scanning from the end.
  for (let i = b.length - 22; i >= 0; i--) {
    if (b.readUInt32LE(i) === 0x06054b50) return i;
  }
  throw new Error('not a zip: no EOCD record');
}

function centralDirectory(b) {
  const eocd = findEOCD(b);
  const count = b.readUInt16LE(eocd + 10);
  let off = b.readUInt32LE(eocd + 16);
  const entries = new Map();
  for (let n = 0; n < count; n++) {
    if (b.readUInt32LE(off) !== 0x02014b50) throw new Error('bad central directory header');
    const method = b.readUInt16LE(off + 10);
    const compSize = b.readUInt32LE(off + 20);
    const nameLen = b.readUInt16LE(off + 28);
    const extraLen = b.readUInt16LE(off + 30);
    const commentLen = b.readUInt16LE(off + 32);
    const localOff = b.readUInt32LE(off + 42);
    const name = b.toString('utf8', off + 46, off + 46 + nameLen);
    entries.set(name, { method, compSize, localOff });
    off += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

function readEntry(b, e) {
  // Recompute the data offset from the LOCAL header (its name/extra lengths can
  // differ from the central directory's).
  if (b.readUInt32LE(e.localOff) !== 0x04034b50) throw new Error('bad local header');
  const nameLen = b.readUInt16LE(e.localOff + 26);
  const extraLen = b.readUInt16LE(e.localOff + 28);
  const dataStart = e.localOff + 30 + nameLen + extraLen;
  const raw = b.subarray(dataStart, dataStart + e.compSize);
  if (e.method === 0) return raw; // stored
  if (e.method === 8) return inflateRawSync(raw); // deflate
  throw new Error(`unsupported compression method ${e.method}`);
}

const dir = centralDirectory(buf);
const failures = [];
const fail = (msg) => failures.push(msg);
const text = (name) => {
  const e = dir.get(name);
  if (!e) return null;
  return readEntry(buf, e).toString('utf8');
};

// ---- assertions ------------------------------------------------------------

// 1. Manifest: AGPL, valid version, no residual Apache in the extension's own manifest.
const pkgText = text('extension/package.json');
if (!pkgText) {
  fail('extension/package.json missing from the vsix');
} else {
  let pkg;
  try { pkg = JSON.parse(pkgText); } catch (err) { fail(`extension/package.json is not valid JSON: ${err.message}`); }
  if (pkg) {
    if (pkg.license !== 'AGPL-3.0-only') fail(`manifest license is "${pkg.license}", expected "AGPL-3.0-only"`);
    if (!/^\d+\.\d+\.\d+/.test(String(pkg.version || ''))) fail(`manifest version "${pkg.version}" is not a semver`);
    if (expectedVersion && pkg.version !== expectedVersion) {
      fail(`manifest version "${pkg.version}" != expected "${expectedVersion}"`);
    }
    if (/apache/i.test(pkgText)) fail('extension manifest contains a residual "Apache" reference');
    console.log(`  manifest: ${pkg.name}@${pkg.version} license=${pkg.license}`);
  }
}

// 2. The extension's OWN license file is AGPL, never Apache.
const lic = text('extension/LICENSE.txt') || text('extension/LICENSE') || text('extension/LICENSE.md');
if (!lic) {
  fail('extension LICENSE is missing from the vsix');
} else {
  if (!/GNU AFFERO GENERAL PUBLIC LICENSE/.test(lic)) fail('extension LICENSE is not the AGPL text');
  if (/Apache License/.test(lic)) fail('extension LICENSE still contains the Apache License text');
}

// 3. Runtime assets + vendored proto are shipped.
const required = [
  'extension/out/extension.js',
  'extension/media/controlPanel.js',
  'extension/media/controlPanelMutation.js',
  'extension/media/controlPanelFmt.js',
  'extension/media/dashboard.css',
  'extension/proto/awareness_graph.proto',
];
for (const name of required) {
  if (!dir.has(name)) fail(`required shipped file missing: ${name}`);
}

// ---- report ----------------------------------------------------------------
if (failures.length) {
  console.error(`\nVSIX validation FAILED (${failures.length}):`);
  for (const f of failures) console.error(`  ✗ ${f}`);
  process.exit(1);
}
console.log(`VSIX validation OK — ${dir.size} entries, AGPL, control-panel + proto shipped.`);
