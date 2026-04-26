export function buildServerWorkspacePath(serverId) {
  const normalizedId = String(serverId || '').trim();
  return normalizedId ? `/servers/${encodeURIComponent(normalizedId)}` : '/';
}

export function parseWorkspacePath(pathname) {
  const normalizedPath = normalizePathname(pathname);
  const match = normalizedPath.match(/^\/servers\/([^/]+)$/);

  if (!match) {
    return {
      view: 'board',
      serverId: '',
    };
  }

  return {
    view: 'detail',
    serverId: decodeURIComponent(match[1]),
  };
}

function normalizePathname(pathname) {
  const rawPath = typeof pathname === 'string' && pathname.trim() !== '' ? pathname.trim() : '/';
  const withoutQuery = rawPath.split(/[?#]/, 1)[0] || '/';

  if (withoutQuery.length > 1 && withoutQuery.endsWith('/')) {
    return withoutQuery.slice(0, -1);
  }

  return withoutQuery;
}
