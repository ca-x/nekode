package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Task struct {
	ent.Schema
}

func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("tsk") }).Immutable(),
		field.String("summary").NotEmpty(),
		field.String("description").Default(""),
		field.String("state").Default("todo"),
		field.String("target").NotEmpty(),
		field.String("assignee_id").Default(""),
		field.String("created_by_user_id").Default(""),
		field.String("blocked_reason").Default(""),
		field.Int64("version").Default(1),
		field.String("claim_lease_id").Default(""),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (Task) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("state", "updated_unix"),
		index.Fields("target", "updated_unix"),
		index.Fields("target", "state", "updated_unix", "id"),
		index.Fields("assignee_id", "updated_unix"),
	}
}
