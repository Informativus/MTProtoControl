export const inventoryFieldGroups = [
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

export const sameHostImportFieldExample = [
  'host = localhost',
  'ssh_user = <ваш SSH user>',
  'private_key_path = /root/.ssh/<имя_ключа>',
  'remote_base_path = /opt/mtproto-panel/telemt',
].join('\n');

export const sameHostImportSetupExample = [
  'mkdir -p ./ssh',
  'cp ~/.ssh/<ваш_ключ> ./ssh/<ваш_ключ>',
  'ssh-keyscan <server-ip> localhost 127.0.0.1 > ./ssh/known_hosts',
  'chmod 600 ./ssh/<ваш_ключ> ./ssh/known_hosts',
].join('\n');
