#!/usr/bin/env node
// Vendors the canonical gRPC contract into the extension so the published
// .vsix is self-contained (@grpc/proto-loader reads the .proto at runtime).
//
//   node scripts/sync-proto.js          copy proto/ -> editor/vscode/proto/
//   node scripts/sync-proto.js --check  fail if the vendored copy is stale
//
// The --check mode mirrors the repo's "generated artifacts must be fresh" CI
// gate: a proto change that isn't re-vendored fails the build instead of
// shipping an extension that speaks an outdated contract.
'use strict';

const fs = require('fs');
const path = require('path');

const CANONICAL = path.join(__dirname, '..', '..', '..', 'proto', 'awareness_graph.proto');
const VENDORED = path.join(__dirname, '..', 'proto', 'awareness_graph.proto');

const check = process.argv.includes('--check');

if (!fs.existsSync(CANONICAL)) {
  console.error(`sync-proto: canonical proto not found at ${CANONICAL}`);
  process.exit(1);
}

const canonical = fs.readFileSync(CANONICAL);

if (check) {
  const vendored = fs.existsSync(VENDORED) ? fs.readFileSync(VENDORED) : Buffer.alloc(0);
  if (!canonical.equals(vendored)) {
    console.error(
      'sync-proto: editor/vscode/proto/awareness_graph.proto is out of date.\n' +
        '            Run `npm run sync-proto` (in editor/vscode) and commit the result.'
    );
    process.exit(1);
  }
  console.log('sync-proto: vendored proto is up to date.');
  process.exit(0);
}

fs.mkdirSync(path.dirname(VENDORED), { recursive: true });
fs.writeFileSync(VENDORED, canonical);
console.log('sync-proto: vendored proto refreshed from canonical contract.');
