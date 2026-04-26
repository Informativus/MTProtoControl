import assert from 'node:assert/strict';
import { test } from 'node:test';

import { ApiRequestError, getErrorPayload, request, requestUrl, withJsonBody } from './api/http.js';

test('request returns parsed payload', async () => {
  const payload = { servers: [] };

  const result = await request('/api/servers', {
    baseUrl: 'http://localhost:8080',
    fetchImpl: async (url) => {
      assert.equal(url, 'http://localhost:8080/api/servers');
      return {
        ok: true,
        async json() {
          return payload;
        },
      };
    },
  });

  assert.equal(result, payload);
});

test('request returns null when successful response body is not json', async () => {
  const result = await request('/health', {
    baseUrl: 'http://localhost:8080',
    fetchImpl: async () => ({
      ok: true,
      async json() {
        throw new SyntaxError('Unexpected token');
      },
    }),
  });

  assert.equal(result, null);
});

test('request exposes normalized api errors and payload', async () => {
  const payload = {
    error: {
      code: 'config_required',
      message: 'generate or deploy a Telemt config before requesting a proxy link',
    },
  };

  await assert.rejects(
    request('/api/servers/server-1/link', {
      baseUrl: 'http://localhost:8080',
      fetchImpl: async () => ({
        ok: false,
        status: 409,
        async json() {
          return payload;
        },
      }),
    }),
    (error) => {
      assert.equal(error instanceof ApiRequestError, true);
      assert.equal(
        error.message,
        'Сначала сохраните или примените конфиг Telemt, а затем запрашивайте прокси-ссылку.',
      );
      assert.deepEqual(getErrorPayload(error), payload);
      return true;
    },
  );
});

test('requestUrl uses the provided absolute url', async () => {
  const result = await requestUrl('http://localhost:8080/health', {
    fetchImpl: async (url) => {
      assert.equal(url, 'http://localhost:8080/health');
      return {
        ok: true,
        async json() {
          return { service: 'mtproxy-control-api' };
        },
      };
    },
  });

  assert.deepEqual(result, { service: 'mtproxy-control-api' });
});

test('withJsonBody adds json content type and stringifies body', () => {
  assert.deepEqual(withJsonBody({ ok: true }, { method: 'POST' }), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: '{"ok":true}',
  });
});
