package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Tunnel is one preview reverse-proxy binding. Rows are short-lived;
// expired rows stay in place so the Tunnels tab can surface recent
// history before the operator manually prunes.
type Tunnel struct {
	ent.Schema
}

func (Tunnel) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("tun") }).Immutable(),
		// Cryptographically-random URL-safe token embedded in the public
		// URL. Not a secret equivalent to a session cookie — guessing it
		// still lands on a state check — but it must be unpredictable.
		field.String("token").NotEmpty().Unique(),
		field.String("computer_id").NotEmpty(),
		field.String("daemon_id").Default(""),
		field.Uint32("local_port"),
		field.String("label").Default(""),
		// State: pending_approval | active | rejected | closed.
		field.String("state").NotEmpty(),
		// Access policy: private | members | public.
		field.String("access_policy").NotEmpty(),
		field.String("creator_id").NotEmpty(),
		field.String("creator_kind").NotEmpty(),
		field.Int64("created_unix"),
		field.Int64("expires_unix").Default(0),
		field.Int64("approved_unix").Default(0),
		field.String("approved_by").Default(""),
		field.Int64("closed_unix").Default(0),
		field.String("close_reason").Default(""),
	}
}

func (Tunnel) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("computer_id", "state", "created_unix"),
		index.Fields("state", "expires_unix"),
	}
}
