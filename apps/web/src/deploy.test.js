import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createDeployDraft, getDeployDecisionHelp, hasBlockingRisks, serializeDeployRequest, serializeSSHAuthFields } from './deploy.js';

test('createDeployDraft defaults to key path auth', () => {
  const draft = createDeployDraft();

  assert.equal(draft.auth_type, 'private_key_path');
  assert.equal(draft.password, '');
  assert.equal(draft.confirm_blockers, false);
  assert.equal(draft.port_conflict_decision, '');
});

test('serializeDeployRequest keeps only active auth fields', () => {
  assert.deepEqual(
    serializeDeployRequest({
      auth_type: 'private_key_path',
      private_key_path: ' ~/.ssh/proxy-node ',
      private_key_text: 'ignored',
      passphrase: ' hunter2 ',
      confirm_blockers: true,
      port_conflict_decision: ' stop_existing_service ',
    }),
    {
      auth_type: 'private_key_path',
      private_key_path: '~/.ssh/proxy-node',
      passphrase: 'hunter2',
      confirm_blockers: true,
      port_conflict_decision: 'stop_existing_service',
    },
  );
});

test('getDeployDecisionHelp explains non-automatic choices', () => {
  assert.match(getDeployDecisionHelp('use_sni_router'), /не настраивает haproxy-маршрутизацию/i);
});

test('serializeSSHAuthFields keeps only password for password auth', () => {
  assert.deepEqual(
    serializeSSHAuthFields({
      auth_type: 'password',
      password: ' hunter2 ',
      private_key_path: '~/.ssh/proxy-node',
      private_key_text: 'ignored',
      passphrase: 'ignored',
    }),
    {
      auth_type: 'password',
      password: 'hunter2',
    },
  );
});

test('hasBlockingRisks detects preview blockers', () => {
  assert.equal(hasBlockingRisks({ risks: [{ blocking: false }, { blocking: true }] }), true);
  assert.equal(hasBlockingRisks({ risks: [{ blocking: false }] }), false);
});
