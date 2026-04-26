import assert from 'node:assert/strict';
import { test } from 'node:test';

import { buildServerWorkspacePath, parseWorkspacePath } from './workspace-route.js';

test('buildServerWorkspacePath encodes server id for detail route', () => {
  assert.equal(buildServerWorkspacePath('server 42'), '/servers/server%2042');
  assert.equal(buildServerWorkspacePath(''), '/');
});

test('parseWorkspacePath returns board view for root path', () => {
  assert.deepEqual(parseWorkspacePath('/'), {
    view: 'board',
    serverId: '',
  });
});

test('parseWorkspacePath returns detail view for server route', () => {
  assert.deepEqual(parseWorkspacePath('/servers/server%2042/'), {
    view: 'detail',
    serverId: 'server 42',
  });
});
