import { API_BASE_URL } from '../config.js';

import { describeApiError } from './api-errors.js';

export class ApiRequestError extends Error {
  constructor(message, { cause, payload = null, status = 0 } = {}) {
    super(message);
    this.name = 'ApiRequestError';
    this.cause = cause;
    this.payload = payload;
    this.status = status;
  }
}

export async function request(path, options = {}) {
  const {
    baseUrl = API_BASE_URL,
    fetchImpl = fetch,
    getErrorMessage = defaultErrorMessage,
    ...fetchOptions
  } = options;

  let response;
  try {
    response = await fetchImpl(`${baseUrl}${path}`, fetchOptions);
  } catch (error) {
    throw normalizeRequestError(error);
  }

  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    throw new ApiRequestError(getErrorMessage({ payload, response }), {
      payload,
      status: response.status,
    });
  }

  return payload;
}

export function requestUrl(url, options = {}) {
  return request(url, { ...options, baseUrl: '' });
}

export function withJsonBody(body, init = {}) {
  return {
    ...init,
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      ...init.headers,
    },
  };
}

export function getErrorPayload(error) {
  return error instanceof ApiRequestError ? error.payload : null;
}

function defaultErrorMessage({ payload }) {
  return describeApiError(payload);
}

function normalizeRequestError(error) {
  if (error instanceof ApiRequestError) {
    return error;
  }

  return new ApiRequestError(error instanceof Error ? error.message : 'Запрос не выполнен.', {
    cause: error,
  });
}
