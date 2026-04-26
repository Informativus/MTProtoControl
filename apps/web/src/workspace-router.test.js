import assert from 'node:assert/strict';
import { afterEach, test } from 'node:test';

import { navigateWorkspacePath, readWorkspacePath, readWorkspaceRoute } from './workspace-router.js';

const originalWindow = globalThis.window;

afterEach(() => {
  if (typeof originalWindow === 'undefined') {
    delete globalThis.window;
    return;
  }

  globalThis.window = originalWindow;
});

test('readWorkspaceRoute reflects current browser path', () => {
  globalThis.window = createMockWindow('/servers/server%2042');

  assert.equal(readWorkspacePath(), '/servers/server%2042');
  assert.deepEqual(readWorkspaceRoute(), {
    view: 'detail',
    serverId: 'server 42',
  });
});

test('navigateWorkspacePath updates browser history and emits route change event', () => {
  const emittedEvents = [];
  globalThis.window = createMockWindow('/', {
    dispatchEvent(event) {
      emittedEvents.push(event.type);
      return true;
    },
  });

  navigateWorkspacePath('/servers/server-1');
  assert.equal(globalThis.window.location.pathname, '/servers/server-1');
  assert.equal(globalThis.window.history.pushCount, 1);
  assert.equal(globalThis.window.history.replaceCount, 0);
  assert.deepEqual(emittedEvents, ['workspace-route-change']);

  navigateWorkspacePath('/', { replace: true });
  assert.equal(globalThis.window.location.pathname, '/');
  assert.equal(globalThis.window.history.pushCount, 1);
  assert.equal(globalThis.window.history.replaceCount, 1);
  assert.deepEqual(emittedEvents, ['workspace-route-change', 'workspace-route-change']);
});

function createMockWindow(initialPathname, overrides = {}) {
  const location = { pathname: initialPathname };
  const history = {
    pushCount: 0,
    replaceCount: 0,
    pushState(_state, _title, nextPath) {
      history.pushCount += 1;
      location.pathname = nextPath;
    },
    replaceState(_state, _title, nextPath) {
      history.replaceCount += 1;
      location.pathname = nextPath;
    },
  };

  return {
    location,
    history,
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {
      return true;
    },
    ...overrides,
  };
}
