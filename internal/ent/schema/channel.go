package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Channel struct {
	ent.Schema
}

func (Channel) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("chn") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("display_name").NotEmpty(),
		field.String("channel_type").Default("channel"),
		field.String("visibility").Default("public"),
		field.String("created_by_user_id").Default(""),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (Channel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target").Unique(),
		index.Fields("visibility"),
	}
}
