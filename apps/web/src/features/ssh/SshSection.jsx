import { ButtonLabel } from '../../shared/ui/app-ui.jsx';
import { formatDateTime } from '../../shared/lib/workspace-formatters.js';

export function SshSection({ section }) {
  return (
    <section className="panel ssh-panel" id="ssh-section">
      <div className="panel-header">
        <div>
          <p className="eyebrow">Шаг 2</p>
          <h2>SSH-доступ и сведения о хосте</h2>
        </div>

        <div className="button-row">
          <button className="primary-button" disabled={!section.selectedServer || section.sshTestBusy} onClick={section.onTestSSH} title="Проверить SSH-подключение и собрать сведения о хосте." type="button">
            <ButtonLabel icon="key">{section.sshTestBusy ? 'Проверка...' : 'Проверить SSH'}</ButtonLabel>
          </button>
        </div>
      </div>

      {section.sshTestError ? <p className="inline-error">{section.sshTestError}</p> : null}
      {section.sshTestNotice ? <p className="inline-success">{section.sshTestNotice}</p> : null}

      {!section.selectedServer ? (
        <p className="panel-note">Сначала добавьте или выберите сервер, а уже потом настраивайте SSH и запускайте проверку подключения.</p>
      ) : (
        <div className="operations-grid ssh-grid">
          <article className="operations-column">
            <label className="field-card">
              <span className="field-label">Способ входа</span>
              <select className="text-input" onChange={(event) => section.onUpdateDeployField('auth_type', event.target.value)} value={section.deployDraft.auth_type}>
                <option value="password">Пароль пользователя</option>
                <option value="private_key_path">Путь к приватному ключу</option>
                <option value="private_key_text">Текст приватного ключа</option>
              </select>
              <span className="field-hint">Для локальной работы и повторяемых действий удобнее использовать путь к ключу на машине API. Пароль не сохраняется в панели.</span>
            </label>

            {section.deployDraft.auth_type === 'password' ? (
              <label className="field-card">
                <span className="field-label">Пароль SSH</span>
                <input
                  className="text-input"
                  onChange={(event) => section.onUpdateDeployField('password', event.target.value)}
                  placeholder="Не сохраняется"
                  type="password"
                  value={section.deployDraft.password}
                />
                <span className="field-hint">Отправляется только в текущем POST-запросе. Для логов, SSE и live-статуса все равно нужен private_key_path.</span>
              </label>
            ) : section.deployDraft.auth_type === 'private_key_path' ? (
              <label className="field-card">
                <span className="field-label">Путь к приватному ключу</span>
                <input
                  className="text-input"
                  onChange={(event) => section.onUpdateDeployField('private_key_path', event.target.value)}
                  placeholder="~/.ssh/proxy-node"
                  type="text"
                  value={section.deployDraft.private_key_path}
                />
                <span className="field-hint">Путь разворачивается на хосте API перед началом SSH-подключения.</span>
              </label>
            ) : (
              <label className="field-card">
                <span className="field-label">Текст приватного ключа</span>
                <textarea
                  className="code-editor compact-editor"
                  onChange={(event) => section.onUpdateDeployField('private_key_text', event.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  spellCheck="false"
                  value={section.deployDraft.private_key_text}
                />
                <span className="field-hint">Отправляется только с текущим запросом. Не держите raw-ключ в браузере дольше необходимого.</span>
              </label>
            )}

            {section.deployDraft.auth_type !== 'password' ? (
              <label className="field-card">
                <span className="field-label">Парольная фраза</span>
                <input
                  className="text-input"
                  onChange={(event) => section.onUpdateDeployField('passphrase', event.target.value)}
                  placeholder="Необязательно"
                  type="password"
                  value={section.deployDraft.passphrase}
                />
                <span className="field-hint">Нужна только для зашифрованных SSH-ключей.</span>
              </label>
            ) : null}

            {!section.sshAuthReady ? <p className="panel-note">Сначала настройте SSH. Эти же данные используются для превью деплоя и SSH-команд на сервере.</p> : null}
            {section.deployDraft.auth_type === 'password' ? <p className="panel-note">Парольный вход работает для POST-операций. Для live-логов, SSE и live-статуса сохраните <code>private_key_path</code>.</p> : null}
          </article>

          <article className="operations-column">
            <div className="link-card">
              <span className="preview-label">Сохраненный путь к ключу</span>
              <code>{section.savedPrivateKeyPath || 'Путь private_key_path еще не сохранен.'}</code>
              <div className="link-meta-row">
                <span>
                  {section.savedPrivateKeyPathUpdatedAt
                    ? `Сохранено ${formatDateTime(section.savedPrivateKeyPathUpdatedAt)}`
                    : 'Будет сохранен после SSH-проверки, превью или деплоя с path-auth'}
                </span>
                <span>{section.lastSshTestAt ? `Последняя проверка ${formatDateTime(section.lastSshTestAt)}` : 'В этой сессии проверка еще не запускалась'}</span>
              </div>
            </div>

            <dl className="health-list compact-list">
              <div>
                <dt>Целевой хост</dt>
                <dd>{`${section.selectedServer.ssh_user}@${section.selectedServer.host}:${section.selectedServer.ssh_port}`}</dd>
              </div>
              <div>
                <dt>Имя хоста</dt>
                <dd>{section.sshTestResult?.facts?.hostname || 'Запустите SSH-проверку, чтобы получить сведения о хосте'}</dd>
              </div>
              <div>
                <dt>Текущий пользователь</dt>
                <dd>{section.sshTestResult?.facts?.current_user || 'Еще не загружено'}</dd>
              </div>
              <div>
                <dt>Архитектура</dt>
                <dd>{section.sshTestResult?.facts?.architecture || 'Еще не загружено'}</dd>
              </div>
              <div>
                <dt>Docker</dt>
                <dd>{section.sshTestResult?.facts?.docker_version || 'Запустите SSH-проверку, чтобы проверить Docker'}</dd>
              </div>
              <div>
                <dt>Docker Compose</dt>
                <dd>{section.sshTestResult?.facts?.docker_compose_version || 'Запустите SSH-проверку, чтобы проверить Compose'}</dd>
              </div>
            </dl>

            <div className="deploy-list-block">
              <div className="revision-header">
                <span>Команды SSH-проверки</span>
                <span>{section.sshTestResult?.commands?.length || 0}</span>
              </div>
              {section.sshTestResult?.commands?.length ? (
                <div className="deploy-list">
                  {section.sshTestResult.commands.map((command) => (
                    <article className={`revision-item status-command ${command.ok ? 'ok' : 'failed'}`} key={`${command.name}-${command.command}`}>
                      <strong>{command.name}</strong>
                      <span>{command.command}</span>
                      <span>{command.stderr || command.stdout || 'Вывод команды отсутствует'}</span>
                    </article>
                  ))}
                </div>
              ) : (
                <p className="panel-note">Запустите SSH-проверку, чтобы получить имя хоста, архитектуру, результаты команд и проверить Docker до превью деплоя.</p>
              )}
            </div>
          </article>
        </div>
      )}
    </section>
  );
}
