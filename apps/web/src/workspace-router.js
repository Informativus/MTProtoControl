import { useCallback, useSyncExternalStore } from 'react';

import { buildServerWorkspacePath, parseWorkspacePath } from './workspace-route.js';

const workspaceRouteChangeEvent = 'workspace-route-change';

export function readWorkspacePath() {
  return typeof window !== 'undefined' ? window.location.pathname : '/';
}

export function readWorkspaceRoute() {
  return parseWorkspacePath(readWorkspacePath());
}

export function navigateWorkspacePath(nextPath, options = {}) {
  if (typeof window === 'undefined') {
    return nextPath;
  }

  const { replace = false } = options;

  if (window.location.pathname === nextPath) {
    return nextPath;
  }

  window.history[replace ? 'replaceState' : 'pushState']({}, '', nextPath);
  window.dispatchEvent(createWorkspaceRouteChangeEvent());
  return nextPath;
}

export function useWorkspaceRoute() {
  const pathname = useSyncExternalStore(subscribeToWorkspacePath, readWorkspacePath, () => '/');
  const route = parseWorkspacePath(pathname);

  const navigateToPath = useCallback((nextPath, options = {}) => {
    navigateWorkspacePath(nextPath, options);
  }, []);

  const navigateToBoard = useCallback(
    (options = {}) => {
      navigateToPath('/', options);
    },
    [navigateToPath],
  );

  const navigateToServerWorkspace = useCallback(
    (serverId, options = {}) => {
      const normalizedId = String(serverId || '').trim();

      if (normalizedId === '') {
        navigateToBoard(options);
        return;
      }

      navigateToPath(buildServerWorkspacePath(normalizedId), options);
    },
    [navigateToBoard, navigateToPath],
  );

  return {
    pathname,
    route,
    isBoardView: route.view === 'board',
    isDetailView: route.view === 'detail',
    navigateToPath,
    navigateToBoard,
    navigateToServerWorkspace,
  };
}

function subscribeToWorkspacePath(callback) {
  if (typeof window === 'undefined') {
    return () => {};
  }

  window.addEventListener('popstate', callback);
  window.addEventListener(workspaceRouteChangeEvent, callback);

  return () => {
    window.removeEventListener('popstate', callback);
    window.removeEventListener(workspaceRouteChangeEvent, callback);
  };
}

function createWorkspaceRouteChangeEvent() {
  if (typeof Event === 'function') {
    return new Event(workspaceRouteChangeEvent);
  }

  return { type: workspaceRouteChangeEvent };
}
