import { logLevelOptions, telemtFieldDefinitions } from '../../telemt-config.js';
import { advancedTelemtFieldKeys, primaryTelemtFieldKeys } from './config-constants.js';

const telemtFieldByKey = Object.fromEntries(telemtFieldDefinitions.map((field) => [field.key, field]));

export function ConfigDraftSection({ section }) {
  return (
    <section className="content-grid" id="config-section">
      <article className="panel primary-panel">
        <div className="panel-header">
          <div>
            <p className="eyebrow">Шаг 3</p>
            <h2>Черновик MTProto</h2>
          </div>
          <div className="button-row">
            <button
              className="secondary-button"
              disabled={!section.configDiscoveryReady || section.configLoading || section.actionBusy || section.configSyncBusy}
              onClick={section.onSyncDraftFromServer}
              title="Прочитать config.toml и маршрут с сервера по SSH. Ничего не записывает."
              type="button"
            >
              {section.configSyncBusy ? 'Чтение...' : 'Подтянуть MTProto'}
            </button>
            <button className="secondary-button" onClick={section.onResetDraftFields} title="Вернуть поля формы к последнему локальному шаблону." type="button">
              Сбросить
            </button>
            <button className="secondary-button" onClick={section.onClearSecret} title="Сгенерировать новый secret в форме. На сервере он изменится только после сохранения ревизии и deploy." type="button">
              Новый secret
            </button>
          </div>
        </div>

        <div className="config-identity-strip">
          <strong>{section.selectedServer.name}</strong>
          <span>{section.draftFields.public_host || section.selectedServer.public_host || section.selectedServer.host}</span>
          <span>{section.draftFields.public_port || section.selectedServer.mtproto_port || '443'}</span>
          <span>{section.effectiveTLSDomain || 'tls_domain не задан'}</span>
          <span>{section.configDraftSource}</span>
        </div>

        {section.tlsDomainWarning ? <p className="inline-warning compact-note">{section.tlsDomainWarning}</p> : null}
        {!section.configDiscoveryReady ? <p className="panel-note compact-note">Для кнопки «Подтянуть MTProto» сохраните путь к SSH-ключу в карточке сервера.</p> : null}

        <form className="config-form compact-config-form" onSubmit={section.onGenerateDraft}>
          <div className="config-field-grid">
            {primaryTelemtFieldKeys.map((fieldKey) => {
              const field = telemtFieldByKey[fieldKey];

              return (
                <label className="field-card compact-field-card" key={field.key}>
                  <span className="field-label">{field.label}</span>
                  <input
                    className="text-input"
                    onChange={(event) => section.onUpdateField(field.key, event.target.value)}
                    placeholder={field.placeholder}
                    type={field.type}
                    value={section.draftFields[field.key]}
                  />
                </label>
              );
            })}
          </div>

          <details className="advanced-settings">
            <summary>Дополнительно</summary>
            <div className="config-field-grid advanced-field-grid">
              {advancedTelemtFieldKeys.map((fieldKey) => {
                const field = telemtFieldByKey[fieldKey];

                return (
                  <label className="field-card" key={field.key}>
                    <span className="field-label">{field.label}</span>
                    <input
                      className="text-input"
                      onChange={(event) => section.onUpdateField(field.key, event.target.value)}
                      placeholder={field.placeholder}
                      type={field.type}
                      value={section.draftFields[field.key]}
                    />
                  </label>
                );
              })}

              <label className="field-card checkbox-card">
                <span className="field-label">Использовать middle proxy</span>
                <div className="checkbox-row">
                  <input
                    checked={section.draftFields.use_middle_proxy}
                    onChange={(event) => section.onUpdateField('use_middle_proxy', event.target.checked)}
                    type="checkbox"
                  />
                  <span>Оставить включенным</span>
                </div>
              </label>

              <label className="field-card">
                <span className="field-label">Уровень логов</span>
                <select className="text-input" onChange={(event) => section.onUpdateField('log_level', event.target.value)} value={section.draftFields.log_level}>
                  {logLevelOptions.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </details>

          <div className="form-actions">
            <button className="primary-button" disabled={section.actionBusy || section.configLoading} title="Собрать новый config.toml из полей формы и сохранить как ревизию в панели." type="submit">
              {section.actionBusy ? 'Выполняется...' : 'Сгенерировать'}
            </button>
          </div>
        </form>
      </article>

      <article className="panel secondary-panel">
        <div>
          <p className="eyebrow">Превью</p>
          <h2>Ссылка</h2>
        </div>

        <div className="preview-card">
          <span className="preview-label">{section.configDraftSource}</span>
          <code>{section.draftPreviewLink || 'Заполните хост, порт, TLS-домен и secret.'}</code>
        </div>

        <dl className="health-list compact-list">
          <div>
            <dt>Сохранено</dt>
            <dd>{section.currentConfig?.generated_link || 'Ревизии еще нет'}</dd>
          </div>
          <div>
            <dt>Редактор</dt>
            <dd>{section.unsavedEditorChanges ? 'Есть изменения' : 'Без изменений'}</dd>
          </div>
          <div>
            <dt>Путь</dt>
            <dd>{section.selectedServer.remote_base_path}</dd>
          </div>
        </dl>

        {section.revisions.length > 0 ? (
          <details className="advanced-settings revision-details">
            <summary>{`Ревизии (${section.revisions.length})`}</summary>
            <div className="revision-list">
              {section.revisions.map((revision) => (
                <article className="revision-item" key={revision.id}>
                  <strong>{`v${revision.version}`}</strong>
                  <span>{new Date(revision.created_at).toLocaleString('ru-RU')}</span>
                  <span>{revision.generated_link || 'Превью не записано'}</span>
                </article>
              ))}
            </div>
          </details>
        ) : null}
      </article>
    </section>
  );
}
