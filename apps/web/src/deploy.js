export const deployDecisionOptions = [
  { value: 'stop_existing_service', label: 'Остановить текущий сервис' },
  { value: 'use_sni_router', label: 'Использовать SNI-роутер' },
  { value: 'choose_another_port', label: 'Выбрать другой порт' },
  { value: 'cancel', label: 'Отменить деплой' },
];

export function createDeployDraft() {
  return {
    auth_type: 'private_key_path',
    password: '',
    private_key_path: '',
    private_key_text: '',
    passphrase: '',
    confirm_blockers: false,
    port_conflict_decision: '',
  };
}

export function serializeSSHAuthFields(fields) {
  const payload = {
    auth_type: fields.auth_type,
  };

  const password = (fields.password || '').trim();
  const privateKeyPath = (fields.private_key_path || '').trim();
  const privateKeyText = (fields.private_key_text || '').trim();
  const passphrase = (fields.passphrase || '').trim();

  if (fields.auth_type === 'password' && password) {
    payload.password = password;
  }
  if (fields.auth_type === 'private_key_path' && privateKeyPath) {
    payload.private_key_path = privateKeyPath;
  }
  if (fields.auth_type === 'private_key_text' && privateKeyText) {
    payload.private_key_text = privateKeyText;
  }
  if ((fields.auth_type === 'private_key_path' || fields.auth_type === 'private_key_text') && passphrase) {
    payload.passphrase = passphrase;
  }

  return payload;
}

export function serializeDeployRequest(fields) {
  const payload = {
    ...serializeSSHAuthFields(fields),
    confirm_blockers: Boolean(fields.confirm_blockers),
  };

  const decision = (fields.port_conflict_decision || '').trim();

  if (decision) {
    payload.port_conflict_decision = decision;
  }

  return payload;
}

export function getDeployDecisionHelp(decision) {
  switch (decision) {
    case 'stop_existing_service':
      return 'Продолжайте только если вы осознанно остановили сервис, который занимает публичный порт MTProto.';
    case 'use_sni_router':
      return 'Панель не настраивает HAProxy-маршрутизацию автоматически. Настройте роутер отдельно, затем повторно запустите превью.';
    case 'choose_another_port':
      return 'Измените конфиг Telemt на другой публичный порт и повторно запустите превью перед применением.';
    case 'cancel':
      return 'Применение останется заблокированным, пока деплой отменен.';
    default:
      return '';
  }
}

export function hasBlockingRisks(preview) {
  return Array.isArray(preview?.risks) && preview.risks.some((risk) => risk.blocking);
}
