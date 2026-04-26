CREATE TABLE IF NOT EXISTS server_relationships (
	id TEXT PRIMARY KEY,
	relation_type TEXT NOT NULL,
	source_server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	target_server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	CHECK (source_server_id <> target_server_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_server_relationships_unique_pair
	ON server_relationships(relation_type, source_server_id, target_server_id);

CREATE INDEX IF NOT EXISTS idx_server_relationships_source_server_id
	ON server_relationships(source_server_id);

CREATE INDEX IF NOT EXISTS idx_server_relationships_target_server_id
	ON server_relationships(target_server_id);
