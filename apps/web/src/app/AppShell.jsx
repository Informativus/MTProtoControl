import { TelegramAlertsSection } from '../features/alerts/TelegramAlertsSection.jsx';
import { ConfigDraftSection } from '../features/config/ConfigDraftSection.jsx';
import { ConfigEditorSection } from '../features/config/ConfigEditorSection.jsx';
import { DeploySection } from '../features/deploy/DeploySection.jsx';
import { HealthSection } from '../features/health/HealthSection.jsx';
import { InventorySection } from '../features/inventory/InventorySection.jsx';
import { OperationsSection } from '../features/operations/OperationsSection.jsx';
import { SshSection } from '../features/ssh/SshSection.jsx';
import { DetailWorkspaceHeader } from '../features/workspace/DetailWorkspaceHeader.jsx';
import { ServerBoard } from '../features/workspace/ServerBoard.jsx';
import { WorkspaceSidebar } from '../features/workspace/WorkspaceSidebar.jsx';
import { useWorkspaceController } from '../features/workspace/useWorkspaceController.js';

export default function AppShell() {
  const {
    view,
    missingServerState,
    emptyState,
    sidebar,
    board,
    detailHeader,
    inventorySection,
    sshSection,
    telegramSection,
    configDraftSection,
    configEditorSection,
    deploySection,
    operationsSection,
    healthSection,
  } = useWorkspaceController();

  const showDetailEmptyState =
    !view.selectedServer &&
    view.isDetailView &&
    (view.shouldRenderSection('config-section') ||
      view.shouldRenderSection('deploy-section') ||
      view.shouldRenderSection('operations-section') ||
      view.shouldRenderSection('health-section'));

  return (
    <div className="app-shell">
      <WorkspaceSidebar sidebar={sidebar} />

      <main className="workspace" ref={view.workspaceRef}>
        {view.isBoardView ? (
          <ServerBoard board={board} />
        ) : (
          <section className="detail-workspace-shell" aria-label="Рабочая зона сервера">
            {!view.selectedServer ? (
              <article className="panel detail-workspace-panel">
                <div className="board-empty-state board-focus-empty">
                  <p>Сервер из маршрута не найден в текущем инвентаре. Вернитесь на доску и выберите актуальную карточку.</p>
                  <button className="primary-button" onClick={() => missingServerState.onBackToBoard({ replace: true })} title="Вернуться к общей доске серверов." type="button">
                    Вернуться к доске
                  </button>
                </div>
              </article>
            ) : (
              <DetailWorkspaceHeader header={detailHeader} />
            )}
          </section>
        )}

        {view.isBoardView && view.inventoryFormVisible ? <InventorySection section={inventorySection} /> : null}

        {view.isDetailView && view.shouldRenderSection('ssh-section') ? <SshSection section={sshSection} /> : null}

        {view.isDetailView && view.shouldRenderSection('telegram-section') ? <TelegramAlertsSection section={telegramSection} /> : null}

        {!view.selectedServer ? (
          showDetailEmptyState ? (
            <section className="panel empty-panel">
              <div>
                <p className="eyebrow">Нет цели</p>
                <h2>Нужен сервер</h2>
              </div>
              <p>
                Сначала добавьте сервер в инвентарь. Затем подтвердите SSH-доступ, и редактор конфига унаследует `public_host`, `mtproto_port` и `sni_domain` из сохраненной записи.
              </p>
              <div className="button-row">
                <button className="primary-button" disabled={emptyState.inventoryBusy} onClick={emptyState.onCreateServer} title="Создать новую запись сервера в инвентаре." type="button">
                  Добавить сервер
                </button>
              </div>
            </section>
          ) : null
        ) : (
          <>
            {view.isDetailView && view.shouldRenderSection('config-section') ? <ConfigDraftSection section={configDraftSection} /> : null}
            {view.isDetailView && view.shouldRenderSection('config-section') ? <ConfigEditorSection section={configEditorSection} /> : null}
            {view.isDetailView && view.shouldRenderSection('deploy-section') ? <DeploySection section={deploySection} /> : null}
            {view.isDetailView && view.shouldRenderSection('operations-section') ? <OperationsSection section={operationsSection} /> : null}
            {view.isDetailView && view.shouldRenderSection('health-section') ? <HealthSection section={healthSection} /> : null}
          </>
        )}
      </main>
    </div>
  );
}
