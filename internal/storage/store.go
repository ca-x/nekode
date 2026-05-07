package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/interactionendpoint"
	"github.com/ca-x/nekode/internal/ent/message"
	"github.com/ca-x/nekode/internal/ent/session"
	"github.com/ca-x/nekode/internal/ent/task"
	"github.com/ca-x/nekode/internal/ent/user"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib-x/entsqlite"
	_ "github.com/lib/pq"
)

type Store struct {
	client *ent.Client
}

type OpenOptions struct {
	Type string
	DSN  string
}

func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithOptions(ctx, OpenOptions{Type: "sqlite", DSN: path})
}

func OpenWithOptions(ctx context.Context, opts OpenOptions) (*Store, error) {
	dbType := normalizeDBType(opts.Type)
	dsn := strings.TrimSpace(opts.DSN)
	if dbType == "sqlite" {
		if dsn == "" {
			return nil, errors.New("sqlite dsn is required")
		}
		if dsn != ":memory:" && !strings.HasPrefix(dsn, "file:") {
			if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
				return nil, err
			}
			dsn = sqliteDSN(dsn)
		} else {
			dsn = ensureSQLiteForeignKeys(dsn)
		}
	} else if dsn == "" {
		return nil, fmt.Errorf("%s dsn is required", dbType)
	}

	client, err := ent.Open(entDialect(dbType), dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{client: client}
	if err := store.Migrate(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.client.Schema.Create(ctx, entschema.WithForeignKeys(true))
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	return s.client.User.Query().Count(ctx)
}

func (s *Store) CreateUser(ctx context.Context, userModel User) (User, error) {
	now := unixNow()
	role := userModel.Role
	if role == "" {
		role = "member"
	}
	create := s.client.User.Create().
		SetUsername(userModel.Username).
		SetDisplayName(userModel.DisplayName).
		SetPasswordHash(userModel.PasswordHash).
		SetRole(role).
		SetCreatedUnix(now).
		SetUpdatedUnix(now)
	if userModel.ID != "" {
		create.SetID(userModel.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	return userFromEnt(row), nil
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	row, err := s.client.User.Query().Where(user.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return userFromEnt(row), nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	row, err := s.client.User.Query().Where(user.UsernameEQ(username)).Only(ctx)
	if ent.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return userFromEnt(row), nil
}

func (s *Store) CreateSession(ctx context.Context, sessionModel Session) (Session, error) {
	now := unixNow()
	create := s.client.Session.Create().
		SetTokenHash(sessionModel.TokenHash).
		SetUserID(sessionModel.UserID).
		SetExpiresUnix(sessionModel.ExpiresUnix).
		SetCreatedUnix(now)
	if sessionModel.ID != "" {
		create.SetID(sessionModel.ID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return Session{}, err
	}
	return sessionFromEnt(row), nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	row, err := s.client.Session.Query().Where(session.TokenHashEQ(tokenHash)).Only(ctx)
	if ent.IsNotFound(err) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if row.ExpiresUnix <= unixNow() {
		_ = s.DeleteSession(ctx, row.ID)
		return Session{}, ErrNotFound
	}
	return sessionFromEnt(row), nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.client.Session.DeleteOneID(id).Exec(ctx)
}

func (s *Store) CreateInteractionEndpoint(ctx context.Context, endpoint InteractionEndpoint) (InteractionEndpoint, error) {
	now := unixNow()
	create := s.client.InteractionEndpoint.Create().
		SetKind(endpoint.Kind).
		SetProvider(endpoint.Provider).
		SetDisplayName(endpoint.DisplayName).
		SetTargetPrefix(endpoint.TargetPrefix).
		SetInboundEnabled(endpoint.InboundEnabled).
		SetOutboundEnabled(endpoint.OutboundEnabled).
		SetAuthMode(endpoint.AuthMode).
		SetConfigJSON(endpoint.ConfigJSON).
		SetCreatedUnix(now).
		SetUpdatedUnix(now)
	if endpoint.ID != "" {
		create.SetID(endpoint.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return InteractionEndpoint{}, ErrConflict
	}
	if err != nil {
		return InteractionEndpoint{}, err
	}
	return endpointFromEnt(row), nil
}

func (s *Store) ListInteractionEndpoints(ctx context.Context, limit int) ([]InteractionEndpoint, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.client.InteractionEndpoint.Query().
		Order(interactionendpoint.ByCreatedUnix(sql.OrderDesc()), interactionendpoint.ByID(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]InteractionEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, endpointFromEnt(row))
	}
	return endpoints, nil
}

func (s *Store) CreateMessage(ctx context.Context, messageModel Message) (Message, error) {
	if messageModel.CreatedUnix == 0 {
		messageModel.CreatedUnix = unixNow()
	}
	create := s.client.Message.Create().
		SetTarget(messageModel.Target).
		SetThreadID(messageModel.ThreadID).
		SetRole(messageModel.Role).
		SetContent(messageModel.Content).
		SetSenderUserID(messageModel.SenderUserID).
		SetSenderAgentID(messageModel.SenderAgentID).
		SetSenderDisplayName(messageModel.SenderDisplayName).
		SetSenderKind(messageModel.SenderKind).
		SetSourceEndpointID(messageModel.SourceEndpointID).
		SetExternalMessageID(messageModel.ExternalMessageID).
		SetMetadataJSON(messageModel.MetadataJSON).
		SetRequestID(messageModel.RequestID).
		SetCreatedUnix(messageModel.CreatedUnix)
	if messageModel.ID != "" {
		create.SetID(messageModel.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return Message{}, ErrConflict
	}
	if err != nil {
		return Message{}, err
	}
	return messageFromEnt(row), nil
}

func (s *Store) ListMessages(ctx context.Context, target string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.client.Message.Query().
		Where(message.TargetEQ(target)).
		Order(message.ByCreatedUnix(sql.OrderDesc()), message.ByID(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, messageFromEnt(row))
	}
	return messages, nil
}

func (s *Store) CreateTask(ctx context.Context, taskModel Task) (Task, error) {
	now := unixNow()
	if taskModel.State == "" {
		taskModel.State = "todo"
	}
	if !validTaskState(taskModel.State) {
		return Task{}, ErrInvalidState
	}
	create := s.client.Task.Create().
		SetSummary(taskModel.Summary).
		SetState(taskModel.State).
		SetTarget(taskModel.Target).
		SetAssigneeID(taskModel.AssigneeID).
		SetCreatedByUserID(taskModel.CreatedByUserID).
		SetCreatedUnix(now).
		SetUpdatedUnix(now)
	if taskModel.ID != "" {
		create.SetID(taskModel.ID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		return Task{}, err
	}
	return taskFromEnt(row), nil
}

func (s *Store) ListTasks(ctx context.Context, state, target string, limit int) ([]Task, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := s.client.Task.Query()
	if state != "" {
		query.Where(task.StateEQ(state))
	}
	if target != "" {
		query.Where(task.TargetEQ(target))
	}
	rows, err := query.
		Order(task.ByUpdatedUnix(sql.OrderDesc()), task.ByID(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, taskFromEnt(row))
	}
	return tasks, nil
}

func (s *Store) UpdateTask(ctx context.Context, id string, patch TaskPatch) (Task, error) {
	update := s.client.Task.UpdateOneID(id)
	if patch.Summary != nil {
		update.SetSummary(*patch.Summary)
	}
	if patch.State != nil {
		if !validTaskState(*patch.State) {
			return Task{}, ErrInvalidState
		}
		update.SetState(*patch.State)
	}
	if patch.AssigneeID != nil {
		update.SetAssigneeID(*patch.AssigneeID)
	}
	update.SetUpdatedUnix(unixNow())
	row, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	return taskFromEnt(row), nil
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	row, err := s.client.Task.Query().Where(task.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	return taskFromEnt(row), nil
}

func userFromEnt(row *ent.User) User {
	return User{
		ID:           row.ID,
		Username:     row.Username,
		DisplayName:  row.DisplayName,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		CreatedUnix:  row.CreatedUnix,
		UpdatedUnix:  row.UpdatedUnix,
	}
}

func sessionFromEnt(row *ent.Session) Session {
	return Session{
		ID:          row.ID,
		TokenHash:   row.TokenHash,
		UserID:      row.UserID,
		ExpiresUnix: row.ExpiresUnix,
		CreatedUnix: row.CreatedUnix,
	}
}

func endpointFromEnt(row *ent.InteractionEndpoint) InteractionEndpoint {
	return InteractionEndpoint{
		ID:              row.ID,
		Kind:            row.Kind,
		Provider:        row.Provider,
		DisplayName:     row.DisplayName,
		TargetPrefix:    row.TargetPrefix,
		InboundEnabled:  row.InboundEnabled,
		OutboundEnabled: row.OutboundEnabled,
		AuthMode:        row.AuthMode,
		ConfigJSON:      row.ConfigJSON,
		CreatedUnix:     row.CreatedUnix,
		UpdatedUnix:     row.UpdatedUnix,
	}
}

func messageFromEnt(row *ent.Message) Message {
	return Message{
		ID:                row.ID,
		Target:            row.Target,
		ThreadID:          row.ThreadID,
		Role:              row.Role,
		Content:           row.Content,
		SenderUserID:      row.SenderUserID,
		SenderAgentID:     row.SenderAgentID,
		SenderDisplayName: row.SenderDisplayName,
		SenderKind:        row.SenderKind,
		SourceEndpointID:  row.SourceEndpointID,
		ExternalMessageID: row.ExternalMessageID,
		MetadataJSON:      row.MetadataJSON,
		RequestID:         row.RequestID,
		CreatedUnix:       row.CreatedUnix,
	}
}

func taskFromEnt(row *ent.Task) Task {
	return Task{
		ID:              row.ID,
		Summary:         row.Summary,
		State:           row.State,
		Target:          row.Target,
		AssigneeID:      row.AssigneeID,
		CreatedByUserID: row.CreatedByUserID,
		CreatedUnix:     row.CreatedUnix,
		UpdatedUnix:     row.UpdatedUnix,
	}
}

func unixNow() int64 {
	return time.Now().Unix()
}

func validTaskState(state string) bool {
	switch state {
	case "todo", "in_progress", "in_review", "done":
		return true
	default:
		return false
	}
}

func normalizeDBType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "sqlite", "sqlite3":
		return "sqlite"
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func entDialect(dbType string) string {
	switch normalizeDBType(dbType) {
	case "postgres":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return "sqlite3"
	}
}

func sqliteDSN(path string) string {
	return fmt.Sprintf("file:%s?cache=shared&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)", path)
}

func ensureSQLiteForeignKeys(dsn string) string {
	if strings.Contains(dsn, "_pragma=foreign_keys") {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_pragma=foreign_keys(1)"
}
