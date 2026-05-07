package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("usr") }).Immutable(),
		field.String("username").NotEmpty(),
		field.String("display_name").NotEmpty(),
		field.String("password_hash").NotEmpty(),
		field.String("role").Default("member"),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("username").Unique(),
		index.Fields("role"),
	}
}
