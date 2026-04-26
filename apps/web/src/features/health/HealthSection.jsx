import { describeHealthFlags, describeHealthMessage, describeHealthProblem } from '../../health-messages.js';
import { formatDateTime, formatHealthState, normalizeHealthState } from '../../shared/lib/workspace-formatters.js';

export function HealthSection({ section }) {
  return (
    <section className="panel operations-panel" id="health-section">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Шаг 7</p>
          <h2>Фоновые проверки</h2>
        </div>

        <div className="button-row">
          <span className={`status-chip ${normalizeHealthState(section.currentHealth?.status || section.selectedServer.status)}`}>
            {formatHealthState(section.currentHealth?.status || section.selectedServer.status)}
          </span>
          <span className="panel-note compact-note">Интервал {section.healthSettings.interval}</span>
        </div>
      </div>

      {section.healthError ? <p className="inline-error">{section.healthError}</p> : null}

      <div className="operations-grid">
        <article className="operations-column">
          <div className="link-card">
            <span className="preview-label">Текущее состояние воркера</span>
            <code>{section.currentHealth ? formatHealthState(section.currentHealth.status) : 'Неизвестно'}</code>
            <div className="link-meta-row">
              <span>{section.currentHealth ? describeHealthFlags(section.currentHealth) : 'Флаги проверки пока не записаны.'}</span>
              <span>{section.currentHealth?.created_at ? formatDateTime(section.currentHealth.created_at) : 'Еще не проверялось'}</span>
            </div>
          </div>

          <dl className="health-list compact-list">
            <div>
              <dt>Последняя проверка</dt>
              <dd>{section.currentHealth?.created_at ? formatDateTime(section.currentHealth.created_at) : 'Никогда'}</dd>
            </div>
            <div>
              <dt>Последняя проблема</dt>
              <dd>{describeHealthProblem(section.currentHealth)}</dd>
            </div>
            <div>
              <dt>Сводка воркера</dt>
              <dd>{describeHealthMessage(section.currentHealth)}</dd>
            </div>
            <div>
              <dt>TCP задержка</dt>
              <dd>{section.currentHealth?.latency_ms != null ? `${section.currentHealth.latency_ms} мс` : 'Не записано'}</dd>
            </div>
            <div>
              <dt>Сохраненный интервал</dt>
              <dd>{section.healthSettings.interval}</dd>
            </div>
          </dl>
        </article>

        <article className="operations-column">
          <div className="deploy-section-header">
            <strong>История проверок</strong>
            <span>{section.healthLoading ? 'Загрузка' : `${section.healthHistory.length} проверок`}</span>
          </div>

          {section.healthLoading ? <p className="panel-note">Загрузка истории проверок воркера...</p> : null}
          {section.healthHistory.length === 0 && !section.healthLoading ? (
            <p className="panel-note">Фоновые проверки пока не записаны. Планировщик начнет сохранять их автоматически после запуска API.</p>
          ) : null}

          <div className="deploy-list">
            {section.healthHistory.map((check) => (
              <article className={`revision-item health-history-item ${normalizeHealthState(check.status)}`} key={check.id}>
                <div className="history-item-header">
                  <strong>{formatHealthState(check.status)}</strong>
                  <span>{formatDateTime(check.created_at)}</span>
                </div>
                <span>{describeHealthMessage(check)}</span>
                <span>{describeHealthFlags(check)}</span>
                <span>{check.latency_ms != null ? `TCP задержка ${check.latency_ms} мс` : 'TCP задержка не записана'}</span>
              </article>
            ))}
          </div>
        </article>
      </div>
    </section>
  );
}
