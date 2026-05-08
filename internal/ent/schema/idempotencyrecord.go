package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IdempotencyRecord struct {
	ent.Schema
}

func (IdempotencyRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("idem") }).Immutable(),
		field.String("scope").NotEmpty(),
		field.String("method").NotEmpty(),
		field.String("actor_id").Default(""),
		field.String("idempotency_key").NotEmpty(),
		field.String("request_hash").Default(""),
		field.String("response_type").Default(""),
		field.String("response_json").Default("{}"),
		field.String("resource_type").Default(""),
		field.String("resource_id").Default(""),
		field.String("status").Default("completed"),
		field.Int64("created_unix"),
		field.Int64("expires_unix"),
	}
}

func (IdempotencyRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope", "method", "actor_id", "idempotency_key").Unique(),
		index.Fields("expires_unix"),
		index.Fields("resource_type", "resource_id"),
	}
}
