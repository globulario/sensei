// SPDX-License-Identifier: Apache-2.0

import test from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import {
  candidatePromotePlan,
  resolveRebuildPlan,
  type RebuildPlanLike,
} from './localOpsPlan';

test('candidatePromotePlan stages candidate without implicit rebuild', () => {
  const rebuild: RebuildPlanLike = {
    senseiPath: 'sensei',
    args: ['rebuild'],
    command: 'sensei rebuild',
  };

  const steps = candidatePromotePlan('candidate.one', rebuild);

  assert.equal(steps.length, 2);
  assert.deepEqual(steps[0].args, ['promote', 'candidate.one', '--no-rebuild']);
  assert.equal(steps[0].command, 'sensei promote candidate.one --no-rebuild');
  assert.deepEqual(steps[1].args, ['rebuild']);
  assert.equal(steps[1].command, 'sensei rebuild');
});

test('candidatePromotePlan preserves project-aware combined rebuild command', () => {
  const rebuild: RebuildPlanLike = {
    senseiPath: '/usr/local/bin/sensei',
    args: ['rebuild', '--services-repo', '/work/services'],
    command: '/usr/local/bin/sensei rebuild --services-repo /work/services',
  };

  const steps = candidatePromotePlan('candidate.two', rebuild);

  assert.deepEqual(steps[0].args, ['promote', 'candidate.two', '--no-rebuild']);
  assert.deepEqual(steps[1].args, ['rebuild', '--services-repo', '/work/services']);
  assert.equal(
    steps[1].command,
    '/usr/local/bin/sensei rebuild --services-repo /work/services'
  );
});

function tempDir(t: test.TestContext): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sensei-vscode-plan-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function mkdirp(p: string): void {
  fs.mkdirSync(p, { recursive: true });
}

test('resolveRebuildPlan stays single-repo outside awareness-graph checkout', (t) => {
  const root = tempDir(t);
  mkdirp(path.join(root, 'docs', 'awareness'));

  const plan = resolveRebuildPlan('sensei', root, '');

  assert.equal(plan.mode, 'single');
  assert.deepEqual(plan.args, ['rebuild']);
  assert.equal(plan.command, 'sensei rebuild');
});

test('resolveRebuildPlan auto-detects sibling services for awareness-graph repo', (t) => {
  const parent = tempDir(t);
  const ag = path.join(parent, 'awareness-graph');
  const services = path.join(parent, 'services');
  mkdirp(path.join(ag, 'golang', 'server', 'embeddata'));
  mkdirp(path.join(services, 'docs', 'awareness'));
  fs.writeFileSync(path.join(services, 'docs', 'awareness', 'namespaces.yaml'), 'namespaces: []\n');
  fs.writeFileSync(path.join(ag, 'golang', 'server', 'embeddata', 'awareness.nt'), '<s> <p> <o> .\n');

  const plan = resolveRebuildPlan('sensei', path.join(ag, 'editor'), '');

  assert.equal(plan.mode, 'combined');
  assert.equal(plan.servicesDetected, true);
  assert.equal(plan.servicesRepoPath, services);
  assert.deepEqual(plan.args, ['rebuild', '--combined', '--services-repo', services, '--tag-by-repo']);
  assert.equal(plan.command, `sensei rebuild --combined --services-repo ${services} --tag-by-repo`);
  assert.equal(plan.seedPath, path.join(ag, 'golang', 'server', 'embeddata', 'awareness.nt'));
});

test('resolveRebuildPlan blocks awareness-graph rebuild when services repo is missing', (t) => {
  const parent = tempDir(t);
  const ag = path.join(parent, 'awareness-graph');
  mkdirp(path.join(ag, 'golang', 'server', 'embeddata'));
  fs.writeFileSync(path.join(ag, 'golang', 'server', 'embeddata', 'awareness.nt'), '<s> <p> <o> .\n');

  const plan = resolveRebuildPlan('sensei', ag, '');

  assert.equal(plan.mode, 'blocked');
  assert.match(plan.reason || '', /Combined graph rebuild requires a services repo path/);
});

test('resolveRebuildPlan blocks invalid configured services repo path', (t) => {
  const parent = tempDir(t);
  const ag = path.join(parent, 'awareness-graph');
  mkdirp(path.join(ag, 'golang', 'server', 'embeddata'));
  fs.writeFileSync(path.join(ag, 'golang', 'server', 'embeddata', 'awareness.nt'), '<s> <p> <o> .\n');
  mkdirp(path.join(parent, 'not-services'));

  const plan = resolveRebuildPlan('sensei', ag, '../not-services');

  assert.equal(plan.mode, 'blocked');
  assert.match(plan.reason || '', /is set to "\.\.\/not-services" but that is not a services repo/);
});
