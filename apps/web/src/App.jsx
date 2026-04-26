import { useEffect, useRef, useState } from 'react';

import {
  createDeployDraft,
  deployDecisionOptions,
  getDeployDecisionHelp,
  hasBlockingRisks,
  serializeDeployRequest,
  serializeSSHAuthFields,
} from './deploy.js';
import {
  buildLogsPage,
  canUseOperationQueryAuth,
  clampLogsPageIndex,
  describeContainerSummary,
  describeGeneratedLinkSource,
  describePublicPortSummary,
  describeTelemtApiSummary,
  formatLogLineCount,
  getOperationAuthHelp,
  getNewestLogsPageIndex,
  LOGS_PAGE_SIZE_OPTIONS,
  LOGS_WINDOW_OPTIONS,
} from './server-operations.js';
import { buildOperatorSteps, hasConfiguredSSHAuth } from './operator-flow.js';
import {
  buildPreviewLink,
  createDraftFields,
  getTLSDomainWarning,
  logLevelOptions,
  serializeDraftFields,
  telemtFieldDefinitions,
} from './telemt-config.js';
import {
  createServerDraft,
  getApiErrorDetails,
  getNextSelectedServerId,
  serializeServerDraft,
  serverInventoryFields,
} from './server-inventory.js';
import { configsApi } from './api/configsApi.js';
import { deployApi } from './api/deployApi.js';
import { healthApi } from './api/healthApi.js';
import { describeApiError } from './api/api-errors.js';
import { getErrorPayload } from './api/http.js';
import { operationsApi } from './api/operationsApi.js';
import { serversApi } from './api/serversApi.js';
import { settingsApi } from './api/settingsApi.js';
import {
  applyDiscoveryToConfigDraft,
  applyDiscoveryToInventoryDraft,
  buildDiscoveryServerPatch,
  hasDiscoveredConfigFields,
  hasDiscoveredConfigText,
  maskImportedSecret,
  shouldSaveDiscoveredConfig,
} from './mtproto-import.js';
import {
  createTelegramSettingsDraft,
  describeRepeatDownPolicy,
  serializeTelegramSettings,
} from './telegram-alerts.js';
import { describeHealthFlags, describeHealthMessage, describeHealthProblem } from './health-messages.js';
import { releaseVersion } from './release.js';
import { buildServerWorkspacePath, parseWorkspacePath } from './workspace-route.js';

const initialApiState = {
  state: 'checking',
  label: 'Проверка',
  detail: 'GET /health',
};

const defaultHealthSettings = {
  interval: '30s',
  interval_seconds: 30,
};

const defaultTelegramSettings = createTelegramSettingsDraft();

const serverInventoryFieldByKey = Object.fromEntries(serverInventoryFields.map((field) => [field.key, field]));
const telemtFieldByKey = Object.fromEntries(telemtFieldDefinitions.map((field) => [field.key, field]));
const primaryTelemtFieldKeys = ['public_host', 'public_port', 'tls_domain', 'secret'];
const advancedTelemtFieldKeys = ['mask_host', 'mask_port', 'api_port'];
const sidebarVisibleServerLimit = 3;
const sectionIconNames = {
  'ssh-section': 'key',
  'config-section': 'file-code',
  'deploy-section': 'deploy',
  'operations-section': 'terminal',
  'health-section': 'pulse',
  'telegram-section': 'bell',
};

const inventoryFieldGroups = [
  {
    id: 'identity',
    title: 'Имя и SSH-адрес',
    description: 'Как сервер будет называться в панели и по какому адресу панель подключается к нему по SSH.',
    keys: ['name', 'host'],
  },
  {
    id: 'ssh',
    title: 'SSH-доступ',
    description: 'Минимальный набор для диагностики, логов, превью деплоя и операций.',
    keys: ['ssh_user', 'ssh_port', 'private_key_path'],
  },
  {
    id: 'public',
    title: 'Публичный маршрут',
    description: 'Данные, которые попадут в MTProto-ссылку и FakeTLS/SNI-настройки.',
    keys: ['public_host', 'public_ip', 'mtproto_port', 'sni_domain'],
  },
  {
    id: 'remote',
    title: 'Размещение',
    description: 'Папка на сервере, где панель хранит compose-файл, config.toml и backup.',
    keys: ['remote_base_path'],
  },
];

const sameHostImportFieldExample = [
  'host = localhost',
  'ssh_user = <ваш SSH user>',
  'private_key_path = /root/.ssh/<имя_ключа>',
  'remote_base_path = /opt/mtproto-panel/telemt',
].join('\n');

const sameHostImportSetupExample = [
  'mkdir -p ./ssh',
  'cp ~/.ssh/<ваш_ключ> ./ssh/<ваш_ключ>',
  'ssh-keyscan <server-ip> localhost 127.0.0.1 > ./ssh/known_hosts',
  'chmod 600 ./ssh/<ваш_ключ> ./ssh/known_hosts',
].join('\n');

function normalizeHealthState(status) {
  switch ((status || '').toLowerCase()) {
    case 'healthy':
    case 'online':
      return 'online';
    case 'degraded':
      return 'degraded';
    case 'offline':
      return 'offline';
    default:
      return 'unknown';
  }
}

function formatHealthState(status) {
  switch ((status || '').toLowerCase()) {
    case 'healthy':
      return 'Исправен';
    case 'online':
      return 'В сети';
    case 'degraded':
      return 'Деградация';
    case 'offline':
      return 'Недоступен';
    default:
      return 'Неизвестно';
  }
}

function formatStepState(state) {
  switch (state) {
    case 'done':
      return 'Готово';
    case 'active':
      return 'Далее';
    case 'blocked':
      return 'Блокер';
    default:
      return state;
  }
}

function formatLogsStreamState(state) {
  switch (state) {
    case 'idle':
      return 'Ожидание';
    case 'connecting':
      return 'Подключение';
    case 'live':
      return 'Live';
    case 'error':
      return 'Ошибка';
    default:
      return state;
  }
}

function formatDateTime(value) {
  if (!value) {
    return 'Никогда';
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 'Неизвестно';
  }

  return date.toLocaleString('ru-RU');
}

function readCurrentPath() {
  return typeof window !== 'undefined' ? window.location.pathname : '/';
}

function AppIcon({ name, size = 16, className = '' }) {
  const props = {
    'aria-hidden': 'true',
    className: `app-icon ${className}`.trim(),
    fill: 'none',
    height: size,
    stroke: 'currentColor',
    strokeLinecap: 'round',
    strokeLinejoin: 'round',
    strokeWidth: 1.9,
    viewBox: '0 0 24 24',
    width: size,
  };

  switch (name) {
    case 'plus':
      return (
        <svg {...props}>
          <path d="M12 5v14" />
          <path d="M5 12h14" />
        </svg>
      );
    case 'server':
      return (
        <svg {...props}>
          <rect x="3" y="4" width="18" height="6" rx="2" />
          <rect x="3" y="14" width="18" height="6" rx="2" />
          <path d="M7 7h.01" />
          <path d="M7 17h.01" />
          <path d="M11 7h6" />
          <path d="M11 17h6" />
        </svg>
      );
    case 'grid':
      return (
        <svg {...props}>
          <rect x="4" y="4" width="6" height="6" rx="1.5" />
          <rect x="14" y="4" width="6" height="6" rx="1.5" />
          <rect x="4" y="14" width="6" height="6" rx="1.5" />
          <rect x="14" y="14" width="6" height="6" rx="1.5" />
        </svg>
      );
    case 'arrow-left':
      return (
        <svg {...props}>
          <path d="M19 12H5" />
          <path d="m12 19-7-7 7-7" />
        </svg>
      );
    case 'chevron-down':
      return (
        <svg {...props}>
          <path d="m6 9 6 6 6-6" />
        </svg>
      );
    case 'chevron-up':
      return (
        <svg {...props}>
          <path d="m6 15 6-6 6 6" />
        </svg>
      );
    case 'chevron-right':
      return (
        <svg {...props}>
          <path d="m9 6 6 6-6 6" />
        </svg>
      );
    case 'refresh':
      return (
        <svg {...props}>
          <path d="M20 11a8 8 0 0 0-14.9-3" />
          <path d="M4 4v4h4" />
          <path d="M4 13a8 8 0 0 0 14.9 3" />
          <path d="M20 20v-4h-4" />
        </svg>
      );
    case 'copy':
      return (
        <svg {...props}>
          <rect x="9" y="9" width="11" height="11" rx="2" />
          <path d="M6 15H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v1" />
        </svg>
      );
    case 'edit':
      return (
        <svg {...props}>
          <path d="M12 20h9" />
          <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
        </svg>
      );
    case 'trash':
      return (
        <svg {...props}>
          <path d="M3 6h18" />
          <path d="M8 6V4.5A1.5 1.5 0 0 1 9.5 3h5A1.5 1.5 0 0 1 16 4.5V6" />
          <path d="m6 6 1 14a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-14" />
          <path d="M10 11v6" />
          <path d="M14 11v6" />
        </svg>
      );
    case 'workflow':
      return (
        <svg {...props}>
          <circle cx="6" cy="6" r="2" />
          <circle cx="18" cy="18" r="2" />
          <circle cx="18" cy="6" r="2" />
          <path d="M8 6h8" />
          <path d="M6 8v8c0 1.1.9 2 2 2h8" />
        </svg>
      );
    case 'key':
      return (
        <svg {...props}>
          <circle cx="8" cy="15" r="4" />
          <path d="M12 15h9" />
          <path d="M18 12v6" />
          <path d="M15 12v3" />
        </svg>
      );
    case 'file-code':
      return (
        <svg {...props}>
          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8Z" />
          <path d="M14 3v5h5" />
          <path d="m10 13-2 2 2 2" />
          <path d="m14 13 2 2-2 2" />
        </svg>
      );
    case 'deploy':
      return (
        <svg {...props}>
          <path d="M12 3v11" />
          <path d="m8 10 4 4 4-4" />
          <path d="M5 17v2a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-2" />
        </svg>
      );
    case 'terminal':
      return (
        <svg {...props}>
          <rect x="3" y="4" width="18" height="16" rx="2" />
          <path d="m7 9 3 3-3 3" />
          <path d="M13 15h4" />
        </svg>
      );
    case 'pulse':
      return (
        <svg {...props}>
          <path d="M22 12h-4l-2 5-4-10-2 5H2" />
        </svg>
      );
    case 'bell':
      return (
        <svg {...props}>
          <path d="M15 17H5.5a1.5 1.5 0 0 1-1.2-2.4L6 12V9a6 6 0 1 1 12 0v3l1.7 2.6A1.5 1.5 0 0 1 18.5 17H17" />
          <path d="M9 20a3 3 0 0 0 6 0" />
        </svg>
      );
    case 'globe':
      return (
        <svg {...props}>
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18" />
          <path d="M12 3a15 15 0 0 1 0 18" />
          <path d="M12 3a15 15 0 0 0 0 18" />
        </svg>
      );
    case 'folder':
      return (
        <svg {...props}>
          <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" />
        </svg>
      );
    default:
      return (
        <svg {...props}>
          <circle cx="12" cy="12" r="8" />
        </svg>
      );
  }
}

function ButtonLabel({ icon, children }) {
  return (
    <span className="button-content">
      <AppIcon name={icon} size={16} />
      <span>{children}</span>
    </span>
  );
}

function MetaChip({ icon, children }) {
  return (
    <span className="meta-chip-item">
      <AppIcon name={icon} size={14} />
      <span>{children}</span>
    </span>
  );
}

function InlineHint({ text }) {
  return (
    <span aria-label={text} className="inline-hint" role="note" tabIndex={0} title={text}>
      ?
    </span>
  );
}

export default function App() {
  const [apiState, setApiState] = useState(initialApiState);
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
  const [logsData, setLogsData] = useState(null);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logsError, setLogsError] = useState('');
  const [logsNotice, setLogsNotice] = useState('');
  const [logsLiveEnabled, setLogsLiveEnabled] = useState(false);
  const [logsStreamState, setLogsStreamState] = useState('idle');
  const [logsWindowSize, setLogsWindowSize] = useState(300);
  const [logsPageSize, setLogsPageSize] = useState(100);
  const [logsPageIndex, setLogsPageIndex] = useState(0);
  const [healthSettings, setHealthSettings] = useState(defaultHealthSettings);
  const [healthHistory, setHealthHistory] = useState([]);
  const [healthLoading, setHealthLoading] = useState(false);
  const [healthError, setHealthError] = useState('');
  const [telegramSettings, setTelegramSettings] = useState(defaultTelegramSettings);
  const [telegramLoading, setTelegramLoading] = useState(false);
  const [telegramSaving, setTelegramSaving] = useState(false);
  const [telegramTesting, setTelegramTesting] = useState(false);
  const [telegramError, setTelegramError] = useState('');
  const [telegramNotice, setTelegramNotice] = useState('');
  const [currentPath, setCurrentPath] = useState(readCurrentPath);
  const [workspaceMode, setWorkspaceMode] = useState('guided');
  const [activeSectionId, setActiveSectionId] = useState('inventory-section');
  const [inventoryFocusRequest, setInventoryFocusRequest] = useState(0);
  const hasLoadedApiState = useRef(false);
  const hasLoadedServers = useRef(false);
  const inventorySectionRef = useRef(null);
  const inventoryPrimaryInputRef = useRef(null);
  const logsOutputRef = useRef(null);
  const workspaceRef = useRef(null);

  useEffect(() => {
    function handlePopState() {
      setCurrentPath(readCurrentPath());
    }

    window.addEventListener('popstate', handlePopState);

    return () => {
      window.removeEventListener('popstate', handlePopState);
    };
  }, []);

  useEffect(() => {
    let ignore = false;

    async function checkApi({ silent = false } = {}) {
      if (!silent || !hasLoadedApiState.current) {
        setApiState(initialApiState);
      }

      try {
        const payload = await healthApi.getAppHealth();
        if (ignore) {
          return;
        }

        setApiState({
          state: 'online',
          label: 'В сети',
          detail: payload?.service || 'mtproxy-control-api',
        });
        hasLoadedApiState.current = true;
      } catch (error) {
        if (ignore) {
          return;
        }

        setApiState({
          state: 'offline',
          label: 'Недоступен',
          detail: error instanceof Error ? error.message : 'нет ответа',
        });
        hasLoadedApiState.current = true;
      }
    }

    void checkApi();
    const timer = window.setInterval(() => {
      void checkApi({ silent: true });
    }, 15000);

    return () => {
      ignore = true;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    let ignore = false;

    async function loadTelegramSettings() {
      setTelegramLoading(true);
      setTelegramError('');

      try {
        const payload = await settingsApi.getTelegram();
        if (ignore) {
          return;
        }

        applyTelegramSettingsPayload(payload);
      } catch (error) {
        if (ignore) {
          return;
        }

        setTelegramError(error instanceof Error ? error.message : 'Не удалось загрузить настройки Telegram-оповещений.');
        setTelegramSettings(defaultTelegramSettings);
      } finally {
        if (!ignore) {
          setTelegramLoading(false);
        }
      }
    }

    void loadTelegramSettings();

    return () => {
      ignore = true;
    };
  }, []);
  useEffect(() => {
    let ignore = false;

    async function loadHealthSettings() {
      try {
        const payload = await healthApi.getHealthcheckSettings();
        if (ignore) {
          return;
        }

        setHealthSettings(payload || defaultHealthSettings);
      } catch {
        if (!ignore) {
          setHealthSettings(defaultHealthSettings);
        }
      }
    }

    void loadHealthSettings();

    return () => {
      ignore = true;
    };
  }, []);
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
          const routeServerId = parseWorkspacePath(readCurrentPath()).serverId;

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

  const workspaceRoute = parseWorkspacePath(currentPath);
  const isDetailView = workspaceRoute.view === 'detail';
  const isBoardView = !isDetailView;

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
      setLogsData(null);
      setLogsLoading(false);
      setLogsError('');
      setLogsNotice('');
      setLogsLiveEnabled(false);
      setLogsStreamState('idle');
      setLogsPageIndex(0);
      setHealthHistory([]);
      setHealthError('');
      setHealthLoading(false);
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
    if (!selectedServerId) {
      return undefined;
    }

    let ignore = false;

    async function loadHealthHistory({ silent = false } = {}) {
      if (!silent) {
        setHealthLoading(true);
      }
      setHealthError('');

      try {
        const payload = await healthApi.getServerHistory(selectedServerId, { limit: 12 });
        if (ignore) {
          return;
        }

        applyHealthPayload(selectedServerId, payload);
      } catch (error) {
        if (ignore) {
          return;
        }

        setHealthError(error instanceof Error ? error.message : 'Не удалось загрузить историю проверок.');
        if (!silent) {
          setHealthHistory([]);
        }
      } finally {
        if (!ignore && !silent) {
          setHealthLoading(false);
        }
      }
    }

    void loadHealthHistory();
    const timer = window.setInterval(() => {
      void loadHealthHistory({ silent: true });
    }, 15000);

    return () => {
      ignore = true;
      window.clearInterval(timer);
    };
  }, [selectedServerId]);

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
    if (!logsLiveEnabled || !selectedServerId) {
      setLogsStreamState('idle');
      return undefined;
    }

    if (!canUseOperationQueryAuth(deployDraft)) {
      setLogsStreamState('error');
      setLogsError(getOperationAuthHelp(deployDraft));
      return undefined;
    }

    setLogsError('');
    setLogsNotice('');
    setLogsStreamState('connecting');

    const source = new EventSource(operationsApi.getLogsStreamUrl(selectedServerId, deployDraft, { tail: logsWindowSize }));

    source.onopen = () => {
      setLogsStreamState('live');
      setLogsNotice(`Live-стрим подключен. Панель держит только последние ${logsWindowSize} строк и не подгружает полный журнал.`);
    };

    source.addEventListener('logs', (event) => {
      try {
        const payload = JSON.parse(event.data);
        setLogsData(payload);
        setLogsPageIndex(getNewestLogsPageIndex(payload?.result?.stdout || '', logsPageSize));
        setLogsError('');
        setLogsStreamState('live');
      } catch (error) {
        setLogsStreamState('error');
        setLogsError(error instanceof Error ? error.message : 'Не удалось разобрать данные live-логов.');
      }
    });

    source.addEventListener('stream-error', (event) => {
      try {
        const payload = JSON.parse(event.data);
        setLogsStreamState('error');
        setLogsError(describeApiError(payload));
      } catch (error) {
        setLogsStreamState('error');
        setLogsError(error instanceof Error ? error.message : 'Live-стрим логов завершился ошибкой.');
      }
      source.close();
    });

    source.onerror = () => {
      setLogsStreamState('error');
      setLogsError('Live-стрим логов отключился. Повторите попытку, когда снова будет доступна авторизация через путь к SSH-ключу.');
      source.close();
    };

    return () => {
      source.close();
    };
  }, [logsLiveEnabled, selectedServerId, deployDraft, logsPageSize, logsWindowSize]);

  useEffect(() => {
    setLogsPageIndex((current) => clampLogsPageIndex(logsData?.result?.stdout || '', logsPageSize, current));
  }, [logsData?.result?.stdout, logsPageSize]);

  useEffect(() => {
    if (!logsLiveEnabled) {
      return;
    }
    if (logsPageIndex !== getNewestLogsPageIndex(logsData?.result?.stdout || '', logsPageSize)) {
      return;
    }

    const element = logsOutputRef.current;
    if (!element) {
      return;
    }
    element.scrollTop = element.scrollHeight;
  }, [logsData?.fetched_at, logsData?.result?.stdout, logsLiveEnabled, logsPageIndex, logsPageSize]);

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

  const selectedServer = servers.find((server) => server.id === selectedServerId) || null;
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
      if (window.location.pathname !== '/') {
        window.history.replaceState({}, '', '/');
      }
      setCurrentPath('/');
    }
  }, [isDetailView, servers, serversLoading, workspaceRoute.serverId]);

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
  const inventoryFormHeading = inventoryMode === 'edit' ? 'Редактировать выбранный сервер' : servers.length === 0 ? 'Добавить первый сервер' : 'Добавить сервер';
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
  const operationQueryAuthReady = canUseOperationQueryAuth(deployDraft);
  const operationAuthHelp = getOperationAuthHelp(deployDraft);
  const logsPage = buildLogsPage(logsData?.result?.stdout || '', logsPageSize, logsPageIndex);
  const logsWindowSummary = logsLiveEnabled ? `Live-окно ${logsWindowSize} строк` : `Окно ${logsWindowSize} строк`;
  const logsPageSummary =
    logsPage.totalLines > 0
      ? `Страница ${logsPage.currentPage} из ${logsPage.totalPages} · строки ${logsPage.startLine}-${logsPage.endLine} из ${formatLogLineCount(logsPage.totalLines)}`
      : 'Логи еще не загружались';
  const logsCoverageNote = logsLiveEnabled
    ? `Live-режим держит только скользящее окно: до ${logsWindowSize} строк Telemt. Полный журнал контейнера в браузер не подгружается.`
    : `Панель загружает не весь журнал контейнера, а только последний фрагмент: до ${logsWindowSize} строк Telemt по SSH.`;
  const activeLink =
    (linkInfo?.source === 'telemt_api' ? linkInfo.generated_link : '') ||
    serverStatus?.generated_link ||
    linkInfo?.generated_link ||
    currentConfig?.generated_link ||
    '';
  const currentHealth = healthHistory[0] || serverStatus?.latest_health || null;
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
  const telegramRepeatPolicy = describeRepeatDownPolicy(telegramSettings);
  const savedTelegramTargetReady = telegramSettings.telegram_bot_token_configured && telegramSettings.telegram_chat_id.trim() !== '';
  const editorHasContent = editorText.trim() !== '';
  const unsavedEditorChanges = currentConfig
    ? currentConfig.config_text !== editorText
    : editorText.trim() !== '';
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
  const editorSyncLabel = !editorHasContent && !currentConfig ? (configError ? 'Не синхронизировано' : configWarning ? 'Частично синхронизировано' : 'Ожидает загрузки') : unsavedEditorChanges ? 'Есть изменения' : currentConfig ? 'Сохранено' : 'Локально';
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
  const syncDraftTitle = 'Прочитать config.toml и маршрут с сервера по SSH. Ничего не записывает.';
  const resetDraftTitle = 'Вернуть поля формы к последнему локальному шаблону.';
  const newSecretTitle = 'Сгенерировать новый secret в форме. На сервере он изменится только после сохранения ревизии и deploy.';
  const generateDraftTitle = 'Собрать новый config.toml из полей формы и сохранить как ревизию в панели.';
  const rollbackEditorTitle = 'Вернуть текст редактора к последней сохраненной ревизии.';
  const saveRevisionTitle = 'Сохранить текст из редактора как новую ревизию в панели. На сервере пока ничего не меняется.';
  const loadPreviewTitle = 'Проверить текущее состояние сервера и показать, что изменит deploy. Ничего не записывает.';
  const applyDeployTitle = 'Записать текущую ревизию на сервер, сделать backup старых файлов и перезапустить Telemt.';
  const updateStatusTitle = 'Проверить контейнер, Telemt API и текущую ссылку на сервере.';
  const copyLinkTitle = 'Скопировать текущую MTProto-ссылку из панели.';
  const restartTelemtTitle = 'Перезапустить контейнер Telemt на сервере без изменения файлов.';
  const addServerTitle = 'Создать новую запись сервера в инвентаре.';
  const editServerTitle = 'Изменить сохраненные поля выбранного сервера.';
  const deleteServerTitle = 'Удалить сервер и связанные данные из панели.';
  const boardTitle = 'Вернуться к общей доске серверов.';
  const inventoryDetectTitle = 'Прочитать уже установленный Telemt по SSH и перенести найденные настройки MTProto в форму сервера.';
  const inventoryResetTitle = 'Очистить форму и вернуть стартовые значения.';
  const inventoryCancelTitle = 'Закрыть форму без сохранения текущих изменений.';
  const inventorySaveTitle = 'Сохранить сервер в локальный инвентарь панели.';
  const testSshTitle = 'Проверить SSH-подключение и собрать сведения о хосте.';
  const saveTelegramTitle = 'Сохранить настройки Telegram-оповещений в панели.';
  const sendTelegramTestTitle = 'Отправить тестовое сообщение в сохраненный Telegram-чат.';
  const loadLogsTitle = `Загрузить только часть журнала: последние ${logsWindowSize} строк логов Telemt по SSH.`;
  const toggleLogsStreamTitle = logsLiveEnabled
    ? `Остановить live-стрим логов через SSE. Сейчас удерживается окно до ${logsWindowSize} строк.`
    : `Запустить live-стрим логов через SSE. В браузер будет приходить только окно до ${logsWindowSize} строк.`;
  const deployGuideText = selectedServer
    ? `Если Telemt уже запущен вручную и его нужно только взять под мониторинг, сначала нажмите «Подтянуть MTProto» в шаге выше. «Загрузить превью» только читает текущее состояние, а «Применить деплой» заменит panel-managed файлы в ${selectedServer.remote_base_path || 'remote_base_path'}, сохранит backup прошлых версий и перезапустит Telemt.`
    : '«Загрузить превью» только проверяет текущее состояние сервера. «Применить деплой» загружает текущую ревизию, делает backup старых файлов и перезапускает Telemt.';
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
      state: savedTelegramTargetReady ? 'done' : 'active',
      statusLabel: savedTelegramTargetReady ? 'Готово' : 'Опция',
      summary: savedTelegramTargetReady
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
  const nextStepTitle = `Перейти к следующему рекомендуемому шагу: ${nextOperatorStepLabel}.`;

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

  function handleBoardCardKeyDown(event, serverId) {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return;
    }

    event.preventDefault();
    navigateToServerWorkspace(serverId);
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

  function openSection(sectionId, nextMode = 'guided') {
    const resolvedSectionId =
      !selectedServer && ['config-section', 'deploy-section', 'operations-section', 'health-section'].includes(sectionId)
        ? 'inventory-section'
        : sectionId;

    setWorkspaceMode(nextMode);
    setActiveSectionId(resolvedSectionId);

    window.setTimeout(() => {
      if (nextMode === 'all') {
        document.getElementById(resolvedSectionId)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        return;
      }

      window.scrollTo({ top: 0, behavior: 'smooth' });
    }, 0);
  }

  function setWorkspacePath(nextPath, options = {}) {
    const { replace = false } = options;

    if (window.location.pathname !== nextPath) {
      window.history[replace ? 'replaceState' : 'pushState']({}, '', nextPath);
    }

    setCurrentPath(nextPath);
  }

  function navigateToBoard(options = {}) {
    setWorkspacePath('/', options);
  }

  function navigateToServerWorkspace(serverId, options = {}) {
    if (!serverId) {
      navigateToBoard(options);
      return;
    }

    setSelectedServerId(serverId);
    setWorkspaceMode('guided');
    setActiveSectionId((current) => (current === 'inventory-section' ? 'ssh-section' : current));
    setWorkspacePath(buildServerWorkspacePath(serverId), options);
    window.setTimeout(() => {
      if (workspaceRef.current) {
        workspaceRef.current.scrollIntoView({ behavior: 'smooth', block: 'start' });
        return;
      }

      window.scrollTo({ top: 0, behavior: 'smooth' });
    }, 0);
  }

  const shouldRenderSection = (sectionId) => showAllSections || activeSectionId === sectionId;

  function applyHealthPayload(serverId, payload) {
    const checks = Array.isArray(payload?.checks) ? payload.checks : [];
    const latest = payload?.latest || checks[0] || null;

    setHealthHistory(checks);
    setServers((current) =>
      current.map((server) => {
        if (server.id !== serverId || !latest) {
          return server;
        }

        return {
          ...server,
          status: latest.status || server.status,
          last_checked_at: latest.created_at || server.last_checked_at,
        };
      }),
    );
    if (latest) {
      setServerStatus((current) => (current ? { ...current, latest_health: latest } : current));
    }
  }

  function applyTelegramSettingsPayload(payload) {
    setTelegramSettings(createTelegramSettingsDraft(payload?.settings || {}));
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
        isEdit
          ? current.map((server) => (server.id === savedServer.id ? savedServer : server))
          : [...current, savedServer],
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
          ? `MTProto-ссылка для ${server.name} скопирована из источника: ${describeGeneratedLinkSource(nextLink.source).toLowerCase()}.`
          : `MTProto-ссылка для ${server.name} загружена из источника: ${describeGeneratedLinkSource(nextLink.source).toLowerCase()}.`,
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
        await handleLoadLogs({ silent: true });
      }
    } catch (error) {
      setOperationError(error instanceof Error ? error.message : 'Не удалось перезапустить Telemt.');
    } finally {
      setRestartBusy(false);
    }
  }

  async function handleLoadLogs(options = {}) {
    if (!selectedServerId) {
      return;
    }

    if (!operationQueryAuthReady) {
      setLogsError(operationAuthHelp);
      return;
    }

    const { silent = false } = options;
    setLogsLoading(true);
    setLogsError('');
    if (!silent) {
      setLogsNotice('');
    }

    try {
      const payload = await operationsApi.getLogs(selectedServerId, deployDraft, { tail: logsWindowSize });

      setLogsData(payload?.logs || null);
      setLogsPageIndex(getNewestLogsPageIndex(payload?.logs?.result?.stdout || '', logsPageSize));
      syncSavedKeyPathFromDraft();
      if (!silent) {
        setLogsNotice(`Загружен только последний фрагмент журнала: до ${logsWindowSize} строк Telemt. Полный журнал здесь не подгружается.`);
      }
    } catch (error) {
      setLogsError(error instanceof Error ? error.message : 'Не удалось загрузить логи сервера.');
    } finally {
      setLogsLoading(false);
    }
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

  function updateTelegramSetting(key, value) {
    setTelegramSettings((current) => ({
      ...current,
      [key]: value,
    }));
  }

  async function handleSaveTelegramSettings() {
    setTelegramSaving(true);
    setTelegramError('');
    setTelegramNotice('');

    try {
      const payload = await settingsApi.saveTelegram(serializeTelegramSettings(telegramSettings));

      applyTelegramSettingsPayload(payload);
      setTelegramNotice('Настройки Telegram-оповещений сохранены. Пустое поле токена оставляет текущий сохраненный токен без изменений.');
    } catch (error) {
      setTelegramError(error instanceof Error ? error.message : 'Не удалось сохранить настройки Telegram-оповещений.');
    } finally {
      setTelegramSaving(false);
    }
  }

  async function handleSendTelegramTestAlert() {
    setTelegramTesting(true);
    setTelegramError('');
    setTelegramNotice('');

    try {
      await settingsApi.sendTelegramTest();

      setTelegramNotice('Тестовое Telegram-оповещение отправлено с использованием сохраненных токена и chat id.');
    } catch (error) {
      setTelegramError(error instanceof Error ? error.message : 'Не удалось отправить тестовое Telegram-оповещение.');
    } finally {
      setTelegramTesting(false);
    }
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button className="brand" onClick={() => navigateToBoard()} title={boardTitle} type="button">
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
              <span className="sidebar-meta">{servers.length}</span>
              <button className="sidebar-button" onClick={handleStartCreateServer} title={addServerTitle} type="button">
                <ButtonLabel icon="plus">Добавить сервер</ButtonLabel>
              </button>
            </div>
          </div>

          {serversLoading ? <p className="sidebar-note">Загрузка инвентаря...</p> : null}
          {!serversLoading && serversError ? <p className="sidebar-note error-text">{serversError}</p> : null}
          {!serversLoading && !serversError && servers.length === 0 ? (
            <p className="sidebar-note">Добавьте первый сервер, чтобы открыть конфиг, деплой и операционные действия прямо из браузера.</p>
          ) : null}

          <div className="server-list" role="list">
            {visibleSidebarServers.map((server) => {
              const isActive = server.id === selectedServerId;
              const serverHealthState = normalizeHealthState(server.status);
              const serverHealthLabel = formatHealthState(server.status);
              return (
                <button
                  className={`server-item ${isActive ? 'active' : ''}`}
                  key={server.id}
                  onClick={() => navigateToServerWorkspace(server.id)}
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

          {servers.length > sidebarVisibleServerLimit ? (
            <div className="server-list-controls" aria-label="Навигация по списку серверов">
              <button
                aria-label="Показать предыдущие серверы"
                className="server-list-nav-button"
                disabled={!canPageSidebarServersUp}
                onClick={() => shiftSidebarServerWindow(-1)}
                title="Показать предыдущие серверы"
                type="button"
              >
                <AppIcon name="chevron-up" size={16} />
              </button>
              <span className="server-list-window">{`${sidebarVisibleRangeStart}-${sidebarVisibleRangeEnd} из ${servers.length}`}</span>
              <button
                aria-label="Показать следующие серверы"
                className="server-list-nav-button"
                disabled={!canPageSidebarServersDown}
                onClick={() => shiftSidebarServerWindow(1)}
                title="Показать следующие серверы"
                type="button"
              >
                <AppIcon name="chevron-down" size={16} />
              </button>
            </div>
          ) : null}
        </section>
      </aside>

      <main className="workspace" ref={workspaceRef}>
        {isBoardView ? (
          <section className="server-board-shell" aria-label="Серверная доска">
            <article className="panel server-board-panel">
              <div className="board-toolbar">
                <div>
                  <h1>Серверная доска</h1>
                </div>

                <div className="board-toolbar-actions">
                  <div className="board-stat-list" aria-label="Сводка по доске">
                    <span className="board-stat-chip">{`Серверов ${servers.length}`}</span>
                  </div>

                  <button className="primary-button" onClick={handleStartCreateServer} title={addServerTitle} type="button">
                    <ButtonLabel icon="plus">Добавить сервер</ButtonLabel>
                  </button>

                  <div className={`api-pill ${apiState.state}`}>
                    <span className="status-dot" aria-hidden="true" />
                    <span>{apiState.label}</span>
                    <small>{apiState.detail}</small>
                  </div>
                </div>
              </div>

              {operationError ? <p className="inline-error">{operationError}</p> : null}
              {operationNotice ? <p className="inline-success">{operationNotice}</p> : null}
              {serversLoading ? <p className="panel-note">Загрузка инвентаря...</p> : null}
              {!serversLoading && serversError ? <p className="inline-error">{serversError}</p> : null}
              {!serversLoading && !serversError && servers.length === 0 ? (
                <div className="board-empty-state">
                  <p>На доске пока нет серверов. Добавьте первый хост и затем подтяните существующий Telemt прямо из UI.</p>
                </div>
              ) : null}

              <div className="server-board-grid">
                {servers.map((server) => {
                  const isActive = server.id === selectedServerId;
                  const serverHealthState = normalizeHealthState(server.status);

                  return (
                    <article
                      className={`server-board-card ${isActive ? 'active' : ''}`}
                      key={server.id}
                      onClick={() => navigateToServerWorkspace(server.id)}
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
                            navigateToServerWorkspace(server.id);
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
                            void copyServerLink(server);
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
        ) : (
          <section className="detail-workspace-shell" aria-label="Рабочая зона сервера">
            {!selectedServer ? (
              <article className="panel detail-workspace-panel">
                <div className="board-empty-state board-focus-empty">
                  <p>Сервер из маршрута не найден в текущем инвентаре. Вернитесь на доску и выберите актуальную карточку.</p>
                  <button className="primary-button" onClick={() => navigateToBoard({ replace: true })} title={boardTitle} type="button">
                    Вернуться к доске
                  </button>
                </div>
              </article>
            ) : (
              <article className="panel detail-workspace-panel">
                <div className="detail-workspace-header">
                  <div>
                    <p className="eyebrow">Рабочая зона</p>
                    <h1>{selectedServer.name}</h1>
                  </div>

                  <div className="detail-workspace-actions">
                    <button className="secondary-button workspace-action-button" onClick={() => navigateToBoard()} title={boardTitle} type="button">
                      <ButtonLabel icon="arrow-left">К доске</ButtonLabel>
                    </button>
                    <span className={`workspace-action-badge ${normalizeHealthState(currentHealth?.status || selectedServer.status)}`}>{healthSummary}</span>
                    <button className="secondary-button workspace-action-button workspace-action-accent" onClick={() => openSection(nextOperatorStep.target)} title={nextStepTitle} type="button">
                      <ButtonLabel icon="workflow">{`Следующий шаг: ${nextOperatorStepLabel}`}</ButtonLabel>
                    </button>
                    <button className="secondary-button workspace-action-button" onClick={() => handleLoadStatus()} title={updateStatusTitle} type="button">
                      <ButtonLabel icon="refresh">Обновить статус</ButtonLabel>
                    </button>
                    <button className="secondary-button workspace-action-button" onClick={handleCopyLink} title={copyLinkTitle} type="button">
                      <ButtonLabel icon="copy">Скопировать ссылку</ButtonLabel>
                    </button>
                    <button className="secondary-button workspace-action-button" onClick={handleStartEditServer} title={editServerTitle} type="button">
                      <ButtonLabel icon="edit">Редактировать сервер</ButtonLabel>
                    </button>
                    <button className="secondary-button workspace-action-button workspace-action-danger" disabled={inventoryBusy} onClick={handleDeleteServer} title={deleteServerTitle} type="button">
                      <ButtonLabel icon="trash">Удалить сервер</ButtonLabel>
                    </button>
                  </div>
                </div>

                <div className="board-focus-meta detail-workspace-meta">
                  <MetaChip icon="globe">{selectedServerAddress}</MetaChip>
                  <MetaChip icon="key">{selectedServerSshTarget}</MetaChip>
                  <MetaChip icon="folder">{selectedServer.remote_base_path}</MetaChip>
                  <MetaChip icon="key">{savedPrivateKeyPath || 'SSH key не сохранен'}</MetaChip>
                  <MetaChip icon="file-code">{currentConfig ? `Ревизия v${currentConfig.version}` : 'Конфиг еще не сохранен'}</MetaChip>
                  <MetaChip icon="refresh">{lastDiagnosticsAt ? `Preview ${formatDateTime(lastDiagnosticsAt)}` : 'Preview не запускался'}</MetaChip>
                  <MetaChip icon="pulse">{currentHealth?.created_at ? `Health ${formatDateTime(currentHealth.created_at)}` : 'Health еще не записан'}</MetaChip>
                </div>

                <nav className="section-switcher detail-section-switcher" aria-label="Разделы рабочей зоны">
                  {detailSectionTabs.map((section) => {
                    const isActive = activeSectionId === section.id && !showAllSections;

                    return (
                      <button className={`section-tab state-${section.state} ${isActive ? 'active' : ''}`} key={section.id} onClick={() => openSection(section.id)} title={`Открыть раздел «${section.label}».`} type="button">
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
            )}
          </section>
        )}

        {isBoardView && inventoryFormVisible ? (
          <section className="panel inventory-panel" id="inventory-section" ref={inventorySectionRef}>
          <div className="panel-header">
            <div>
              <p className="eyebrow">Шаг 1</p>
              <h2>Инвентарь серверов</h2>
            </div>

            <div className="button-row">
              <button className="secondary-button" disabled={inventoryBusy} onClick={handleStartCreateServer} title={addServerTitle} type="button">
                Добавить сервер
              </button>
            </div>
          </div>

          {inventoryError ? <p className="inline-error">{inventoryError}</p> : null}
          {inventoryNotice ? <p className="inline-success">{inventoryNotice}</p> : null}

          <div className="inventory-grid">
            <article className="inventory-column inventory-form-column">
              <div className="deploy-section-header">
                <strong>{inventoryFormHeading}</strong>
              </div>

              {inventoryFormVisible ? (
                <>
                  <article className="inventory-import-card">
                    <div className="inventory-import-header">
                      <div>
                        <span className="preview-label">Импорт из существующего Telemt</span>
                        <div className="title-with-hint inventory-import-title">
                          <strong>Импортировать MTProto</strong>
                          <InlineHint text={inventoryImportHint} />
                        </div>
                      </div>
                      <span className={`status-chip ${inventoryImportBadge.tone}`}>{inventoryImportBadge.label}</span>
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
                        disabled={inventoryBusy || inventoryDetectBusy}
                        onClick={handleDetectServerSettings}
                        title={inventoryDetectTitle}
                        type="button"
                      >
                        {inventoryDetectBusy ? 'Читаем config.toml...' : 'Импортировать MTProto'}
                      </button>
                    </div>

                    {inventoryImportStatus === 'success' ? <p className="inline-success">{inventoryImportMessage}</p> : null}
                    {inventoryImportStatus === 'error' ? <p className="inline-error">{inventoryImportMessage}</p> : null}

                    {inventoryImportDiscovery ? (
                      <dl className="health-list compact-list inventory-import-list">
                        <div>
                          <dt>Config path</dt>
                          <dd>{inventoryImportDiscovery.config_path}</dd>
                        </div>
                        <div>
                          <dt>public_host</dt>
                          <dd>{inventoryImportDiscovery.public_host || 'Не найден'}</dd>
                        </div>
                        <div>
                          <dt>mtproto_port</dt>
                          <dd>{inventoryImportDiscovery.mtproto_port != null ? String(inventoryImportDiscovery.mtproto_port) : 'Не найден'}</dd>
                        </div>
                        <div>
                          <dt>sni_domain</dt>
                          <dd>{inventoryImportDiscovery.sni_domain || 'Не найден'}</dd>
                        </div>
                        <div>
                          <dt>Папка Telemt на сервере</dt>
                          <dd>{inventoryImportDiscovery.remote_base_path || 'Не найден'}</dd>
                        </div>
                        <div>
                          <dt>secret</dt>
                          <dd>{maskImportedSecret(inventoryImportDiscovery.secret) || 'Не найден'}</dd>
                        </div>
                      </dl>
                    ) : null}
                  </article>

                  <form className="config-form server-inventory-form" onSubmit={handleSubmitServerForm}>
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
                            const fieldError = inventoryFieldErrors[field.key] || '';

                            return (
                              <label className="field-card" key={field.key}>
                                <span className="field-label title-with-hint">
                                  <span>{field.label}</span>
                                  {field.description ? <InlineHint text={field.description} /> : null}
                                </span>
                                <input
                                  className={`text-input ${fieldError ? 'input-error' : ''}`}
                                  min={field.type === 'number' ? '1' : undefined}
                                  onChange={(event) => updateInventoryField(field.key, event.target.value)}
                                  placeholder={field.placeholder}
                                  ref={field.key === 'name' ? inventoryPrimaryInputRef : undefined}
                                  type={field.type}
                                  value={inventoryDraft[field.key]}
                                />
                                {fieldError ? <span className="field-error">{fieldError}</span> : null}
                              </label>
                            );
                          })}
                        </div>
                      </section>
                    ))}

                    <div className="form-actions">
                      <button className="secondary-button" disabled={inventoryBusy} onClick={handleResetInventoryForm} title={inventoryResetTitle} type="button">
                        Сбросить
                      </button>
                      {servers.length > 0 ? (
                        <button className="secondary-button" disabled={inventoryBusy} onClick={handleCancelInventoryForm} title={inventoryCancelTitle} type="button">
                          Отмена
                        </button>
                      ) : null}
                      <button className="primary-button" disabled={inventoryBusy} title={inventorySaveTitle} type="submit">
                        {inventoryBusy ? 'Сохранение...' : inventoryActionLabel}
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
        ) : null}

        {isDetailView && shouldRenderSection('ssh-section') ? (
          <section className="panel ssh-panel" id="ssh-section">
          <div className="panel-header">
            <div>
              <p className="eyebrow">Шаг 2</p>
              <h2>SSH-доступ и сведения о хосте</h2>
            </div>

            <div className="button-row">
              <button className="primary-button" disabled={!selectedServer || sshTestBusy} onClick={handleTestSSH} title={testSshTitle} type="button">
                <ButtonLabel icon="key">{sshTestBusy ? 'Проверка...' : 'Проверить SSH'}</ButtonLabel>
              </button>
            </div>
          </div>

          {sshTestError ? <p className="inline-error">{sshTestError}</p> : null}
          {sshTestNotice ? <p className="inline-success">{sshTestNotice}</p> : null}

          {!selectedServer ? (
            <p className="panel-note">Сначала добавьте или выберите сервер, а уже потом настраивайте SSH и запускайте проверку подключения.</p>
          ) : (
            <div className="operations-grid ssh-grid">
              <article className="operations-column">
                <label className="field-card">
                  <span className="field-label">Способ входа</span>
                  <select className="text-input" onChange={(event) => updateDeployField('auth_type', event.target.value)} value={deployDraft.auth_type}>
                    <option value="password">Пароль пользователя</option>
                    <option value="private_key_path">Путь к приватному ключу</option>
                    <option value="private_key_text">Текст приватного ключа</option>
                  </select>
                  <span className="field-hint">Для локальной работы и повторяемых действий удобнее использовать путь к ключу на машине API. Пароль не сохраняется в панели.</span>
                </label>

                {deployDraft.auth_type === 'password' ? (
                  <label className="field-card">
                    <span className="field-label">Пароль SSH</span>
                    <input
                      className="text-input"
                      onChange={(event) => updateDeployField('password', event.target.value)}
                      placeholder="Не сохраняется"
                      type="password"
                      value={deployDraft.password}
                    />
                    <span className="field-hint">Отправляется только в текущем POST-запросе. Для логов, SSE и live-статуса все равно нужен private_key_path.</span>
                  </label>
                ) : deployDraft.auth_type === 'private_key_path' ? (
                  <label className="field-card">
                    <span className="field-label">Путь к приватному ключу</span>
                    <input
                      className="text-input"
                      onChange={(event) => updateDeployField('private_key_path', event.target.value)}
                      placeholder="~/.ssh/proxy-node"
                      type="text"
                      value={deployDraft.private_key_path}
                    />
                    <span className="field-hint">Путь разворачивается на хосте API перед началом SSH-подключения.</span>
                  </label>
                ) : (
                  <label className="field-card">
                    <span className="field-label">Текст приватного ключа</span>
                    <textarea
                      className="code-editor compact-editor"
                      onChange={(event) => updateDeployField('private_key_text', event.target.value)}
                      placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                      spellCheck="false"
                      value={deployDraft.private_key_text}
                    />
                    <span className="field-hint">Отправляется только с текущим запросом. Не держите raw-ключ в браузере дольше необходимого.</span>
                  </label>
                )}

                {deployDraft.auth_type !== 'password' ? (
                  <label className="field-card">
                    <span className="field-label">Парольная фраза</span>
                    <input
                      className="text-input"
                      onChange={(event) => updateDeployField('passphrase', event.target.value)}
                      placeholder="Необязательно"
                      type="password"
                      value={deployDraft.passphrase}
                    />
                    <span className="field-hint">Нужна только для зашифрованных SSH-ключей.</span>
                  </label>
                ) : null}

                {!sshAuthReady ? <p className="panel-note">Сначала настройте SSH. Эти же данные используются для превью деплоя и SSH-команд на сервере.</p> : null}
                {deployDraft.auth_type === 'password' ? <p className="panel-note">Парольный вход работает для POST-операций. Для live-логов, SSE и live-статуса сохраните <code>private_key_path</code>.</p> : null}
              </article>

              <article className="operations-column">
                <div className="link-card">
                  <span className="preview-label">Сохраненный путь к ключу</span>
                  <code>{savedPrivateKeyPath || 'Путь private_key_path еще не сохранен.'}</code>
                  <div className="link-meta-row">
                    <span>{savedPrivateKeyPathUpdatedAt ? `Сохранено ${formatDateTime(savedPrivateKeyPathUpdatedAt)}` : 'Будет сохранен после SSH-проверки, превью или деплоя с path-auth'}</span>
                    <span>{lastSshTestAt ? `Последняя проверка ${formatDateTime(lastSshTestAt)}` : 'В этой сессии проверка еще не запускалась'}</span>
                  </div>
                </div>

                <dl className="health-list compact-list">
                  <div>
                    <dt>Целевой хост</dt>
                    <dd>{`${selectedServer.ssh_user}@${selectedServer.host}:${selectedServer.ssh_port}`}</dd>
                  </div>
                  <div>
                    <dt>Имя хоста</dt>
                    <dd>{sshTestResult?.facts?.hostname || 'Запустите SSH-проверку, чтобы получить сведения о хосте'}</dd>
                  </div>
                  <div>
                    <dt>Текущий пользователь</dt>
                    <dd>{sshTestResult?.facts?.current_user || 'Еще не загружено'}</dd>
                  </div>
                  <div>
                    <dt>Архитектура</dt>
                    <dd>{sshTestResult?.facts?.architecture || 'Еще не загружено'}</dd>
                  </div>
                  <div>
                    <dt>Docker</dt>
                    <dd>{sshTestResult?.facts?.docker_version || 'Запустите SSH-проверку, чтобы проверить Docker'}</dd>
                  </div>
                  <div>
                    <dt>Docker Compose</dt>
                    <dd>{sshTestResult?.facts?.docker_compose_version || 'Запустите SSH-проверку, чтобы проверить Compose'}</dd>
                  </div>
                </dl>

                <div className="deploy-list-block">
                  <div className="revision-header">
                    <span>Команды SSH-проверки</span>
                    <span>{sshTestResult?.commands?.length || 0}</span>
                  </div>
                  {sshTestResult?.commands?.length ? (
                    <div className="deploy-list">
                      {sshTestResult.commands.map((command) => (
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
        ) : null}

        {isDetailView && shouldRenderSection('telegram-section') ? (
          <section className="panel telegram-panel" id="telegram-section">
          <div className="panel-header">
            <div>
              <p className="eyebrow">Опционально</p>
              <h2>Telegram-оповещения</h2>
            </div>

            <div className="button-row">
              <button className="secondary-button" disabled={telegramSaving || telegramLoading} onClick={handleSaveTelegramSettings} title={saveTelegramTitle} type="button">
                {telegramSaving ? 'Сохранение...' : 'Сохранить'}
              </button>
              <button className="primary-button" disabled={telegramTesting || telegramLoading || !savedTelegramTargetReady} onClick={handleSendTelegramTestAlert} title={sendTelegramTestTitle} type="button">
                {telegramTesting ? 'Отправка...' : 'Тестовое сообщение'}
              </button>
            </div>
          </div>

          {telegramError ? <p className="inline-error">{telegramError}</p> : null}
          {telegramNotice ? <p className="inline-success">{telegramNotice}</p> : null}

          <div className="operations-grid telegram-grid">
            <article className="operations-column">
              <div className="link-card telegram-status-card">
                <span className="preview-label">Текущий маршрут оповещений</span>
                <code>{savedTelegramTargetReady ? telegramSettings.telegram_chat_id : 'Получатель Telegram еще не сохранен.'}</code>
                <div className="link-meta-row">
                  <span>{telegramSettings.telegram_bot_token_configured ? `Токен ${telegramSettings.telegram_bot_token_masked || 'настроен'}` : 'Токен еще не сохранен'}</span>
                  <span>{telegramSettings.alerts_enabled ? 'Оповещения включены' : 'Оповещения выключены'}</span>
                </div>
              </div>

              <dl className="health-list compact-list">
                <div>
                  <dt>Сохраненный токен</dt>
                  <dd>{telegramSettings.telegram_bot_token_configured ? telegramSettings.telegram_bot_token_masked || 'Настроен' : 'Не настроен'}</dd>
                </div>
                <div>
                  <dt>Сохраненный чат</dt>
                  <dd>{telegramSettings.telegram_chat_id || 'Не настроен'}</dd>
                </div>
                <div>
                  <dt>Политика повторов</dt>
                  <dd>{telegramRepeatPolicy}</dd>
                </div>
              </dl>
            </article>

            <article className="operations-column">
              {telegramLoading ? <p className="panel-note">Загрузка настроек Telegram-оповещений...</p> : null}

              <div className="config-form telegram-settings-form">
                <label className="field-card">
                  <span className="field-label">Токен бота</span>
                  <input
                    className="text-input"
                    onChange={(event) => updateTelegramSetting('telegram_bot_token', event.target.value)}
                    placeholder={telegramSettings.telegram_bot_token_configured ? 'Оставьте пустым, чтобы сохранить текущий токен' : '123456:ABCDEF'}
                    type="password"
                    value={telegramSettings.telegram_bot_token}
                  />
                  <span className="field-hint">В ответах plaintext-токен никогда не возвращается. Сохраняйте новое значение только если хотите ротировать токен.</span>
                </label>

                <label className="field-card">
                  <span className="field-label">Chat id</span>
                  <input
                    className="text-input"
                    onChange={(event) => updateTelegramSetting('telegram_chat_id', event.target.value)}
                    placeholder="-100123456"
                    type="text"
                    value={telegramSettings.telegram_chat_id}
                  />
                  <span className="field-hint">Используйте прямой chat id или username канала, куда бот может писать.</span>
                </label>

                <label className="field-card">
                  <span className="field-label">Повторять down через минут</span>
                  <input
                    className="text-input"
                    min="0"
                    onChange={(event) => updateTelegramSetting('repeat_down_after_minutes', event.target.value)}
                    placeholder="0"
                    type="number"
                    value={telegramSettings.repeat_down_after_minutes}
                  />
                  <span className="field-hint">Укажите `0`, чтобы отправлять оповещения только при смене состояния. Любое положительное значение включает повторные down-алерты с ограничением частоты.</span>
                </label>

                <label className="field-card checkbox-card telegram-toggle-card">
                  <span className="field-label">Оповещения включены</span>
                  <div className="checkbox-row">
                    <input
                      checked={telegramSettings.alerts_enabled}
                      onChange={(event) => updateTelegramSetting('alerts_enabled', event.target.checked)}
                      type="checkbox"
                    />
                    <span>Отправлять в Telegram оповещения о недоступности, деградации, восстановлении и неудачном деплое.</span>
                  </div>
                </label>
              </div>

              <p className="panel-note">Сохраните настройки перед тестовой отправкой. Тест использует сохраненные токен и chat id, а не несохраненные значения формы.</p>
            </article>
          </div>
          </section>
        ) : null}

        {!selectedServer ? (
          isDetailView && (
            shouldRenderSection('config-section') ||
            shouldRenderSection('deploy-section') ||
            shouldRenderSection('operations-section') ||
            shouldRenderSection('health-section')
          ) ? (
          <section className="panel empty-panel">
            <div>
              <p className="eyebrow">Нет цели</p>
              <h2>Нужен сервер</h2>
            </div>
            <p>
              Сначала добавьте сервер в инвентарь. Затем подтвердите SSH-доступ, и редактор конфига унаследует `public_host`, `mtproto_port` и
              `sni_domain` из сохраненной записи.
            </p>
            <div className="button-row">
              <button className="primary-button" disabled={inventoryBusy} onClick={handleStartCreateServer} title={addServerTitle} type="button">
                Добавить сервер
              </button>
            </div>
          </section>
          ) : null
        ) : (
          <>
            {isDetailView && shouldRenderSection('config-section') ? (
              <section className="content-grid" id="config-section">
              <article className="panel primary-panel">
                <div className="panel-header">
                  <div>
                    <p className="eyebrow">Шаг 3</p>
                    <h2>Черновик MTProto</h2>
                  </div>
                  <div className="button-row">
                    <button
                      className="secondary-button"
                      disabled={!configDiscoveryReady || configLoading || actionBusy || configSyncBusy}
                      onClick={handleSyncDraftFromServer}
                      title={syncDraftTitle}
                      type="button"
                    >
                        {configSyncBusy ? 'Чтение...' : 'Подтянуть MTProto'}
                    </button>
                    <button
                      className="secondary-button"
                      onClick={() => setDraftFields(createDraftFields(draftTemplate))}
                      title={resetDraftTitle}
                      type="button"
                    >
                      Сбросить
                    </button>
                    <button
                      className="secondary-button"
                      onClick={() => updateField('secret', '')}
                      title={newSecretTitle}
                      type="button"
                    >
                      Новый secret
                    </button>
                  </div>
                </div>

                <div className="config-identity-strip">
                  <strong>{selectedServer.name}</strong>
                  <span>{draftFields.public_host || selectedServer.public_host || selectedServer.host}</span>
                  <span>{draftFields.public_port || selectedServer.mtproto_port || '443'}</span>
                  <span>{effectiveTLSDomain || 'tls_domain не задан'}</span>
                  <span>{configDraftSource}</span>
                </div>

                {tlsDomainWarning ? <p className="inline-warning compact-note">{tlsDomainWarning}</p> : null}
                {!configDiscoveryReady ? <p className="panel-note compact-note">Для кнопки «Подтянуть MTProto» сохраните путь к SSH-ключу в карточке сервера.</p> : null}

                <form className="config-form compact-config-form" onSubmit={handleGenerateDraft}>
                  <div className="config-field-grid">
                    {primaryTelemtFieldKeys.map((fieldKey) => {
                      const field = telemtFieldByKey[fieldKey];

                      return (
                        <label className="field-card compact-field-card" key={field.key}>
                          <span className="field-label">{field.label}</span>
                          <input
                            className="text-input"
                            onChange={(event) => updateField(field.key, event.target.value)}
                            placeholder={field.placeholder}
                            type={field.type}
                            value={draftFields[field.key]}
                          />
                        </label>
                      );
                    })}
                  </div>

                  <details className="advanced-settings">
                    <summary>Дополнительно</summary>
                    <div className="config-field-grid advanced-field-grid">
                      {advancedTelemtFieldKeys.map((fieldKey) => {
                        const field = telemtFieldByKey[fieldKey];

                        return (
                          <label className="field-card" key={field.key}>
                            <span className="field-label">{field.label}</span>
                            <input
                              className="text-input"
                              onChange={(event) => updateField(field.key, event.target.value)}
                              placeholder={field.placeholder}
                              type={field.type}
                              value={draftFields[field.key]}
                            />
                          </label>
                        );
                      })}

                      <label className="field-card checkbox-card">
                        <span className="field-label">Использовать middle proxy</span>
                        <div className="checkbox-row">
                          <input
                            checked={draftFields.use_middle_proxy}
                            onChange={(event) => updateField('use_middle_proxy', event.target.checked)}
                            type="checkbox"
                          />
                          <span>Оставить включенным</span>
                        </div>
                      </label>

                      <label className="field-card">
                        <span className="field-label">Уровень логов</span>
                        <select
                          className="text-input"
                          onChange={(event) => updateField('log_level', event.target.value)}
                          value={draftFields.log_level}
                        >
                          {logLevelOptions.map((option) => (
                            <option key={option} value={option}>
                              {option}
                            </option>
                          ))}
                        </select>
                      </label>
                    </div>
                  </details>

                  <div className="form-actions">
                    <button className="primary-button" disabled={actionBusy || configLoading} title={generateDraftTitle} type="submit">
                      {actionBusy ? 'Выполняется...' : 'Сгенерировать'}
                    </button>
                  </div>
                </form>
              </article>

              <article className="panel secondary-panel">
                <div>
                  <p className="eyebrow">Превью</p>
                  <h2>Ссылка</h2>
                </div>

                <div className="preview-card">
                  <span className="preview-label">{configDraftSource}</span>
                  <code>{draftPreviewLink || 'Заполните хост, порт, TLS-домен и secret.'}</code>
                </div>

                <dl className="health-list compact-list">
                  <div>
                    <dt>Сохранено</dt>
                    <dd>{currentConfig?.generated_link || 'Ревизии еще нет'}</dd>
                  </div>
                  <div>
                    <dt>Редактор</dt>
                    <dd>{unsavedEditorChanges ? 'Есть изменения' : 'Без изменений'}</dd>
                  </div>
                  <div>
                    <dt>Путь</dt>
                    <dd>{selectedServer.remote_base_path}</dd>
                  </div>
                </dl>

                {revisions.length > 0 ? (
                  <details className="advanced-settings revision-details">
                    <summary>{`Ревизии (${revisions.length})`}</summary>
                    <div className="revision-list">
                      {revisions.map((revision) => (
                        <article className="revision-item" key={revision.id}>
                          <strong>{`v${revision.version}`}</strong>
                          <span>{new Date(revision.created_at).toLocaleString('ru-RU')}</span>
                          <span>{revision.generated_link || 'Превью не записано'}</span>
                        </article>
                      ))}
                    </div>
                  </details>
                ) : null}
              </article>
              </section>
            ) : null}

            {isDetailView && shouldRenderSection('config-section') ? (
              <section className="panel editor-panel config-editor-panel">
              <div className="panel-header editor-panel-header">
                <div>
                  <p className="eyebrow">Исходный TOML</p>
                  <h2>Текст конфига Telemt</h2>
                </div>

                <div className="editor-panel-controls">
                  <div className="editor-status-strip">
                    <span className={`editor-state-badge ${editorHasContent ? 'ready' : 'idle'}`}>{editorVersionLabel}</span>
                    <span className={`editor-state-badge ${unsavedEditorChanges ? 'warning' : 'synced'}`}>{editorSyncLabel}</span>
                  </div>

                  <div className="button-row">
                    <button
                      className="secondary-button"
                      disabled={actionBusy || configLoading}
                      onClick={() => setEditorText(currentConfig?.config_text || '')}
                      title={rollbackEditorTitle}
                      type="button"
                    >
                      Откатить редактор
                    </button>
                    <button
                      className="primary-button"
                      disabled={actionBusy || configLoading || editorText.trim() === ''}
                      onClick={handleSaveRevision}
                      title={saveRevisionTitle}
                      type="button"
                    >
                      {actionBusy ? 'Выполняется...' : 'Сохранить ревизию'}
                    </button>
                  </div>
                </div>
              </div>

              {configLoading ? <p className="panel-note">Загрузка состояния конфига...</p> : null}
              {configError ? <p className="inline-error">{configError}</p> : null}
              {configWarning ? <p className="inline-warning">{configWarning}</p> : null}
              {configNotice ? <p className="inline-success">{configNotice}</p> : null}

              {!editorHasContent ? (
                <div className="editor-empty-note">
                  <strong>Редактор пока пустой</strong>
                  <p>{editorEmptyHint}</p>
                  <div className="button-row">
                    <button className="primary-button" disabled={actionBusy || configLoading} onClick={handleGenerateDraft} title={generateDraftTitle} type="button">
                      {actionBusy ? 'Выполняется...' : 'Сгенерировать TOML'}
                    </button>
                  </div>
                </div>
              ) : null}

              <div className={`editor-frame ${!editorHasContent ? 'empty' : ''}`}>
                <div className="editor-frame-header">
                  <span className="editor-frame-label">config.toml</span>
                  <span className="editor-frame-path">{editorConfigPath}</span>
                </div>

                <textarea
                  className={`code-editor ${!editorHasContent ? 'code-editor-empty' : ''}`}
                  onChange={(event) => setEditorText(event.target.value)}
                  placeholder="Здесь появится config.toml. Нажмите «Подтянуть MTProto», чтобы открыть файл с сервера, или «Сгенерировать», чтобы собрать новый TOML из полей выше."
                  spellCheck="false"
                  value={editorText}
                />
              </div>

              <div className="editor-footer">
                <span>{editorFooterLabel}</span>
                <span>{editorConfigPath}</span>
              </div>
              </section>
            ) : null}

            {isDetailView && shouldRenderSection('deploy-section') ? (
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
                  <button className="secondary-button" disabled={deployBusy} onClick={handleLoadDeployPreview} title={loadPreviewTitle} type="button">
                    {deployBusy ? 'Выполняется...' : 'Загрузить превью'}
                  </button>
                  <button className="primary-button" disabled={deployBusy} onClick={handleApplyDeploy} title={applyDeployTitle} type="button">
                    {deployBusy ? 'Выполняется...' : 'Применить деплой'}
                  </button>
                </div>
              </div>

              <p className="panel-note deploy-guide-note">{deployGuideText}</p>

              {deployError ? <p className="inline-error">{deployError}</p> : null}
              {deployNotice ? <p className="inline-success">{deployNotice}</p> : null}
              {deployHasBlockingRisks ? (
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
                    <code>{currentConfig ? `Конфиг v${currentConfig.version}` : 'Сохраненного конфига пока нет.'}</code>
                    <div className="link-meta-row">
                      <span>{savedPrivateKeyPath || 'Путь к ключу еще не сохранен'}</span>
                      <span>{lastDiagnosticsAt ? `Превью ${formatDateTime(lastDiagnosticsAt)}` : 'Превью еще не загружено'}</span>
                    </div>
                  </div>

                  <label className="field-card checkbox-card">
                    <span className="field-label title-with-hint">
                      <span>Подтвердить блокеры</span>
                      <InlineHint text="Разрешает deploy даже при блокирующих рисках из превью. Включайте только после проверки причин." />
                    </span>
                    <div className="checkbox-row">
                      <input
                        checked={deployDraft.confirm_blockers}
                        onChange={(event) => updateDeployField('confirm_blockers', event.target.checked)}
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
                      onChange={(event) => updateDeployField('port_conflict_decision', event.target.value)}
                      title="Выберите действие только если превью требует решения по занятому публичному порту."
                      value={deployDraft.port_conflict_decision}
                    >
                      <option value="">Выбирайте только если этого требует превью</option>
                      {deployDecisionOptions.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                    <span className="field-hint">Нужно только если публичный порт MTProto уже занят другим сервисом.</span>
                  </label>

                  {!currentConfig ? <p className="panel-note">Сначала сгенерируйте или сохраните конфиг. Без сохраненной ревизии превью деплоя не запустится.</p> : null}
                  {!sshAuthReady ? <p className="panel-note">Настройте SSH в блоке «SSH-доступ» перед загрузкой превью деплоя.</p> : null}
                  {deployDecisionHelp ? <p className="panel-note">{deployDecisionHelp}</p> : null}
                </article>

                <article className="deploy-column">
                  <div className="deploy-section-header">
                    <strong className="title-with-hint">
                      <span>Превью</span>
                      <InlineHint text="Превью ничего не меняет на сервере. Оно только показывает будущие файлы, команды, порты и риски." />
                    </strong>
                    <span>{deployPreview ? 'Диагностика загружена' : 'Сначала запустите превью'}</span>
                  </div>

                  {!deployPreview ? (
                    <p className="panel-note">Запустите превью деплоя после настройки SSH и сохранения конфига, чтобы проверить удаленные файлы, порты, бэкапы, риски и точные команды.</p>
                  ) : (
                    <div className="deploy-stack">
                      <dl className="health-list compact-list">
                        <div>
                          <dt>Последняя диагностика</dt>
                          <dd>{lastDiagnosticsAt ? formatDateTime(lastDiagnosticsAt) : 'Загружено в текущей сессии'}</dd>
                        </div>
                        <div>
                          <dt>Папка Telemt на сервере</dt>
                          <dd>{deployPreview.remote_base_path}</dd>
                        </div>
                        <div>
                          <dt>Docker-образ</dt>
                          <dd>{deployPreview.docker_image}</dd>
                        </div>
                        <div>
                          <dt>Требует подтверждения</dt>
                          <dd>{deployPreview.requires_confirmation ? 'Да' : 'Нет'}</dd>
                        </div>
                        <div>
                          <dt>Уже развернутый инстанс панели</dt>
                          <dd>{deployPreview.existing_panel_instance ? 'Обнаружен' : 'Не обнаружен'}</dd>
                        </div>
                      </dl>

                      {deployPreview.required_decision ? <p className="inline-warning">{deployPreview.required_decision.reason}</p> : null}

                      <div className="deploy-list-block">
                        <div className="revision-header">
                          <span>Файлы для загрузки</span>
                          <span>{deployPreview.files.length}</span>
                        </div>
                        <div className="deploy-list">
                          {deployPreview.files.map((file) => (
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
                          <span>{deployPreview.ports.length}</span>
                        </div>
                        <div className="deploy-list">
                          {deployPreview.ports.map((port) => (
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
                          <span>{deployPreview.risks.length}</span>
                        </div>
                        {deployPreview.risks.length === 0 ? <p className="panel-note">В последнем превью рисков не обнаружено.</p> : null}
                        <div className="deploy-list">
                          {deployPreview.risks.map((risk) => (
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
                          <span>{deployPreview.checks.length}</span>
                        </div>
                        <div className="deploy-list">
                          {deployPreview.checks.map((check) => (
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
                          <span>{deployPreview.commands.length}</span>
                        </div>
                        <div className="deploy-list">
                          {deployPreview.commands.map((command) => (
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
                  <span>{deployEvents.length}</span>
                </div>
                {deployEvents.length === 0 ? <p className="panel-note">События применения появятся здесь после попытки деплоя.</p> : null}
                <div className="deploy-list">
                  {deployEvents.map((event) => (
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
            ) : null}

            {isDetailView && shouldRenderSection('operations-section') ? (
              <section className="panel operations-panel" id="operations-section">
              <div className="panel-header">
                <div>
                  <p className="eyebrow">Шаг 6</p>
                  <h2>Перезапуск, статус и логи в реальном времени</h2>
                </div>

                <div className="button-row">
                  <button className="secondary-button" disabled={statusLoading} onClick={() => handleLoadStatus()} title={updateStatusTitle} type="button">
                    <ButtonLabel icon="refresh">{statusLoading ? 'Обновление...' : 'Обновить статус'}</ButtonLabel>
                  </button>
                  <button className="secondary-button" onClick={handleCopyLink} title={copyLinkTitle} type="button">
                    <ButtonLabel icon="copy">Скопировать ссылку</ButtonLabel>
                  </button>
                  <button className="primary-button" disabled={restartBusy} onClick={handleRestartServer} title={restartTelemtTitle} type="button">
                    {restartBusy ? 'Перезапуск...' : 'Перезапустить Telemt'}
                  </button>
                </div>
              </div>

              {statusError ? <p className="inline-error">{statusError}</p> : null}
              {statusNotice ? <p className="inline-success">{statusNotice}</p> : null}
              {operationError ? <p className="inline-error">{operationError}</p> : null}
              {operationNotice ? <p className="inline-success">{operationNotice}</p> : null}

              <div className="operations-grid">
                <article className="operations-column">
                  <div className="deploy-section-header">
                    <strong>Снимок статуса</strong>
                    <span>{statusLoading ? 'Обновление' : 'Последнее известное состояние'}</span>
                  </div>

                  {linkInfo?.warning ? <p className="panel-note">{linkInfo.warning}</p> : null}
                  {!operationQueryAuthReady ? <p className="panel-note">{operationAuthHelp}</p> : null}

                  <dl className="health-list compact-list">
                    <div>
                      <dt>Путь к ключу</dt>
                      <dd>{savedPrivateKeyPath || 'Еще не сохранен'}</dd>
                    </div>
                    <div>
                      <dt>Последняя проверка</dt>
                      <dd>{currentHealth ? formatHealthState(currentHealth.status) : 'Фоновый воркер еще не записал проверку'}</dd>
                    </div>
                    <div>
                      <dt>Контейнер</dt>
                      <dd>{describeContainerSummary(serverStatus?.container) || 'Загрузите статус, чтобы проверить удаленный контейнер Telemt.'}</dd>
                    </div>
                    <div>
                      <dt>Telemt API</dt>
                      <dd>{describeTelemtApiSummary(serverStatus?.telemt_api) || 'Загрузите статус, чтобы проверить live API, пользователей и ссылку.'}</dd>
                    </div>
                    <div>
                      <dt>Публичный порт</dt>
                      <dd>
                        {serverStatus
                          ? `${describePublicPortSummary(serverStatus.public_port)}${serverStatus.public_port.target ? ` (${serverStatus.public_port.target})` : ''}`
                          : 'Еще не проверен'}
                      </dd>
                    </div>
                    <div>
                      <dt>Доступность</dt>
                      <dd>
                        {serverStatus?.public_port?.checked
                          ? serverStatus.public_port.reachable
                            ? `Доступен за ${serverStatus.public_port.latency_ms || 0} мс`
                            : serverStatus.public_port.error || 'Недоступен'
                          : 'Пропущено'}
                      </dd>
                    </div>
                    <div>
                      <dt>Комментарий воркера</dt>
                      <dd>{describeHealthMessage(currentHealth)}</dd>
                    </div>
                  </dl>

                  <div className="operations-stack">
                    {serverStatus?.container?.result ? (
                      <article className={`revision-item status-command ${serverStatus.container.status}`}>
                        <strong>{`container · ${serverStatus.container.status}`}</strong>
                        <span>{serverStatus.container.result.command}</span>
                          <span>{serverStatus.container.result.stderr || serverStatus.container.result.stdout || 'Вывод команды отсутствует'}</span>
                        </article>
                      ) : null}
                    {serverStatus?.telemt_api?.result ? (
                      <article className={`revision-item status-command ${serverStatus.telemt_api.status}`}>
                        <strong>{`telemt_api · ${serverStatus.telemt_api.status}`}</strong>
                        <span>{serverStatus.telemt_api.result.command}</span>
                          <span>{serverStatus.telemt_api.result.stderr || serverStatus.telemt_api.result.stdout || 'Вывод команды отсутствует'}</span>
                        </article>
                      ) : null}
                  </div>
                </article>

                <article className="operations-column">
                  <div className="deploy-section-header">
                    <strong>Логи</strong>
                    <span>{logsWindowSummary}</span>
                  </div>

                  <div className="button-row">
                    <button className="secondary-button" disabled={logsLoading || !operationQueryAuthReady} onClick={() => handleLoadLogs()} title={loadLogsTitle} type="button">
                      {logsLoading ? 'Загрузка...' : 'Загрузить логи'}
                    </button>
                    <button
                      className="secondary-button"
                      disabled={!operationQueryAuthReady}
                      onClick={() => setLogsLiveEnabled((current) => !current)}
                      title={toggleLogsStreamTitle}
                      type="button"
                    >
                      {logsLiveEnabled ? 'Остановить стрим' : 'Запустить стрим'}
                    </button>
                  </div>

                  <div className="logs-settings-grid">
                    <label className="logs-setting">
                      <span>Окно журнала</span>
                      <select className="text-input" onChange={(event) => setLogsWindowSize(Number.parseInt(event.target.value, 10) || 300)} value={logsWindowSize}>
                        {LOGS_WINDOW_OPTIONS.map((option) => (
                          <option key={option} value={option}>
                            {`${option} строк`}
                          </option>
                        ))}
                      </select>
                    </label>

                    <label className="logs-setting">
                      <span>На странице</span>
                      <select
                        className="text-input"
                        onChange={(event) => {
                          const nextPageSize = Number.parseInt(event.target.value, 10) || 100;
                          setLogsPageSize(nextPageSize);
                          setLogsPageIndex(getNewestLogsPageIndex(logsData?.result?.stdout || '', nextPageSize));
                        }}
                        value={logsPageSize}
                      >
                        {LOGS_PAGE_SIZE_OPTIONS.map((option) => (
                          <option key={option} value={option}>
                            {`${option} строк`}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>

                  {logsError ? <p className="inline-error">{logsError}</p> : null}
                  {logsNotice ? <p className="inline-success">{logsNotice}</p> : null}
                  {!operationQueryAuthReady ? <p className="panel-note">{operationAuthHelp}</p> : null}
                  {operationQueryAuthReady ? <p className="panel-note">{logsCoverageNote}</p> : null}

                  <div className="logs-toolbar">
                    <span className={`status-chip ${logsStreamState}`}>{formatLogsStreamState(logsStreamState)}</span>
                    <span>{logsData?.fetched_at ? new Date(logsData.fetched_at).toLocaleString('ru-RU') : 'Логи еще не загружались'}</span>
                    <span>{logsPageSummary}</span>
                  </div>

                  <div className="logs-pager">
                    <div className="logs-pager-actions">
                      <button className="secondary-button" disabled={!logsPage.hasPrevious} onClick={() => setLogsPageIndex((current) => Math.max(0, current - 1))} type="button">
                        Предыдущая страница
                      </button>
                      <button
                        className="secondary-button"
                        disabled={!logsPage.hasNext}
                        onClick={() => setLogsPageIndex((current) => Math.min(logsPage.totalPages - 1, current + 1))}
                        type="button"
                      >
                        Следующая страница
                      </button>
                    </div>
                    <span>{logsPage.totalLines > 0 ? `Показан диапазон ${logsPage.startLine}-${logsPage.endLine}.` : 'Диапазон появится после первой загрузки.'}</span>
                  </div>

                  <pre className="logs-output" ref={logsOutputRef}>
                    {logsPage.text || 'Загрузите логи или запустите SSE-стрим, чтобы посмотреть текущий вывод контейнера Telemt.'}
                  </pre>

                  {logsData?.result?.stderr ? <p className="panel-note mono-text">stderr: {logsData.result.stderr}</p> : null}
                </article>
              </div>

              <div className="deploy-list-block">
                <div className="revision-header">
                  <span>История операционных событий</span>
                  <span>{operationEvents.length}</span>
                </div>
                {operationEvents.length === 0 ? <p className="panel-note">События перезапуска появятся здесь после выполнения действия.</p> : null}
                <div className="deploy-list">
                  {operationEvents.map((event) => (
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
            ) : null}

            {isDetailView && shouldRenderSection('health-section') ? (
              <section className="panel operations-panel" id="health-section">
              <div className="panel-header">
                <div>
                  <p className="eyebrow">Шаг 7</p>
                  <h2>Фоновые проверки</h2>
                </div>

                <div className="button-row">
                  <span className={`status-chip ${normalizeHealthState(currentHealth?.status || selectedServer.status)}`}>
                    {formatHealthState(currentHealth?.status || selectedServer.status)}
                  </span>
                  <span className="panel-note compact-note">Интервал {healthSettings.interval}</span>
                </div>
              </div>

              {healthError ? <p className="inline-error">{healthError}</p> : null}

              <div className="operations-grid">
                <article className="operations-column">
                  <div className="link-card">
                    <span className="preview-label">Текущее состояние воркера</span>
                    <code>{currentHealth ? formatHealthState(currentHealth.status) : 'Неизвестно'}</code>
                    <div className="link-meta-row">
                      <span>{currentHealth ? describeHealthFlags(currentHealth) : 'Флаги проверки пока не записаны.'}</span>
                      <span>{currentHealth?.created_at ? formatDateTime(currentHealth.created_at) : 'Еще не проверялось'}</span>
                    </div>
                  </div>

                  <dl className="health-list compact-list">
                    <div>
                      <dt>Последняя проверка</dt>
                      <dd>{currentHealth?.created_at ? formatDateTime(currentHealth.created_at) : 'Никогда'}</dd>
                    </div>
                    <div>
                      <dt>Последняя проблема</dt>
                      <dd>{describeHealthProblem(currentHealth)}</dd>
                    </div>
                    <div>
                      <dt>Сводка воркера</dt>
                      <dd>{describeHealthMessage(currentHealth)}</dd>
                    </div>
                    <div>
                      <dt>TCP задержка</dt>
                      <dd>{currentHealth?.latency_ms != null ? `${currentHealth.latency_ms} мс` : 'Не записано'}</dd>
                    </div>
                    <div>
                      <dt>Сохраненный интервал</dt>
                      <dd>{healthSettings.interval}</dd>
                    </div>
                  </dl>
                </article>

                <article className="operations-column">
                  <div className="deploy-section-header">
                    <strong>История проверок</strong>
                    <span>{healthLoading ? 'Загрузка' : `${healthHistory.length} проверок`}</span>
                  </div>

                  {healthLoading ? <p className="panel-note">Загрузка истории проверок воркера...</p> : null}
                  {healthHistory.length === 0 && !healthLoading ? (
                    <p className="panel-note">Фоновые проверки пока не записаны. Планировщик начнет сохранять их автоматически после запуска API.</p>
                  ) : null}

                  <div className="deploy-list">
                    {healthHistory.map((check) => (
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
            ) : null}
          </>
        )}
      </main>
    </div>
  );
}
