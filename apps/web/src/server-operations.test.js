import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  buildLogsPage,
  buildOperationsUrl,
  canUseOperationQueryAuth,
  clampLogsPageIndex,
  describeContainerSummary,
  describeGeneratedLinkSource,
  describePublicPortSummary,
  describeTelemtApiSummary,
  formatLogLineCount,
  getOperationAuthHelp,
  getNewestLogsPageIndex,
  serializeOperationsQuery,
} from './server-operations.js';

test('canUseOperationQueryAuth requires private key path auth', () => {
  assert.equal(canUseOperationQueryAuth({ auth_type: 'private_key_path', private_key_path: '~/.ssh/proxy-node' }), true);
  assert.equal(canUseOperationQueryAuth({ auth_type: 'password', private_key_path: '~/.ssh/proxy-node' }), false);
  assert.equal(canUseOperationQueryAuth({ auth_type: 'private_key_text', private_key_path: '~/.ssh/proxy-node' }), false);
});

test('serializeOperationsQuery includes path passphrase and tail', () => {
  assert.equal(
    serializeOperationsQuery(
      {
        auth_type: 'private_key_path',
        private_key_path: ' ~/.ssh/proxy-node ',
        passphrase: ' hunter2 ',
      },
      { tail: 50 },
    ),
    'auth_type=private_key_path&private_key_path=%7E%2F.ssh%2Fproxy-node&passphrase=hunter2&tail=50',
  );
});

test('buildOperationsUrl omits query when live auth is unavailable', () => {
  assert.equal(
    buildOperationsUrl('http://localhost:8080', 'server-1', '/status', {
      auth_type: 'private_key_text',
      private_key_path: '',
      passphrase: '',
    }),
    'http://localhost:8080/api/servers/server-1/status',
  );
});

test('getOperationAuthHelp explains live query limitation', () => {
  assert.match(
    getOperationAuthHelp({ auth_type: 'password', private_key_path: '', passphrase: '' }),
    /ssh-пароль сюда не передается/i,
  );
  assert.match(
    getOperationAuthHelp({ auth_type: 'private_key_text', private_key_path: '', passphrase: '' }),
    /поддерживают только авторизацию через private_key_path/i,
  );
});

test('describeGeneratedLinkSource maps known sources', () => {
  assert.equal(describeGeneratedLinkSource('telemt_api'), 'Telemt API (онлайн)');
  assert.equal(describeGeneratedLinkSource('config_revision'), 'Сохраненная ревизия конфига');
});

test('describeContainerSummary localizes common docker status text', () => {
  assert.equal(describeContainerSummary({ status: 'ok', summary: 'Up 13 hours (healthy)' }), 'Работает 13 часов (исправен)');
  assert.equal(describeContainerSummary({ status: 'missing', summary: 'Panel-managed Telemt container was not found on the host.' }), 'Контейнер Telemt не найден на сервере.');
});

test('describeTelemtApiSummary localizes reachable summary', () => {
  assert.equal(describeTelemtApiSummary({ summary: 'Telemt API reachable, 0 user(s) returned.' }), 'Telemt API доступен, найдено 0 пользователей.');
});

test('describePublicPortSummary localizes public TCP summary', () => {
  assert.equal(
    describePublicPortSummary({ summary: 'Public TCP endpoint accepted a connection from the panel host.' }),
    'Публичный TCP-эндпоинт принимает соединение с хоста панели.',
  );
});

test('buildLogsPage strips ansi sequences and paginates output', () => {
  const page = buildLogsPage('\u001b[32mline one\u001b[0m\nline two\nline three\n', 2, 1);
  assert.equal(page.text, 'line three');
  assert.equal(page.totalLines, 3);
  assert.equal(page.currentPage, 2);
  assert.equal(page.totalPages, 2);
  assert.equal(page.startLine, 3);
  assert.equal(page.endLine, 3);
});

test('getNewestLogsPageIndex and clampLogsPageIndex stay within range', () => {
  assert.equal(getNewestLogsPageIndex('one\ntwo\nthree\nfour\n', 3), 1);
  assert.equal(clampLogsPageIndex('one\ntwo\n', 50, 99), 0);
});

test('formatLogLineCount uses russian plural forms', () => {
  assert.equal(formatLogLineCount(1), '1 строка');
  assert.equal(formatLogLineCount(2), '2 строки');
  assert.equal(formatLogLineCount(5), '5 строк');
});
