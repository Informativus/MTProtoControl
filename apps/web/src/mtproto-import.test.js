import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  applyDiscoveryToConfigDraft,
  applyDiscoveryToInventoryDraft,
  buildDiscoveryServerPatch,
  hasDiscoveredConfigFields,
  hasDiscoveredConfigText,
  maskImportedSecret,
  shouldSaveDiscoveredConfig,
} from './mtproto-import.js';

test('applyDiscoveryToInventoryDraft copies discovered route fields', () => {
  assert.deepEqual(
    applyDiscoveryToInventoryDraft(
      {
        public_host: '',
        mtproto_port: '443',
        sni_domain: '',
        remote_base_path: '',
      },
      {
        public_host: 'mt.example.com',
        mtproto_port: 8443,
        sni_domain: 'edge.example.com',
        remote_base_path: '/srv/telemt',
        secret: '0123456789abcdef0123456789abcdef',
      },
    ),
    {
      public_host: 'mt.example.com',
      mtproto_port: '8443',
      sni_domain: 'edge.example.com',
      remote_base_path: '/srv/telemt',
    },
  );
});

test('applyDiscoveryToConfigDraft copies secret and mtproto settings', () => {
  assert.deepEqual(
    applyDiscoveryToConfigDraft(
      {
        public_host: 'current.example.com',
        public_port: '443',
        tls_domain: 'current.example.com',
        secret: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        mask_host: 'www.yandex.ru',
      },
      {
        public_host: 'mt.example.com',
        mtproto_port: 8443,
        sni_domain: 'edge.example.com',
        secret: '0123456789abcdef0123456789abcdef',
      },
    ),
    {
      public_host: 'mt.example.com',
      public_port: '8443',
      tls_domain: 'edge.example.com',
      secret: '0123456789abcdef0123456789abcdef',
      mask_host: 'www.yandex.ru',
    },
  );
});

test('maskImportedSecret shortens long secrets for status UI', () => {
  assert.equal(maskImportedSecret('0123456789abcdef0123456789abcdef'), '01234567...cdef');
  assert.equal(maskImportedSecret('abc123'), 'abc123');
  assert.equal(maskImportedSecret('  '), '');
});

test('buildDiscoveryServerPatch returns only changed server fields', () => {
  assert.deepEqual(
    buildDiscoveryServerPatch(
      {
        public_host: 'current.example.com',
        mtproto_port: 443,
        sni_domain: 'current.example.com',
        remote_base_path: '/opt/mtproto-panel/telemt',
      },
      {
        public_host: 'mt.example.com',
        mtproto_port: 8443,
        sni_domain: 'edge.example.com',
        remote_base_path: '/srv/telemt',
      },
    ),
    {
      public_host: 'mt.example.com',
      mtproto_port: 8443,
      sni_domain: 'edge.example.com',
      remote_base_path: '/srv/telemt',
    },
  );
});

test('buildDiscoveryServerPatch returns null when server already matches discovery', () => {
  assert.equal(
    buildDiscoveryServerPatch(
      {
        public_host: 'mt.example.com',
        mtproto_port: 443,
        sni_domain: 'mt.example.com',
        remote_base_path: '/srv/telemt',
      },
      {
        public_host: 'mt.example.com',
        mtproto_port: 443,
        sni_domain: 'mt.example.com',
        remote_base_path: '/srv/telemt',
      },
    ),
    null,
  );
});

test('shouldSaveDiscoveredConfig skips identical config text', () => {
  assert.equal(
    shouldSaveDiscoveredConfig(
      {
        config_text: '[general]\nlog_level = "normal"\n',
      },
      {
        config_text: '[general]\r\nlog_level = "normal"\r\n',
      },
    ),
    false,
  );
});

test('shouldSaveDiscoveredConfig requires a new revision for changed config text', () => {
  assert.equal(
    shouldSaveDiscoveredConfig(
      {
        config_text: '[general]\nlog_level = "normal"\n',
      },
      {
        config_text: '[general]\nlog_level = "debug"\n',
      },
    ),
    true,
  );
});

test('hasDiscoveredConfigText detects a non-empty remote config', () => {
  assert.equal(hasDiscoveredConfigText({ config_text: '\n[general]\n' }), true);
  assert.equal(hasDiscoveredConfigText({ config_text: '   ' }), false);
});

test('hasDiscoveredConfigFields detects partial MTProto discovery data', () => {
  assert.equal(hasDiscoveredConfigFields({ secret: '0123456789abcdef0123456789abcdef' }), true);
  assert.equal(hasDiscoveredConfigFields({ mtproto_port: 443 }), true);
  assert.equal(hasDiscoveredConfigFields({ public_host: '   ', sni_domain: '', secret: '' }), false);
});
