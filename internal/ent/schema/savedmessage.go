package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SavedMessage struct {
	ent.Schema
}

func (SavedMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("sav") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("message_id").NotEmpty(),
		field.String("saved_by_user_id").Default(""),
		field.String("saved_by_agent_id").Default(""),
		field.Int64("created_unix"),
	}
}

func (SavedMessage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "message_id", "saved_by_user_id", "saved_by_agent_id").Unique(),
		index.Fields("saved_by_user_id", "created_unix", "id"),
		index.Fields("saved_by_agent_id", "created_unix", "id"),
		index.Fields("target", "created_unix", "id"),
	}
}
