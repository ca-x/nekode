package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ChannelMember struct {
	ent.Schema
}

func (ChannelMember) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("chm") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("member_id").NotEmpty(),
		field.String("username").Default(""),
		field.String("display_name").NotEmpty(),
		field.String("kind").Default("human"),
		field.String("role").Default("member"),
		field.Int64("joined_time_unix"),
		field.Int64("updated_unix"),
	}
}

func (ChannelMember) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "kind", "member_id").Unique(),
		index.Fields("target", "role"),
	}
}
