import { useEffect, useRef, useState } from 'react';

import {
  createDeployDraft,
  deployDecisionOptions,
  getDeployDecisionHelp,
  hasBlockingRisks,
  serializeDeployRequest,
  serializeSSHAuthFields,
} from '../../deploy.js';
import { canUseOperationQueryAuth, getOperationAuthHelp } from '../../server-operations.js';
import { buildOperatorSteps, hasConfiguredSSHAuth } from '../../operator-flow.js';
import { buildPreviewLink, createDraftFields, getTLSDomainWarning, serializeDraftFields } from '../../telemt-config.js';
import {
  createServerDraft,
  getApiErrorDetails,
  getNextSelectedServerId,
  serializeServerDraft,
} from '../../server-inventory.js';
import { configsApi } from '../../api/configsApi.js';
import { deployApi } from '../../api/deployApi.js';
import { getErrorPayload } from '../../api/http.js';
import { operationsApi } from '../../api/operationsApi.js';
import { serversApi } from '../../api/serversApi.js';
import {
  applyDiscoveryToConfigDraft,
  applyDiscoveryToInventoryDraft,
  buildDiscoveryServerPatch,
  hasDiscoveredConfigFields,
  hasDiscoveredConfigText,
  shouldSaveDiscoveredConfig,
} from '../../mtproto-import.js';
import { readWorkspaceRoute, useWorkspaceRoute } from '../../workspace-router.js';
import {
  formatDateTime,
  formatHealthState,
  formatStepState,
  scheduleViewportUpdate,
} from '../../shared/lib/workspace-formatters.js';
import { useTelegramAlertsController } from '../alerts/useTelegramAlertsController.js';
import { useServerHealth } from '../health/useServerHealth.js';
import { useLogsController } from '../operations/useLogsController.js';
import { sectionIconNames, sidebarVisibleServerLimit } from './workspace-constants.js';
import { useApiHealth } from './useApiHealth.js';

export function useWorkspaceController() {
  const [servers, setServers] = useState([]);
  const [serversLoading, setServersLoading] = useState(true);
  const [serversError, setServersError] = useState('');
  const [inventoryMode, setInventoryMode] = useState('');
  const [inventoryDraft, setInventoryDraft] = useState(createServerDraft());
  const [inventoryFieldErrors, setInventoryFieldErrors] = useState({});
  const [inventoryBusy, setInventoryBusy] = useState(false);
  const [inventoryDetectBusy, setInventoryDetectBusy] = useState(false);
  const [inventoryError, setInventoryError] = useState('');
  const [inventoryNotice, setInventoryNotice] = useState('');
  const [inventoryImportStatus, setInventoryImportStatus] = useState('idle');
  const [inventoryImportMessage, setInventoryImportMessage] = useState('');
  const [inventoryImportDiscovery, setInventoryImportDiscovery] = useState(null);
  const [pendingConfigImport, setPendingConfigImport] = useState(null);
  const [selectedServerId, setSelectedServerId] = useState('');
  const [sidebarServerOffset, setSidebarServerOffset] = useState(0);
  const [currentConfig, setCurrentConfig] = useState(null);
  const [revisions, setRevisions] = useState([]);
  const [draftTemplate, setDraftTemplate] = useState(createDraftFields());
  const [draftFields, setDraftFields] = useState(createDraftFields());
  const [editorText, setEditorText] = useState('');
  const [configLoading, setConfigLoading] = useState(false);
  const [configSyncBusy, setConfigSyncBusy] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [configError, setConfigError] = useState('');
  const [configWarning, setConfigWarning] = useState('');
  const [configNotice, setConfigNotice] = useState('');
  const [deployDraft, setDeployDraft] = useState(createDeployDraft());
  const [sshTestResult, setSshTestResult] = useState(null);
  const [sshTestBusy, setSshTestBusy] = useState(false);
  const [sshTestError, setSshTestError] = useState('');
  const [sshTestNotice, setSshTestNotice] = useState('');
  const [lastSshTestAt, setLastSshTestAt] = useState('');
  const [lastDiagnosticsAt, setLastDiagnosticsAt] = useState('');
  const [deployPreview, setDeployPreview] = useState(null);
  const [deployEvents, setDeployEvents] = useState([]);
  const [deployBusy, setDeployBusy] = useState(false);
  const [deployError, setDeployError] = useState('');
  const [deployNotice, setDeployNotice] = useState('');
  const [serverStatus, setServerStatus] = useState(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [statusError, setStatusError] = useState('');
  const [statusNotice, setStatusNotice] = useState('');
  const [linkInfo, setLinkInfo] = useState(null);
  const [restartBusy, setRestartBusy] = useState(false);
  const [operationEvents, setOperationEvents] = useState([]);
  const [operationError, setOperationError] = useState('');
  const [operationNotice, setOperationNotice] = useState('');
  const [workspaceMode, setWorkspaceMode] = useState('guided');
  const [activeSectionId, setActiveSectionId] = useState('inventory-section');
  const [inventoryFocusRequest, setInventoryFocusRequest] = useState(0);
  const hasLoadedServers = useRef(false);
  const inventorySectionRef = useRef(null);
  const inventoryPrimaryInputRef = useRef(null);
  const logsOutputRef = useRef(null);
  const workspaceRef = useRef(null);
  const {
    pathname: currentPath,
    route: workspaceRoute,
    isBoardView,
    isDetailView,
    navigateToBoard: routeNavigateToBoard,
    navigateToServerWorkspace: routeNavigateToServerWorkspace,
  } = useWorkspaceRoute();

  const apiState = useApiHealth();
  const telegram = useTelegramAlertsController();

  const selectedServer = servers.find((server) => server.id === selectedServerId) || null;
  const operationQueryAuthReady = canUseOperationQueryAuth(deployDraft);
  const operationAuthHelp = getOperationAuthHelp(deployDraft);

  const logs = useLogsController({
    selectedServerId,
    deployDraft,
    operationQueryAuthReady,
    operationAuthHelp,
    logsOutputRef,
    syncSavedKeyPathFromDraft,
  });

  const health = useServerHealth({
    selectedServerId,
    setServers,
    setServerStatus,
  });

  useEffect(() => {
    let ignore = false;

    async function loadServers({ silent = false } = {}) {
      if (!silent || !hasLoadedServers.current) {
        setServersLoading(true);
      }
      setServersError('');

      try {
        const payload = await serversApi.list();

        const nextServers = Array.isArray(payload?.servers) ? payload.servers : [];
        if (ignore) {
          return;
        }

        setServers(nextServers);
        setSelectedServerId((current) => {
          const routeServerId = readWorkspaceRoute().serverId;

          if (routeServerId && nextServers.some((server) => server.id === routeServerId)) {
            return routeServerId;
          }

          if (current && nextServers.some((server) => server.id === current)) {
            return current;
          }

          return nextServers[0]?.id || '';
        });
        hasLoadedServers.current = true;
      } catch (error) {
        if (ignore) {
          return;
        }

        setServersError(error instanceof Error ? error.message : 'Не удалось загрузить список серверов.');
        setServers([]);
        setSelectedServerId('');
        hasLoadedServers.current = true;
      } finally {
        if (!ignore && (!silent || !hasLoadedServers.current)) {
          setServersLoading(false);
        }
      }
    }

    void loadServers();
    const timer = window.setInterval(() => {
      void loadServers({ silent: true });
    }, 15000);

    return () => {
      ignore = true;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    const currentSelectedServer = servers.find((server) => server.id === selectedServerId) || null;

    setDeployDraft(
      currentSelectedServer
        ? {
            ...createDeployDraft(),
            private_key_path: currentSelectedServer.saved_private_key_path || '',
          }
        : createDeployDraft(),
    );
    setSshTestResult(null);
    setSshTestBusy(false);
    setSshTestError('');
    setSshTestNotice('');
    setLastSshTestAt('');
    setLastDiagnosticsAt('');

    if (!selectedServerId) {
      setCurrentConfig(null);
      setRevisions([]);
      setDraftTemplate(createDraftFields());
      setDraftFields(createDraftFields());
      setEditorText('');
      setConfigError('');
      setConfigWarning('');
      setConfigNotice('');
      setConfigLoading(false);
      setConfigSyncBusy(false);
      setDeployPreview(null);
      setDeployEvents([]);
      setDeployError('');
      setDeployNotice('');
      setServerStatus(null);
      setStatusError('');
      setStatusNotice('');
      setStatusLoading(false);
      setLinkInfo(null);
      setRestartBusy(false);
      setOperationEvents([]);
      setOperationError('');
      setOperationNotice('');
      return;
    }

    let ignore = false;

    async function loadLatestSSHTestState() {
      try {
        const payload = await serversApi.getLatestSshTest(selectedServerId);
        if (ignore) {
          return;
        }

        setSshTestResult(payload?.result || null);
        setLastSshTestAt(payload?.tested_at || '');
        if (!payload?.result && payload?.error_message) {
          setSshTestError(`Последняя SSH-проверка завершилась ошибкой: ${payload.error_message}`);
        }
      } catch (error) {
        if (ignore) {
          return;
        }

        setSshTestResult(null);
        setLastSshTestAt('');
        setSshTestError(error instanceof Error ? error.message : 'Не удалось загрузить сохраненную SSH-проверку.');
      }
    }

    async function loadConfigState() {
      setConfigLoading(true);
      setConfigError('');
      setConfigWarning('');
      setConfigNotice('');

      try {
        const payload = await configsApi.getCurrent(selectedServerId);
        if (ignore) {
          return;
        }

        applyConfigPayload(payload);
      } catch (error) {
        if (ignore) {
          return;
        }

        setConfigError(error instanceof Error ? error.message : 'Не удалось загрузить состояние конфига.');
        setCurrentConfig(null);
        setRevisions([]);
        setDraftTemplate(createDraftFields());
        setDraftFields(createDraftFields());
        setEditorText('');
        setDeployPreview(null);
        setDeployEvents([]);
      } finally {
        if (!ignore) {
          setConfigLoading(false);
        }
      }
    }

    void loadLatestSSHTestState();
    void loadConfigState();

    return () => {
      ignore = true;
    };
  }, [selectedServerId]); // eslint-disable-line react-hooks/exhaustive-deps -- switching servers resets workspace state in one place

  useEffect(() => {
    if (!selectedServerId) {
      return;
    }

    let ignore = false;

    async function loadInitialStatus() {
      setStatusLoading(true);
      setStatusError('');

      try {
        const payload = await operationsApi.getStatus(selectedServerId, deployDraft);
        if (ignore) {
          return;
        }

        setServerStatus(payload?.status || null);
      } catch (error) {
        if (ignore) {
          return;
        }

        setStatusError(error instanceof Error ? error.message : 'Не удалось загрузить статус сервера.');
        setServerStatus(null);
      } finally {
        if (!ignore) {
          setStatusLoading(false);
        }
      }
    }

    void loadInitialStatus();

    return () => {
      ignore = true;
    };
  }, [selectedServerId]); // eslint-disable-line react-hooks/exhaustive-deps -- initial status load follows selection; later auth edits use manual refresh

  useEffect(() => {
    if (!currentConfig) {
      setLinkInfo(null);
      return;
    }

    setLinkInfo((current) => {
      if (current?.source === 'telemt_api' && current.generated_link) {
        return current;
      }

      return {
        generated_link: currentConfig.generated_link,
        source: 'config_revision',
        config_version: currentConfig.version,
        warning: '',
      };
    });
  }, [currentConfig]);

  useEffect(() => {
    if (inventoryMode !== 'edit') {
      return;
    }

    const currentSelectedServer = servers.find((server) => server.id === selectedServerId) || null;
    if (!currentSelectedServer) {
      return;
    }

    setInventoryDraft(createServerDraft(currentSelectedServer));
    setInventoryFieldErrors({});
    setInventoryError('');
  }, [inventoryMode, selectedServerId]); // eslint-disable-line react-hooks/exhaustive-deps -- avoid clobbering edit inputs on background list refresh

  const maxSidebarServerOffset = Math.max(0, servers.length - sidebarVisibleServerLimit);
  const visibleSidebarServers = servers.slice(sidebarServerOffset, sidebarServerOffset + sidebarVisibleServerLimit);
  const canPageSidebarServersUp = sidebarServerOffset > 0;
  const canPageSidebarServersDown = sidebarServerOffset < maxSidebarServerOffset;
  const sidebarVisibleRangeStart = servers.length === 0 ? 0 : sidebarServerOffset + 1;
  const sidebarVisibleRangeEnd = sidebarServerOffset + visibleSidebarServers.length;

  useEffect(() => {
    setSidebarServerOffset((current) => {
      const clamped = Math.min(current, maxSidebarServerOffset);
      const selectedIndex = servers.findIndex((server) => server.id === selectedServerId);

      if (selectedIndex < 0 || servers.length <= sidebarVisibleServerLimit) {
        return clamped;
      }

      if (selectedIndex < clamped) {
        return selectedIndex;
      }

      if (selectedIndex >= clamped + sidebarVisibleServerLimit) {
        return Math.min(selectedIndex - sidebarVisibleServerLimit + 1, maxSidebarServerOffset);
      }

      return clamped;
    });
  }, [maxSidebarServerOffset, selectedServerId, servers]);

  useEffect(() => {
    if (!isDetailView) {
      return;
    }

    if (workspaceRoute.serverId && workspaceRoute.serverId !== selectedServerId) {
      setSelectedServerId(workspaceRoute.serverId);
    }

    if (activeSectionId === 'inventory-section') {
      setActiveSectionId('ssh-section');
    }
  }, [activeSectionId, isDetailView, selectedServerId, workspaceRoute.serverId]);

  useEffect(() => {
    if (!isDetailView || serversLoading) {
      return;
    }

    if (!workspaceRoute.serverId || !servers.some((server) => server.id === workspaceRoute.serverId)) {
      if (currentPath !== '/') {
        routeNavigateToBoard({ replace: true });
      }
    }
  }, [currentPath, isDetailView, routeNavigateToBoard, servers, serversLoading, workspaceRoute.serverId]);

  useEffect(() => {
    if (!selectedServer?.saved_private_key_path) {
      return;
    }

    setDeployDraft((current) => {
      if (current.auth_type !== 'private_key_path' || current.private_key_path.trim() !== '') {
        return current;
      }

      return {
        ...current,
        private_key_path: selectedServer.saved_private_key_path,
      };
    });
  }, [selectedServer?.id, selectedServer?.saved_private_key_path]);

  useEffect(() => {
    if (!pendingConfigImport || pendingConfigImport.serverId !== selectedServerId || configLoading || !selectedServer) {
      return;
    }

    const baseDraft = currentConfig?.fields
      ? createDraftFields(currentConfig.fields)
      : createDraftFields({
          public_host: selectedServer.public_host || selectedServer.host,
          public_port: selectedServer.mtproto_port || 443,
          tls_domain: selectedServer.sni_domain || selectedServer.public_host || selectedServer.host,
        });
    const nextDraft = applyDiscoveryToConfigDraft(baseDraft, pendingConfigImport.discovery);

    setDraftTemplate(nextDraft);
    setDraftFields(nextDraft);
    setConfigError('');
    setConfigNotice(
      pendingConfigImport.discovery.secret
        ? 'Черновик MTProto заполнен из найденного Telemt: public_host, mtproto_port, sni_domain и secret синхронизированы.'
        : 'Черновик MTProto заполнен из найденного Telemt: маршрут синхронизирован, secret на сервере не найден.',
    );
    setPendingConfigImport(null);
  }, [pendingConfigImport, selectedServerId, configLoading, selectedServer, currentConfig]);

  const inventoryFormVisible = inventoryMode !== '' || (!serversLoading && servers.length === 0);
  const inventoryFormHeading =
    inventoryMode === 'edit' ? 'Редактировать выбранный сервер' : servers.length === 0 ? 'Добавить первый сервер' : 'Добавить сервер';
  const inventoryActionLabel = inventoryMode === 'edit' ? 'Сохранить сервер' : 'Создать сервер';
  const inventoryDiscoveryReady =
    inventoryDraft.host.trim() !== '' &&
    inventoryDraft.ssh_user.trim() !== '' &&
    inventoryDraft.private_key_path.trim() !== '';
  const inventoryRouteReady =
    inventoryDraft.public_host.trim() !== '' && inventoryDraft.mtproto_port.trim() !== '' && inventoryDraft.sni_domain.trim() !== '';
  const inventoryImportBadge = inventoryDetectBusy
    ? { label: 'Чтение', tone: 'active' }
    : inventoryImportStatus === 'success'
      ? { label: 'Найден', tone: 'done' }
      : inventoryImportStatus === 'error'
        ? { label: 'Ошибка', tone: 'blocked' }
        : inventoryDiscoveryReady
          ? { label: 'Готово', tone: 'active' }
          : { label: 'Ждём SSH', tone: 'unknown' };
  const inventoryImportHint = inventoryDiscoveryReady
    ? inventoryRouteReady
      ? 'Используйте это только для уже установленного прокси: импорт обновит найденные значения поверх текущего маршрута.'
      : 'Используйте это только для уже установленного прокси: импорт заполнит маршрут MTProto автоматически.'
    : 'Используйте это только если прокси уже существует на сервере и вы хотите импортировать его настройки. Для импорта заполните host, ssh_user и private_key_path. Если панель и Telemt на одном сервере, воспользуйтесь подсказками ниже.';

  const tlsDomainWarning = getTLSDomainWarning(draftFields);
  const draftPreviewLink = buildPreviewLink(draftFields);
  const deployDecisionHelp = getDeployDecisionHelp(deployDraft.port_conflict_decision);
  const deployHasBlockingRisks = hasBlockingRisks(deployPreview);
  const activeLink =
    (linkInfo?.source === 'telemt_api' ? linkInfo.generated_link : '') ||
    serverStatus?.generated_link ||
    linkInfo?.generated_link ||
    currentConfig?.generated_link ||
    '';
  const currentHealth = health.healthHistory[0] || serverStatus?.latest_health || null;
  const sshAuthReady = hasConfiguredSSHAuth(deployDraft);
  const healthSummary = currentHealth ? formatHealthState(currentHealth.status) : formatHealthState(selectedServer?.status);
  const savedPrivateKeyPath = selectedServer?.saved_private_key_path || '';
  const savedPrivateKeyPathUpdatedAt = selectedServer?.saved_private_key_path_updated_at || '';
  const configDiscoveryReady = Boolean(selectedServer && savedPrivateKeyPath.trim() !== '');
  const effectiveTLSDomain = draftFields.tls_domain.trim() || draftFields.public_host.trim();
  const configDraftSource = currentConfig ? `Ревизия v${currentConfig.version}` : 'Черновик из инвентаря';
  const diagnosticsStatusLabel = lastDiagnosticsAt ? `Превью загружено ${formatDateTime(lastDiagnosticsAt)}.` : '';
  const appliedStatusLabel = currentConfig?.applied_at ? `Применено ${formatDateTime(currentConfig.applied_at)}.` : '';
  const healthStatusLabel = currentHealth?.created_at ? `Последняя проверка воркера ${formatDateTime(currentHealth.created_at)}.` : '';
  const editorHasContent = editorText.trim() !== '';
  const unsavedEditorChanges = currentConfig ? currentConfig.config_text !== editorText : editorText.trim() !== '';
  const editorConfigPath = selectedServer?.remote_base_path ? `${selectedServer.remote_base_path}/config.toml` : 'config.toml';
  const editorVersionLabel = currentConfig
    ? `Ревизия v${currentConfig.version}`
    : editorHasContent
      ? 'Локальный TOML'
      : configSyncBusy || configLoading
        ? 'Чтение TOML'
        : configError
          ? 'Ошибка чтения'
          : 'Ревизия не загружена';
  const editorSyncLabel =
    !editorHasContent && !currentConfig
      ? configError
        ? 'Не синхронизировано'
        : configWarning
          ? 'Частично синхронизировано'
          : 'Ожидает загрузки'
      : unsavedEditorChanges
        ? 'Есть изменения'
        : currentConfig
          ? 'Сохранено'
          : 'Локально';
  const editorFooterLabel = unsavedEditorChanges
    ? 'В редакторе есть несохраненные изменения.'
    : currentConfig
      ? 'Редактор совпадает с последней сохраненной ревизией.'
      : editorHasContent
        ? 'В редакторе локальный TOML, но ревизия еще не сохранена.'
        : 'Ревизия Telemt еще не загружена.';
  const editorEmptyHint = currentConfig
    ? 'У текущей ревизии нет текста. Подтяните config.toml с сервера или соберите новый TOML из полей выше.'
    : 'Поля маршрута и редактор разделены: «Подтянуть MTProto» переносит найденный config.toml с сервера, а «Сгенерировать» собирает новый TOML из формы выше.';
  const operatorSteps = buildOperatorSteps({
    selectedServer,
    sshAuthReady,
    sshTestSuccessful: Boolean(sshTestResult?.ok),
    currentConfig,
    deployPreview,
    deployHasBlockingRisks,
    activeLink,
    currentHealth,
    diagnosticsLabel: diagnosticsStatusLabel,
    appliedLabel: appliedStatusLabel,
    healthLabel: healthStatusLabel,
  });
  const nextOperatorStep = operatorSteps.find((step) => step.state !== 'done') || operatorSteps[operatorSteps.length - 1];
  const workspaceSections = [
    ...operatorSteps.map((step) => ({
      id: step.target,
      label: step.title.replace(/^\d+\.\s*/, ''),
      state: step.state,
      statusLabel: formatStepState(step.state),
      summary: step.blocker || step.summary,
      required: true,
    })),
    {
      id: 'telegram-section',
      label: 'Оповещения',
      state: telegram.savedTelegramTargetReady ? 'done' : 'active',
      statusLabel: telegram.savedTelegramTargetReady ? 'Готово' : 'Опция',
      summary: telegram.savedTelegramTargetReady
        ? 'Telegram-оповещения настроены и готовы к тестовой отправке.'
        : 'Настройте Telegram, чтобы получать уведомления о down, recovery и ошибках деплоя.',
      required: false,
    },
  ];
  const workspaceSectionById = new Map(workspaceSections.map((section) => [section.id, section]));
  const detailSectionTabs = [
    ['ssh-section', 'SSH'],
    ['config-section', 'MTProto config'],
    ['deploy-section', 'Deploy'],
    ['operations-section', 'Status / Logs'],
    ['health-section', 'Health'],
    ['telegram-section', 'Alerts'],
  ]
    .map(([sectionId, label]) => {
      const section = workspaceSectionById.get(sectionId);
      return section ? { ...section, icon: sectionIconNames[sectionId] || 'server', label } : null;
    })
    .filter(Boolean);
  const showAllSections = workspaceMode === 'all';
  const selectedServerAddress = selectedServer ? `${selectedServer.public_host || selectedServer.host}:${selectedServer.mtproto_port || 443}` : '';
  const selectedServerSshTarget = selectedServer ? `${selectedServer.ssh_user}@${selectedServer.host}:${selectedServer.ssh_port}` : '';
  const nextOperatorStepLabel = nextOperatorStep.title.replace(/^\d+\.\s*/, '');

  useEffect(() => {
    if (inventoryFocusRequest === 0 || !isBoardView || !inventoryFormVisible) {
      return undefined;
    }

    const frame = window.requestAnimationFrame(() => {
      const target = inventoryPrimaryInputRef.current || inventorySectionRef.current;
      target?.scrollIntoView({ behavior: 'smooth', block: inventoryPrimaryInputRef.current ? 'center' : 'start' });
      inventoryPrimaryInputRef.current?.focus();
    });

    return () => {
      window.cancelAnimationFrame(frame);
    };
  }, [inventoryFocusRequest, inventoryFormVisible, isBoardView]);

  useEffect(() => {
    if (showAllSections) {
      return;
    }

    if (activeSectionId === 'telegram-section') {
      return;
    }

    if (!selectedServer && ['config-section', 'deploy-section', 'operations-section', 'health-section'].includes(activeSectionId)) {
      setActiveSectionId('inventory-section');
    }
  }, [activeSectionId, selectedServer, showAllSections]);

  function navigateToBoard(options = {}) {
    routeNavigateToBoard(options);
  }

  function navigateToServerWorkspace(serverId, options = {}) {
    if (!serverId) {
      navigateToBoard(options);
      return;
    }

    setSelectedServerId(serverId);
    setWorkspaceMode('guided');
    setActiveSectionId((current) => (current === 'inventory-section' ? 'ssh-section' : current));
    routeNavigateToServerWorkspace(serverId, options);
    scheduleViewportUpdate(() => {
      if (workspaceRef.current) {
        workspaceRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' });
        return;
      }

      window.scrollTo({ top: 0, behavior: 'smooth' });
    });
  }

  function openSection(sectionId, nextMode = 'guided') {
    const resolvedSectionId =
      !selectedServer && ['config-section', 'deploy-section', 'operations-section', 'health-section'].includes(sectionId)
        ? 'inventory-section'
        : sectionId;

    setWorkspaceMode(nextMode);
    setActiveSectionId(resolvedSectionId);

    scheduleViewportUpdate(() => {
      if (nextMode === 'all') {
        document.getElementById(resolvedSectionId)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        return;
      }

      window.scrollTo({ top: 0, behavior: 'smooth' });
    });
  }

  function applyConfigPayload(payload) {
    const nextCurrent = payload?.current || null;
    const nextRevisions = Array.isArray(payload?.revisions) ? payload.revisions : [];
    const nextDraftSource = nextCurrent?.fields || payload?.draft_fields || {};
    const nextDraftFields = createDraftFields(nextDraftSource);

    setCurrentConfig(nextCurrent);
    setRevisions(nextRevisions);
    setDraftTemplate(nextDraftFields);
    setDraftFields(nextDraftFields);
    setEditorText(nextCurrent?.config_text || '');
  }

  function applyServerPayload(payload) {
    const nextServer = payload?.server || null;
    if (!nextServer) {
      return null;
    }

    setServers((current) => current.map((server) => (server.id === nextServer.id ? nextServer : server)));
    return nextServer;
  }

  function rememberSavedKeyPath(serverId, privateKeyPath, updatedAt = new Date().toISOString()) {
    const trimmedPath = privateKeyPath.trim();
    if (!serverId || trimmedPath === '') {
      return;
    }

    setServers((current) =>
      current.map((server) =>
        server.id === serverId
          ? {
              ...server,
              saved_private_key_path: trimmedPath,
              saved_private_key_path_updated_at: updatedAt,
            }
          : server,
      ),
    );
  }

  function syncSavedKeyPathFromDraft(serverId = selectedServerId) {
    if (deployDraft.auth_type !== 'private_key_path') {
      return;
    }

    rememberSavedKeyPath(serverId, deployDraft.private_key_path);
  }

  function resetInventoryImportState() {
    setInventoryImportStatus('idle');
    setInventoryImportMessage('');
    setInventoryImportDiscovery(null);
  }

  function handleStartCreateServer() {
    setInventoryFocusRequest((current) => current + 1);
    navigateToBoard();
    setWorkspaceMode('guided');
    setActiveSectionId('inventory-section');
    setInventoryMode('create');
    setInventoryDraft(createServerDraft());
    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');
    resetInventoryImportState();
  }

  function handleStartEditServer() {
    if (!selectedServer) {
      return;
    }

    setInventoryFocusRequest((current) => current + 1);
    navigateToBoard();
    setWorkspaceMode('guided');
    setActiveSectionId('inventory-section');
    setInventoryMode('edit');
    setInventoryDraft(createServerDraft(selectedServer));
    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');
    resetInventoryImportState();
  }

  function handleResetInventoryForm() {
    if (inventoryMode === 'edit' && selectedServer) {
      setInventoryDraft(createServerDraft(selectedServer));
    } else {
      setInventoryDraft(createServerDraft());
    }

    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');
    resetInventoryImportState();
  }

  function handleCancelInventoryForm() {
    setInventoryMode('');
    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');
    resetInventoryImportState();
    if (selectedServer) {
      setInventoryDraft(createServerDraft(selectedServer));
      return;
    }

    setInventoryDraft(createServerDraft());
  }

  function updateInventoryField(key, value) {
    setInventoryDraft((current) => ({
      ...current,
      [key]: value,
    }));
    setInventoryFieldErrors((current) => {
      if (!current[key]) {
        return current;
      }

      const next = { ...current };
      delete next[key];
      return next;
    });
  }

  async function requestDiscoveredServerSettings(request) {
    const payload = await serversApi.discover(request);

    const discovery = payload?.discovery || null;
    if (!discovery?.remote_base_path) {
      throw new Error('Сервер не вернул данные Telemt для автозаполнения.');
    }

    return discovery;
  }

  async function handleDetectServerSettings() {
    if (!inventoryDiscoveryReady) {
      setInventoryError('');
      setInventoryNotice('');
      setInventoryImportStatus('error');
      setInventoryImportMessage('Чтобы получить MTProto, укажите хост, пользователя SSH и путь к приватному ключу.');
      setInventoryImportDiscovery(null);
      return;
    }

    setInventoryDetectBusy(true);
    setInventoryError('');
    setInventoryNotice('');
    setInventoryImportStatus('idle');
    setInventoryImportMessage('');
    setInventoryImportDiscovery(null);

    try {
      const discovery = await requestDiscoveredServerSettings({
        server_id: selectedServer?.id || undefined,
        host: inventoryDraft.host.trim(),
        ssh_user: inventoryDraft.ssh_user.trim(),
        ssh_port: Number.parseInt(inventoryDraft.ssh_port || '0', 10) || 22,
        auth_type: 'private_key_path',
        private_key_path: inventoryDraft.private_key_path.trim(),
        remote_base_path_hint: inventoryDraft.remote_base_path.trim() || undefined,
      });

      setInventoryDraft((current) => applyDiscoveryToInventoryDraft(current, discovery));
      setInventoryImportDiscovery(discovery);
      setInventoryImportStatus('success');
      setInventoryImportMessage(
        discovery.secret
          ? `Найден существующий Telemt в ${discovery.remote_base_path}. Публичный маршрут уже перенесен в форму, secret будет добавлен в черновик MTProto после сохранения сервера.`
          : `Найден существующий Telemt в ${discovery.remote_base_path}. Публичный маршрут уже перенесен в форму, но secret в config.toml не найден.`,
      );

      if (selectedServer?.id) {
        rememberSavedKeyPath(selectedServer.id, inventoryDraft.private_key_path);
      }
    } catch (error) {
      setInventoryImportStatus('error');
      setInventoryImportMessage(error instanceof Error ? error.message : 'Не удалось определить значения с сервера.');
      setInventoryImportDiscovery(null);
    } finally {
      setInventoryDetectBusy(false);
    }
  }

  async function handleSyncDraftFromServer() {
    if (!selectedServer || savedPrivateKeyPath.trim() === '') {
      setConfigError('Сначала сохраните путь к SSH-ключу в шаге с инвентарем сервера.');
      setConfigWarning('');
      setConfigNotice('');
      return;
    }

    setConfigSyncBusy(true);
    setConfigError('');
    setConfigWarning('');
    setConfigNotice('');

    try {
      const discovery = await requestDiscoveredServerSettings({
        server_id: selectedServer.id,
        host: selectedServer.host,
        ssh_user: selectedServer.ssh_user,
        ssh_port: selectedServer.ssh_port,
        auth_type: 'private_key_path',
        private_key_path: savedPrivateKeyPath,
        remote_base_path_hint: selectedServer.remote_base_path || undefined,
      });

      const hasConfigText = hasDiscoveredConfigText(discovery);
      if (!hasConfigText && !hasDiscoveredConfigFields(discovery)) {
        throw new Error(
          `Telemt найден, но ${discovery.config_path || 'config.toml'} пришел пустым. Проверьте содержимое файла и права чтения на сервере.`,
        );
      }

      let syncedServer = selectedServer;
      const serverPatch = buildDiscoveryServerPatch(selectedServer, discovery);
      if (serverPatch) {
        const serverPayload = await serversApi.update(selectedServer.id, serverPatch);
        syncedServer = applyServerPayload(serverPayload);
        if (!syncedServer) {
          throw new Error('В ответе API отсутствуют обновленные данные сервера после синхронизации MTProto.');
        }
      }

      const importedAsNewRevision = hasConfigText && shouldSaveDiscoveredConfig(currentConfig, discovery);
      const shouldMarkImportedConfigApplied = hasConfigText && (importedAsNewRevision || !currentConfig?.applied_at);
      if (hasConfigText && shouldMarkImportedConfigApplied) {
        const configPayload = await configsApi.saveCurrent(
          selectedServer.id,
          importedAsNewRevision ? { config_text: discovery.config_text, mark_as_applied: true } : { mark_as_applied: true },
        );

        applyConfigPayload(configPayload);
      } else if (hasConfigText && currentConfig) {
        applyConfigPayload({
          current: currentConfig,
          revisions,
          draft_fields: currentConfig.fields,
        });
      } else if (!hasConfigText) {
        const baseDraft = currentConfig?.fields
          ? createDraftFields(currentConfig.fields)
          : createDraftFields({
              public_host: syncedServer.public_host || syncedServer.host,
              public_port: syncedServer.mtproto_port || 443,
              tls_domain: syncedServer.sni_domain || syncedServer.public_host || syncedServer.host,
            });
        const nextDraft = applyDiscoveryToConfigDraft(baseDraft, discovery);
        setDraftTemplate(nextDraft);
        setDraftFields(nextDraft);
      }

      setDeployPreview(null);
      setLastDiagnosticsAt('');
      setDeployEvents([]);
      if (hasConfigText) {
        setConfigWarning('');
        setConfigNotice(
          importedAsNewRevision
            ? 'Конфиг MTProto подтянут с сервера, сохранен как текущая ревизия и помечен как уже примененный. Панель теперь использует его для status, logs и health.'
            : shouldMarkImportedConfigApplied
              ? 'Серверный MTProto уже совпадает с сохраненной ревизией. Панель пометила ее как уже примененную и переключила status, logs и health на этот инстанс.'
              : 'Серверный MTProto уже совпадает с сохраненной ревизией. Подтянутый конфиг подтвержден и остается источником для deploy, status и health.',
        );
        openSection('operations-section');
      } else {
        setConfigNotice('');
        setConfigWarning(
          'Панель получила маршрут и secret, но не получила содержимое config.toml. Редактор и ревизии не обновлены, переход к deploy остановлен.',
        );
        openSection('config-section');
      }
      await handleLoadStatus({ silent: true });
    } catch (error) {
      setConfigWarning('');
      setConfigError(error instanceof Error ? error.message : 'Не удалось прочитать текущие настройки Telemt с сервера.');
    } finally {
      setConfigSyncBusy(false);
    }
  }

  async function handleSubmitServerForm(event) {
    event.preventDefault();

    const isEdit = inventoryMode === 'edit' && selectedServer;
    const importedDiscovery = inventoryImportStatus === 'success' ? inventoryImportDiscovery : null;

    setInventoryBusy(true);
    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');

    try {
      const payload = isEdit
        ? await serversApi.update(selectedServer.id, serializeServerDraft(inventoryDraft))
        : await serversApi.create(serializeServerDraft(inventoryDraft));

      const savedServer = payload?.server;
      if (!savedServer) {
        throw new Error('В ответе API отсутствуют данные сервера.');
      }

      setServers((current) =>
        isEdit ? current.map((server) => (server.id === savedServer.id ? savedServer : server)) : [...current, savedServer],
      );
      if (importedDiscovery) {
        setPendingConfigImport({
          serverId: savedServer.id,
          discovery: importedDiscovery,
        });
      }
      setSelectedServerId(savedServer.id);
      setInventoryMode('');
      setInventoryDraft(createServerDraft(savedServer));
      setInventoryNotice(
        `${
          isEdit
            ? 'Запись о сервере обновлена. Меню и карточка сервера уже используют сохраненные значения.'
            : 'Сервер создан и выбран для дальнейшей работы по сценарию оператора.'
        }${
          importedDiscovery
            ? importedDiscovery.secret
              ? ' Найденный MTProto сохранен: публичный маршрут обновлен, secret переносится в черновик MTProto.'
              : ' Найденный MTProto сохранен: публичный маршрут обновлен, но secret на сервере не найден.'
            : ''
        }`,
      );
      resetInventoryImportState();
      openSection('ssh-section');
      navigateToServerWorkspace(savedServer.id);

      if (isEdit && savedServer.id === selectedServerId) {
        try {
          const configPayload = await configsApi.getCurrent(savedServer.id);
          applyConfigPayload(configPayload);
        } catch {
          // Keep the saved inventory change even if the detail refresh fails.
        }
      }
    } catch (error) {
      const nextFieldErrors = getApiErrorDetails(getErrorPayload(error));
      if (Object.keys(nextFieldErrors).length > 0) {
        setInventoryFieldErrors(nextFieldErrors);
        setInventoryError('Исправьте поля сервера с ошибками и повторите попытку.');
        return;
      }
      setInventoryError(error instanceof Error ? error.message : 'Не удалось сохранить запись о сервере.');
    } finally {
      setInventoryBusy(false);
    }
  }

  async function handleDeleteServer() {
    if (!selectedServer) {
      return;
    }

    const confirmed = window.confirm(
      `Удалить сервер "${selectedServer.name}"? Будет удалена запись из инвентаря, а также связанные данные конфига, проверок и SSH-учетных данных.`,
    );
    if (!confirmed) {
      return;
    }

    const deletedServerId = selectedServer.id;
    const nextSelectedServerId = getNextSelectedServerId(servers, deletedServerId, selectedServerId);

    setInventoryBusy(true);
    setInventoryFieldErrors({});
    setInventoryError('');
    setInventoryNotice('');

    try {
      await serversApi.remove(deletedServerId);

      setServers((current) => current.filter((server) => server.id !== deletedServerId));
      setSelectedServerId(nextSelectedServerId);
      setInventoryMode('');
      setInventoryDraft(createServerDraft());
      setInventoryNotice('Запись о сервере удалена, а текущее выделение обновлено без перезагрузки страницы.');

      if (isDetailView) {
        if (nextSelectedServerId) {
          navigateToServerWorkspace(nextSelectedServerId, { replace: true });
        } else {
          navigateToBoard({ replace: true });
        }
      }
    } catch (error) {
      setInventoryError(error instanceof Error ? error.message : 'Не удалось удалить запись о сервере.');
    } finally {
      setInventoryBusy(false);
    }
  }

  async function handleTestSSH() {
    if (!selectedServer) {
      return;
    }

    if (!sshAuthReady) {
      setSshTestResult(null);
      setSshTestError('Перед проверкой SSH укажите пароль, путь к приватному ключу или вставьте сам ключ.');
      setSshTestNotice('');
      return;
    }

    const requestBody = {
      server_id: selectedServer.id,
      host: selectedServer.host,
      ssh_user: selectedServer.ssh_user,
      ssh_port: selectedServer.ssh_port,
      ...serializeSSHAuthFields(deployDraft),
    };

    const privateKeyPath = deployDraft.private_key_path.trim();

    setSshTestBusy(true);
    setSshTestError('');
    setSshTestNotice('');

    try {
      const payload = await serversApi.testSsh(requestBody);

      setSshTestResult(payload);
      setLastSshTestAt(new Date().toISOString());
      setSshTestNotice('SSH-подключение прошло успешно. Ниже доступны сведения о хосте и проверки Docker.');
      openSection('config-section');
      if (deployDraft.auth_type === 'private_key_path' && privateKeyPath) {
        rememberSavedKeyPath(selectedServer.id, privateKeyPath);
      }
    } catch (error) {
      setSshTestResult(null);
      setSshTestError(error instanceof Error ? error.message : 'Не удалось выполнить проверку SSH-доступа.');
    } finally {
      setSshTestBusy(false);
    }
  }

  async function handleGenerateDraft(event) {
    event?.preventDefault();
    if (!selectedServerId) {
      return;
    }

    setActionBusy(true);
    setConfigError('');
    setConfigWarning('');
    setConfigNotice('');

    try {
      const payload = await configsApi.generate(selectedServerId, serializeDraftFields(draftFields));

      applyConfigPayload(payload);
      setDeployPreview(null);
      setLastDiagnosticsAt('');
      setDeployEvents([]);
      setConfigNotice('Черновик TOML для Telemt успешно сгенерирован и сохранен как новая ревизия.');
      openSection('deploy-section');
      await handleLoadStatus({ silent: true });
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : 'Не удалось сгенерировать черновик.');
    } finally {
      setActionBusy(false);
    }
  }

  async function handleSaveRevision() {
    if (!selectedServerId) {
      return;
    }

    setActionBusy(true);
    setConfigError('');
    setConfigWarning('');
    setConfigNotice('');

    try {
      const payload = await configsApi.saveCurrent(selectedServerId, { config_text: editorText });

      applyConfigPayload(payload);
      setDeployPreview(null);
      setLastDiagnosticsAt('');
      setDeployEvents([]);
      setConfigNotice('Текущий TOML сохранен как новая ревизия конфига.');
      openSection('deploy-section');
      await handleLoadStatus({ silent: true });
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : 'Не удалось сохранить ревизию.');
    } finally {
      setActionBusy(false);
    }
  }

  async function handleLoadDeployPreview() {
    if (!selectedServerId) {
      return;
    }

    setDeployBusy(true);
    setDeployError('');
    setDeployNotice('');
    setDeployEvents([]);

    try {
      const payload = await deployApi.preview(selectedServerId, serializeDeployRequest(deployDraft));

      setDeployPreview(payload?.preview || null);
      setLastDiagnosticsAt(new Date().toISOString());
      setDeployEvents([]);
      syncSavedKeyPathFromDraft();
      setDeployNotice('Превью деплоя загружено: диагностика, план загрузки файлов и сводка рисков доступны ниже.');
      openSection('deploy-section');
    } catch (error) {
      const payload = getErrorPayload(error);
      if (payload?.preview) {
        setDeployPreview(payload.preview);
        setLastDiagnosticsAt(new Date().toISOString());
      }
      if (Array.isArray(payload?.events)) {
        setDeployEvents(payload.events);
      }
      setDeployError(error instanceof Error ? error.message : 'Не удалось загрузить превью деплоя.');
    } finally {
      setDeployBusy(false);
    }
  }

  async function handleApplyDeploy() {
    if (!selectedServerId) {
      return;
    }

    setDeployBusy(true);
    setDeployError('');
    setDeployNotice('');
    setDeployEvents([]);

    try {
      const payload = await deployApi.apply(selectedServerId, serializeDeployRequest(deployDraft));

      if (payload?.config) {
        applyConfigPayload(payload.config);
      }
      setDeployPreview(payload?.result?.preview || null);
      setLastDiagnosticsAt(new Date().toISOString());
      setDeployEvents(Array.isArray(payload?.result?.events) ? payload.result.events : []);
      syncSavedKeyPathFromDraft();
      setDeployNotice('Текущий конфиг и compose-файл загружены, Telemt запущен, а ссылка сохранена из Telemt API.');
      openSection('operations-section');
      await handleLoadStatus({ silent: true });
    } catch (error) {
      const payload = getErrorPayload(error);
      if (payload?.preview) {
        setDeployPreview(payload.preview);
      }
      if (Array.isArray(payload?.events)) {
        setDeployEvents(payload.events);
      }
      setDeployError(error instanceof Error ? error.message : 'Не удалось применить деплой.');
    } finally {
      setDeployBusy(false);
    }
  }

  async function handleLoadStatus(options = {}) {
    if (!selectedServerId) {
      return;
    }

    const { silent = false } = options;
    setStatusLoading(true);
    setStatusError('');
    if (!silent) {
      setStatusNotice('');
    }

    try {
      const payload = await operationsApi.getStatus(selectedServerId, deployDraft);

      setServerStatus(payload?.status || null);
      if (operationQueryAuthReady) {
        syncSavedKeyPathFromDraft();
      }
      if (!silent) {
        setStatusNotice(
          operationQueryAuthReady
            ? 'Обновлены статус контейнера в реальном времени, данные Telemt API, публичная доступность и последняя сохраненная проверка.'
            : 'Загружены текущее состояние конфига, последняя проверка и доступные поля статуса без live SSH-авторизации.',
        );
      }
    } catch (error) {
      setStatusError(error instanceof Error ? error.message : 'Не удалось загрузить статус сервера.');
    } finally {
      setStatusLoading(false);
    }
  }

  function getOperationFieldsForServer(server) {
    if (!server) {
      return createDeployDraft();
    }

    if (server.id === selectedServerId) {
      return deployDraft;
    }

    const savedPrivateKeyPathForServer = (server.saved_private_key_path || '').trim();
    if (savedPrivateKeyPathForServer === '') {
      return createDeployDraft();
    }

    return {
      ...createDeployDraft(),
      private_key_path: savedPrivateKeyPathForServer,
      passphrase: deployDraft.passphrase,
    };
  }

  async function copyServerLink(server) {
    if (!server?.id) {
      return;
    }

    const operationFields = getOperationFieldsForServer(server);
    setOperationError('');
    setOperationNotice('');

    try {
      const payload = await operationsApi.getLink(server.id, operationFields);

      const nextLink = payload?.link || null;
      if (server.id === selectedServerId) {
        setLinkInfo(nextLink);
        if (canUseOperationQueryAuth(operationFields)) {
          syncSavedKeyPathFromDraft(server.id);
        }
      }

      if (!nextLink?.generated_link) {
        throw new Error('Прокси-ссылка пустая. Сначала сгенерируйте конфиг или выполните деплой.');
      }

      let copied = false;
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(nextLink.generated_link);
        copied = true;
      }

      setOperationNotice(
        copied
          ? `MTProto-ссылка для ${server.name} скопирована из источника: ${nextLink.source === 'telemt_api' ? 'telemt api (онлайн)' : 'сохраненная ревизия конфига'}.`
          : `MTProto-ссылка для ${server.name} загружена из источника: ${nextLink.source === 'telemt_api' ? 'telemt api (онлайн)' : 'сохраненная ревизия конфига'}.`,
      );
    } catch (error) {
      setOperationError(error instanceof Error ? error.message : 'Не удалось получить или скопировать прокси-ссылку.');
    }
  }

  async function handleCopyLink() {
    if (!selectedServer) {
      return;
    }

    await copyServerLink(selectedServer);
  }

  async function handleRestartServer() {
    if (!selectedServerId) {
      return;
    }

    setRestartBusy(true);
    setOperationError('');
    setOperationNotice('');

    try {
      const payload = await operationsApi.restart(selectedServerId, serializeDeployRequest(deployDraft));

      setOperationEvents(Array.isArray(payload?.result?.events) ? payload.result.events : []);
      syncSavedKeyPathFromDraft();
      setOperationNotice('Команда перезапуска выполнена. История событий обновлена, теперь можно перечитать статус или логи для проверки восстановления.');
      await handleLoadStatus({ silent: true });
      if (operationQueryAuthReady) {
        await logs.handleLoadLogs({ silent: true });
      }
    } catch (error) {
      setOperationError(error instanceof Error ? error.message : 'Не удалось перезапустить Telemt.');
    } finally {
      setRestartBusy(false);
    }
  }

  function updateField(key, value) {
    setDraftFields((current) => ({
      ...current,
      [key]: value,
    }));
  }

  function updateDeployField(key, value) {
    setDeployDraft((current) => ({
      ...current,
      [key]: value,
    }));
  }

  function shiftSidebarServerWindow(step) {
    setSidebarServerOffset((current) => {
      const next = current + step;

      if (next < 0) {
        return 0;
      }

      if (next > maxSidebarServerOffset) {
        return maxSidebarServerOffset;
      }

      return next;
    });
  }

  const shouldRenderSection = (sectionId) => showAllSections || activeSectionId === sectionId;

  return {
    view: {
      inventoryFormVisible,
      isBoardView,
      isDetailView,
      selectedServer,
      shouldRenderSection,
      workspaceRef,
    },
    missingServerState: {
      onBackToBoard: navigateToBoard,
    },
    emptyState: {
      inventoryBusy,
      onCreateServer: handleStartCreateServer,
    },
    sidebar: {
      canPageDown: canPageSidebarServersDown,
      canPageUp: canPageSidebarServersUp,
      onCreateServer: handleStartCreateServer,
      onOpenBoard: navigateToBoard,
      onPageDown: () => shiftSidebarServerWindow(1),
      onPageUp: () => shiftSidebarServerWindow(-1),
      onSelectServer: navigateToServerWorkspace,
      selectedServerId,
      servers,
      serversError,
      serversLoading,
      visibleLimit: sidebarVisibleServerLimit,
      visibleRangeEnd: sidebarVisibleRangeEnd,
      visibleRangeStart: sidebarVisibleRangeStart,
      visibleServers: visibleSidebarServers,
    },
    board: {
      apiState,
      onCopyLink: copyServerLink,
      onCreateServer: handleStartCreateServer,
      onOpenServer: navigateToServerWorkspace,
      operationError,
      operationNotice,
      selectedServerId,
      servers,
      serversError,
      serversLoading,
    },
    detailHeader: selectedServer
      ? {
          activeSectionId,
          currentConfig,
          currentHealth,
          detailSectionTabs,
          healthSummary,
          inventoryBusy,
          lastDiagnosticsAt,
          nextOperatorStepLabel,
          nextOperatorStepTarget: nextOperatorStep.target,
          onBackToBoard: navigateToBoard,
          onCopyLink: handleCopyLink,
          onDeleteServer: handleDeleteServer,
          onEditServer: handleStartEditServer,
          onOpenSection: openSection,
          onRefreshStatus: handleLoadStatus,
          savedPrivateKeyPath,
          selectedServer,
          selectedServerAddress,
          selectedServerSshTarget,
          showAllSections,
        }
      : null,
    inventorySection: {
      inventoryActionLabel,
      inventoryBusy,
      inventoryDetectBusy,
      inventoryDraft,
      inventoryError,
      inventoryFieldErrors,
      inventoryFormHeading,
      inventoryFormVisible,
      inventoryImportBadge,
      inventoryImportDiscovery,
      inventoryImportHint,
      inventoryImportMessage,
      inventoryImportStatus,
      inventoryNotice,
      inventoryPrimaryInputRef,
      inventorySectionRef,
      onCancelInventoryForm: handleCancelInventoryForm,
      onCreateServer: handleStartCreateServer,
      onDetectServerSettings: handleDetectServerSettings,
      onResetInventoryForm: handleResetInventoryForm,
      onSubmitServerForm: handleSubmitServerForm,
      onUpdateInventoryField: updateInventoryField,
      serversLength: servers.length,
    },
    sshSection: {
      deployDraft,
      lastSshTestAt,
      onTestSSH: handleTestSSH,
      onUpdateDeployField: updateDeployField,
      savedPrivateKeyPath,
      savedPrivateKeyPathUpdatedAt,
      selectedServer,
      sshAuthReady,
      sshTestBusy,
      sshTestError,
      sshTestNotice,
      sshTestResult,
    },
    telegramSection: {
      onSaveTelegramSettings: telegram.handleSaveTelegramSettings,
      onSendTelegramTestAlert: telegram.handleSendTelegramTestAlert,
      onUpdateTelegramSetting: telegram.updateTelegramSetting,
      savedTelegramTargetReady: telegram.savedTelegramTargetReady,
      telegramError: telegram.telegramError,
      telegramLoading: telegram.telegramLoading,
      telegramNotice: telegram.telegramNotice,
      telegramRepeatPolicy: telegram.telegramRepeatPolicy,
      telegramSaving: telegram.telegramSaving,
      telegramSettings: telegram.telegramSettings,
      telegramTesting: telegram.telegramTesting,
    },
    configDraftSection: selectedServer
      ? {
          actionBusy,
          configDiscoveryReady,
          configDraftSource,
          configLoading,
          configSyncBusy,
          currentConfig,
          draftFields,
          draftPreviewLink,
          effectiveTLSDomain,
          onClearSecret: () => updateField('secret', ''),
          onGenerateDraft: handleGenerateDraft,
          onResetDraftFields: () => setDraftFields(createDraftFields(draftTemplate)),
          onSyncDraftFromServer: handleSyncDraftFromServer,
          onUpdateField: updateField,
          revisions,
          selectedServer,
          tlsDomainWarning,
          unsavedEditorChanges,
        }
      : null,
    configEditorSection: {
      actionBusy,
      configError,
      configLoading,
      configNotice,
      configWarning,
      editorConfigPath,
      editorEmptyHint,
      editorFooterLabel,
      editorHasContent,
      editorSyncLabel,
      editorText,
      editorVersionLabel,
      onChangeEditorText: setEditorText,
      onGenerateDraft: handleGenerateDraft,
      onRollbackEditor: () => setEditorText(currentConfig?.config_text || ''),
      onSaveRevision: handleSaveRevision,
      unsavedEditorChanges,
    },
    deploySection: {
      currentConfig,
      deployBusy,
      deployDecisionHelp,
      deployDecisionOptions,
      deployDraft,
      deployError,
      deployEvents,
      deployHasBlockingRisks,
      deployNotice,
      deployPreview,
      lastDiagnosticsAt,
      lastDiagnosticsAtLabel: formatDateTime(lastDiagnosticsAt),
      onApplyDeploy: handleApplyDeploy,
      onLoadDeployPreview: handleLoadDeployPreview,
      onUpdateDeployField: updateDeployField,
      savedPrivateKeyPath,
      selectedServer,
      sshAuthReady,
    },
    operationsSection: {
      currentHealth,
      linkInfo,
      logsCoverageNote: logs.logsCoverageNote,
      logsData: logs.logsData,
      logsError: logs.logsError,
      logsLiveEnabled: logs.logsLiveEnabled,
      logsLoading: logs.logsLoading,
      logsNotice: logs.logsNotice,
      logsOutputRef,
      logsPage: logs.logsPage,
      logsPageSize: logs.logsPageSize,
      logsPageSummary: logs.logsPageSummary,
      logsStreamState: logs.logsStreamState,
      logsWindowSize: logs.logsWindowSize,
      logsWindowSummary: logs.logsWindowSummary,
      onChangeLogsPageSize: logs.handleLogsPageSizeChange,
      onChangeLogsWindowSize: logs.handleLogsWindowSizeChange,
      onCopyLink: handleCopyLink,
      onLoadLogs: logs.handleLoadLogs,
      onLoadStatus: handleLoadStatus,
      onRestartServer: handleRestartServer,
      operationAuthHelp,
      operationError,
      operationEvents,
      operationNotice,
      operationQueryAuthReady,
      restartBusy,
      savedPrivateKeyPath,
      serverStatus,
      setLogsLiveEnabled: logs.setLogsLiveEnabled,
      setLogsPageIndex: logs.setLogsPageIndex,
      statusError,
      statusLoading,
      statusNotice,
    },
    healthSection: selectedServer
      ? {
          currentHealth,
          healthError: health.healthError,
          healthHistory: health.healthHistory,
          healthLoading: health.healthLoading,
          healthSettings: health.healthSettings,
          selectedServer,
        }
      : null,
  };
}
