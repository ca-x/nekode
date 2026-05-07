package storage

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_unix INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		created_unix INTEGER NOT NULL,
		updated_unix INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		token_hash TEXT NOT NULL UNIQUE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_unix INTEGER NOT NULL,
		created_unix INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
	`CREATE TABLE IF NOT EXISTS interaction_endpoints (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		provider TEXT NOT NULL,
		display_name TEXT NOT NULL,
		target_prefix TEXT NOT NULL,
		inbound_enabled INTEGER NOT NULL,
		outbound_enabled INTEGER NOT NULL,
		auth_mode TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		created_unix INTEGER NOT NULL,
		updated_unix INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_interaction_endpoints_kind ON interaction_endpoints(kind)`,
	`CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		target TEXT NOT NULL,
		thread_id TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		sender_user_id TEXT NOT NULL DEFAULT '',
		sender_agent_id TEXT NOT NULL DEFAULT '',
		sender_display_name TEXT NOT NULL DEFAULT '',
		sender_kind TEXT NOT NULL,
		source_endpoint_id TEXT NOT NULL DEFAULT '',
		external_message_id TEXT NOT NULL DEFAULT '',
		metadata_json TEXT NOT NULL DEFAULT '{}',
		request_id TEXT NOT NULL DEFAULT '',
		created_unix INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_messages_target_created ON messages(target, created_unix DESC)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_request_id ON messages(request_id) WHERE request_id <> ''`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		summary TEXT NOT NULL,
		state TEXT NOT NULL,
		target TEXT NOT NULL,
		assignee_id TEXT NOT NULL DEFAULT '',
		created_by_user_id TEXT NOT NULL DEFAULT '',
		created_unix INTEGER NOT NULL,
		updated_unix INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_state_updated ON tasks(state, updated_unix DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_tasks_target_updated ON tasks(target, updated_unix DESC)`,
	`INSERT OR IGNORE INTO schema_migrations (version, applied_unix)
		VALUES (1, unixepoch())`,
}
