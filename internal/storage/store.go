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
	"github.com/ca-x/nekode/internal/ent/collaborationevent"
	"github.com/ca-x/nekode/internal/ent/idempotencyrecord"
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

func (s *Store) CreateFirstAdmin(ctx context.Context, userModel User) (User, error) {
	now := unixNow()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	_, err = tx.IdempotencyRecord.Create().
		SetScope("server").
		SetMethod("bootstrap").
		SetActorID("").
		SetIdempotencyKey("first_admin").
		SetStatus("completed").
		SetCreatedUnix(now).
		SetExpiresUnix(0).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	count, err := tx.User.Query().Count(ctx)
	if err != nil {
		return User{}, err
	}
	if count != 0 {
		return User{}, ErrConflict
	}
	role := userModel.Role
	if role == "" {
		role = "admin"
	}
	row, err := tx.User.Create().
		SetUsername(userModel.Username).
		SetDisplayName(userModel.DisplayName).
		SetPasswordHash(userModel.PasswordHash).
		SetRole(role).
		SetCreatedUnix(now).
		SetUpdatedUnix(now).
		Save(ctx)
	if ent.IsConstraintError(err) {
		_ = tx.Rollback()
		return User{}, ErrConflict
	}
	if err != nil {
		_ = tx.Rollback()
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
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
	taskModel.State = normalizeTaskState(taskModel.State)
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
		SetVersion(1).
		SetClaimLeaseID(taskModel.ClaimLeaseID).
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
	state = normalizeTaskState(state)
	if state != "" && !validTaskState(state) {
		return nil, ErrInvalidState
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
		state := normalizeTaskState(*patch.State)
		if !validTaskState(state) {
			return Task{}, ErrInvalidState
		}
		update.SetState(state)
	}
	if patch.AssigneeID != nil {
		update.SetAssigneeID(*patch.AssigneeID)
	}
	update.AddVersion(1)
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

func (s *Store) ClaimTaskCAS(ctx context.Context, id, assigneeID, leaseID string) (Task, bool, error) {
	now := unixNow()
	affected, err := s.client.Task.Update().
		Where(
			task.IDEQ(id),
			task.Or(task.AssigneeIDEQ(""), task.AssigneeIDEQ(assigneeID)),
		).
		SetAssigneeID(assigneeID).
		SetClaimLeaseID(leaseID).
		AddVersion(1).
		SetUpdatedUnix(now).
		Save(ctx)
	if err != nil {
		return Task{}, false, err
	}
	if affected == 0 {
		current, getErr := s.GetTask(ctx, id)
		if getErr != nil {
			return Task{}, false, getErr
		}
		return current, false, nil
	}
	current, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, false, err
	}
	return current, true, nil
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

func (s *Store) AppendCollaborationEvent(ctx context.Context, event CollaborationEvent) (CollaborationEvent, error) {
	if event.ServerID == "" {
		return CollaborationEvent{}, errors.New("server_id is required")
	}
	if event.EventID == "" {
		event.EventID = NewID("cev")
	}
	if event.CreatedUnix == 0 {
		event.CreatedUnix = unixNow()
	}
	if event.ProtocolVersion == 0 {
		event.ProtocolVersion = 1
	}
	if event.PayloadJSON == "" {
		event.PayloadJSON = "{}"
	}
	if event.ScopeID == "" {
		event.ScopeID = firstNonEmpty(event.AggregateID, event.Target)
	}
	for attempt := 0; attempt < 5; attempt++ {
		tx, err := s.client.Tx(ctx)
		if err != nil {
			return CollaborationEvent{}, err
		}
		last, err := tx.CollaborationEvent.Query().
			Where(collaborationevent.ServerIDEQ(event.ServerID)).
			Order(collaborationevent.BySequence(sql.OrderDesc())).
			First(ctx)
		if ent.IsNotFound(err) {
			event.Sequence = 1
		} else if err != nil {
			_ = tx.Rollback()
			return CollaborationEvent{}, err
		} else {
			event.Sequence = last.Sequence + 1
		}
		row, err := tx.CollaborationEvent.Create().
			SetServerID(event.ServerID).
			SetSequence(event.Sequence).
			SetEventID(event.EventID).
			SetTarget(event.Target).
			SetAggregateID(event.AggregateID).
			SetKind(event.Kind).
			SetOperation(event.Operation).
			SetScopeType(event.ScopeType).
			SetScopeID(event.ScopeID).
			SetWorkspaceID(event.WorkspaceID).
			SetActivityID(event.ActivityID).
			SetPayloadJSON(event.PayloadJSON).
			SetCreatedUnix(event.CreatedUnix).
			SetProtocolVersion(event.ProtocolVersion).
			Save(ctx)
		if ent.IsConstraintError(err) {
			_ = tx.Rollback()
			continue
		}
		if err != nil {
			_ = tx.Rollback()
			return CollaborationEvent{}, err
		}
		if err := tx.Commit(); err != nil {
			return CollaborationEvent{}, err
		}
		return collaborationEventFromEnt(row), nil
	}
	return CollaborationEvent{}, ErrConflict
}

func (s *Store) ListCollaborationEvents(ctx context.Context, serverID, target, aggregateID string, afterSequence int64, limit int) ([]CollaborationEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := s.client.CollaborationEvent.Query().Where(
		collaborationevent.ServerIDEQ(serverID),
		collaborationevent.SequenceGT(afterSequence),
	)
	if target != "" {
		query.Where(collaborationevent.TargetEQ(target))
	}
	if aggregateID != "" {
		query.Where(collaborationevent.AggregateIDEQ(aggregateID))
	}
	rows, err := query.
		Order(collaborationevent.BySequence(sql.OrderAsc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	events := make([]CollaborationEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, collaborationEventFromEnt(row))
	}
	return events, nil
}

func (s *Store) ListRecentCollaborationEvents(ctx context.Context, serverID, target, kind string, limit int) ([]CollaborationEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := s.client.CollaborationEvent.Query().Where(collaborationevent.ServerIDEQ(serverID))
	if target != "" {
		query.Where(collaborationevent.TargetEQ(target))
	}
	if kind != "" {
		query.Where(collaborationevent.KindEQ(kind))
	}
	rows, err := query.
		Order(collaborationevent.BySequence(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	events := make([]CollaborationEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, collaborationEventFromEnt(row))
	}
	return events, nil
}

func (s *Store) GetIdempotencyRecord(ctx context.Context, scope, method, actorID, key string) (IdempotencyRecord, error) {
	row, err := s.client.IdempotencyRecord.Query().
		Where(
			idempotencyrecord.ScopeEQ(scope),
			idempotencyrecord.MethodEQ(method),
			idempotencyrecord.ActorIDEQ(actorID),
			idempotencyrecord.IdempotencyKeyEQ(key),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return IdempotencyRecord{}, ErrNotFound
	}
	if err != nil {
		return IdempotencyRecord{}, err
	}
	return idempotencyRecordFromEnt(row), nil
}

func (s *Store) ReserveIdempotencyRecord(ctx context.Context, record IdempotencyRecord) (IdempotencyRecord, bool, error) {
	now := unixNow()
	if record.CreatedUnix == 0 {
		record.CreatedUnix = now
	}
	if record.ExpiresUnix == 0 {
		record.ExpiresUnix = now + int64((24 * time.Hour).Seconds())
	}
	if record.Status == "" {
		record.Status = "pending"
	}
	row, err := s.client.IdempotencyRecord.Create().
		SetScope(record.Scope).
		SetMethod(record.Method).
		SetActorID(record.ActorID).
		SetIdempotencyKey(record.IdempotencyKey).
		SetRequestHash(record.RequestHash).
		SetResponseType(record.ResponseType).
		SetResponseJSON(record.ResponseJSON).
		SetResourceType(record.ResourceType).
		SetResourceID(record.ResourceID).
		SetStatus(record.Status).
		SetCreatedUnix(record.CreatedUnix).
		SetExpiresUnix(record.ExpiresUnix).
		Save(ctx)
	if ent.IsConstraintError(err) {
		existing, getErr := s.GetIdempotencyRecord(ctx, record.Scope, record.Method, record.ActorID, record.IdempotencyKey)
		if getErr != nil {
			return IdempotencyRecord{}, false, getErr
		}
		return existing, false, nil
	}
	if err != nil {
		return IdempotencyRecord{}, false, err
	}
	return idempotencyRecordFromEnt(row), true, nil
}

func (s *Store) CompleteIdempotencyRecord(ctx context.Context, record IdempotencyRecord) error {
	affected, err := s.client.IdempotencyRecord.Update().
		Where(
			idempotencyrecord.ScopeEQ(record.Scope),
			idempotencyrecord.MethodEQ(record.Method),
			idempotencyrecord.ActorIDEQ(record.ActorID),
			idempotencyrecord.IdempotencyKeyEQ(record.IdempotencyKey),
		).
		SetRequestHash(record.RequestHash).
		SetResponseType(record.ResponseType).
		SetResponseJSON(record.ResponseJSON).
		SetResourceType(record.ResourceType).
		SetResourceID(record.ResourceID).
		SetStatus("completed").
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
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
		Version:         row.Version,
		ClaimLeaseID:    row.ClaimLeaseID,
		CreatedUnix:     row.CreatedUnix,
		UpdatedUnix:     row.UpdatedUnix,
	}
}

func collaborationEventFromEnt(row *ent.CollaborationEvent) CollaborationEvent {
	return CollaborationEvent{
		ID:              row.ID,
		ServerID:        row.ServerID,
		Sequence:        row.Sequence,
		EventID:         row.EventID,
		Target:          row.Target,
		AggregateID:     row.AggregateID,
		Kind:            row.Kind,
		Operation:       row.Operation,
		ScopeType:       row.ScopeType,
		ScopeID:         row.ScopeID,
		WorkspaceID:     row.WorkspaceID,
		ActivityID:      row.ActivityID,
		PayloadJSON:     row.PayloadJSON,
		CreatedUnix:     row.CreatedUnix,
		ProtocolVersion: row.ProtocolVersion,
	}
}

func idempotencyRecordFromEnt(row *ent.IdempotencyRecord) IdempotencyRecord {
	return IdempotencyRecord{
		ID:             row.ID,
		Scope:          row.Scope,
		Method:         row.Method,
		ActorID:        row.ActorID,
		IdempotencyKey: row.IdempotencyKey,
		RequestHash:    row.RequestHash,
		ResponseType:   row.ResponseType,
		ResponseJSON:   row.ResponseJSON,
		ResourceType:   row.ResourceType,
		ResourceID:     row.ResourceID,
		Status:         row.Status,
		CreatedUnix:    row.CreatedUnix,
		ExpiresUnix:    row.ExpiresUnix,
	}
}

func unixNow() int64 {
	return time.Now().Unix()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validTaskState(state string) bool {
	switch normalizeTaskState(state) {
	case "todo", "in_progress", "in_review", "blocked", "done", "canceled":
		return true
	default:
		return false
	}
}

func normalizeTaskState(state string) string {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "cancelled" {
		return "canceled"
	}
	return state
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
	return fmt.Sprintf("file:%s?cache=shared&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(10000)", path)
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
