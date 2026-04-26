export function describeApiError(payload) {
  if (!payload || typeof payload !== 'object' || !payload.error) {
    return 'Запрос завершился ошибкой.';
  }

  const { error } = payload;
  const normalizedMessage = normalizeApiErrorMessage(error.code, error.message);
  const details = error.details && typeof error.details === 'object' ? Object.entries(error.details) : [];
  if (details.length === 0) {
    return normalizedMessage;
  }

  return `${normalizedMessage}: ${details.map(([key, value]) => `${key} ${value}`).join(', ')}`;
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

function normalizeApiErrorMessage(code, message) {
  switch (code) {
    case 'config_required':
      return 'Сначала сохраните или примените конфиг Telemt, а затем запрашивайте прокси-ссылку.';
    default:
      return message || 'Запрос завершился ошибкой.';
  }
}
