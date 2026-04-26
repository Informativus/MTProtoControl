import assert from 'node:assert/strict';
import { test } from 'node:test';

import { describeHealthFlags, describeHealthMessage, describeHealthProblem, translateHealthMessage } from './health-messages.js';

test('translateHealthMessage localizes stored worker issues', () => {
  assert.equal(
    translateHealthMessage('no saved Telemt config is available for API and link checks; no generated proxy link is available'),
    'Нет сохраненной ревизии Telemt для проверки API и ссылки.; Нет сохраненной proxy-ссылки.',
  );
});

test('translateHealthMessage keeps unknown text but localizes common prefixes', () => {
  assert.equal(translateHealthMessage('SSH check failed: i/o timeout'), 'SSH-проверка не прошла: i/o timeout');
});

test('describeHealthMessage returns translated fallback text', () => {
  assert.equal(
    describeHealthMessage({ status: 'degraded', message: 'worker SSH checks skipped because no saved private_key_path is available' }),
    'SSH-проверка воркера пропущена: не сохранен путь к приватному ключу.',
  );
});

test('describeHealthProblem hides errors for online state', () => {
  assert.equal(describeHealthProblem({ status: 'online', message: 'All health checks passed.' }), 'Нет');
});

test('describeHealthFlags formats boolean checks for UI', () => {
  assert.equal(
    describeHealthFlags({ dns_ok: true, tcp_ok: true, ssh_ok: false, docker_ok: true, telemt_api_ok: false, link_ok: false }),
    'DNS ок · TCP ок · SSH ошибка · Docker ок · API ошибка · Ссылка отсутствует',
  );
});
