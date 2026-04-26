import assert from 'node:assert/strict';
import { test } from 'node:test';

import { resolveReleaseVersion } from './release.js';

test('resolveReleaseVersion uses dev fallback when env is empty', () => {
  assert.equal(resolveReleaseVersion({}), 'v0.0.0-dev');
});

test('resolveReleaseVersion prefixes semver values without v', () => {
  assert.equal(resolveReleaseVersion({ VITE_RELEASE_VERSION: '0.1.0-beta.2' }), 'v0.1.0-beta.2');
});

test('resolveReleaseVersion keeps git tag values as is', () => {
  assert.equal(resolveReleaseVersion({ VITE_RELEASE_VERSION: 'v0.1.0-beta.2' }), 'v0.1.0-beta.2');
});
