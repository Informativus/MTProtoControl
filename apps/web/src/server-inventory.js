export const defaultRemoteBasePath = '/opt/mtproto-panel/telemt';

export const serverInventoryFields = [
  {
    key: 'name',
    label: 'Имя',
    type: 'text',
    placeholder: 'proxy_node_1',
    description: 'Короткое имя сервера в меню и истории событий.',
  },
  {
    key: 'host',
    label: 'Хост',
    type: 'text',
    placeholder: '203.0.113.10',
    description: 'SSH-адрес, к которому будет подключаться панель. Если панель и Telemt стоят на одном сервере, можно указать localhost или 127.0.0.1.',
  },
  {
    key: 'ssh_port',
    label: 'Порт SSH',
    type: 'number',
    placeholder: '22',
    description: 'Если оставить пустым при создании, будет использован порт 22.',
  },
  {
    key: 'ssh_user',
    label: 'Пользователь SSH',
    type: 'text',
    placeholder: 'operator',
    description: 'Пользователь для диагностики, деплоя и операций на сервере.',
  },
  {
    key: 'private_key_path',
    label: 'Путь к SSH-ключу',
    type: 'text',
    placeholder: '~/.ssh/proxy-node',
    description: 'Локальный путь к приватному ключу на машине API. Для docker-установки ключ и known_hosts обычно лежат в ./ssh и внутри API доступны как /root/.ssh/*.',
  },
  {
    key: 'public_host',
    label: 'Публичный хост',
    type: 'text',
    placeholder: 'mt.example.com',
    description: 'Публичный hostname MTProto, который попадет в сгенерированные ссылки.',
  },
  {
    key: 'public_ip',
    label: 'Публичный IP',
    type: 'text',
    placeholder: '203.0.113.10',
    description: 'Необязательная подсказка для оператора при диагностике и DNS-проверках.',
  },
  {
    key: 'mtproto_port',
    label: 'Порт MTProto',
    type: 'number',
    placeholder: '443',
    description: 'Публичный порт слушателя MTProto.',
  },
  {
    key: 'sni_domain',
    label: 'SNI-домен',
    type: 'text',
    placeholder: 'mt.example.com',
    description: 'Домен FakeTLS SNI, который встраивается в ee-secret.',
  },
  {
    key: 'remote_base_path',
    label: 'Папка Telemt на сервере',
    type: 'text',
    placeholder: defaultRemoteBasePath,
    description: 'Папка на сервере, где лежат docker-compose.yml, config.toml и backups. Если прокси уже установлен, укажите его текущую папку.',
  },
];

export function createServerDraft(server = {}) {
  return {
    name: toText(server.name),
    host: toText(server.host),
    ssh_port: toNumericText(server.ssh_port, '22'),
    ssh_user: toText(server.ssh_user),
    private_key_path: toText(server.saved_private_key_path || server.private_key_path),
    public_host: toText(server.public_host),
    public_ip: toText(server.public_ip),
    mtproto_port: toNumericText(server.mtproto_port, '443'),
    sni_domain: toText(server.sni_domain),
    remote_base_path: toText(server.remote_base_path || ''),
  };
}

export function serializeServerDraft(draft) {
  return {
    name: draft.name.trim(),
    host: draft.host.trim(),
    ssh_port: Number.parseInt(draft.ssh_port || '0', 10) || 0,
    ssh_user: draft.ssh_user.trim(),
    private_key_path: draft.private_key_path.trim(),
    public_host: draft.public_host.trim(),
    public_ip: draft.public_ip.trim(),
    mtproto_port: Number.parseInt(draft.mtproto_port || '0', 10) || 0,
    sni_domain: draft.sni_domain.trim(),
    remote_base_path: draft.remote_base_path.trim(),
  };
}

export function getApiErrorDetails(payload) {
  if (!payload || typeof payload !== 'object' || !payload.error || typeof payload.error !== 'object') {
    return {};
  }

  if (!payload.error.details || typeof payload.error.details !== 'object') {
    return {};
  }

  return payload.error.details;
}

export function getNextSelectedServerId(servers, deletedServerId, selectedServerId) {
  if (selectedServerId !== deletedServerId) {
    return selectedServerId;
  }

  const remaining = servers.filter((server) => server.id !== deletedServerId);
  if (remaining.length === 0) {
    return '';
  }

  const deletedIndex = servers.findIndex((server) => server.id === deletedServerId);
  const fallbackIndex = deletedIndex < 0 ? 0 : Math.min(deletedIndex, remaining.length - 1);
  return remaining[fallbackIndex]?.id || '';
}

function toText(value) {
  return value == null ? '' : String(value);
}

function toNumericText(value, fallback) {
  if (value == null || value === '') {
    return fallback;
  }

  return String(value);
}
