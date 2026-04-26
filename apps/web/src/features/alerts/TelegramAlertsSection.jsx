export function TelegramAlertsSection({ section }) {
  return (
    <section className="panel telegram-panel" id="telegram-section">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Опционально</p>
          <h2>Telegram-оповещения</h2>
        </div>

        <div className="button-row">
          <button className="secondary-button" disabled={section.telegramSaving || section.telegramLoading} onClick={section.onSaveTelegramSettings} title="Сохранить настройки Telegram-оповещений в панели." type="button">
            {section.telegramSaving ? 'Сохранение...' : 'Сохранить'}
          </button>
          <button
            className="primary-button"
            disabled={section.telegramTesting || section.telegramLoading || !section.savedTelegramTargetReady}
            onClick={section.onSendTelegramTestAlert}
            title="Отправить тестовое сообщение в сохраненный Telegram-чат."
            type="button"
          >
            {section.telegramTesting ? 'Отправка...' : 'Тестовое сообщение'}
          </button>
        </div>
      </div>

      {section.telegramError ? <p className="inline-error">{section.telegramError}</p> : null}
      {section.telegramNotice ? <p className="inline-success">{section.telegramNotice}</p> : null}

      <div className="operations-grid telegram-grid">
        <article className="operations-column">
          <div className="link-card telegram-status-card">
            <span className="preview-label">Текущий маршрут оповещений</span>
            <code>{section.savedTelegramTargetReady ? section.telegramSettings.telegram_chat_id : 'Получатель Telegram еще не сохранен.'}</code>
            <div className="link-meta-row">
              <span>
                {section.telegramSettings.telegram_bot_token_configured
                  ? `Токен ${section.telegramSettings.telegram_bot_token_masked || 'настроен'}`
                  : 'Токен еще не сохранен'}
              </span>
              <span>{section.telegramSettings.alerts_enabled ? 'Оповещения включены' : 'Оповещения выключены'}</span>
            </div>
          </div>

          <dl className="health-list compact-list">
            <div>
              <dt>Сохраненный токен</dt>
              <dd>{section.telegramSettings.telegram_bot_token_configured ? section.telegramSettings.telegram_bot_token_masked || 'Настроен' : 'Не настроен'}</dd>
            </div>
            <div>
              <dt>Сохраненный чат</dt>
              <dd>{section.telegramSettings.telegram_chat_id || 'Не настроен'}</dd>
            </div>
            <div>
              <dt>Политика повторов</dt>
              <dd>{section.telegramRepeatPolicy}</dd>
            </div>
          </dl>
        </article>

        <article className="operations-column">
          {section.telegramLoading ? <p className="panel-note">Загрузка настроек Telegram-оповещений...</p> : null}

          <div className="config-form telegram-settings-form">
            <label className="field-card">
              <span className="field-label">Токен бота</span>
              <input
                className="text-input"
                onChange={(event) => section.onUpdateTelegramSetting('telegram_bot_token', event.target.value)}
                placeholder={section.telegramSettings.telegram_bot_token_configured ? 'Оставьте пустым, чтобы сохранить текущий токен' : '123456:ABCDEF'}
                type="password"
                value={section.telegramSettings.telegram_bot_token}
              />
              <span className="field-hint">В ответах plaintext-токен никогда не возвращается. Сохраняйте новое значение только если хотите ротировать токен.</span>
            </label>

            <label className="field-card">
              <span className="field-label">Chat id</span>
              <input
                className="text-input"
                onChange={(event) => section.onUpdateTelegramSetting('telegram_chat_id', event.target.value)}
                placeholder="-100123456"
                type="text"
                value={section.telegramSettings.telegram_chat_id}
              />
              <span className="field-hint">Используйте прямой chat id или username канала, куда бот может писать.</span>
            </label>

            <label className="field-card">
              <span className="field-label">Повторять down через минут</span>
              <input
                className="text-input"
                min="0"
                onChange={(event) => section.onUpdateTelegramSetting('repeat_down_after_minutes', event.target.value)}
                placeholder="0"
                type="number"
                value={section.telegramSettings.repeat_down_after_minutes}
              />
              <span className="field-hint">Укажите `0`, чтобы отправлять оповещения только при смене состояния. Любое положительное значение включает повторные down-алерты с ограничением частоты.</span>
            </label>

            <label className="field-card checkbox-card telegram-toggle-card">
              <span className="field-label">Оповещения включены</span>
              <div className="checkbox-row">
                <input
                  checked={section.telegramSettings.alerts_enabled}
                  onChange={(event) => section.onUpdateTelegramSetting('alerts_enabled', event.target.checked)}
                  type="checkbox"
                />
                <span>Отправлять в Telegram оповещения о недоступности, деградации, восстановлении и неудачном деплое.</span>
              </div>
            </label>
          </div>

          <p className="panel-note">Сохраните настройки перед тестовой отправкой. Тест использует сохраненные токен и chat id, а не несохраненные значения формы.</p>
        </article>
      </div>
    </section>
  );
}
