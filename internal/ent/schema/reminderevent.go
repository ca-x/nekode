package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ReminderEvent struct {
	ent.Schema
}

func (ReminderEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("rev") }).Immutable(),
		field.String("reminder_id").NotEmpty(),
		field.String("event_type").NotEmpty(),
		field.String("actor_type").Default("system"),
		field.String("actor_id").Default(""),
		field.Int64("occurred_time_unix"),
		field.Int64("next_fire_time_unix").Default(0),
		field.String("detail").Default(""),
	}
}

func (ReminderEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("reminder_id", "occurred_time_unix", "id"),
		index.Fields("event_type", "occurred_time_unix"),
	}
}
