package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type InteractionEndpoint struct {
	ent.Schema
}

func (InteractionEndpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("iep") }).Immutable(),
		field.String("kind").NotEmpty(),
		field.String("provider").NotEmpty(),
		field.String("display_name").NotEmpty(),
		field.String("target_prefix").Default("#"),
		field.Bool("inbound_enabled").Default(true),
		field.Bool("outbound_enabled").Default(true),
		field.String("auth_mode").Default("bearer"),
		field.String("config_json").Default("{}"),
		field.Int64("created_unix"),
		field.Int64("updated_unix"),
	}
}

func (InteractionEndpoint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind"),
		index.Fields("provider"),
	}
}
