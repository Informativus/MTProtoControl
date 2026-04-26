import { AppIcon, ButtonLabel } from '../../shared/ui/app-ui.jsx';
import { formatHealthState, normalizeHealthState } from '../../shared/lib/workspace-formatters.js';

export function ServerBoard({ board }) {
  function handleBoardCardKeyDown(event, serverId) {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return;
    }

    event.preventDefault();
    board.onOpenServer(serverId);
  }

  return (
    <section className="server-board-shell" aria-label="Серверная доска">
      <article className="panel server-board-panel">
        <div className="board-toolbar">
          <div>
            <h1>Серверная доска</h1>
          </div>

          <div className="board-toolbar-actions">
            <div className="board-stat-list" aria-label="Сводка по доске">
              <span className="board-stat-chip">{`Серверов ${board.servers.length}`}</span>
            </div>

            <button className="primary-button" onClick={board.onCreateServer} title="Создать новую запись сервера в инвентаре." type="button">
              <ButtonLabel icon="plus">Добавить сервер</ButtonLabel>
            </button>

            <div className={`api-pill ${board.apiState.state}`}>
              <span className="status-dot" aria-hidden="true" />
              <span>{board.apiState.label}</span>
              <small>{board.apiState.detail}</small>
            </div>
          </div>
        </div>

        {board.operationError ? <p className="inline-error">{board.operationError}</p> : null}
        {board.operationNotice ? <p className="inline-success">{board.operationNotice}</p> : null}
        {board.serversLoading ? <p className="panel-note">Загрузка инвентаря...</p> : null}
        {!board.serversLoading && board.serversError ? <p className="inline-error">{board.serversError}</p> : null}
        {!board.serversLoading && !board.serversError && board.servers.length === 0 ? (
          <div className="board-empty-state">
            <p>На доске пока нет серверов. Добавьте первый хост и затем подтяните существующий Telemt прямо из UI.</p>
          </div>
        ) : null}

        <div className="server-board-grid">
          {board.servers.map((server) => {
            const isActive = server.id === board.selectedServerId;
            const serverHealthState = normalizeHealthState(server.status);

            return (
              <article
                className={`server-board-card ${isActive ? 'active' : ''}`}
                key={server.id}
                onClick={() => board.onOpenServer(server.id)}
                onKeyDown={(event) => handleBoardCardKeyDown(event, server.id)}
                role="button"
                tabIndex={0}
                title={`Открыть рабочую зону сервера ${server.name}.`}
              >
                <div className="server-board-header">
                  <strong>{server.name}</strong>
                  <span className={`status-chip ${serverHealthState}`}>{formatHealthState(server.status)}</span>
                </div>
                <span className="server-board-route">{`${server.public_host || server.host}:${server.mtproto_port || 443}`}</span>
                <span className="server-board-caption" title={`${server.ssh_user}@${server.host}:${server.ssh_port}`}>
                  {`${server.ssh_user}@${server.host}:${server.ssh_port}`}
                </span>
                <span className="server-board-path" title={server.remote_base_path}>
                  {server.remote_base_path}
                </span>
                <div className="server-board-meta">
                  <span className="meta-chip">{server.sni_domain || 'SNI не задан'}</span>
                  <span className="meta-chip">{server.saved_private_key_path ? 'SSH key сохранен' : 'SSH key не сохранен'}</span>
                </div>

                <div className="server-board-actions">
                  <button
                    className="primary-button server-board-open-button"
                    onClick={(event) => {
                      event.stopPropagation();
                      board.onOpenServer(server.id);
                    }}
                    title={`Открыть рабочую зону сервера ${server.name}.`}
                    type="button"
                  >
                    Открыть
                  </button>
                  <button
                    aria-label={`Скопировать MTProto-ссылку сервера ${server.name}`}
                    className="secondary-button icon-button"
                    onClick={(event) => {
                      event.stopPropagation();
                      void board.onCopyLink(server);
                    }}
                    title={`Скопировать MTProto-ссылку сервера ${server.name}.`}
                    type="button"
                  >
                    <AppIcon name="copy" size={16} />
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      </article>
    </section>
  );
}
