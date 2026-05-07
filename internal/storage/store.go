package storage

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	for _, statement := range schemaStatements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) CreateUser(ctx context.Context, user User) (User, error) {
	now := unixNow()
	if user.ID == "" {
		user.ID = NewID("usr")
	}
	if user.Role == "" {
		user.Role = "member"
	}
	user.CreatedUnix = now
	user.UpdatedUnix = now

	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (id, username, display_name, password_hash, role, created_unix, updated_unix)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.DisplayName, user.PasswordHash, user.Role, user.CreatedUnix, user.UpdatedUnix)
	if isConstraint(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
SELECT id, username, display_name, password_hash, role, created_unix, updated_unix
FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
SELECT id, username, display_name, password_hash, role, created_unix, updated_unix
FROM users WHERE username = ?`, username))
}

func (s *Store) CreateSession(ctx context.Context, session Session) (Session, error) {
	now := unixNow()
	if session.ID == "" {
		session.ID = NewID("ses")
	}
	session.CreatedUnix = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (id, token_hash, user_id, expires_unix, created_unix)
VALUES (?, ?, ?, ?, ?)`, session.ID, session.TokenHash, session.UserID, session.ExpiresUnix, session.CreatedUnix)
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, token_hash, user_id, expires_unix, created_unix
FROM sessions WHERE token_hash = ?`, tokenHash)
	var session Session
	if err := row.Scan(&session.ID, &session.TokenHash, &session.UserID, &session.ExpiresUnix, &session.CreatedUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	if session.ExpiresUnix <= unixNow() {
		_ = s.DeleteSession(ctx, session.ID)
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	return err
}

func (s *Store) CreateInteractionEndpoint(ctx context.Context, endpoint InteractionEndpoint) (InteractionEndpoint, error) {
	now := unixNow()
	if endpoint.ID == "" {
		endpoint.ID = NewID("iep")
	}
	endpoint.CreatedUnix = now
	endpoint.UpdatedUnix = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO interaction_endpoints
  (id, kind, provider, display_name, target_prefix, inbound_enabled, outbound_enabled, auth_mode, config_json, created_unix, updated_unix)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		endpoint.ID, endpoint.Kind, endpoint.Provider, endpoint.DisplayName, endpoint.TargetPrefix,
		endpoint.InboundEnabled, endpoint.OutboundEnabled, endpoint.AuthMode, endpoint.ConfigJSON,
		endpoint.CreatedUnix, endpoint.UpdatedUnix)
	if isConstraint(err) {
		return InteractionEndpoint{}, ErrConflict
	}
	if err != nil {
		return InteractionEndpoint{}, err
	}
	return endpoint, nil
}

func (s *Store) ListInteractionEndpoints(ctx context.Context, limit int) ([]InteractionEndpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, kind, provider, display_name, target_prefix, inbound_enabled, outbound_enabled, auth_mode, config_json, created_unix, updated_unix
FROM interaction_endpoints
ORDER BY created_unix DESC, id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []InteractionEndpoint
	for rows.Next() {
		var endpoint InteractionEndpoint
		if err := rows.Scan(&endpoint.ID, &endpoint.Kind, &endpoint.Provider, &endpoint.DisplayName,
			&endpoint.TargetPrefix, &endpoint.InboundEnabled, &endpoint.OutboundEnabled, &endpoint.AuthMode,
			&endpoint.ConfigJSON, &endpoint.CreatedUnix, &endpoint.UpdatedUnix); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, rows.Err()
}

func (s *Store) CreateMessage(ctx context.Context, message Message) (Message, error) {
	if message.ID == "" {
		message.ID = NewID("msg")
	}
	if message.CreatedUnix == 0 {
		message.CreatedUnix = unixNow()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO messages
  (id, target, thread_id, role, content, sender_user_id, sender_agent_id, sender_display_name, sender_kind, source_endpoint_id, external_message_id, metadata_json, request_id, created_unix)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.Target, message.ThreadID, message.Role, message.Content, message.SenderUserID,
		message.SenderAgentID, message.SenderDisplayName, message.SenderKind, message.SourceEndpointID,
		message.ExternalMessageID, message.MetadataJSON, message.RequestID, message.CreatedUnix)
	if isConstraint(err) {
		return Message{}, ErrConflict
	}
	if err != nil {
		return Message{}, err
	}
	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, target string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, target, thread_id, role, content, sender_user_id, sender_agent_id, sender_display_name, sender_kind, source_endpoint_id, external_message_id, metadata_json, request_id, created_unix
FROM messages
WHERE target = ?
ORDER BY created_unix DESC, id DESC
LIMIT ?`, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, task Task) (Task, error) {
	now := unixNow()
	if task.ID == "" {
		task.ID = NewID("tsk")
	}
	if task.State == "" {
		task.State = "todo"
	}
	if !validTaskState(task.State) {
		return Task{}, ErrInvalidState
	}
	task.CreatedUnix = now
	task.UpdatedUnix = now
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, summary, state, target, assignee_id, created_by_user_id, created_unix, updated_unix)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Summary, task.State, task.Target, task.AssigneeID, task.CreatedByUserID, task.CreatedUnix, task.UpdatedUnix)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context, state, target string, limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `
SELECT id, summary, state, target, assignee_id, created_by_user_id, created_unix, updated_unix
FROM tasks`
	var conditions []string
	var args []any
	if state != "" {
		conditions = append(conditions, "state = ?")
		args = append(args, state)
	}
	if target != "" {
		conditions = append(conditions, "target = ?")
		args = append(args, target)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY updated_unix DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateTask(ctx context.Context, id string, patch TaskPatch) (Task, error) {
	current, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if patch.Summary != nil {
		current.Summary = *patch.Summary
	}
	if patch.State != nil {
		if !validTaskState(*patch.State) {
			return Task{}, ErrInvalidState
		}
		current.State = *patch.State
	}
	if patch.AssigneeID != nil {
		current.AssigneeID = *patch.AssigneeID
	}
	current.UpdatedUnix = unixNow()
	_, err = s.db.ExecContext(ctx, `
UPDATE tasks SET summary = ?, state = ?, assignee_id = ?, updated_unix = ? WHERE id = ?`,
		current.Summary, current.State, current.AssigneeID, current.UpdatedUnix, current.ID)
	if err != nil {
		return Task{}, err
	}
	return current, nil
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, summary, state, target, assignee_id, created_by_user_id, created_unix, updated_unix
FROM tasks WHERE id = ?`, id)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

func (s *Store) scanUser(row interface{ Scan(dest ...any) error }) (User, error) {
	var user User
	if err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.PasswordHash, &user.Role, &user.CreatedUnix, &user.UpdatedUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

func scanMessage(row interface{ Scan(dest ...any) error }) (Message, error) {
	var message Message
	if err := row.Scan(&message.ID, &message.Target, &message.ThreadID, &message.Role, &message.Content,
		&message.SenderUserID, &message.SenderAgentID, &message.SenderDisplayName, &message.SenderKind,
		&message.SourceEndpointID, &message.ExternalMessageID, &message.MetadataJSON, &message.RequestID,
		&message.CreatedUnix); err != nil {
		return Message{}, err
	}
	return message, nil
}

func scanTask(row interface{ Scan(dest ...any) error }) (Task, error) {
	var task Task
	if err := row.Scan(&task.ID, &task.Summary, &task.State, &task.Target, &task.AssigneeID, &task.CreatedByUserID, &task.CreatedUnix, &task.UpdatedUnix); err != nil {
		return Task{}, err
	}
	return task, nil
}

func unixNow() int64 {
	return time.Now().Unix()
}

func isConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "constraint")
}

func validTaskState(state string) bool {
	switch state {
	case "todo", "in_progress", "in_review", "done":
		return true
	default:
		return false
	}
}
