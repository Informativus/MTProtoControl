import { serverInventoryFields } from '../../server-inventory.js';
import { maskImportedSecret } from '../../mtproto-import.js';
import { InlineHint } from '../../shared/ui/app-ui.jsx';
import {
  inventoryFieldGroups,
  sameHostImportFieldExample,
  sameHostImportSetupExample,
} from './inventory-constants.js';

const serverInventoryFieldByKey = Object.fromEntries(serverInventoryFields.map((field) => [field.key, field]));

export function InventorySection({ section }) {
  return (
    <section className="panel inventory-panel" id="inventory-section" ref={section.inventorySectionRef}>
      <div className="panel-header">
        <div>
          <p className="eyebrow">Шаг 1</p>
          <h2>Инвентарь серверов</h2>
        </div>

        <div className="button-row">
          <button className="secondary-button" disabled={section.inventoryBusy} onClick={section.onCreateServer} title="Создать новую запись сервера в инвентаре." type="button">
            Добавить сервер
          </button>
        </div>
      </div>

      {section.inventoryError ? <p className="inline-error">{section.inventoryError}</p> : null}
      {section.inventoryNotice ? <p className="inline-success">{section.inventoryNotice}</p> : null}

      <div className="inventory-grid">
        <article className="inventory-column inventory-form-column">
          <div className="deploy-section-header">
            <strong>{section.inventoryFormHeading}</strong>
          </div>

          {section.inventoryFormVisible ? (
            <>
              <article className="inventory-import-card">
                <div className="inventory-import-header">
                  <div>
                    <span className="preview-label">Импорт из существующего Telemt</span>
                    <div className="title-with-hint inventory-import-title">
                      <strong>Импортировать MTProto</strong>
                      <InlineHint text={section.inventoryImportHint} />
                    </div>
                  </div>
                  <span className={`status-chip ${section.inventoryImportBadge.tone}`}>{section.inventoryImportBadge.label}</span>
                </div>

                <div className="inventory-import-guide" aria-label="Подсказка для импорта с того же сервера">
                  <article className="preview-card">
                    <span className="preview-label">Если это тот же сервер</span>
                    <strong>Что ввести в поля</strong>
                    <pre className="inventory-guide-code"><code>{sameHostImportFieldExample}</code></pre>
                    <span>Если `localhost` не сработал, временно укажите внешний IP сервера вместо `localhost`, а потом повторите импорт.</span>
                  </article>

                  <article className="preview-card">
                    <span className="preview-label">Подготовка на хосте</span>
                    <strong>Что сделать один раз</strong>
                    <pre className="inventory-guide-code"><code>{sameHostImportSetupExample}</code></pre>
                    <span>Панель в docker-установке видит ключи только из папки `./ssh` рядом с release `docker-compose.yml`; внутри API это `/root/.ssh/*`.</span>
                  </article>
                </div>

                <div className="button-row">
                  <button
                    className="secondary-button"
                    disabled={section.inventoryBusy || section.inventoryDetectBusy}
                    onClick={section.onDetectServerSettings}
                    title="Прочитать уже установленный Telemt по SSH и перенести найденные настройки MTProto в форму сервера."
                    type="button"
                  >
                    {section.inventoryDetectBusy ? 'Читаем config.toml...' : 'Импортировать MTProto'}
                  </button>
                </div>

                {section.inventoryImportStatus === 'success' ? <p className="inline-success">{section.inventoryImportMessage}</p> : null}
                {section.inventoryImportStatus === 'error' ? <p className="inline-error">{section.inventoryImportMessage}</p> : null}

                {section.inventoryImportDiscovery ? (
                  <dl className="health-list compact-list inventory-import-list">
                    <div>
                      <dt>Config path</dt>
                      <dd>{section.inventoryImportDiscovery.config_path}</dd>
                    </div>
                    <div>
                      <dt>public_host</dt>
                      <dd>{section.inventoryImportDiscovery.public_host || 'Не найден'}</dd>
                    </div>
                    <div>
                      <dt>mtproto_port</dt>
                      <dd>{section.inventoryImportDiscovery.mtproto_port != null ? String(section.inventoryImportDiscovery.mtproto_port) : 'Не найден'}</dd>
                    </div>
                    <div>
                      <dt>sni_domain</dt>
                      <dd>{section.inventoryImportDiscovery.sni_domain || 'Не найден'}</dd>
                    </div>
                    <div>
                      <dt>Папка Telemt на сервере</dt>
                      <dd>{section.inventoryImportDiscovery.remote_base_path || 'Не найден'}</dd>
                    </div>
                    <div>
                      <dt>secret</dt>
                      <dd>{maskImportedSecret(section.inventoryImportDiscovery.secret) || 'Не найден'}</dd>
                    </div>
                  </dl>
                ) : null}
              </article>

              <form className="config-form server-inventory-form" onSubmit={section.onSubmitServerForm}>
                {inventoryFieldGroups.map((group) => (
                  <section className="inventory-field-group" key={group.id}>
                    <div className="inventory-field-group-header">
                      <div>
                        <strong>{group.title}</strong>
                        {group.description ? <p>{group.description}</p> : null}
                      </div>
                    </div>

                    <div className="inventory-field-grid">
                      {group.keys.map((fieldKey) => {
                        const field = serverInventoryFieldByKey[fieldKey];
                        const fieldError = section.inventoryFieldErrors[field.key] || '';

                        return (
                          <label className="field-card" key={field.key}>
                            <span className="field-label title-with-hint">
                              <span>{field.label}</span>
                              {field.description ? <InlineHint text={field.description} /> : null}
                            </span>
                            <input
                              className={`text-input ${fieldError ? 'input-error' : ''}`}
                              min={field.type === 'number' ? '1' : undefined}
                              onChange={(event) => section.onUpdateInventoryField(field.key, event.target.value)}
                              placeholder={field.placeholder}
                              ref={field.key === 'name' ? section.inventoryPrimaryInputRef : undefined}
                              type={field.type}
                              value={section.inventoryDraft[field.key]}
                            />
                            {fieldError ? <span className="field-error">{fieldError}</span> : null}
                          </label>
                        );
                      })}
                    </div>
                  </section>
                ))}

                <div className="form-actions">
                  <button className="secondary-button" disabled={section.inventoryBusy} onClick={section.onResetInventoryForm} title="Очистить форму и вернуть стартовые значения." type="button">
                    Сбросить
                  </button>
                  {section.serversLength > 0 ? (
                    <button className="secondary-button" disabled={section.inventoryBusy} onClick={section.onCancelInventoryForm} title="Закрыть форму без сохранения текущих изменений." type="button">
                      Отмена
                    </button>
                  ) : null}
                  <button className="primary-button" disabled={section.inventoryBusy} title="Сохранить сервер в локальный инвентарь панели." type="submit">
                    {section.inventoryBusy ? 'Сохранение...' : section.inventoryActionLabel}
                  </button>
                </div>
              </form>
            </>
          ) : (
            <p className="panel-note">Используйте «Добавить сервер», чтобы создать новую запись, или «Редактировать», чтобы изменить активный хост на месте.</p>
          )}
        </article>
      </div>
    </section>
  );
}
