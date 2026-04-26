import { useEffect, useState } from 'react';

import { settingsApi } from '../../api/settingsApi.js';
import {
  createTelegramSettingsDraft,
  describeRepeatDownPolicy,
  serializeTelegramSettings,
} from '../../telegram-alerts.js';

const defaultTelegramSettings = createTelegramSettingsDraft();

export function useTelegramAlertsController() {
  const [telegramSettings, setTelegramSettings] = useState(defaultTelegramSettings);
  const [telegramLoading, setTelegramLoading] = useState(false);
  const [telegramSaving, setTelegramSaving] = useState(false);
  const [telegramTesting, setTelegramTesting] = useState(false);
  const [telegramError, setTelegramError] = useState('');
  const [telegramNotice, setTelegramNotice] = useState('');

  useEffect(() => {
    let ignore = false;

    async function loadTelegramSettings() {
      setTelegramLoading(true);
      setTelegramError('');

      try {
        const payload = await settingsApi.getTelegram();
        if (ignore) {
          return;
        }

        setTelegramSettings(createTelegramSettingsDraft(payload?.settings || {}));
      } catch (error) {
        if (ignore) {
          return;
        }

        setTelegramError(error instanceof Error ? error.message : 'Не удалось загрузить настройки Telegram-оповещений.');
        setTelegramSettings(defaultTelegramSettings);
      } finally {
        if (!ignore) {
          setTelegramLoading(false);
        }
      }
    }

    void loadTelegramSettings();

    return () => {
      ignore = true;
    };
  }, []);

  function updateTelegramSetting(key, value) {
    setTelegramSettings((current) => ({
      ...current,
      [key]: value,
    }));
  }

  async function handleSaveTelegramSettings() {
    setTelegramSaving(true);
    setTelegramError('');
    setTelegramNotice('');

    try {
      const payload = await settingsApi.saveTelegram(serializeTelegramSettings(telegramSettings));

      setTelegramSettings(createTelegramSettingsDraft(payload?.settings || {}));
      setTelegramNotice('Настройки Telegram-оповещений сохранены. Пустое поле токена оставляет текущий сохраненный токен без изменений.');
    } catch (error) {
      setTelegramError(error instanceof Error ? error.message : 'Не удалось сохранить настройки Telegram-оповещений.');
    } finally {
      setTelegramSaving(false);
    }
  }

  async function handleSendTelegramTestAlert() {
    setTelegramTesting(true);
    setTelegramError('');
    setTelegramNotice('');

    try {
      await settingsApi.sendTelegramTest();

      setTelegramNotice('Тестовое Telegram-оповещение отправлено с использованием сохраненных токена и chat id.');
    } catch (error) {
      setTelegramError(error instanceof Error ? error.message : 'Не удалось отправить тестовое Telegram-оповещение.');
    } finally {
      setTelegramTesting(false);
    }
  }

  const telegramRepeatPolicy = describeRepeatDownPolicy(telegramSettings);
  const savedTelegramTargetReady =
    telegramSettings.telegram_bot_token_configured && telegramSettings.telegram_chat_id.trim() !== '';

  return {
    savedTelegramTargetReady,
    telegramError,
    telegramLoading,
    telegramNotice,
    telegramRepeatPolicy,
    telegramSaving,
    telegramSettings,
    telegramTesting,
    handleSaveTelegramSettings,
    handleSendTelegramTestAlert,
    updateTelegramSetting,
  };
}
