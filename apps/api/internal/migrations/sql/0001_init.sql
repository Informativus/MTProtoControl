CREATE TABLE IF NOT EXISTS servers (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	host TEXT NOT NULL,
	ssh_port INTEGER NOT NULL DEFAULT 22,
	ssh_user TEXT NOT NULL,
	public_host TEXT,
	public_ip TEXT,
	mtproto_port INTEGER NOT NULL DEFAULT 443,
	sni_domain TEXT,
	remote_base_path TEXT NOT NULL DEFAULT '/opt/mtproto-panel/telemt',
	status TEXT NOT NULL DEFAULT 'unknown',
	last_checked_at TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ssh_credentials (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	auth_type TEXT NOT NULL,
	private_key_encrypted BLOB,
	private_key_path TEXT,
	passphrase_encrypted BLOB,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS telemt_configs (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	version INTEGER NOT NULL,
	config_text TEXT NOT NULL,
	generated_link TEXT,
	applied_at TEXT,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS server_events (
	id TEXT PRIMARY KEY,
	server_id TEXT REFERENCES servers(id) ON DELETE CASCADE,
	level TEXT NOT NULL,
	event_type TEXT NOT NULL,
	message TEXT NOT NULL,
	stdout TEXT,
	stderr TEXT,
	exit_code INTEGER,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS health_checks (
	id TEXT PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	status TEXT NOT NULL,
	tcp_ok INTEGER NOT NULL,
	telemt_api_ok INTEGER NOT NULL,
	docker_ok INTEGER NOT NULL,
	latency_ms INTEGER,
	message TEXT,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_settings (
	key TEXT PRIMARY KEY,
	value_encrypted BLOB,
	value_plain TEXT,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ssh_credentials_server_id ON ssh_credentials(server_id);
CREATE INDEX IF NOT EXISTS idx_telemt_configs_server_id ON telemt_configs(server_id);
CREATE INDEX IF NOT EXISTS idx_server_events_server_id ON server_events(server_id);
CREATE INDEX IF NOT EXISTS idx_health_checks_server_id ON health_checks(server_id);
