package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CollaborationEvent struct {
	ent.Schema
}

func (CollaborationEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("cev") }).Immutable(),
		field.String("server_id").NotEmpty(),
		field.Int64("sequence").Positive(),
		field.String("event_id").NotEmpty(),
		field.String("target").Default(""),
		field.String("aggregate_id").Default(""),
		field.String("kind").NotEmpty(),
		field.String("operation").Default(""),
		field.String("scope_type").Default(""),
		field.String("scope_id").Default(""),
		field.String("workspace_id").Default(""),
		field.String("activity_id").Default(""),
		field.String("payload_json").Default("{}"),
		field.Int64("created_unix"),
		field.Int("protocol_version").Positive(),
	}
}

func (CollaborationEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("server_id", "sequence").Unique(),
		index.Fields("event_id").Unique(),
		index.Fields("target", "sequence"),
		index.Fields("aggregate_id", "sequence"),
		index.Fields("kind", "sequence"),
		index.Fields("operation", "sequence"),
		index.Fields("scope_type", "scope_id", "sequence"),
	}
}
