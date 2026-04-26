export function createTelegramSettingsDraft(settings = {}) {
  return {
    telegram_bot_token: '',
    telegram_bot_token_configured: Boolean(settings.telegram_bot_token_configured),
    telegram_bot_token_masked: toText(settings.telegram_bot_token_masked),
    telegram_chat_id: toText(settings.telegram_chat_id),
    alerts_enabled: Boolean(settings.alerts_enabled),
    repeat_down_after_minutes: toNumericText(settings.repeat_down_after_minutes, '0'),
  };
}

export function serializeTelegramSettings(fields) {
  const payload = {
    telegram_chat_id: fields.telegram_chat_id.trim(),
    alerts_enabled: Boolean(fields.alerts_enabled),
    repeat_down_after_minutes: Number.parseInt(fields.repeat_down_after_minutes || '0', 10) || 0,
  };

  const token = fields.telegram_bot_token.trim();
  if (token) {
    payload.telegram_bot_token = token;
  }

  return payload;
}

export function describeRepeatDownPolicy(fields = {}) {
  const minutes = Number.parseInt(fields.repeat_down_after_minutes || '0', 10) || 0;
  if (minutes <= 0) {
    return 'Оповещения отправляются только при смене состояния. Повторные down-алерты отключены.';
  }
  return `При смене состояния оповещение уходит сразу. Повторные down-алерты отправляются не чаще одного раза в ${minutes} мин.`;
}

function toText(value) {
  return value == null ? '' : String(value);
}

function toNumericText(value, fallback) {
  if (value == null || value === '') {
    return fallback;
  }

  return String(value);
}
