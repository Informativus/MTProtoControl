const fallbackReleaseVersion = 'v0.0.0-dev';

export function resolveReleaseVersion(env = {}) {
  const value = Object.prototype.hasOwnProperty.call(env, 'VITE_RELEASE_VERSION')
    ? String(env.VITE_RELEASE_VERSION).trim()
    : '';

  if (!value) {
    return fallbackReleaseVersion;
  }

  return value.startsWith('v') ? value : `v${value}`;
}

export const releaseVersion = resolveReleaseVersion(import.meta.env);
