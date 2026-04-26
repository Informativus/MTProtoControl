import { formatDateTime, normalizeHealthState } from '../../shared/lib/workspace-formatters.js';
import { ButtonLabel, MetaChip, AppIcon } from '../../shared/ui/app-ui.jsx';

export function DetailWorkspaceHeader({ header }) {
  const nextStepTitle = `Перейти к следующему рекомендуемому шагу: ${header.nextOperatorStepLabel}.`;

  return (
    <article className="panel detail-workspace-panel">
      <div className="detail-workspace-header">
        <div>
          <p className="eyebrow">Рабочая зона</p>
          <h1>{header.selectedServer.name}</h1>
        </div>

        <div className="detail-workspace-actions">
          <button className="secondary-button workspace-action-button" onClick={() => header.onBackToBoard()} title="Вернуться к общей доске серверов." type="button">
            <ButtonLabel icon="arrow-left">К доске</ButtonLabel>
          </button>
          <span className={`workspace-action-badge ${normalizeHealthState(header.currentHealth?.status || header.selectedServer.status)}`}>
            {header.healthSummary}
          </span>
          <button
            className="secondary-button workspace-action-button workspace-action-accent"
            onClick={() => header.onOpenSection(header.nextOperatorStepTarget)}
            title={nextStepTitle}
            type="button"
          >
            <ButtonLabel icon="workflow">{`Следующий шаг: ${header.nextOperatorStepLabel}`}</ButtonLabel>
          </button>
          <button className="secondary-button workspace-action-button" onClick={() => header.onRefreshStatus()} title="Проверить контейнер, Telemt API и текущую ссылку на сервере." type="button">
            <ButtonLabel icon="refresh">Обновить статус</ButtonLabel>
          </button>
          <button className="secondary-button workspace-action-button" onClick={header.onCopyLink} title="Скопировать текущую MTProto-ссылку из панели." type="button">
            <ButtonLabel icon="copy">Скопировать ссылку</ButtonLabel>
          </button>
          <button className="secondary-button workspace-action-button" onClick={header.onEditServer} title="Изменить сохраненные поля выбранного сервера." type="button">
            <ButtonLabel icon="edit">Редактировать сервер</ButtonLabel>
          </button>
          <button
            className="secondary-button workspace-action-button workspace-action-danger"
            disabled={header.inventoryBusy}
            onClick={header.onDeleteServer}
            title="Удалить сервер и связанные данные из панели."
            type="button"
          >
            <ButtonLabel icon="trash">Удалить сервер</ButtonLabel>
          </button>
        </div>
      </div>

      <div className="board-focus-meta detail-workspace-meta">
        <MetaChip icon="globe">{header.selectedServerAddress}</MetaChip>
        <MetaChip icon="key">{header.selectedServerSshTarget}</MetaChip>
        <MetaChip icon="folder">{header.selectedServer.remote_base_path}</MetaChip>
        <MetaChip icon="key">{header.savedPrivateKeyPath || 'SSH key не сохранен'}</MetaChip>
        <MetaChip icon="file-code">{header.currentConfig ? `Ревизия v${header.currentConfig.version}` : 'Конфиг еще не сохранен'}</MetaChip>
        <MetaChip icon="refresh">{header.lastDiagnosticsAt ? `Preview ${formatDateTime(header.lastDiagnosticsAt)}` : 'Preview не запускался'}</MetaChip>
        <MetaChip icon="pulse">{header.currentHealth?.created_at ? `Health ${formatDateTime(header.currentHealth.created_at)}` : 'Health еще не записан'}</MetaChip>
      </div>

      <nav className="section-switcher detail-section-switcher" aria-label="Разделы рабочей зоны">
        {header.detailSectionTabs.map((section) => {
          const isActive = header.activeSectionId === section.id && !header.showAllSections;

          return (
            <button className={`section-tab state-${section.state} ${isActive ? 'active' : ''}`} key={section.id} onClick={() => header.onOpenSection(section.id)} title={`Открыть раздел «${section.label}».`} type="button">
              <div className="section-tab-header">
                <span className={`section-tab-icon state-${section.state}`}>
                  <AppIcon name={section.icon} size={17} />
                </span>
                <span className={`section-tab-state state-${section.state}`}>{section.statusLabel}</span>
              </div>
              <strong>{section.label}</strong>
            </button>
          );
        })}
      </nav>
    </article>
  );
}
