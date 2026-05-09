package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Message struct {
	ent.Schema
}

func (Message) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("msg") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("thread_id").Default(""),
		field.String("role").NotEmpty(),
		field.String("content").NotEmpty(),
		field.String("reply_to_message_id").Default(""),
		field.String("sender_user_id").Default(""),
		field.String("sender_agent_id").Default(""),
		field.String("sender_display_name").Default(""),
		field.String("sender_kind").NotEmpty(),
		field.String("source_endpoint_id").Default(""),
		field.String("external_message_id").Default(""),
		field.String("metadata_json").Default("{}"),
		field.String("attachments_json").Default("[]"),
		field.String("request_id").Default(""),
		field.Int64("created_unix"),
		// Semantic classification: note (default), decision, blocker, status.
		// Empty string is treated as "note" on the server side for rows
		// written before this field existed.
		field.String("kind").Default(""),
	}
}

func (Message) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "created_unix", "id"),
		index.Fields("thread_id", "created_unix", "id"),
		index.Fields("reply_to_message_id"),
		index.Fields("request_id"),
		index.Fields("target", "kind", "created_unix"),
	}
}
