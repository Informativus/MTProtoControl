import { releaseVersion } from '../../release.js';
import { formatHealthState, normalizeHealthState } from '../../shared/lib/workspace-formatters.js';
import { AppIcon, ButtonLabel } from '../../shared/ui/app-ui.jsx';

export function WorkspaceSidebar({ sidebar }) {
  return (
    <aside className="sidebar">
      <button className="brand" onClick={() => sidebar.onOpenBoard()} title="Вернуться к общей доске серверов." type="button">
        <span className="brand-mark" aria-hidden="true">
          <img alt="" src="/brand-mark.svg" />
        </span>
        <div>
          <div className="brand-title">MTProxy Control</div>
          <p className="sidebar-caption">{releaseVersion}</p>
        </div>
      </button>

      <section className="sidebar-section">
        <div className="sidebar-section-header">
          <p className="sidebar-title">Серверы</p>
          <div className="sidebar-section-actions">
            <span className="sidebar-meta">{sidebar.servers.length}</span>
            <button className="sidebar-button" onClick={sidebar.onCreateServer} title="Создать новую запись сервера в инвентаре." type="button">
              <ButtonLabel icon="plus">Добавить сервер</ButtonLabel>
            </button>
          </div>
        </div>

        {sidebar.serversLoading ? <p className="sidebar-note">Загрузка инвентаря...</p> : null}
        {!sidebar.serversLoading && sidebar.serversError ? <p className="sidebar-note error-text">{sidebar.serversError}</p> : null}
        {!sidebar.serversLoading && !sidebar.serversError && sidebar.servers.length === 0 ? (
          <p className="sidebar-note">Добавьте первый сервер, чтобы открыть конфиг, деплой и операционные действия прямо из браузера.</p>
        ) : null}

        <div className="server-list" role="list">
          {sidebar.visibleServers.map((server) => {
            const isActive = server.id === sidebar.selectedServerId;
            const serverHealthState = normalizeHealthState(server.status);
            const serverHealthLabel = formatHealthState(server.status);
            return (
              <button
                className={`server-item ${isActive ? 'active' : ''}`}
                key={server.id}
                onClick={() => sidebar.onSelectServer(server.id)}
                title={`Открыть сервер ${server.name}.`}
                type="button"
              >
                <div className="server-item-row">
                  <div className="server-item-title">
                    <AppIcon name="server" size={14} />
                    <strong>{server.name}</strong>
                  </div>
                  <span
                    aria-label={`Статус: ${serverHealthLabel}`}
                    className={`server-item-status-dot ${serverHealthState}`}
                    role="img"
                    title={`Статус: ${serverHealthLabel}`}
                  />
                </div>
              </button>
            );
          })}
        </div>

        {sidebar.servers.length > sidebar.visibleLimit ? (
          <div className="server-list-controls" aria-label="Навигация по списку серверов">
            <button
              aria-label="Показать предыдущие серверы"
              className="server-list-nav-button"
              disabled={!sidebar.canPageUp}
              onClick={sidebar.onPageUp}
              title="Показать предыдущие серверы"
              type="button"
            >
              <AppIcon name="chevron-up" size={16} />
            </button>
            <span className="server-list-window">{`${sidebar.visibleRangeStart}-${sidebar.visibleRangeEnd} из ${sidebar.servers.length}`}</span>
            <button
              aria-label="Показать следующие серверы"
              className="server-list-nav-button"
              disabled={!sidebar.canPageDown}
              onClick={sidebar.onPageDown}
              title="Показать следующие серверы"
              type="button"
            >
              <AppIcon name="chevron-down" size={16} />
            </button>
          </div>
        ) : null}
      </section>
    </aside>
  );
}
