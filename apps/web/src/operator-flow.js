export function hasConfiguredSSHAuth(fields) {
  if (!fields || typeof fields !== 'object') {
    return false;
  }

  if (fields.auth_type === 'private_key_path') {
    return (fields.private_key_path || '').trim() !== '';
  }

  if (fields.auth_type === 'password') {
    return (fields.password || '').trim() !== '';
  }

  if (fields.auth_type === 'private_key_text') {
    return (fields.private_key_text || '').trim() !== '';
  }

  return false;
}

export function buildOperatorSteps(context) {
  const {
    selectedServer,
    sshAuthReady,
    sshTestSuccessful,
    currentConfig,
    deployPreview,
    deployHasBlockingRisks,
    activeLink,
    currentHealth,
    diagnosticsLabel,
    appliedLabel,
    healthLabel,
  } = context;

  const hasServer = Boolean(selectedServer);
  const hasSavedKeyPath = Boolean(selectedServer?.saved_private_key_path);
  const hasConfig = Boolean(currentConfig);
  const hasDiagnostics = Boolean(deployPreview);
  const isApplied = Boolean(currentConfig?.applied_at);
  const hasOperations = isApplied;
  const hasLinkPreview = activeLink.trim() !== '';
  const hasHealth = Boolean(currentHealth?.created_at);

  return [
    createStep(
      'inventory',
      '1. Сервер',
      'inventory-section',
      hasServer ? 'done' : 'active',
      hasServer ? `Для работы выбран сервер ${selectedServer.name}.` : 'Сервер еще не выбран.',
      hasServer ? '' : 'Создайте первую запись о сервере в разделе инвентаря.',
    ),
    createStep(
      'ssh',
      '2. SSH-доступ',
      'ssh-section',
      !hasServer ? 'blocked' : sshTestSuccessful ? 'done' : sshAuthReady || hasSavedKeyPath ? 'active' : 'blocked',
      !hasServer
        ? 'Перед проверкой SSH нужно выбрать сервер.'
        : sshTestSuccessful
          ? 'SSH-доступ подтвержден, сведения о хосте загружены.'
          : hasSavedKeyPath
            ? 'Для выбранного сервера уже сохранен путь к приватному ключу.'
            : sshAuthReady
              ? 'SSH-авторизация готова к проверке подключения.'
              : 'SSH-авторизация не настроена.',
      !hasServer
        ? 'Сначала добавьте сервер, затем проверяйте SSH.'
        : sshTestSuccessful
          ? ''
          : sshAuthReady || hasSavedKeyPath
            ? 'Запустите проверку SSH, чтобы подтвердить доступ к Docker и сведения о хосте.'
            : 'Укажите пароль SSH, путь к приватному ключу или вставьте ключ перед проверкой SSH.',
    ),
    createStep(
      'config',
      '3. Конфиг',
      'config-section',
      !hasServer ? 'blocked' : hasConfig ? 'done' : 'active',
      !hasServer ? 'Перед генерацией конфига нужно выбрать сервер.' : hasConfig ? `Конфиг v${currentConfig.version} готов.` : 'Сохраненного конфига Telemt пока нет.',
      !hasServer ? 'Сначала добавьте сервер, затем генерируйте конфиг.' : hasConfig ? '' : 'Сгенерируйте черновик Telemt или сохраните raw TOML как ревизию.',
    ),
    createStep(
      'diagnostics',
      '4. Диагностика',
      'deploy-section',
      !hasServer ? 'blocked' : !hasConfig ? 'blocked' : isApplied || hasDiagnostics ? 'done' : !sshAuthReady ? 'blocked' : 'active',
      !hasServer
        ? 'Перед диагностикой превью нужно выбрать сервер.'
        : !hasConfig
          ? 'Для превью деплоя нужна сохраненная ревизия конфига.'
          : isApplied
            ? appliedLabel || 'Текущий конфиг уже применяется на сервере. Превью понадобится только перед следующими изменениями.'
          : !sshAuthReady
            ? 'Для диагностики превью деплоя нужна SSH-авторизация.'
            : hasDiagnostics
              ? diagnosticsLabel || 'Последняя диагностика превью уже загружена.'
              : 'Диагностика превью пока не запускалась.',
      !hasServer
        ? 'Сначала добавьте сервер, затем запускайте диагностику.'
        : !hasConfig
          ? 'Сгенерируйте или сохраните конфиг перед загрузкой превью деплоя.'
          : isApplied
            ? ''
          : !sshAuthReady
            ? 'Настройте SSH-авторизацию перед загрузкой превью деплоя.'
            : hasDiagnostics
              ? ''
              : 'Запустите превью деплоя, чтобы проверить порты, файлы, команды и риски.',
    ),
    createStep(
      'deploy',
      '5. Деплой',
      'deploy-section',
      !hasServer ? 'blocked' : isApplied ? 'done' : !hasConfig ? 'blocked' : !hasDiagnostics ? 'blocked' : 'active',
      !hasServer
        ? 'Перед деплоем нужно выбрать сервер.'
        : isApplied
          ? appliedLabel || 'Текущий конфиг уже применен.'
          : !hasConfig
            ? 'Для деплоя нет сохраненного конфига.'
            : !hasDiagnostics
              ? 'Перед применением нужно запустить превью деплоя.'
              : deployHasBlockingRisks
                ? 'В превью найдены блокирующие риски, их нужно проверить вручную.'
                : 'Деплой можно применять, если превью выглядит безопасно.',
      !hasServer
        ? 'Сначала добавьте сервер, затем применяйте деплой.'
        : isApplied
          ? ''
          : !hasConfig
            ? 'Сгенерируйте или сохраните конфиг перед деплоем.'
            : !hasDiagnostics
              ? 'Сначала загрузите превью деплоя.'
              : deployHasBlockingRisks
                ? 'Устраните блокеры превью или явно подтвердите их перед применением.'
                : 'Примените деплой, чтобы загрузить файлы и запустить Telemt.',
    ),
    createStep(
      'operations',
      '6. Операции',
      'operations-section',
      !hasServer ? 'blocked' : hasOperations ? 'done' : hasLinkPreview ? 'active' : 'blocked',
      !hasServer
        ? 'Перед live-операциями нужно выбрать сервер.'
        : hasOperations
          ? activeLink.trim() !== ''
            ? 'Статус, ссылка, логи и перезапуск уже доступны.'
            : 'Операции после деплоя доступны из консоли.'
          : hasLinkPreview
            ? 'Предпросмотр ссылки уже есть, но операции в реальном времени станут доступны после первого деплоя.'
            : 'Операции в реальном времени становятся полезны после первого деплоя.',
      !hasServer ? 'Сначала добавьте сервер, затем используйте операции в реальном времени.' : hasOperations ? '' : 'Сначала примените деплой, затем обновите статус, логи и ссылку.',
    ),
    createStep(
      'health',
      '7. Мониторинг',
      'health-section',
      !hasServer ? 'blocked' : hasHealth ? 'done' : 'active',
      !hasServer ? 'Перед историей проверок нужно выбрать сервер.' : hasHealth ? healthLabel || 'Фоновые проверки уже сохраняются.' : 'Фоновая проверка воркера пока не записана.',
      !hasServer ? 'Сначала добавьте сервер, затем проверяйте историю воркера.' : hasHealth ? '' : 'Оставьте API запущенным и дождитесь первой проверки по расписанию.',
    ),
  ];
}

function createStep(id, title, target, state, summary, blocker) {
  return {
    id,
    title,
    target,
    state,
    summary,
    blocker,
  };
}
