import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createTelegramSettingsDraft, describeRepeatDownPolicy, serializeTelegramSettings } from './telegram-alerts.js';

test('createTelegramSettingsDraft keeps token input blank while exposing saved state', () => {
  const draft = createTelegramSettingsDraft({
    telegram_bot_token_configured: true,
    telegram_bot_token_masked: '1234...CDEF',
    telegram_chat_id: '-100123456',
    alerts_enabled: true,
    repeat_down_after_minutes: 30,
  });

  assert.equal(draft.telegram_bot_token, '');
  assert.equal(draft.telegram_bot_token_configured, true);
  assert.equal(draft.telegram_bot_token_masked, '1234...CDEF');
  assert.equal(draft.repeat_down_after_minutes, '30');
});

test('serializeTelegramSettings omits blank token and normalizes repeat value', () => {
  assert.deepEqual(
    serializeTelegramSettings({
      telegram_bot_token: ' ',
      telegram_chat_id: ' -100123456 ',
      alerts_enabled: true,
      repeat_down_after_minutes: '15',
    }),
    {
      telegram_chat_id: '-100123456',
      alerts_enabled: true,
      repeat_down_after_minutes: 15,
    },
  );
});

test('describeRepeatDownPolicy explains disabled and throttled repeat alerts', () => {
  assert.match(describeRepeatDownPolicy({ repeat_down_after_minutes: '0' }), /отключены/i);
  assert.match(describeRepeatDownPolicy({ repeat_down_after_minutes: '15' }), /15 мин/i);
});
