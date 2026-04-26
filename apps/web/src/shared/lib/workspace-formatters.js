export function normalizeHealthState(status) {
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

export function formatHealthState(status) {
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

export function formatStepState(state) {
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

export function formatLogsStreamState(state) {
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

export function formatDateTime(value) {
  if (!value) {
    return 'Никогда';
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 'Неизвестно';
  }

  return date.toLocaleString('ru-RU');
}

export function scheduleViewportUpdate(callback) {
  if (typeof window === 'undefined') {
    callback();
    return;
  }

  window.requestAnimationFrame(callback);
}
