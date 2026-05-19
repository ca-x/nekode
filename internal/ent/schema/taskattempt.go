package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TaskAttempt struct {
	ent.Schema
}

func (TaskAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("tat") }).Immutable(),
		field.String("task_id").NotEmpty(),
		field.Uint32("attempt").Default(1),
		field.String("run_id").Default(""),
		field.String("agent_id").Default(""),
		field.String("claim_lease_id").Default(""),
		field.String("status").Default("claimed"),
		field.String("output_json").Default(""),
		field.String("output_digest").Default(""),
		field.String("output_signature").Default(""),
		field.String("signature_public_key").Default(""),
		field.String("error_message").Default(""),
		field.Int64("claimed_unix").Default(0),
		field.Int64("started_unix").Default(0),
		field.Int64("completed_unix").Default(0),
		field.Int64("updated_unix").Default(0),
	}
}

func (TaskAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "attempt").Unique(),
		index.Fields("run_id"),
		index.Fields("agent_id", "updated_unix"),
		index.Fields("status", "updated_unix"),
	}
}
