export function canUseOperationQueryAuth(fields) {
  return fields.auth_type === 'private_key_path' && fields.private_key_path.trim() !== '';
}

export function getOperationAuthHelp(fields) {
  if (fields.auth_type === 'password') {
    return 'Live-статус, логи и SSE-стрим используют GET-эндпоинты, поэтому SSH-пароль сюда не передается. Для этих экранов нужен private_key_path.';
  }

  if (fields.auth_type !== 'private_key_path') {
    return 'Live-статус, логи и SSE-стрим используют GET-эндпоинты, поэтому сейчас они поддерживают только авторизацию через private_key_path.';
  }

  if (!fields.private_key_path.trim()) {
    return 'Укажите путь к приватному ключу, чтобы загружать логи, запускать SSE-стрим и live-проверки статуса.';
  }

  return '';
}

export function serializeOperationsQuery(fields, options = {}) {
  if (!canUseOperationQueryAuth(fields)) {
    return '';
  }

  const params = new URLSearchParams();
  params.set('auth_type', 'private_key_path');
  params.set('private_key_path', fields.private_key_path.trim());

  const passphrase = fields.passphrase.trim();
  if (passphrase) {
    params.set('passphrase', passphrase);
  }

  if (options.tail) {
    params.set('tail', String(options.tail));
  }

  return params.toString();
}

export function buildOperationsUrl(apiBaseUrl, serverId, suffix, fields, options = {}) {
  const query = serializeOperationsQuery(fields, options);
  const base = `${apiBaseUrl}/api/servers/${encodeURIComponent(serverId)}${suffix}`;
  return query ? `${base}?${query}` : base;
}

export const LOGS_WINDOW_OPTIONS = [200, 300, 500, 1000];
export const LOGS_PAGE_SIZE_OPTIONS = [50, 100, 200];

export function buildLogsPage(output, pageSize, pageIndex) {
  const lines = splitLogLines(output);
  const safePageSize = Number.isFinite(pageSize) && pageSize > 0 ? Math.floor(pageSize) : 100;
  const totalPages = Math.max(1, Math.ceil(lines.length / safePageSize));
  const safePageIndex = clampPageIndex(pageIndex, totalPages);
  const startOffset = safePageIndex * safePageSize;
  const pageLines = lines.slice(startOffset, startOffset + safePageSize);

  return {
    currentPage: safePageIndex + 1,
    endLine: startOffset + pageLines.length,
    hasNext: safePageIndex + 1 < totalPages,
    hasPrevious: safePageIndex > 0,
    pageIndex: safePageIndex,
    startLine: pageLines.length === 0 ? 0 : startOffset + 1,
    text: pageLines.join('\n'),
    totalLines: lines.length,
    totalPages,
  };
}

export function clampLogsPageIndex(output, pageSize, pageIndex) {
  return buildLogsPage(output, pageSize, pageIndex).pageIndex;
}

export function getNewestLogsPageIndex(output, pageSize) {
  return buildLogsPage(output, pageSize, Number.MAX_SAFE_INTEGER).pageIndex;
}

export function formatLogLineCount(value) {
  return `${value} ${pluralizeRu(value, ['строка', 'строки', 'строк'])}`;
}

export function describeGeneratedLinkSource(source) {
  switch (source) {
    case 'telemt_api':
      return 'Telemt API (онлайн)';
    case 'config_revision':
      return 'Сохраненная ревизия конфига';
    default:
      return 'Недоступно';
  }
}

export function describeContainerSummary(container) {
  const summary = toText(container?.summary).trim();
  if (!summary) {
    return '';
  }

  if (container?.status === 'ok') {
    return translateDockerContainerSummary(summary);
  }

  switch (summary) {
    case 'Panel-managed Telemt container was not found on the host.':
      return 'Контейнер Telemt не найден на сервере.';
    case 'SSH command failed before container status was collected.':
      return 'SSH-команда завершилась ошибкой до проверки контейнера.';
    default:
      return summary;
  }
}

export function describeTelemtApiSummary(telemtApi) {
  const summary = toText(telemtApi?.summary).trim();
  if (!summary) {
    return '';
  }

  const withLinkMatch = summary.match(/^Telemt API reachable, (\d+) user\(s\) returned, but no proxy link was found\.$/);
  if (withLinkMatch) {
    return `Telemt API доступен, найдено ${formatUserCount(Number.parseInt(withLinkMatch[1], 10))}, но proxy-ссылка не обнаружена.`;
  }

  const okMatch = summary.match(/^Telemt API reachable, (\d+) user\(s\) returned\.$/);
  if (okMatch) {
    return `Telemt API доступен, найдено ${formatUserCount(Number.parseInt(okMatch[1], 10))}.`;
  }

  switch (summary) {
    case 'SSH command failed before the Telemt API was queried.':
      return 'SSH-команда завершилась ошибкой до запроса к Telemt API.';
    case 'Telemt API query failed':
      return 'Не удалось запросить Telemt API.';
    default:
      return summary;
  }
}

export function describePublicPortSummary(publicPort) {
  const summary = toText(publicPort?.summary).trim();
  switch (summary) {
    case 'Public TCP endpoint accepted a connection from the panel host.':
      return 'Публичный TCP-эндпоинт принимает соединение с хоста панели.';
    case 'Public TCP endpoint is not reachable from the panel host.':
      return 'Публичный TCP-эндпоинт недоступен с хоста панели.';
    case 'No public endpoint is available for a TCP reachability check.':
      return 'Нет публичного адреса для TCP-проверки.';
    default:
      return summary;
  }
}

function splitLogLines(value) {
  const sanitized = sanitizeLogsOutput(value);
  if (sanitized === '') {
    return [];
  }

  const lines = sanitized.split('\n');
  if (lines[lines.length - 1] === '') {
    lines.pop();
  }
  return lines;
}

function sanitizeLogsOutput(value) {
  return toText(value)
    .replace(/\r\n?/gu, '\n')
    // Strip ANSI escape sequences from remote logs before paging them in the UI.
    // eslint-disable-next-line no-control-regex
    .replace(/\u001b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/gu, '');
}

function clampPageIndex(pageIndex, totalPages) {
  const maxPageIndex = Math.max(0, totalPages - 1);
  if (!Number.isFinite(pageIndex) || pageIndex < 0) {
    return 0;
  }
  return Math.min(Math.floor(pageIndex), maxPageIndex);
}

function translateDockerContainerSummary(summary) {
  return summary
    .replace(/^Up\s+/u, 'Работает ')
    .replace(/^Exited\s+/u, 'Остановлен ')
    .replace(/^Created$/u, 'Создан')
    .replace(/^Restarting\s+/u, 'Перезапускается ')
    .replace(/^Paused$/u, 'Приостановлен')
    .replace(/About an hour/gu, 'около часа')
    .replace(/Less than a second/gu, 'меньше секунды')
    .replace(/\b(\d+)\s+(seconds?|minutes?|hours?|days?|weeks?|months?)\b/gu, (_, value, unit) => {
      const count = Number.parseInt(value, 10);
      return `${count} ${translateDurationUnit(count, unit)}`;
    })
    .replace(/\(healthy\)/gu, '(исправен)')
    .replace(/\(unhealthy\)/gu, '(неисправен)')
    .replace(/\s+ago\b/gu, ' назад');
}

function translateDurationUnit(value, unit) {
  switch (unit) {
    case 'second':
    case 'seconds':
      return pluralizeRu(value, ['секунду', 'секунды', 'секунд']);
    case 'minute':
    case 'minutes':
      return pluralizeRu(value, ['минуту', 'минуты', 'минут']);
    case 'hour':
    case 'hours':
      return pluralizeRu(value, ['час', 'часа', 'часов']);
    case 'day':
    case 'days':
      return pluralizeRu(value, ['день', 'дня', 'дней']);
    case 'week':
    case 'weeks':
      return pluralizeRu(value, ['неделю', 'недели', 'недель']);
    case 'month':
    case 'months':
      return pluralizeRu(value, ['месяц', 'месяца', 'месяцев']);
    default:
      return unit;
  }
}

function formatUserCount(value) {
  return `${value} ${pluralizeRu(value, ['пользователь', 'пользователя', 'пользователей'])}`;
}

function pluralizeRu(value, forms) {
  const mod10 = value % 10;
  const mod100 = value % 100;
  if (mod10 === 1 && mod100 !== 11) {
    return forms[0];
  }
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
    return forms[1];
  }
  return forms[2];
}

function toText(value) {
  return value == null ? '' : String(value);
}
