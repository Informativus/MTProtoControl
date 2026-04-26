import assert from 'node:assert/strict';
import { test } from 'node:test';

import { buildPreviewLink, createDraftFields, describeApiError, getTLSDomainWarning } from './telemt-config.js';

test('createDraftFields applies task defaults', () => {
  const draft = createDraftFields();

  assert.equal(draft.public_port, '443');
  assert.equal(draft.mask_host, 'www.yandex.ru');
  assert.equal(draft.mask_port, '443');
  assert.equal(draft.api_port, '9091');
  assert.equal(draft.log_level, 'normal');
  assert.equal(draft.use_middle_proxy, true);
});

test('buildPreviewLink uses ee plus secret plus tls hex', () => {
  const link = buildPreviewLink({
    public_host: 'mt.example.com',
    public_port: '443',
    tls_domain: 'mt.example.com',
    secret: '0123456789abcdef0123456789abcdef',
  });

  assert.equal(
    link,
    'https://t.me/proxy?server=mt.example.com&port=443&secret=ee0123456789abcdef0123456789abcdef6d742e6578616d706c652e636f6d',
  );
});

test('getTLSDomainWarning flags mismatch', () => {
  assert.match(
    getTLSDomainWarning({
      public_host: 'mt.example.com',
      tls_domain: 'edge.example.com',
    }),
    /tls_domain отличается от public_host/i,
  );
});

test('describeApiError localizes config_required', () => {
  assert.equal(
    describeApiError({
      error: {
        code: 'config_required',
        message: 'generate or deploy a Telemt config before requesting a proxy link',
      },
    }),
    'Сначала сохраните или примените конфиг Telemt, а затем запрашивайте прокси-ссылку.',
  );
});
