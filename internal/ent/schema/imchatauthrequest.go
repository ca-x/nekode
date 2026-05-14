package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IMChatAuthRequest struct {
	ent.Schema
}

func (IMChatAuthRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("imreq") }).Immutable(),
		field.String("endpoint_id").NotEmpty(),
		field.String("provider").Default(""),
		field.String("conversation_id").NotEmpty(),
		field.String("external_thread_id").Default(""),
		field.String("chat_title").Default(""),
		field.String("sender_external_id").Default(""),
		field.String("token_hash").NotEmpty(),
		field.String("token_prefix").Default(""),
		field.String("status").Default("pending"),
		field.String("requested_target").Default(""),
		field.String("requested_thread_id").Default(""),
		field.Int64("expires_unix").Default(0),
		field.String("resolved_by_user_id").Default(""),
		field.Int64("resolved_unix").Default(0),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (IMChatAuthRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_hash").Unique(),
		index.Fields("endpoint_id", "status", "created_unix"),
		index.Fields("endpoint_id", "conversation_id", "external_thread_id", "status"),
	}
}
