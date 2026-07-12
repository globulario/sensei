// SPDX-License-Identifier: AGPL-3.0-only

import * as fs from 'fs';
import * as path from 'path';

export interface RebuildPlanLike {
  senseiPath: string;
  args: string[];
  command: string;
}

export type RebuildMode = 'single' | 'combined' | 'blocked';

export interface RebuildPlan extends RebuildPlanLike {
  mode: RebuildMode;
  cwd?: string;
  servicesRepoPath?: string;
  servicesDetected: boolean;
  seedPath?: string;
  reason?: string;
}

export interface AwgCommandStep {
  label: string;
  args: string[];
  command: string;
}

/** The guarded single-candidate promote flow.
 * Promote must not trigger its own implicit rebuild because the correct rebuild
 * shape depends on the current workspace (single repo vs combined AG+services). */
export function candidatePromotePlan(
  id: string,
  plan: RebuildPlanLike
): AwgCommandStep[] {
  return [
    {
      label: 'promote',
      args: ['promote', id, '--no-rebuild'],
      command: `${plan.senseiPath} promote ${id} --no-rebuild`,
    },
    {
      label: 'rebuild',
      args: [...plan.args],
      command: plan.command,
    },
  ];
}

const SEED_REL = path.join('golang', 'server', 'embeddata', 'awareness.nt');

/** A services checkout is identified by namespaces.yaml — the same marker sensei's
 * own resolveServicesRepo() walks up to find. */
export function isServicesRepo(p: string): boolean {
  return fs.existsSync(path.join(p, 'docs', 'awareness', 'namespaces.yaml'));
}

/** Find the awareness-graph repo root by walking up from `start`, looking for
 * the embeddata seed dir. */
export function findAwarenessGraphRoot(start: string): string | undefined {
  let dir = start;
  for (;;) {
    if (fs.existsSync(path.join(dir, 'golang', 'server', 'embeddata'))) {
      return dir;
    }
    const parent = path.dirname(dir);
    if (parent === dir) {
      return undefined;
    }
    dir = parent;
  }
}

/** Work out the correct rebuild command for the current workspace/repo shape. */
export function resolveRebuildPlan(
  senseiPath: string,
  cwd: string | undefined,
  servicesRepoSetting: string
): RebuildPlan {
  const single: RebuildPlan = {
    mode: 'single',
    senseiPath,
    cwd,
    args: ['rebuild'],
    servicesDetected: false,
    command: `${senseiPath} rebuild`,
  };
  if (!cwd) {
    return single;
  }
  const agRoot = findAwarenessGraphRoot(cwd);
  if (!agRoot) {
    return single;
  }

  const seedPath = fs.existsSync(path.join(agRoot, SEED_REL))
    ? path.join(agRoot, SEED_REL)
    : undefined;

  const configured = servicesRepoSetting.trim();
  let svc: string | undefined;
  let detected = false;
  if (configured) {
    const abs = path.isAbsolute(configured) ? configured : path.resolve(agRoot, configured);
    if (!isServicesRepo(abs)) {
      return {
        ...single,
        seedPath,
        mode: 'blocked',
        reason:
          `sensei.servicesRepoPath is set to "${configured}" but that is not a ` +
          `services repo (no docs/awareness/namespaces.yaml). Fix the path to rebuild the combined graph.`,
      };
    }
    svc = abs;
  } else {
    const sibling = path.resolve(agRoot, '..', 'services');
    if (isServicesRepo(sibling)) {
      svc = sibling;
      detected = true;
    }
  }

  if (!svc) {
    return {
      ...single,
      seedPath,
      mode: 'blocked',
      reason:
        'Combined graph rebuild requires a services repo path. Configure ' +
        'sensei.servicesRepoPath (or place the services repo at ../services).',
    };
  }

  // --tag-by-repo: a combined build spans multiple repos, so tag each repo's
  // nodes with its own domain (from its git remote) — otherwise Rebuild would
  // collapse the whole graph into one home domain and lose per-repo filtering.
  return {
    mode: 'combined',
    senseiPath,
    cwd,
    seedPath,
    servicesRepoPath: svc,
    servicesDetected: detected,
    args: ['rebuild', '--services-repo', svc, '--tag-by-repo'],
    command: `${senseiPath} rebuild --services-repo ${svc} --tag-by-repo`,
  };
}
