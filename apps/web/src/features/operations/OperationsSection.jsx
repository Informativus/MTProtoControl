import {
  describeContainerSummary,
  describePublicPortSummary,
  describeTelemtApiSummary,
  LOGS_PAGE_SIZE_OPTIONS,
  LOGS_WINDOW_OPTIONS,
} from '../../server-operations.js';
import { describeHealthMessage } from '../../health-messages.js';
import { formatDateTime, formatHealthState, formatLogsStreamState } from '../../shared/lib/workspace-formatters.js';
import { ButtonLabel } from '../../shared/ui/app-ui.jsx';

export function OperationsSection({ section }) {
  const loadLogsTitle = `Загрузить только часть журнала: последние ${section.logsWindowSize} строк логов Telemt по SSH.`;
  const toggleLogsStreamTitle = section.logsLiveEnabled
    ? `Остановить live-стрим логов через SSE. Сейчас удерживается окно до ${section.logsWindowSize} строк.`
    : `Запустить live-стрим логов через SSE. В браузер будет приходить только окно до ${section.logsWindowSize} строк.`;

  return (
    <section className="panel operations-panel" id="operations-section">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Шаг 6</p>
          <h2>Перезапуск, статус и логи в реальном времени</h2>
        </div>

        <div className="button-row">
          <button className="secondary-button" disabled={section.statusLoading} onClick={() => section.onLoadStatus()} title="Проверить контейнер, Telemt API и текущую ссылку на сервере." type="button">
            <ButtonLabel icon="refresh">{section.statusLoading ? 'Обновление...' : 'Обновить статус'}</ButtonLabel>
          </button>
          <button className="secondary-button" onClick={section.onCopyLink} title="Скопировать текущую MTProto-ссылку из панели." type="button">
            <ButtonLabel icon="copy">Скопировать ссылку</ButtonLabel>
          </button>
          <button className="primary-button" disabled={section.restartBusy} onClick={section.onRestartServer} title="Перезапустить контейнер Telemt на сервере без изменения файлов." type="button">
            {section.restartBusy ? 'Перезапуск...' : 'Перезапустить Telemt'}
          </button>
        </div>
      </div>

      {section.statusError ? <p className="inline-error">{section.statusError}</p> : null}
      {section.statusNotice ? <p className="inline-success">{section.statusNotice}</p> : null}
      {section.operationError ? <p className="inline-error">{section.operationError}</p> : null}
      {section.operationNotice ? <p className="inline-success">{section.operationNotice}</p> : null}

      <div className="operations-grid">
        <article className="operations-column">
          <div className="deploy-section-header">
            <strong>Снимок статуса</strong>
            <span>{section.statusLoading ? 'Обновление' : 'Последнее известное состояние'}</span>
          </div>

          {section.linkInfo?.warning ? <p className="panel-note">{section.linkInfo.warning}</p> : null}
          {!section.operationQueryAuthReady ? <p className="panel-note">{section.operationAuthHelp}</p> : null}

          <dl className="health-list compact-list">
            <div>
              <dt>Путь к ключу</dt>
              <dd>{section.savedPrivateKeyPath || 'Еще не сохранен'}</dd>
            </div>
            <div>
              <dt>Последняя проверка</dt>
              <dd>{section.currentHealth ? formatHealthState(section.currentHealth.status) : 'Фоновый воркер еще не записал проверку'}</dd>
            </div>
            <div>
              <dt>Контейнер</dt>
              <dd>{describeContainerSummary(section.serverStatus?.container) || 'Загрузите статус, чтобы проверить удаленный контейнер Telemt.'}</dd>
            </div>
            <div>
              <dt>Telemt API</dt>
              <dd>{describeTelemtApiSummary(section.serverStatus?.telemt_api) || 'Загрузите статус, чтобы проверить live API, пользователей и ссылку.'}</dd>
            </div>
            <div>
              <dt>Публичный порт</dt>
              <dd>
                {section.serverStatus
                  ? `${describePublicPortSummary(section.serverStatus.public_port)}${section.serverStatus.public_port.target ? ` (${section.serverStatus.public_port.target})` : ''}`
                  : 'Еще не проверен'}
              </dd>
            </div>
            <div>
              <dt>Доступность</dt>
              <dd>
                {section.serverStatus?.public_port?.checked
                  ? section.serverStatus.public_port.reachable
                    ? `Доступен за ${section.serverStatus.public_port.latency_ms || 0} мс`
                    : section.serverStatus.public_port.error || 'Недоступен'
                  : 'Пропущено'}
              </dd>
            </div>
            <div>
              <dt>Комментарий воркера</dt>
              <dd>{describeHealthMessage(section.currentHealth)}</dd>
            </div>
          </dl>

          <div className="operations-stack">
            {section.serverStatus?.container?.result ? (
              <article className={`revision-item status-command ${section.serverStatus.container.status}`}>
                <strong>{`container · ${section.serverStatus.container.status}`}</strong>
                <span>{section.serverStatus.container.result.command}</span>
                <span>{section.serverStatus.container.result.stderr || section.serverStatus.container.result.stdout || 'Вывод команды отсутствует'}</span>
              </article>
            ) : null}
            {section.serverStatus?.telemt_api?.result ? (
              <article className={`revision-item status-command ${section.serverStatus.telemt_api.status}`}>
                <strong>{`telemt_api · ${section.serverStatus.telemt_api.status}`}</strong>
                <span>{section.serverStatus.telemt_api.result.command}</span>
                <span>{section.serverStatus.telemt_api.result.stderr || section.serverStatus.telemt_api.result.stdout || 'Вывод команды отсутствует'}</span>
              </article>
            ) : null}
          </div>
        </article>

        <article className="operations-column">
          <div className="deploy-section-header">
            <strong>Логи</strong>
            <span>{section.logsWindowSummary}</span>
          </div>

          <div className="button-row">
            <button className="secondary-button" disabled={section.logsLoading || !section.operationQueryAuthReady} onClick={() => section.onLoadLogs()} title={loadLogsTitle} type="button">
              {section.logsLoading ? 'Загрузка...' : 'Загрузить логи'}
            </button>
            <button
              className="secondary-button"
              disabled={!section.operationQueryAuthReady}
              onClick={() => section.setLogsLiveEnabled((current) => !current)}
              title={toggleLogsStreamTitle}
              type="button"
            >
              {section.logsLiveEnabled ? 'Остановить стрим' : 'Запустить стрим'}
            </button>
          </div>

          <div className="logs-settings-grid">
            <label className="logs-setting">
              <span>Окно журнала</span>
              <select className="text-input" onChange={(event) => section.onChangeLogsWindowSize(event.target.value)} value={section.logsWindowSize}>
                {LOGS_WINDOW_OPTIONS.map((option) => (
                  <option key={option} value={option}>
                    {`${option} строк`}
                  </option>
                ))}
              </select>
            </label>

            <label className="logs-setting">
              <span>На странице</span>
              <select className="text-input" onChange={(event) => section.onChangeLogsPageSize(event.target.value)} value={section.logsPageSize}>
                {LOGS_PAGE_SIZE_OPTIONS.map((option) => (
                  <option key={option} value={option}>
                    {`${option} строк`}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {section.logsError ? <p className="inline-error">{section.logsError}</p> : null}
          {section.logsNotice ? <p className="inline-success">{section.logsNotice}</p> : null}
          {!section.operationQueryAuthReady ? <p className="panel-note">{section.operationAuthHelp}</p> : null}
          {section.operationQueryAuthReady ? <p className="panel-note">{section.logsCoverageNote}</p> : null}

          <div className="logs-toolbar">
            <span className={`status-chip ${section.logsStreamState}`}>{formatLogsStreamState(section.logsStreamState)}</span>
            <span>{section.logsData?.fetched_at ? new Date(section.logsData.fetched_at).toLocaleString('ru-RU') : 'Логи еще не загружались'}</span>
            <span>{section.logsPageSummary}</span>
          </div>

          <div className="logs-pager">
            <div className="logs-pager-actions">
              <button className="secondary-button" disabled={!section.logsPage.hasPrevious} onClick={() => section.setLogsPageIndex((current) => Math.max(0, current - 1))} type="button">
                Предыдущая страница
              </button>
              <button
                className="secondary-button"
                disabled={!section.logsPage.hasNext}
                onClick={() => section.setLogsPageIndex((current) => Math.min(section.logsPage.totalPages - 1, current + 1))}
                type="button"
              >
                Следующая страница
              </button>
            </div>
            <span>{section.logsPage.totalLines > 0 ? `Показан диапазон ${section.logsPage.startLine}-${section.logsPage.endLine}.` : 'Диапазон появится после первой загрузки.'}</span>
          </div>

          <pre className="logs-output" ref={section.logsOutputRef}>
            {section.logsPage.text || 'Загрузите логи или запустите SSE-стрим, чтобы посмотреть текущий вывод контейнера Telemt.'}
          </pre>

          {section.logsData?.result?.stderr ? <p className="panel-note mono-text">stderr: {section.logsData.result.stderr}</p> : null}
        </article>
      </div>

      <div className="deploy-list-block">
        <div className="revision-header">
          <span>История операционных событий</span>
          <span>{section.operationEvents.length}</span>
        </div>
        {section.operationEvents.length === 0 ? <p className="panel-note">События перезапуска появятся здесь после выполнения действия.</p> : null}
        <div className="deploy-list">
          {section.operationEvents.map((event) => (
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
