package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NotificationRoute struct {
	ent.Schema
}

func (NotificationRoute) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("nroute") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("thread_id").Default(""),
		field.String("endpoint_id").NotEmpty(),
		field.String("event_kind").Default("message"),
		field.String("preference").Default("all"),
		field.Bool("enabled").Default(true),
		field.String("config_json").Default("{}"),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (NotificationRoute) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "thread_id", "event_kind"),
		index.Fields("endpoint_id", "enabled"),
		index.Fields("target", "endpoint_id", "event_kind", "thread_id").Unique(),
	}
}
