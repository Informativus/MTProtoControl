const fallbackApiBaseUrl = 'http://localhost:8080';

export function resolveApiBaseUrl(env = {}) {
  const value = Object.prototype.hasOwnProperty.call(env, 'VITE_API_BASE_URL')
    ? env.VITE_API_BASE_URL
    : fallbackApiBaseUrl;
  return value.replace(/\/+$/, '');
}

export const API_BASE_URL = resolveApiBaseUrl(import.meta.env);
