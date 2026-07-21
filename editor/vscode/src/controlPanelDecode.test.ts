// SPDX-License-Identifier: AGPL-3.0-only
//
// Proof 31 (an absent source never synthesizes zero): pins how the wire decodes
// the control-snapshot `optional` counts under the SAME options grpcClient uses
// (keepCase / longs:String / enums:String / defaults:true / oneofs:true). The
// renderer's unknown-versus-zero guarantee depends on this exact behavior, so it
// is locked here with a real protobuf round-trip over the vendored proto.

import test from 'node:test';
import assert from 'node:assert/strict';
import * as path from 'path';

// protobufjs is the engine @grpc/proto-loader runs on; use it directly with the
// same field-naming + conversion options so the assertions reflect runtime decode.
// eslint-disable-next-line @typescript-eslint/no-var-requires
const protobuf = require('protobufjs');

const CONV = { keepCase: true, longs: String, enums: String, defaults: true, oneofs: true };
const PROTO = path.join(__dirname, '..', 'proto', 'awareness_graph.proto');

function loadSnapshotType(): any {
  const root = new protobuf.Root();
  // keepCase at PARSE time — matches proto-loader, keeping snake_case names.
  root.loadSync(PROTO, { keepCase: true });
  return root.lookupType('globular.awareness_graph.ArchitectureControlSnapshot');
}

function roundtrip(Snap: any, obj: Record<string, unknown>): any {
  // fromObject accepts enum values by NAME (string) and coerces to the message;
  // encode then validates. (verify() is skipped: it expects numeric enums.)
  const buf = Snap.encode(Snap.fromObject(obj)).finish();
  return Snap.toObject(Snap.decode(buf), CONV);
}

test('proto3 optional int64 counts are modeled with explicit presence', () => {
  const Snap = loadSnapshotType();
  const field = Snap.fields.open_question_count;
  assert.ok(field, 'open_question_count field exists');
  assert.equal(field.optional, true, 'the count is a proto3 optional (presence-tracked)');
  assert.ok(field.partOf && field.partOf.name === '_open_question_count', 'synthetic-oneof marker present');
});

test('an absent optional count decodes to undefined, never 0', () => {
  const Snap = loadSnapshotType();
  // open_question_count SET to 0; missing_evidence_count SET to 5;
  // contradiction_count and the rest ABSENT.
  const out = roundtrip(Snap, { open_question_count: 0, missing_evidence_count: 5 });

  assert.equal(out.open_question_count, '0', 'an observed zero survives as "0"');
  assert.ok('_open_question_count' in out, 'presence marker set for an observed value');
  assert.equal(out.missing_evidence_count, '5');

  assert.equal(out.contradiction_count, undefined, 'an unobserved count is undefined, NOT 0');
  assert.equal('_contradiction_count' in out, false, 'no presence marker for an absent count');
  assert.equal(out.missing_test_count, undefined);
  assert.equal(out.lifecycle_unknown_count, undefined);
});

test('an absent sub-message decodes to null (source unavailable), not an empty object', () => {
  const Snap = loadSnapshotType();
  const out = roundtrip(Snap, { registry_digest: 'r1' });
  assert.equal(out.coverage, null, 'coverage absent → null');
  assert.equal(out.active_task, null);
  assert.equal(out.completion, null);
  assert.equal(out.feedback_context, null);
});

test('closed enums decode to their full proto-name strings', () => {
  const Snap = loadSnapshotType();
  const out = roundtrip(Snap, {
    meta: { availability: 'ARCHITECTURE_AVAILABILITY_PARTIAL' },
  });
  assert.equal(out.meta.availability, 'ARCHITECTURE_AVAILABILITY_PARTIAL');
});
