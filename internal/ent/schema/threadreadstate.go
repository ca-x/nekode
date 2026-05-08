package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ThreadReadState struct {
	ent.Schema
}

func (ThreadReadState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("trs") }).Immutable(),
		field.String("user_id").NotEmpty(),
		field.String("target").NotEmpty(),
		field.String("thread_id").NotEmpty(),
		field.String("last_read_message_id").Default(""),
		field.Int64("last_read_unix").Default(0),
		field.Int64("updated_unix"),
	}
}

func (ThreadReadState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "target", "thread_id").Unique(),
		index.Fields("user_id", "updated_unix"),
	}
}
