import { InlineHint } from '../../shared/ui/app-ui.jsx';

export function DeploySection({ section }) {
  const deployGuideText = section.selectedServer
    ? `Если Telemt уже запущен вручную и его нужно только взять под мониторинг, сначала нажмите «Подтянуть MTProto» в шаге выше. «Загрузить превью» только читает текущее состояние, а «Применить деплой» заменит panel-managed файлы в ${section.selectedServer.remote_base_path || 'remote_base_path'}, сохранит backup прошлых версий и перезапустит Telemt.`
    : '«Загрузить превью» только проверяет текущее состояние сервера. «Применить деплой» загружает текущую ревизию, делает backup старых файлов и перезапускает Telemt.';

  return (
    <section className="panel deploy-panel" id="deploy-section">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Шаг 4-5</p>
          <h2 className="title-with-hint">
            <span>Превью деплоя и применение</span>
            <InlineHint text="Deploy записывает текущую ревизию панели в удаленный config.toml и compose, делает backup старых файлов и перезапускает Telemt." />
          </h2>
        </div>

        <div className="button-row">
          <button className="secondary-button" disabled={section.deployBusy} onClick={section.onLoadDeployPreview} title="Проверить текущее состояние сервера и показать, что изменит deploy. Ничего не записывает." type="button">
            {section.deployBusy ? 'Выполняется...' : 'Загрузить превью'}
          </button>
          <button className="primary-button" disabled={section.deployBusy} onClick={section.onApplyDeploy} title="Записать текущую ревизию на сервер, сделать backup старых файлов и перезапустить Telemt." type="button">
            {section.deployBusy ? 'Выполняется...' : 'Применить деплой'}
          </button>
        </div>
      </div>

      <p className="panel-note deploy-guide-note">{deployGuideText}</p>

      {section.deployError ? <p className="inline-error">{section.deployError}</p> : null}
      {section.deployNotice ? <p className="inline-success">{section.deployNotice}</p> : null}
      {section.deployHasBlockingRisks ? (
        <p className="inline-warning">
          В превью обнаружены блокирующие риски. Проверьте решение и подтверждайте блокеры только когда уверены, что владелец порта и путь деплоя безопасны.
        </p>
      ) : null}

      <div className="deploy-grid">
        <article className="deploy-column">
          <div className="deploy-section-header">
            <strong>Управление деплоем</strong>
            <span>SSH-доступ настраивается в блоке выше</span>
          </div>

          <div className="link-card compact-link-card">
            <span className="preview-label">Текущее состояние деплоя</span>
            <code>{section.currentConfig ? `Конфиг v${section.currentConfig.version}` : 'Сохраненного конфига пока нет.'}</code>
            <div className="link-meta-row">
              <span>{section.savedPrivateKeyPath || 'Путь к ключу еще не сохранен'}</span>
              <span>{section.lastDiagnosticsAt ? `Превью ${section.lastDiagnosticsAtLabel}` : 'Превью еще не загружено'}</span>
            </div>
          </div>

          <label className="field-card checkbox-card">
            <span className="field-label title-with-hint">
              <span>Подтвердить блокеры</span>
              <InlineHint text="Разрешает deploy даже при блокирующих рисках из превью. Включайте только после проверки причин." />
            </span>
            <div className="checkbox-row">
              <input
                checked={section.deployDraft.confirm_blockers}
                onChange={(event) => section.onUpdateDeployField('confirm_blockers', event.target.checked)}
                title="Разрешить deploy, даже если превью нашло блокирующие риски."
                type="checkbox"
              />
              <span>Разрешить продолжение только если вы проверили превью и осознанно принимаете блокеры.</span>
            </div>
          </label>

          <label className="field-card">
            <span className="field-label title-with-hint">
              <span>Решение по конфликту порта</span>
              <InlineHint text="Используется только если превью показало, что нужный публичный порт уже занят." />
            </span>
            <select
              className="text-input"
              onChange={(event) => section.onUpdateDeployField('port_conflict_decision', event.target.value)}
              title="Выберите действие только если превью требует решения по занятому публичному порту."
              value={section.deployDraft.port_conflict_decision}
            >
              <option value="">Выбирайте только если этого требует превью</option>
              {section.deployDecisionOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <span className="field-hint">Нужно только если публичный порт MTProto уже занят другим сервисом.</span>
          </label>

          {!section.currentConfig ? <p className="panel-note">Сначала сгенерируйте или сохраните конфиг. Без сохраненной ревизии превью деплоя не запустится.</p> : null}
          {!section.sshAuthReady ? <p className="panel-note">Настройте SSH в блоке «SSH-доступ» перед загрузкой превью деплоя.</p> : null}
          {section.deployDecisionHelp ? <p className="panel-note">{section.deployDecisionHelp}</p> : null}
        </article>

        <article className="deploy-column">
          <div className="deploy-section-header">
            <strong className="title-with-hint">
              <span>Превью</span>
              <InlineHint text="Превью ничего не меняет на сервере. Оно только показывает будущие файлы, команды, порты и риски." />
            </strong>
            <span>{section.deployPreview ? 'Диагностика загружена' : 'Сначала запустите превью'}</span>
          </div>

          {!section.deployPreview ? (
            <p className="panel-note">Запустите превью деплоя после настройки SSH и сохранения конфига, чтобы проверить удаленные файлы, порты, бэкапы, риски и точные команды.</p>
          ) : (
            <div className="deploy-stack">
              <dl className="health-list compact-list">
                <div>
                  <dt>Последняя диагностика</dt>
                  <dd>{section.lastDiagnosticsAt ? section.lastDiagnosticsAtLabel : 'Загружено в текущей сессии'}</dd>
                </div>
                <div>
                  <dt>Папка Telemt на сервере</dt>
                  <dd>{section.deployPreview.remote_base_path}</dd>
                </div>
                <div>
                  <dt>Docker-образ</dt>
                  <dd>{section.deployPreview.docker_image}</dd>
                </div>
                <div>
                  <dt>Требует подтверждения</dt>
                  <dd>{section.deployPreview.requires_confirmation ? 'Да' : 'Нет'}</dd>
                </div>
                <div>
                  <dt>Уже развернутый инстанс панели</dt>
                  <dd>{section.deployPreview.existing_panel_instance ? 'Обнаружен' : 'Не обнаружен'}</dd>
                </div>
              </dl>

              {section.deployPreview.required_decision ? <p className="inline-warning">{section.deployPreview.required_decision.reason}</p> : null}

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>Файлы для загрузки</span>
                  <span>{section.deployPreview.files.length}</span>
                </div>
                <div className="deploy-list">
                  {section.deployPreview.files.map((file) => (
                    <article className="revision-item" key={file.path}>
                      <strong>{file.path}</strong>
                      <span>{file.size_bytes ? `${file.size_bytes} байт` : 'Только директория'}</span>
                      <span>{file.will_backup ? 'Существующий файл будет сохранен в backup' : file.exists ? 'Уже существует без изменения backup' : 'Будет создан'}</span>
                    </article>
                  ))}
                </div>
              </div>

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>Порты</span>
                  <span>{section.deployPreview.ports.length}</span>
                </div>
                <div className="deploy-list">
                  {section.deployPreview.ports.map((port) => (
                    <article className="revision-item" key={`${port.label}-${port.host_address}-${port.host_port}`}>
                      <strong>{port.label}</strong>
                      <span>{`${port.host_address}:${port.host_port} -> container ${port.container_port}`}</span>
                    </article>
                  ))}
                </div>
              </div>

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>Риски</span>
                  <span>{section.deployPreview.risks.length}</span>
                </div>
                {section.deployPreview.risks.length === 0 ? <p className="panel-note">В последнем превью рисков не обнаружено.</p> : null}
                <div className="deploy-list">
                  {section.deployPreview.risks.map((risk) => (
                    <article className={`revision-item deploy-risk ${risk.severity}`} key={`${risk.code}-${risk.message}`}>
                      <strong>{risk.code}</strong>
                      <span>{risk.message}</span>
                      <span>{risk.blocking ? 'Блокирующий риск' : 'Информационный риск'}</span>
                    </article>
                  ))}
                </div>
              </div>

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>Диагностика</span>
                  <span>{section.deployPreview.checks.length}</span>
                </div>
                <div className="deploy-list">
                  {section.deployPreview.checks.map((check) => (
                    <article className="revision-item" key={check.name}>
                      <strong>{`${check.name} · ${check.status}`}</strong>
                      <span>{check.summary}</span>
                      <span>{check.result.stderr || check.result.stdout || 'Вывод отсутствует'}</span>
                    </article>
                  ))}
                </div>
              </div>

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>Команды</span>
                  <span>{section.deployPreview.commands.length}</span>
                </div>
                <div className="deploy-list">
                  {section.deployPreview.commands.map((command) => (
                    <article className="revision-item" key={command}>
                      <span className="mono-text">{command}</span>
                    </article>
                  ))}
                </div>
              </div>
            </div>
          )}
        </article>
      </div>

      <div className="deploy-list-block">
        <div className="revision-header">
          <span>Журнал применения</span>
          <span>{section.deployEvents.length}</span>
        </div>
        {section.deployEvents.length === 0 ? <p className="panel-note">События применения появятся здесь после попытки деплоя.</p> : null}
        <div className="deploy-list">
          {section.deployEvents.map((event) => (
            <article className={`revision-item deploy-event ${event.level}`} key={event.id}>
              <strong>{event.event_type}</strong>
              <span>{event.message}</span>
              <span>{new Date(event.created_at).toLocaleString('ru-RU')}</span>
              {event.stdout ? <span className="mono-text">stdout: {event.stdout}</span> : null}
              {event.stderr ? <span className="mono-text">stderr: {event.stderr}</span> : null}
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
