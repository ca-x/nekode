package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Reminder struct {
	ent.Schema
}

func (Reminder) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("rem") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("schedule_kind").Default("at"),
		field.String("schedule").Default(""),
		field.String("prompt").Default(""),
		field.Bool("enabled").Default(true),
		field.Int64("next_run_unix").Default(0),
		field.Int64("last_run_unix").Default(0),
		field.Int64("run_count").Default(0),
		field.String("last_error").Default(""),
		field.String("title").Default(""),
		field.String("status").Default("active"),
		field.String("msg_ref").Default(""),
		field.String("recurrence_rule").Default(""),
		field.String("recurrence_description").Default(""),
		field.String("recurrence_timezone").Default(""),
		field.String("cancel_token").Default(""),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (Reminder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "status", "next_run_unix"),
		index.Fields("status", "next_run_unix"),
		index.Fields("updated_unix"),
	}
}
