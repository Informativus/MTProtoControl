# Data Model Draft

## `servers`

```sql
id TEXT PRIMARY KEY;
name TEXT NOT NULL;
host TEXT NOT NULL;
ssh_port INTEGER NOT NULL DEFAULT 22;
ssh_user TEXT NOT NULL;
public_host TEXT;
public_ip TEXT;
mtproto_port INTEGER NOT NULL DEFAULT 443;
sni_domain TEXT;
remote_base_path TEXT NOT NULL DEFAULT '/opt/mtproto-panel/telemt';
status TEXT NOT NULL DEFAULT 'unknown';
last_checked_at TEXT;
created_at TEXT NOT NULL;
updated_at TEXT NOT NULL;
```

## `ssh_credentials`

```sql
id TEXT PRIMARY KEY;
server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE;
auth_type TEXT NOT NULL; -- private_key_text | private_key_path
private_key_encrypted BLOB;
private_key_path TEXT;
passphrase_encrypted BLOB;
created_at TEXT NOT NULL;
updated_at TEXT NOT NULL;
```

## `telemt_configs`

```sql
id TEXT PRIMARY KEY;
server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE;
version INTEGER NOT NULL;
config_text TEXT NOT NULL;
generated_link TEXT;
applied_at TEXT;
created_at TEXT NOT NULL;
```

## `server_events`

```sql
id TEXT PRIMARY KEY;
server_id TEXT REFERENCES servers(id) ON DELETE CASCADE;
level TEXT NOT NULL; -- info | warning | error
event_type TEXT NOT NULL;
message TEXT NOT NULL;
stdout TEXT;
stderr TEXT;
exit_code INTEGER;
created_at TEXT NOT NULL;
```

## `health_checks`

```sql
id TEXT PRIMARY KEY;
server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE;
status TEXT NOT NULL; -- online | degraded | offline
tcp_ok INTEGER NOT NULL;
telemt_api_ok INTEGER NOT NULL;
docker_ok INTEGER NOT NULL;
latency_ms INTEGER;
message TEXT;
created_at TEXT NOT NULL;
```

## `app_settings`

```sql
key TEXT PRIMARY KEY;
value_encrypted BLOB;
value_plain TEXT;
updated_at TEXT NOT NULL;
```

