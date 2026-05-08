package storage

import (
	"context"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/ca-x/nekode/internal/ent"
	"github.com/ca-x/nekode/internal/ent/reminder"
	"github.com/ca-x/nekode/internal/ent/reminderevent"
)

func (s *Store) CreateReminder(ctx context.Context, reminderModel Reminder, actorType, actorID, detail string) (Reminder, error) {
	now := unixNow()
	reminderModel.Status = normalizeReminderStatus(reminderModel.Status)
	if reminderModel.Status == "" {
		reminderModel.Status = "active"
	}
	if !validReminderStatus(reminderModel.Status) {
		return Reminder{}, ErrInvalidState
	}
	reminderModel.ScheduleKind = normalizeReminderScheduleKind(reminderModel.ScheduleKind)
	if reminderModel.ScheduleKind == "" {
		reminderModel.ScheduleKind = "at"
	}
	if !validReminderScheduleKind(reminderModel.ScheduleKind) {
		return Reminder{}, ErrInvalidState
	}
	if reminderModel.CreatedUnix == 0 {
		reminderModel.CreatedUnix = now
	}
	reminderModel.UpdatedUnix = now
	reminderModel.Enabled = reminderModel.Status == "active"

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback()

	create := tx.Reminder.Create().
		SetTarget(reminderModel.Target).
		SetScheduleKind(reminderModel.ScheduleKind).
		SetSchedule(reminderModel.Schedule).
		SetPrompt(reminderModel.Prompt).
		SetEnabled(reminderModel.Enabled).
		SetNextRunUnix(reminderModel.NextRunUnix).
		SetLastRunUnix(reminderModel.LastRunUnix).
		SetRunCount(reminderModel.RunCount).
		SetLastError(reminderModel.LastError).
		SetTitle(reminderModel.Title).
		SetStatus(reminderModel.Status).
		SetMsgRef(reminderModel.MsgRef).
		SetRecurrenceRule(reminderModel.RecurrenceRule).
		SetRecurrenceDescription(reminderModel.RecurrenceDescription).
		SetRecurrenceTimezone(reminderModel.RecurrenceTimezone).
		SetCancelToken(reminderModel.CancelToken).
		SetCreatedUnix(reminderModel.CreatedUnix).
		SetUpdatedUnix(reminderModel.UpdatedUnix)
	if reminderModel.ID != "" {
		create.SetID(reminderModel.ID)
	}
	row, err := create.Save(ctx)
	if ent.IsConstraintError(err) {
		_ = tx.Rollback()
		return Reminder{}, ErrConflict
	}
	if err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if _, err := createReminderEvent(ctx, tx, ReminderEvent{
		ReminderID:       row.ID,
		EventType:        "created",
		ActorType:        normalizeReminderActorType(actorType),
		ActorID:          strings.TrimSpace(actorID),
		OccurredTimeUnix: now,
		NextFireTimeUnix: row.NextRunUnix,
		Detail:           strings.TrimSpace(detail),
	}); err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, err
	}
	return reminderFromEnt(row), nil
}

func (s *Store) ListReminders(ctx context.Context, target string, statuses []string, includeCanceled bool, limit int) ([]Reminder, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := s.client.Reminder.Query()
	if strings.TrimSpace(target) != "" {
		query.Where(reminder.TargetEQ(strings.TrimSpace(target)))
	}
	if len(statuses) > 0 {
		normalized := make([]string, 0, len(statuses))
		for _, status := range statuses {
			status = normalizeReminderStatus(status)
			if status == "" {
				continue
			}
			if !validReminderStatus(status) {
				return nil, ErrInvalidState
			}
			normalized = append(normalized, status)
		}
		if len(normalized) > 0 {
			query.Where(reminder.StatusIn(normalized...))
		}
	} else if !includeCanceled {
		query.Where(reminder.StatusNEQ("canceled"))
	}
	rows, err := query.
		Order(reminder.ByUpdatedUnix(sql.OrderDesc()), reminder.ByID(sql.OrderDesc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	reminders := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		reminders = append(reminders, reminderFromEnt(row))
	}
	return reminders, nil
}

func (s *Store) GetReminder(ctx context.Context, id string) (Reminder, error) {
	row, err := s.client.Reminder.Query().Where(reminder.IDEQ(id)).Only(ctx)
	if ent.IsNotFound(err) {
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		return Reminder{}, err
	}
	return reminderFromEnt(row), nil
}

func (s *Store) CancelReminder(ctx context.Context, id, actorType, actorID, detail string) (Reminder, error) {
	now := unixNow()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback()
	row, err := tx.Reminder.UpdateOneID(id).
		SetStatus("canceled").
		SetEnabled(false).
		SetUpdatedUnix(now).
		Save(ctx)
	if ent.IsNotFound(err) {
		_ = tx.Rollback()
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if _, err := createReminderEvent(ctx, tx, ReminderEvent{
		ReminderID:       id,
		EventType:        "canceled",
		ActorType:        normalizeReminderActorType(actorType),
		ActorID:          strings.TrimSpace(actorID),
		OccurredTimeUnix: now,
		NextFireTimeUnix: row.NextRunUnix,
		Detail:           strings.TrimSpace(detail),
	}); err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, err
	}
	return reminderFromEnt(row), nil
}

func (s *Store) SnoozeReminder(ctx context.Context, id string, nextRunUnix int64, schedule string, actorType, actorID, detail string) (Reminder, error) {
	now := unixNow()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback()
	row, err := tx.Reminder.UpdateOneID(id).
		SetScheduleKind("at").
		SetSchedule(strings.TrimSpace(schedule)).
		SetNextRunUnix(nextRunUnix).
		SetStatus("active").
		SetEnabled(true).
		SetUpdatedUnix(now).
		Save(ctx)
	if ent.IsNotFound(err) {
		_ = tx.Rollback()
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if _, err := createReminderEvent(ctx, tx, ReminderEvent{
		ReminderID:       id,
		EventType:        "snoozed",
		ActorType:        normalizeReminderActorType(actorType),
		ActorID:          strings.TrimSpace(actorID),
		OccurredTimeUnix: now,
		NextFireTimeUnix: nextRunUnix,
		Detail:           strings.TrimSpace(detail),
	}); err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, err
	}
	return reminderFromEnt(row), nil
}

func (s *Store) UpdateReminder(ctx context.Context, id string, patch ReminderPatch, actorType, actorID, detail string) (Reminder, error) {
	now := unixNow()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return Reminder{}, err
	}
	defer tx.Rollback()
	update := tx.Reminder.UpdateOneID(id).SetUpdatedUnix(now)
	if patch.Title != nil {
		update.SetTitle(strings.TrimSpace(*patch.Title))
	}
	if patch.ScheduleKind != nil {
		kind := normalizeReminderScheduleKind(*patch.ScheduleKind)
		if !validReminderScheduleKind(kind) {
			_ = tx.Rollback()
			return Reminder{}, ErrInvalidState
		}
		update.SetScheduleKind(kind)
		update.SetStatus("active").SetEnabled(true)
	}
	if patch.Schedule != nil {
		update.SetSchedule(strings.TrimSpace(*patch.Schedule))
		update.SetStatus("active").SetEnabled(true)
	}
	if patch.NextRunUnix != nil {
		update.SetNextRunUnix(*patch.NextRunUnix)
		update.SetStatus("active").SetEnabled(true)
	}
	if patch.RecurrenceRule != nil {
		update.SetRecurrenceRule(strings.TrimSpace(*patch.RecurrenceRule))
	}
	if patch.RecurrenceDescription != nil {
		update.SetRecurrenceDescription(strings.TrimSpace(*patch.RecurrenceDescription))
	}
	if patch.RecurrenceTimezone != nil {
		update.SetRecurrenceTimezone(strings.TrimSpace(*patch.RecurrenceTimezone))
	}
	row, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		_ = tx.Rollback()
		return Reminder{}, ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if _, err := createReminderEvent(ctx, tx, ReminderEvent{
		ReminderID:       id,
		EventType:        "updated",
		ActorType:        normalizeReminderActorType(actorType),
		ActorID:          strings.TrimSpace(actorID),
		OccurredTimeUnix: now,
		NextFireTimeUnix: row.NextRunUnix,
		Detail:           strings.TrimSpace(detail),
	}); err != nil {
		_ = tx.Rollback()
		return Reminder{}, err
	}
	if err := tx.Commit(); err != nil {
		return Reminder{}, err
	}
	return reminderFromEnt(row), nil
}

func (s *Store) ListReminderEvents(ctx context.Context, id string, limit int) ([]ReminderEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if _, err := s.GetReminder(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.client.ReminderEvent.Query().
		Where(reminderevent.ReminderIDEQ(id)).
		Order(reminderevent.ByOccurredTimeUnix(sql.OrderAsc()), reminderevent.ByID(sql.OrderAsc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	events := make([]ReminderEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, reminderEventFromEnt(row))
	}
	return events, nil
}

func createReminderEvent(ctx context.Context, tx *ent.Tx, event ReminderEvent) (ReminderEvent, error) {
	if event.OccurredTimeUnix == 0 {
		event.OccurredTimeUnix = unixNow()
	}
	event.EventType = normalizeReminderEventType(event.EventType)
	if event.EventType == "" {
		return ReminderEvent{}, ErrInvalidState
	}
	event.ActorType = normalizeReminderActorType(event.ActorType)
	row, err := tx.ReminderEvent.Create().
		SetReminderID(event.ReminderID).
		SetEventType(event.EventType).
		SetActorType(event.ActorType).
		SetActorID(event.ActorID).
		SetOccurredTimeUnix(event.OccurredTimeUnix).
		SetNextFireTimeUnix(event.NextFireTimeUnix).
		SetDetail(event.Detail).
		Save(ctx)
	if err != nil {
		return ReminderEvent{}, err
	}
	return reminderEventFromEnt(row), nil
}

func reminderFromEnt(row *ent.Reminder) Reminder {
	return Reminder{
		ID:                    row.ID,
		Target:                row.Target,
		ScheduleKind:          row.ScheduleKind,
		Schedule:              row.Schedule,
		Prompt:                row.Prompt,
		Enabled:               row.Enabled,
		NextRunUnix:           row.NextRunUnix,
		LastRunUnix:           row.LastRunUnix,
		RunCount:              row.RunCount,
		LastError:             row.LastError,
		Title:                 row.Title,
		Status:                row.Status,
		MsgRef:                row.MsgRef,
		RecurrenceRule:        row.RecurrenceRule,
		RecurrenceDescription: row.RecurrenceDescription,
		RecurrenceTimezone:    row.RecurrenceTimezone,
		CancelToken:           row.CancelToken,
		CreatedUnix:           row.CreatedUnix,
		UpdatedUnix:           row.UpdatedUnix,
	}
}

func reminderEventFromEnt(row *ent.ReminderEvent) ReminderEvent {
	return ReminderEvent{
		ID:               row.ID,
		ReminderID:       row.ReminderID,
		EventType:        row.EventType,
		ActorType:        row.ActorType,
		ActorID:          row.ActorID,
		OccurredTimeUnix: row.OccurredTimeUnix,
		NextFireTimeUnix: row.NextFireTimeUnix,
		Detail:           row.Detail,
	}
}

func normalizeReminderStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "cancelled" {
		return "canceled"
	}
	return value
}

func validReminderStatus(value string) bool {
	switch normalizeReminderStatus(value) {
	case "active", "done", "canceled", "paused", "failed":
		return true
	default:
		return false
	}
}

func normalizeReminderScheduleKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "reminder_schedule_kind_cron":
		return "cron"
	case "reminder_schedule_kind_every":
		return "every"
	case "reminder_schedule_kind_at":
		return "at"
	case "reminder_schedule_kind_rrule":
		return "rrule"
	case "reminder_schedule_kind_natural":
		return "natural"
	default:
		return value
	}
}

func validReminderScheduleKind(value string) bool {
	switch normalizeReminderScheduleKind(value) {
	case "cron", "every", "at", "rrule", "natural":
		return true
	default:
		return false
	}
}

func normalizeReminderEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "reminder_event_type_created":
		return "created"
	case "reminder_event_type_fired":
		return "fired"
	case "reminder_event_type_snoozed":
		return "snoozed"
	case "reminder_event_type_updated":
		return "updated"
	case "reminder_event_type_canceled", "cancelled":
		return "canceled"
	case "reminder_event_type_failed":
		return "failed"
	default:
		return value
	}
}

func normalizeReminderActorType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "human", "agent", "system":
		return value
	case "":
		return "system"
	default:
		return "system"
	}
}
