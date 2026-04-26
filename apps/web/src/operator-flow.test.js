import assert from 'node:assert/strict';
import { test } from 'node:test';

import { buildOperatorSteps, hasConfiguredSSHAuth } from './operator-flow.js';

test('hasConfiguredSSHAuth supports password, path and text auth', () => {
  assert.equal(
    hasConfiguredSSHAuth({
      auth_type: 'password',
      password: ' hunter2 ',
      private_key_path: '',
      private_key_text: '',
    }),
    true,
  );

  assert.equal(
    hasConfiguredSSHAuth({
      auth_type: 'private_key_path',
      password: '',
      private_key_path: ' ~/.ssh/proxy-node ',
      private_key_text: '',
    }),
    true,
  );

  assert.equal(
    hasConfiguredSSHAuth({
      auth_type: 'private_key_text',
      password: '',
      private_key_path: '',
      private_key_text: '-----BEGIN OPENSSH PRIVATE KEY-----',
    }),
    true,
  );

  assert.equal(
    hasConfiguredSSHAuth({
      auth_type: 'private_key_path',
      password: '',
      private_key_path: '   ',
      private_key_text: '',
    }),
    false,
  );
});

test('buildOperatorSteps blocks later stages until a server exists', () => {
  const steps = buildOperatorSteps({
    selectedServer: null,
    sshAuthReady: false,
    sshTestSuccessful: false,
    currentConfig: null,
    deployPreview: null,
    deployHasBlockingRisks: false,
    activeLink: '',
    currentHealth: null,
    diagnosticsLabel: '',
    appliedLabel: '',
    healthLabel: '',
  });

  assert.equal(steps[0].state, 'active');
  assert.equal(steps[1].state, 'blocked');
  assert.equal(steps[4].blocker, 'Сначала добавьте сервер, затем применяйте деплой.');
});

test('buildOperatorSteps shows config and preview prerequisites', () => {
  const selectedServer = {
    id: 'server-1',
    name: 'proxy_node_1',
    saved_private_key_path: '~/.ssh/proxy-node',
  };

  const steps = buildOperatorSteps({
    selectedServer,
    sshAuthReady: true,
    sshTestSuccessful: true,
    currentConfig: null,
    deployPreview: null,
    deployHasBlockingRisks: false,
    activeLink: '',
    currentHealth: null,
    diagnosticsLabel: '',
    appliedLabel: '',
    healthLabel: '',
  });

  assert.equal(steps[1].state, 'done');
  assert.equal(steps[2].state, 'active');
  assert.equal(steps[3].state, 'blocked');
  assert.equal(steps[3].blocker, 'Сгенерируйте или сохраните конфиг перед загрузкой превью деплоя.');
});

test('buildOperatorSteps marks post-deploy operations and health as done', () => {
  const steps = buildOperatorSteps({
    selectedServer: {
      id: 'server-1',
      name: 'proxy_node_1',
      saved_private_key_path: '~/.ssh/proxy-node',
    },
    sshAuthReady: true,
    sshTestSuccessful: true,
    currentConfig: {
      version: 3,
      applied_at: '2026-04-25T12:00:00Z',
    },
    deployPreview: {
      risks: [],
    },
    deployHasBlockingRisks: false,
    activeLink: 'https://t.me/proxy?server=mt.example.com',
    currentHealth: {
      created_at: '2026-04-25T12:05:00Z',
    },
    diagnosticsLabel: 'Превью загружено 25.04.2026, 15:00:00',
    appliedLabel: 'Применено 25.04.2026, 15:01:00',
    healthLabel: 'Последняя проверка воркера 25.04.2026, 15:05:00',
  });

  assert.equal(steps[3].state, 'done');
  assert.equal(steps[4].state, 'done');
  assert.equal(steps[5].state, 'done');
  assert.equal(steps[6].state, 'done');
});

test('buildOperatorSteps keeps operations pending when only a saved link preview exists', () => {
  const steps = buildOperatorSteps({
    selectedServer: {
      id: 'server-1',
      name: 'proxy_node_1',
      saved_private_key_path: '',
    },
    sshAuthReady: false,
    sshTestSuccessful: false,
    currentConfig: {
      version: 1,
      applied_at: null,
    },
    deployPreview: null,
    deployHasBlockingRisks: false,
    activeLink: 'https://t.me/proxy?server=mt.example.com',
    currentHealth: null,
    diagnosticsLabel: '',
    appliedLabel: '',
    healthLabel: '',
  });

  assert.equal(steps[5].state, 'active');
  assert.equal(steps[5].blocker, 'Сначала примените деплой, затем обновите статус, логи и ссылку.');
});

test('buildOperatorSteps treats imported applied config as completed deploy flow', () => {
  const steps = buildOperatorSteps({
    selectedServer: {
      id: 'server-1',
      name: 'proxy_node_1',
      saved_private_key_path: '~/.ssh/proxy-node',
    },
    sshAuthReady: true,
    sshTestSuccessful: true,
    currentConfig: {
      version: 2,
      applied_at: '2026-04-25T12:00:00Z',
    },
    deployPreview: null,
    deployHasBlockingRisks: false,
    activeLink: 'https://t.me/proxy?server=mt.example.com',
    currentHealth: null,
    diagnosticsLabel: '',
    appliedLabel: 'Применено 25.04.2026, 15:01:00',
    healthLabel: '',
  });

  assert.equal(steps[3].state, 'done');
  assert.equal(steps[4].state, 'done');
  assert.equal(steps[5].state, 'done');
  assert.equal(steps[3].blocker, '');
});
