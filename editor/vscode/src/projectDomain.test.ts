// SPDX-License-Identifier: AGPL-3.0-only

import assert from 'node:assert';
import { test } from 'node:test';
import { chooseAutomaticGraphDomain, domainFromRemoteUrl } from './projectDomain';

test('domainFromRemoteUrl maps git remotes to host/owner/repo', () => {
  const cases: Array<[string, string | undefined]> = [
    ['https://github.com/globulario/globular-admin.git', 'github.com/globulario/globular-admin'],
    ['https://github.com/globulario/globular-admin', 'github.com/globulario/globular-admin'],
    ['git@github.com:globulario/globular-admin.git', 'github.com/globulario/globular-admin'],
    ['ssh://git@github.com/globulario/sensei.git', 'github.com/globulario/sensei'],
    ['ssh://git@gitlab.example.com:2222/team/proj.git', 'gitlab.example.com/team/proj'],
    ['https://user@bitbucket.org/team/repo.git', 'bitbucket.org/team/repo'],
    ['  https://github.com/A/B.git\n', 'github.com/A/B'], // host lowercased, path case preserved, trimmed
    ['', undefined],
    ['not a url', undefined],
  ];
  for (const [input, want] of cases) {
    assert.equal(domainFromRemoteUrl(input), want, `for ${JSON.stringify(input)}`);
  }
});

test('chooseAutomaticGraphDomain keeps graph-wide after rejected workspace domain', () => {
  assert.equal(
    chooseAutomaticGraphDomain('github.com/globulario/sensei', ['globular'], true),
    ''
  );
});

test('chooseAutomaticGraphDomain uses requested domain only when graph advertises it', () => {
  assert.equal(
    chooseAutomaticGraphDomain(
      'github.com/globulario/sensei',
      ['github.com/caddyserver/caddy', 'github.com/globulario/sensei'],
      false
    ),
    'github.com/globulario/sensei'
  );
  assert.equal(
    chooseAutomaticGraphDomain('github.com/globulario/sensei', ['globular'], false),
    ''
  );
});

test('chooseAutomaticGraphDomain adopts sole domain only without a requested workspace domain', () => {
  assert.equal(chooseAutomaticGraphDomain('', ['globular'], false), 'globular');
  assert.equal(
    chooseAutomaticGraphDomain('', ['github.com/caddyserver/caddy', 'github.com/gin-gonic/gin'], false),
    ''
  );
});
