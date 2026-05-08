package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type OutboundDelivery struct {
	ent.Schema
}

func (OutboundDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("odlv") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("message_id").NotEmpty(),
		field.String("endpoint_id").NotEmpty(),
		field.String("endpoint_kind").Default(""),
		field.String("external_message_id").Default(""),
		field.String("status").Default("pending"),
		field.Uint32("attempt_count").Default(0),
		field.Int64("next_retry_time_unix").Default(0),
		field.Int64("delivered_time_unix").Default(0),
		field.String("last_error").Default(""),
		field.String("request_id").Default(""),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (OutboundDelivery) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "status", "updated_unix"),
		index.Fields("message_id"),
		index.Fields("endpoint_id", "status"),
		index.Fields("status", "next_retry_time_unix"),
		index.Fields("request_id"),
	}
}
