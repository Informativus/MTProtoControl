export function describeHealthMessage(check) {
  if (!check) {
    return 'Проверка от фонового воркера еще не записана.';
  }

  const translated = translateHealthMessage(check.message);
  if (translated) {
    return translated;
  }

  return normalizeHealthStatus(check.status) === 'online' ? 'Все проверки здоровья прошли успешно.' : 'Сообщение об ошибке отсутствует.';
}

export function describeHealthProblem(check) {
  if (!check) {
    return 'Проверка от фонового воркера еще не записана.';
  }

  return normalizeHealthStatus(check.status) === 'online' ? 'Нет' : describeHealthMessage(check);
}

export function describeHealthFlags(check) {
  if (!check) {
    return 'Флаги проверки пока не записаны.';
  }

  return [
    check.dns_ok ? 'DNS ок' : 'DNS ошибка',
    check.tcp_ok ? 'TCP ок' : 'TCP ошибка',
    check.ssh_ok ? 'SSH ок' : 'SSH ошибка',
    check.docker_ok ? 'Docker ок' : 'Docker ошибка',
    check.telemt_api_ok ? 'API ок' : 'API ошибка',
    check.link_ok ? 'Ссылка ок' : 'Ссылка отсутствует',
  ].join(' · ');
}

export function translateHealthMessage(message) {
  const trimmed = toText(message).trim();
  if (trimmed === '') {
    return '';
  }

  return trimmed
    .split(/\s*;\s*/)
    .map(translateHealthSegment)
    .filter(Boolean)
    .join('; ');
}

function translateHealthSegment(segment) {
  const trimmed = segment.trim();
  if (trimmed === '') {
    return '';
  }

  if (/^All health checks passed\.?$/i.test(trimmed)) {
    return 'Все проверки здоровья прошли успешно.';
  }

  const dnsMatch = trimmed.match(/^DNS lookup failed for (.+)$/i);
  if (dnsMatch) {
    return `DNS не разрешился для ${dnsMatch[1]}`;
  }

  const replacements = [
    [/^worker SSH checks skipped because no saved private_key_path is available$/i, 'SSH-проверка воркера пропущена: не сохранен путь к приватному ключу.'],
    [/^no saved Telemt config is available for API and link checks$/i, 'Нет сохраненной ревизии Telemt для проверки API и ссылки.'],
    [/^no generated proxy link is available$/i, 'Нет сохраненной proxy-ссылки.'],
    [/^public TCP endpoint is not configured$/i, 'Публичный TCP-адрес не настроен'],
    [/^Telemt API query failed$/i, 'Запрос к Telemt API завершился ошибкой'],
    [/^Telemt container is not running or healthy$/i, 'Контейнер Telemt не запущен или не в состоянии healthy'],
  ];

  for (const [pattern, replacement] of replacements) {
    if (pattern.test(trimmed)) {
      return replacement;
    }
  }

  const prefixedReplacements = [
    [/^SSH check failed: (.+)$/i, 'SSH-проверка не прошла: $1'],
    [/^Telemt API SSH check failed: (.+)$/i, 'Проверка Telemt API по SSH не прошла: $1'],
  ];

  for (const [pattern, replacement] of prefixedReplacements) {
    if (pattern.test(trimmed)) {
      return trimmed.replace(pattern, replacement);
    }
  }

  return trimmed;
}

function normalizeHealthStatus(status) {
  const normalized = toText(status).trim().toLowerCase();
  return normalized === 'healthy' ? 'online' : normalized;
}

function toText(value) {
  return value == null ? '' : String(value);
}
