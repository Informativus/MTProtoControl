const encoder = new TextEncoder();

export { describeApiError } from './api/api-errors.js';

export const telemtFieldDefinitions = [
  {
    key: 'public_host',
    label: 'Публичный хост',
    type: 'text',
    placeholder: 'mt.example.com',
    description: 'Домен, который будет использоваться в Telegram-ссылке.',
  },
  {
    key: 'public_port',
    label: 'Публичный порт',
    type: 'number',
    placeholder: '443',
    description: 'Публичный порт MTProto, доступный Telegram-клиентам.',
  },
  {
    key: 'tls_domain',
    label: 'TLS-домен',
    type: 'text',
    placeholder: 'mt.example.com',
    description: 'Домен SNI/FakeTLS, встраиваемый в ee-secret.',
  },
  {
    key: 'secret',
    label: 'Секрет',
    type: 'text',
    placeholder: '32 hex-символа',
    description: '32 hex-символа для пользователя Telemt.',
  },
  {
    key: 'mask_host',
    label: 'Хост маскировки',
    type: 'text',
    placeholder: 'www.yandex.ru',
    description: 'Запасной хост для маскировки обычного TLS-трафика.',
  },
  {
    key: 'mask_port',
    label: 'Порт маскировки',
    type: 'number',
    placeholder: '443',
    description: 'TLS-порт, используемый для маскировочного хоста.',
  },
  {
    key: 'api_port',
    label: 'Порт API',
    type: 'number',
    placeholder: '9091',
    description: 'Локальный порт API Telemt. Он не должен быть публичным.',
  },
];

export const logLevelOptions = ['debug', 'verbose', 'normal', 'silent'];

export function createDraftFields(fields = {}) {
  return {
    public_host: toText(fields.public_host),
    public_port: toNumericText(fields.public_port, '443'),
    tls_domain: toText(fields.tls_domain),
    secret: toText(fields.secret),
    mask_host: toText(fields.mask_host || 'www.yandex.ru'),
    mask_port: toNumericText(fields.mask_port, '443'),
    api_port: toNumericText(fields.api_port, '9091'),
    use_middle_proxy: fields.use_middle_proxy !== false,
    log_level: toText(fields.log_level || 'normal'),
  };
}

export function serializeDraftFields(fields) {
  return {
    public_host: fields.public_host,
    public_port: Number.parseInt(fields.public_port || '0', 10) || 0,
    tls_domain: fields.tls_domain,
    secret: fields.secret,
    mask_host: fields.mask_host,
    mask_port: Number.parseInt(fields.mask_port || '0', 10) || 0,
    api_port: Number.parseInt(fields.api_port || '0', 10) || 0,
    use_middle_proxy: Boolean(fields.use_middle_proxy),
    log_level: fields.log_level,
  };
}

export function buildPreviewLink(fields) {
  const publicHost = fields.public_host.trim();
  const tlsDomain = (fields.tls_domain || publicHost).trim();
  const secret = fields.secret.trim().toLowerCase();
  const publicPort = Number.parseInt(fields.public_port || '0', 10) || 0;

  if (!publicHost || !tlsDomain || !secret || !publicPort) {
    return '';
  }

  return `https://t.me/proxy?server=${encodeURIComponent(publicHost)}&port=${publicPort}&secret=${encodeURIComponent(`ee${secret}${hexEncode(tlsDomain)}`)}`;
}

export function getTLSDomainWarning(fields) {
  const publicHost = fields.public_host.trim();
  const tlsDomain = fields.tls_domain.trim();

  if (!publicHost || !tlsDomain || publicHost === tlsDomain) {
    return '';
  }

  return `tls_domain отличается от public_host: ссылка -> ${publicHost}, ee-secret -> ${tlsDomain}.`;
}

function hexEncode(value) {
  return Array.from(encoder.encode(value), (byte) => byte.toString(16).padStart(2, '0')).join('');
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
