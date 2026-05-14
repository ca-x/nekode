package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IMChatSubscription struct {
	ent.Schema
}

func (IMChatSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("imsub") }).Immutable(),
		field.String("endpoint_id").NotEmpty(),
		field.String("provider").Default(""),
		field.String("conversation_id").NotEmpty(),
		field.String("external_thread_id").Default(""),
		field.String("chat_title").Default(""),
		field.String("target").Default(""),
		field.String("thread_id").Default(""),
		field.String("sender_external_id").Default(""),
		field.String("authorized_by_request_id").Default(""),
		field.Bool("subscribed").Default(true),
		field.Bool("verbose").Default(false),
		field.Int64("authorized_unix").Default(0),
		field.Int64("subscribed_unix").Default(0),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (IMChatSubscription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("endpoint_id", "conversation_id", "external_thread_id").Unique(),
		index.Fields("endpoint_id", "subscribed"),
		index.Fields("provider", "subscribed"),
	}
}
