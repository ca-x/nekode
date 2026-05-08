package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/collaborationevent"
	"github.com/ca-x/nekode/internal/ent/idempotencyrecord"
	"github.com/ca-x/nekode/internal/ent/interactionendpoint"
	"github.com/ca-x/nekode/internal/ent/message"
	"github.com/ca-x/nekode/internal/ent/notificationroute"
	"github.com/ca-x/nekode/internal/ent/outbounddelivery"
	"github.com/ca-x/nekode/internal/ent/predicate"
	"github.com/ca-x/nekode/internal/ent/savedmessage"
	"github.com/ca-x/nekode/internal/ent/session"
	"github.com/ca-x/nekode/internal/ent/task"
	"github.com/ca-x/nekode/internal/ent/threadreadstate"
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

func (s *Store) IsInitialized(ctx context.Context) (bool, error) {
	count, err := s.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	_, err = s.client.IdempotencyRecord.Query().
		Where(
			idempotencyrecord.ScopeEQ("server"),
			idempotencyrecord.MethodEQ("bootstrap"),
			idempotencyrecord.IdempotencyKeyEQ("first_admin"),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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

func (s *Store) GetInteractionEndpoint(ctx context.Context, id string) (InteractionEndpoint, error) {
	row, err := s.client.InteractionEndpoint.Query().Where(interactionendpoint.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return InteractionEndpoint{}, ErrNotFound
	}
	if err != nil {
		return InteractionEndpoint{}, err
	}
	return endpointFromEnt(row), nil
}

func (s *Store) CreateMessage(ctx context.Context, messageModel Message) (Message, error) {
	if messageModel.CreatedUnix == 0 {
		messageModel.CreatedUnix = unixNow()
	}
	attachmentsJSON, err := marshalAttachments(messageModel.Attachments)
	if err != nil {
		return Message{}, err
	}
	create := s.client.Message.Create().
		SetTarget(messageModel.Target).
		SetThreadID(messageModel.ThreadID).
		SetRole(messageModel.Role).
		SetContent(messageModel.Content).
		SetReplyToMessageID(messageModel.ReplyToMessageID).
		SetSenderUserID(messageModel.SenderUserID).
		SetSenderAgentID(messageModel.SenderAgentID).
		SetSenderDisplayName(messageModel.SenderDisplayName).
		SetSenderKind(messageModel.SenderKind).
		SetSourceEndpointID(messageModel.SourceEndpointID).
		SetExternalMessageID(messageModel.ExternalMessageID).
		SetMetadataJSON(messageModel.MetadataJSON).
		SetAttachmentsJSON(attachmentsJSON).
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

func (s *Store) GetMessage(ctx context.Context, target, id string) (Message, error) {
	query := s.client.Message.Query().Where(message.IDEQ(id))
	if target != "" {
		query.Where(message.TargetEQ(target))
	}
	row, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return messageFromEnt(row), nil
}

func (s *Store) ListMessages(ctx context.Context, target, threadID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := s.client.Message.Query().Where(message.TargetEQ(target))
	if threadID != "" {
		query.Where(message.ThreadIDEQ(threadID))
	} else {
		query.Where(message.ThreadIDEQ(""))
	}
	rows, err := query.
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

func (s *Store) CreateOutboundDelivery(ctx context.Context, delivery OutboundDelivery) (OutboundDelivery, error) {
	status := normalizeOutboundDeliveryStatus(delivery.Status)
	if !validOutboundDeliveryStatus(status) {
		return OutboundDelivery{}, ErrInvalidState
	}
	now := unixNow()
	if delivery.CreatedUnix == 0 {
		delivery.CreatedUnix = now
	}
	delivery.UpdatedUnix = now
	create := s.client.OutboundDelivery.Create().
		SetTarget(delivery.Target).
		SetMessageID(delivery.MessageID).
		SetEndpointID(delivery.EndpointID).
		SetEndpointKind(delivery.EndpointKind).
		SetExternalMessageID(delivery.ExternalMessageID).
		SetStatus(status).
		SetAttemptCount(delivery.AttemptCount).
		SetNextRetryTimeUnix(delivery.NextRetryTimeUnix).
		SetDeliveredTimeUnix(delivery.DeliveredTimeUnix).
		SetLastError(delivery.LastError).
		SetRequestID(delivery.RequestID).
		SetCreatedUnix(delivery.CreatedUnix).
		SetUpdatedUnix(delivery.UpdatedUnix)
	if delivery.ID != "" {
		create.SetID(delivery.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return OutboundDelivery{}, ErrConflict
	}
	if err != nil {
		return OutboundDelivery{}, err
	}
	return outboundDeliveryFromEnt(row), nil
}

func (s *Store) GetOutboundDelivery(ctx context.Context, id string) (OutboundDelivery, error) {
	row, err := s.client.OutboundDelivery.Query().Where(outbounddelivery.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return OutboundDelivery{}, ErrNotFound
	}
	if err != nil {
		return OutboundDelivery{}, err
	}
	return outboundDeliveryFromEnt(row), nil
}

func (s *Store) ListOutboundDeliveries(ctx context.Context, opts OutboundDeliveryListOptions) ([]OutboundDelivery, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	query := s.client.OutboundDelivery.Query()
	if opts.Target != "" {
		query.Where(outbounddelivery.TargetEQ(opts.Target))
	}
	if opts.MessageID != "" {
		query.Where(outbounddelivery.MessageIDEQ(opts.MessageID))
	}
	if opts.EndpointID != "" {
		query.Where(outbounddelivery.EndpointIDEQ(opts.EndpointID))
	}
	if len(opts.Statuses) > 0 {
		statuses := make([]string, 0, len(opts.Statuses))
		for _, status := range opts.Statuses {
			normalized := normalizeOutboundDeliveryStatus(status)
			if !validOutboundDeliveryStatus(normalized) {
				return nil, ErrInvalidState
			}
			statuses = append(statuses, normalized)
		}
		query.Where(outbounddelivery.StatusIn(statuses...))
	}
	rows, err := query.
		Order(outbounddelivery.ByUpdatedUnix(sql.OrderDesc()), outbounddelivery.ByID(sql.OrderDesc())).
		Limit(opts.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	deliveries := make([]OutboundDelivery, 0, len(rows))
	for _, row := range rows {
		deliveries = append(deliveries, outboundDeliveryFromEnt(row))
	}
	return deliveries, nil
}

func (s *Store) UpdateOutboundDeliveryStatus(ctx context.Context, id, statusValue, lastError string, nextRetryUnix, deliveredUnix int64) (OutboundDelivery, error) {
	statusValue = normalizeOutboundDeliveryStatus(statusValue)
	if !validOutboundDeliveryStatus(statusValue) {
		return OutboundDelivery{}, ErrInvalidState
	}
	update := s.client.OutboundDelivery.UpdateOneID(id).
		SetStatus(statusValue).
		SetLastError(lastError).
		SetNextRetryTimeUnix(nextRetryUnix).
		SetDeliveredTimeUnix(deliveredUnix).
		SetUpdatedUnix(unixNow())
	row, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		return OutboundDelivery{}, ErrNotFound
	}
	if err != nil {
		return OutboundDelivery{}, err
	}
	return outboundDeliveryFromEnt(row), nil
}

func (s *Store) ScheduleOutboundDeliveryRetry(ctx context.Context, id string, nextRetryUnix int64) (OutboundDelivery, error) {
	now := unixNow()
	if nextRetryUnix == 0 {
		nextRetryUnix = now
	}
	row, err := s.client.OutboundDelivery.UpdateOneID(id).
		SetStatus("retrying").
		AddAttemptCount(1).
		SetNextRetryTimeUnix(nextRetryUnix).
		SetDeliveredTimeUnix(0).
		SetLastError("").
		SetUpdatedUnix(now).
		Save(ctx)
	if ent.IsNotFound(err) {
		return OutboundDelivery{}, ErrNotFound
	}
	if err != nil {
		return OutboundDelivery{}, err
	}
	return outboundDeliveryFromEnt(row), nil
}

func (s *Store) CreateNotificationRoute(ctx context.Context, route NotificationRoute) (NotificationRoute, error) {
	normalized, err := normalizeNotificationRoute(route)
	if err != nil {
		return NotificationRoute{}, err
	}
	now := unixNow()
	create := s.client.NotificationRoute.Create().
		SetTarget(normalized.Target).
		SetThreadID(normalized.ThreadID).
		SetEndpointID(normalized.EndpointID).
		SetEventKind(normalized.EventKind).
		SetPreference(normalized.Preference).
		SetEnabled(normalized.Enabled).
		SetConfigJSON(normalized.ConfigJSON).
		SetCreatedUnix(now).
		SetUpdatedUnix(now)
	if normalized.ID != "" {
		create.SetID(normalized.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return NotificationRoute{}, ErrConflict
	}
	if err != nil {
		return NotificationRoute{}, err
	}
	return notificationRouteFromEnt(row), nil
}

func (s *Store) GetNotificationRoute(ctx context.Context, id string) (NotificationRoute, error) {
	row, err := s.client.NotificationRoute.Query().Where(notificationroute.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return NotificationRoute{}, ErrNotFound
	}
	if err != nil {
		return NotificationRoute{}, err
	}
	return notificationRouteFromEnt(row), nil
}

func (s *Store) ListNotificationRoutes(ctx context.Context, opts NotificationRouteListOptions) ([]NotificationRoute, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 100
	}
	query := s.client.NotificationRoute.Query()
	if opts.Target != "" {
		query.Where(notificationroute.TargetEQ(strings.TrimSpace(opts.Target)))
	}
	if opts.ThreadID != "" {
		query.Where(notificationroute.ThreadIDEQ(strings.TrimSpace(opts.ThreadID)))
	}
	if opts.EndpointID != "" {
		query.Where(notificationroute.EndpointIDEQ(strings.TrimSpace(opts.EndpointID)))
	}
	if opts.EventKind != "" {
		eventKind := normalizeNotificationEventKind(opts.EventKind)
		if !validNotificationEventKind(eventKind) {
			return nil, ErrInvalidState
		}
		query.Where(notificationroute.EventKindEQ(eventKind))
	}
	if opts.Enabled != nil {
		query.Where(notificationroute.EnabledEQ(*opts.Enabled))
	}
	rows, err := query.
		Order(notificationroute.ByUpdatedUnix(sql.OrderDesc()), notificationroute.ByID(sql.OrderDesc())).
		Limit(opts.Limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	routes := make([]NotificationRoute, 0, len(rows))
	for _, row := range rows {
		routes = append(routes, notificationRouteFromEnt(row))
	}
	return routes, nil
}

func (s *Store) UpdateNotificationRoute(ctx context.Context, id string, patch NotificationRoutePatch) (NotificationRoute, error) {
	update := s.client.NotificationRoute.UpdateOneID(strings.TrimSpace(id)).
		SetUpdatedUnix(unixNow())
	if patch.EventKind != nil {
		eventKind := normalizeNotificationEventKind(*patch.EventKind)
		if !validNotificationEventKind(eventKind) {
			return NotificationRoute{}, ErrInvalidState
		}
		update.SetEventKind(eventKind)
	}
	if patch.Preference != nil {
		preference := normalizeNotificationPreference(*patch.Preference)
		if !validNotificationPreference(preference) {
			return NotificationRoute{}, ErrInvalidState
		}
		update.SetPreference(preference)
	}
	if patch.Enabled != nil {
		update.SetEnabled(*patch.Enabled)
	}
	if patch.ConfigJSON != nil {
		configJSON, err := normalizeJSONDocument(*patch.ConfigJSON)
		if err != nil {
			return NotificationRoute{}, ErrInvalidState
		}
		update.SetConfigJSON(configJSON)
	}
	row, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		return NotificationRoute{}, ErrNotFound
	}
	if ent.IsConstraintError(err) {
		return NotificationRoute{}, ErrConflict
	}
	if err != nil {
		return NotificationRoute{}, err
	}
	return notificationRouteFromEnt(row), nil
}

func (s *Store) ResolveNotificationRoutes(ctx context.Context, opts NotificationRouteResolveOptions) ([]NotificationRoute, error) {
	target := strings.TrimSpace(opts.Target)
	if target == "" {
		return nil, ErrInvalidState
	}
	eventKind := normalizeNotificationEventKind(opts.EventKind)
	if !validNotificationEventKind(eventKind) || eventKind == "all" {
		return nil, ErrInvalidState
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	enabled := true
	routes, err := s.ListNotificationRoutes(ctx, NotificationRouteListOptions{
		Target:  target,
		Enabled: &enabled,
		Limit:   200,
	})
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(opts.ThreadID)
	scored := make([]scoredNotificationRoute, 0, len(routes))
	for _, route := range routes {
		score, ok := notificationRouteScore(route, threadID, eventKind)
		if !ok {
			continue
		}
		scored = append(scored, scoredNotificationRoute{route: route, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].route.UpdatedUnix != scored[j].route.UpdatedUnix {
			return scored[i].route.UpdatedUnix > scored[j].route.UpdatedUnix
		}
		return scored[i].route.ID > scored[j].route.ID
	})
	out := make([]NotificationRoute, 0, len(scored))
	seenEndpoint := make(map[string]struct{}, len(scored))
	for _, item := range scored {
		if _, ok := seenEndpoint[item.route.EndpointID]; ok {
			continue
		}
		seenEndpoint[item.route.EndpointID] = struct{}{}
		out = append(out, item.route)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) SearchMessages(ctx context.Context, opts MessageSearchOptions) ([]Message, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}
	query := s.client.Message.Query()
	if opts.Target != "" {
		query.Where(message.TargetEQ(opts.Target))
	}
	search := strings.TrimSpace(opts.Query)
	if search != "" {
		query.Where(message.Or(
			message.IDContainsFold(search),
			message.ContentContainsFold(search),
			message.SenderDisplayNameContainsFold(search),
			message.SenderAgentIDContainsFold(strings.TrimPrefix(search, "@")),
			message.SenderUserIDContainsFold(strings.TrimPrefix(search, "@")),
		))
	}
	if sender := strings.TrimPrefix(strings.TrimSpace(opts.SenderHandle), "@"); sender != "" {
		query.Where(message.Or(
			message.SenderDisplayNameContainsFold(sender),
			message.SenderAgentIDContainsFold(sender),
			message.SenderUserIDContainsFold(sender),
		))
	}
	order := []message.OrderOption{message.ByCreatedUnix(sql.OrderDesc()), message.ByID(sql.OrderDesc())}
	if strings.EqualFold(opts.Sort, "relevance") && search != "" {
		order = []message.OrderOption{
			message.BySenderDisplayName(sql.OrderDesc()),
			message.ByCreatedUnix(sql.OrderDesc()),
			message.ByID(sql.OrderDesc()),
		}
	}
	rows, err := query.Order(order...).Limit(opts.Limit).All(ctx)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, messageFromEnt(row))
	}
	return messages, nil
}

func (s *Store) SaveMessage(ctx context.Context, target, messageID, userID, agentID string) (SavedMessage, error) {
	messageModel, err := s.GetMessage(ctx, target, messageID)
	if err != nil {
		return SavedMessage{}, err
	}
	now := unixNow()
	create := s.client.SavedMessage.Create().
		SetTarget(messageModel.Target).
		SetMessageID(messageModel.ID).
		SetSavedByUserID(userID).
		SetSavedByAgentID(agentID).
		SetCreatedUnix(now)
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		return s.GetSavedMessage(ctx, messageModel.Target, messageModel.ID, userID, agentID)
	}
	if err != nil {
		return SavedMessage{}, err
	}
	return s.savedMessageFromEnt(ctx, row)
}

func (s *Store) GetSavedMessage(ctx context.Context, target, messageID, userID, agentID string) (SavedMessage, error) {
	row, err := s.savedMessageQuery(target, userID, agentID).
		Where(savedmessage.MessageIDEQ(messageID)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return SavedMessage{}, ErrNotFound
	}
	if err != nil {
		return SavedMessage{}, err
	}
	return s.savedMessageFromEnt(ctx, row)
}

func (s *Store) UnsaveMessage(ctx context.Context, target, messageID, userID, agentID string) (SavedMessage, error) {
	saved, err := s.GetSavedMessage(ctx, target, messageID, userID, agentID)
	if err != nil {
		return SavedMessage{}, err
	}
	_, err = s.client.SavedMessage.Delete().
		Where(savedMessagePredicates(target, userID, agentID, savedmessage.MessageIDEQ(messageID))...).
		Exec(ctx)
	if err != nil {
		return SavedMessage{}, err
	}
	return saved, nil
}

func (s *Store) ListSavedMessages(ctx context.Context, target, userID, agentID string, limit int) ([]SavedMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.savedMessageQuery(target, userID, agentID).
		Order(savedmessage.ByCreatedUnix(sql.OrderDesc()), savedmessage.ByID(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	saved := make([]SavedMessage, 0, len(rows))
	for _, row := range rows {
		record, err := s.savedMessageFromEnt(ctx, row)
		if err != nil {
			return nil, err
		}
		saved = append(saved, record)
	}
	return saved, nil
}

func (s *Store) savedMessageQuery(target, userID, agentID string) *ent.SavedMessageQuery {
	return s.client.SavedMessage.Query().Where(savedMessagePredicates(target, userID, agentID)...)
}

func savedMessagePredicates(target, userID, agentID string, extra ...predicate.SavedMessage) []predicate.SavedMessage {
	predicates := []predicate.SavedMessage{}
	if target != "" {
		predicates = append(predicates, savedmessage.TargetEQ(target))
	}
	if userID != "" {
		predicates = append(predicates, savedmessage.SavedByUserIDEQ(userID))
	}
	if agentID != "" {
		predicates = append(predicates, savedmessage.SavedByAgentIDEQ(agentID))
	}
	return append(predicates, extra...)
}

func (s *Store) savedMessageFromEnt(ctx context.Context, row *ent.SavedMessage) (SavedMessage, error) {
	msg, err := s.GetMessage(ctx, row.Target, row.MessageID)
	if err != nil {
		return SavedMessage{}, err
	}
	return SavedMessage{
		ID:             row.ID,
		Target:         row.Target,
		MessageID:      row.MessageID,
		SavedByUserID:  row.SavedByUserID,
		SavedByAgentID: row.SavedByAgentID,
		CreatedUnix:    row.CreatedUnix,
		Message:        msg,
	}, nil
}

func (s *Store) ListThreadInbox(ctx context.Context, userID, targetPrefix string, limit int) ([]ThreadInboxItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	scanLimit := limit * 100
	if scanLimit < 500 {
		scanLimit = 500
	}
	if scanLimit > 5000 {
		scanLimit = 5000
	}
	query := s.client.Message.Query().Where(message.ThreadIDNEQ(""))
	if strings.TrimSpace(targetPrefix) != "" {
		query.Where(message.TargetHasPrefix(strings.TrimSpace(targetPrefix)))
	}
	rows, err := query.
		Order(message.ByCreatedUnix(sql.OrderDesc()), message.ByID(sql.OrderDesc())).
		Limit(scanLimit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	grouped := make(map[string]*ThreadInboxItem)
	for _, row := range rows {
		msg := messageFromEnt(row)
		key := msg.Target + "\x00" + msg.ThreadID
		item := grouped[key]
		if item == nil {
			item = &ThreadInboxItem{
				Target:        msg.Target,
				ThreadID:      msg.ThreadID,
				LatestMessage: msg,
				UpdatedUnix:   msg.CreatedUnix,
			}
			grouped[key] = item
		}
		item.MessageCount++
		item.FirstMessage = msg
	}

	items := make([]ThreadInboxItem, 0, len(grouped))
	for _, item := range grouped {
		if parent, err := s.getMessage(ctx, item.ThreadID); err == nil && parent.Target == item.Target {
			item.FirstMessage = parent
		}
		item.Topic = previewText(item.FirstMessage.Content, 96)
		readState, err := s.getThreadReadState(ctx, userID, item.Target, item.ThreadID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if err == nil {
			item.LastReadUnix = readState.LastReadUnix
			item.LastReadMessageID = readState.LastReadMessageID
		}
		unreadCount, err := s.countUnreadThreadMessages(ctx, item.Target, item.ThreadID, item.LastReadUnix, item.LastReadMessageID)
		if err != nil {
			return nil, err
		}
		item.UnreadCount = unreadCount
		items = append(items, *item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].UpdatedUnix == items[j].UpdatedUnix {
			return items[i].ThreadID > items[j].ThreadID
		}
		return items[i].UpdatedUnix > items[j].UpdatedUnix
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) MarkThreadRead(ctx context.Context, userID, target, threadID string) error {
	latest, err := s.latestThreadMessage(ctx, target, threadID)
	if err != nil {
		return err
	}
	now := unixNow()
	affected, err := s.client.ThreadReadState.Update().
		Where(
			threadreadstate.UserIDEQ(userID),
			threadreadstate.TargetEQ(target),
			threadreadstate.ThreadIDEQ(threadID),
		).
		SetLastReadMessageID(latest.ID).
		SetLastReadUnix(latest.CreatedUnix).
		SetUpdatedUnix(now).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	_, err = s.client.ThreadReadState.Create().
		SetUserID(userID).
		SetTarget(target).
		SetThreadID(threadID).
		SetLastReadMessageID(latest.ID).
		SetLastReadUnix(latest.CreatedUnix).
		SetUpdatedUnix(now).
		Save(ctx)
	if ent.IsConstraintError(err) {
		_, err = s.client.ThreadReadState.Update().
			Where(
				threadreadstate.UserIDEQ(userID),
				threadreadstate.TargetEQ(target),
				threadreadstate.ThreadIDEQ(threadID),
			).
			SetLastReadMessageID(latest.ID).
			SetLastReadUnix(latest.CreatedUnix).
			SetUpdatedUnix(now).
			Save(ctx)
	}
	return err
}

func (s *Store) MarkThreadInboxRead(ctx context.Context, userID, targetPrefix string) error {
	items, err := s.ListThreadInbox(ctx, userID, targetPrefix, 200)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := s.MarkThreadRead(ctx, userID, item.Target, item.ThreadID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListMessageTargets(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.client.Message.Query().
		Select(message.FieldTarget).
		Unique(true).
		Order(message.ByTarget()).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.Target)
	}
	return compactTargets(targets), nil
}

func (s *Store) ListTaskComments(ctx context.Context, taskID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.client.Message.Query().
		Where(message.ThreadIDEQ(taskID)).
		Order(message.ByCreatedUnix(sql.OrderAsc()), message.ByID(sql.OrderAsc())).
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

type threadReadSnapshot struct {
	LastReadMessageID string
	LastReadUnix      int64
}

func (s *Store) getThreadReadState(ctx context.Context, userID, target, threadID string) (threadReadSnapshot, error) {
	row, err := s.client.ThreadReadState.Query().
		Where(
			threadreadstate.UserIDEQ(userID),
			threadreadstate.TargetEQ(target),
			threadreadstate.ThreadIDEQ(threadID),
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return threadReadSnapshot{}, ErrNotFound
	}
	if err != nil {
		return threadReadSnapshot{}, err
	}
	return threadReadSnapshot{LastReadMessageID: row.LastReadMessageID, LastReadUnix: row.LastReadUnix}, nil
}

func (s *Store) countUnreadThreadMessages(ctx context.Context, target, threadID string, lastReadUnix int64, lastReadMessageID string) (int, error) {
	if lastReadMessageID == "" {
		query := s.client.Message.Query().
			Where(message.TargetEQ(target), message.ThreadIDEQ(threadID))
		if lastReadUnix > 0 {
			query.Where(message.CreatedUnixGT(lastReadUnix))
		}
		return query.Count(ctx)
	}
	rows, err := s.client.Message.Query().
		Where(message.TargetEQ(target), message.ThreadIDEQ(threadID)).
		Order(message.ByCreatedUnix(sql.OrderDesc()), message.ByID(sql.OrderDesc())).
		Limit(5000).
		All(ctx)
	if err != nil {
		return 0, err
	}
	unread := 0
	for _, row := range rows {
		if row.ID == lastReadMessageID {
			return unread, nil
		}
		if lastReadUnix > 0 && row.CreatedUnix < lastReadUnix {
			return unread, nil
		}
		unread++
	}
	if lastReadUnix > 0 {
		return unread, nil
	}
	return len(rows), nil
}

func (s *Store) latestThreadMessage(ctx context.Context, target, threadID string) (Message, error) {
	row, err := s.client.Message.Query().
		Where(message.TargetEQ(target), message.ThreadIDEQ(threadID)).
		Order(message.ByCreatedUnix(sql.OrderDesc()), message.ByID(sql.OrderDesc())).
		First(ctx)
	if ent.IsNotFound(err) {
		parent, parentErr := s.getMessage(ctx, threadID)
		if parentErr != nil {
			return Message{}, parentErr
		}
		if parent.Target != target {
			return Message{}, ErrNotFound
		}
		return parent, nil
	}
	if err != nil {
		return Message{}, err
	}
	return messageFromEnt(row), nil
}

func (s *Store) getMessage(ctx context.Context, id string) (Message, error) {
	row, err := s.client.Message.Query().Where(message.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return messageFromEnt(row), nil
}

func previewText(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if limit <= 0 || len([]rune(trimmed)) <= limit {
		return trimmed
	}
	return string([]rune(trimmed)[:limit]) + "..."
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
		SetDescription(taskModel.Description).
		SetState(taskModel.State).
		SetTarget(taskModel.Target).
		SetAssigneeID(taskModel.AssigneeID).
		SetCreatedByUserID(taskModel.CreatedByUserID).
		SetBlockedReason(taskModel.BlockedReason).
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

func (s *Store) ListTaskTargets(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.client.Task.Query().
		Select(task.FieldTarget).
		Unique(true).
		Order(task.ByTarget()).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, row.Target)
	}
	return compactTargets(targets), nil
}

func (s *Store) UpdateTask(ctx context.Context, id string, patch TaskPatch) (Task, error) {
	update := s.client.Task.UpdateOneID(id)
	if patch.Summary != nil {
		update.SetSummary(*patch.Summary)
	}
	if patch.Description != nil {
		update.SetDescription(*patch.Description)
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
	if patch.BlockedReason != nil {
		update.SetBlockedReason(*patch.BlockedReason)
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
		ReplyToMessageID:  row.ReplyToMessageID,
		SenderUserID:      row.SenderUserID,
		SenderAgentID:     row.SenderAgentID,
		SenderDisplayName: row.SenderDisplayName,
		SenderKind:        row.SenderKind,
		SourceEndpointID:  row.SourceEndpointID,
		ExternalMessageID: row.ExternalMessageID,
		MetadataJSON:      row.MetadataJSON,
		Attachments:       unmarshalAttachments(row.AttachmentsJSON),
		RequestID:         row.RequestID,
		CreatedUnix:       row.CreatedUnix,
	}
}

func outboundDeliveryFromEnt(row *ent.OutboundDelivery) OutboundDelivery {
	return OutboundDelivery{
		ID:                row.ID,
		Target:            row.Target,
		MessageID:         row.MessageID,
		EndpointID:        row.EndpointID,
		EndpointKind:      row.EndpointKind,
		ExternalMessageID: row.ExternalMessageID,
		Status:            row.Status,
		AttemptCount:      row.AttemptCount,
		NextRetryTimeUnix: row.NextRetryTimeUnix,
		DeliveredTimeUnix: row.DeliveredTimeUnix,
		LastError:         row.LastError,
		RequestID:         row.RequestID,
		CreatedUnix:       row.CreatedUnix,
		UpdatedUnix:       row.UpdatedUnix,
	}
}

func notificationRouteFromEnt(row *ent.NotificationRoute) NotificationRoute {
	return NotificationRoute{
		ID:          row.ID,
		Target:      row.Target,
		ThreadID:    row.ThreadID,
		EndpointID:  row.EndpointID,
		EventKind:   row.EventKind,
		Preference:  row.Preference,
		Enabled:     row.Enabled,
		ConfigJSON:  row.ConfigJSON,
		CreatedUnix: row.CreatedUnix,
		UpdatedUnix: row.UpdatedUnix,
	}
}

func marshalAttachments(attachments []Attachment) (string, error) {
	if len(attachments) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(attachments)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalAttachments(value string) []Attachment {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var attachments []Attachment
	if err := json.Unmarshal([]byte(value), &attachments); err != nil {
		return nil
	}
	return attachments
}

func taskFromEnt(row *ent.Task) Task {
	return Task{
		ID:              row.ID,
		Summary:         row.Summary,
		Description:     row.Description,
		State:           row.State,
		Target:          row.Target,
		AssigneeID:      row.AssigneeID,
		CreatedByUserID: row.CreatedByUserID,
		BlockedReason:   row.BlockedReason,
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

func compactTargets(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		target := strings.TrimSpace(value)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		out = append(out, target)
	}
	return out
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

func normalizeOutboundDeliveryStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending":
		return "pending"
	case "delivered":
		return "delivered"
	case "failed":
		return "failed"
	case "retry", "retrying":
		return "retrying"
	case "cancelled", "canceled":
		return "canceled"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func validOutboundDeliveryStatus(status string) bool {
	switch status {
	case "pending", "delivered", "failed", "retrying", "canceled":
		return true
	default:
		return false
	}
}

type scoredNotificationRoute struct {
	route NotificationRoute
	score int
}

func normalizeNotificationRoute(route NotificationRoute) (NotificationRoute, error) {
	route.Target = strings.TrimSpace(route.Target)
	route.ThreadID = strings.TrimSpace(route.ThreadID)
	route.EndpointID = strings.TrimSpace(route.EndpointID)
	route.EventKind = normalizeNotificationEventKind(route.EventKind)
	route.Preference = normalizeNotificationPreference(route.Preference)
	configJSON, err := normalizeJSONDocument(route.ConfigJSON)
	if err != nil {
		return NotificationRoute{}, ErrInvalidState
	}
	route.ConfigJSON = configJSON
	if route.Target == "" || route.EndpointID == "" {
		return NotificationRoute{}, ErrInvalidState
	}
	if !validNotificationEventKind(route.EventKind) || !validNotificationPreference(route.Preference) {
		return NotificationRoute{}, ErrInvalidState
	}
	return route, nil
}

func notificationRouteScore(route NotificationRoute, threadID, eventKind string) (int, bool) {
	if !route.Enabled || route.Preference == "muted" {
		return 0, false
	}
	if route.ThreadID != "" && route.ThreadID != threadID {
		return 0, false
	}
	score := 0
	if threadID != "" && route.ThreadID == threadID {
		score += 4
	}
	switch route.EventKind {
	case eventKind:
		score += 2
	case "all":
		score++
	default:
		return 0, false
	}
	if route.Preference == "mentions" && eventKind != "mention" {
		return 0, false
	}
	return score, true
}

func normalizeNotificationEventKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "-", "_")
	switch kind {
	case "", "messages":
		return "message"
	case "*", "any":
		return "all"
	case "mentions":
		return "mention"
	case "tasks":
		return "task"
	case "reminders":
		return "reminder"
	case "runs":
		return "run"
	case "activities":
		return "activity"
	case "delivery", "delivery_status", "outbound", "outbound_delivery":
		return "delivery_status"
	default:
		return kind
	}
}

func validNotificationEventKind(kind string) bool {
	switch kind {
	case "all", "message", "mention", "task", "reminder", "run", "activity", "delivery_status":
		return true
	default:
		return false
	}
}

func normalizeNotificationPreference(preference string) string {
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "all", "enabled":
		return "all"
	case "mention", "mentions":
		return "mentions"
	case "none", "mute", "muted", "disabled", "off":
		return "muted"
	default:
		return strings.ToLower(strings.TrimSpace(preference))
	}
}

func validNotificationPreference(preference string) bool {
	switch preference {
	case "all", "mentions", "muted":
		return true
	default:
		return false
	}
}

func normalizeJSONDocument(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}", nil
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(value)); err != nil {
		return "", err
	}
	return out.String(), nil
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
