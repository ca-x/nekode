package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("ses") }).Immutable(),
		field.String("token_hash").NotEmpty(),
		field.String("user_id").NotEmpty(),
		field.Int64("expires_unix"),
		field.Int64("created_unix"),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Fields("user_id"),
		index.Fields("expires_unix"),
	}
}
