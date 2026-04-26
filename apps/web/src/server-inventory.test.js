import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createServerDraft, getApiErrorDetails, getNextSelectedServerId, serializeServerDraft } from './server-inventory.js';

test('createServerDraft provides operator defaults', () => {
  const draft = createServerDraft();

  assert.equal(draft.ssh_port, '22');
  assert.equal(draft.mtproto_port, '443');
  assert.equal(draft.remote_base_path, '');
  assert.equal(draft.private_key_path, '');
});

test('serializeServerDraft trims text and parses ports', () => {
  assert.deepEqual(
    serializeServerDraft({
      name: ' proxy_node_1 ',
      host: ' 203.0.113.10 ',
      ssh_port: ' 2222 ',
      ssh_user: ' operator ',
      private_key_path: ' ~/.ssh/proxy-node ',
      public_host: ' mt.example.com ',
      public_ip: ' 203.0.113.10 ',
      mtproto_port: ' 443 ',
      sni_domain: ' mt.example.com ',
      remote_base_path: ' /srv/telemt ',
    }),
    {
      name: 'proxy_node_1',
      host: '203.0.113.10',
      ssh_port: 2222,
      ssh_user: 'operator',
      private_key_path: '~/.ssh/proxy-node',
      public_host: 'mt.example.com',
      public_ip: '203.0.113.10',
      mtproto_port: 443,
      sni_domain: 'mt.example.com',
      remote_base_path: '/srv/telemt',
    },
  );
});

test('getApiErrorDetails returns only field error details', () => {
  assert.deepEqual(
    getApiErrorDetails({
      error: {
        code: 'validation_error',
        details: {
          name: 'is required',
        },
      },
    }),
    { name: 'is required' },
  );

  assert.deepEqual(getApiErrorDetails({ error: { code: 'validation_error' } }), {});
});

test('getNextSelectedServerId keeps selection stable after delete', () => {
  const servers = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];

  assert.equal(getNextSelectedServerId(servers, 'b', 'a'), 'a');
  assert.equal(getNextSelectedServerId(servers, 'b', 'b'), 'c');
  assert.equal(getNextSelectedServerId(servers, 'c', 'c'), 'b');
  assert.equal(getNextSelectedServerId([{ id: 'only' }], 'only', 'only'), '');
});
