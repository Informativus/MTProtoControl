import assert from 'node:assert/strict';
import { test } from 'node:test';

import { resolveApiBaseUrl } from './config.js';

test('resolveApiBaseUrl uses fallback', () => {
  assert.equal(resolveApiBaseUrl({}), 'http://localhost:8080');
});

test('resolveApiBaseUrl trims trailing slash', () => {
  assert.equal(
    resolveApiBaseUrl({ VITE_API_BASE_URL: 'http://localhost:9000/' }),
    'http://localhost:9000',
  );
});

test('resolveApiBaseUrl keeps explicit empty base url for same-origin proxy', () => {
  assert.equal(resolveApiBaseUrl({ VITE_API_BASE_URL: '' }), '');
});
