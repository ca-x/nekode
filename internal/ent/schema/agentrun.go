package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AgentRun is a single agent session. Events are stored on AgentRunEvent.
// The row is created on the first START event and finalized on END.
type AgentRun struct {
	ent.Schema
}

func (AgentRun) Fields() []ent.Field {
	return []ent.Field{
		// run_id is generated on the daemon so both sides agree on the ULID
		// before the server has persisted anything.
		field.String("id").NotEmpty().Immutable(),
		field.String("agent_id").NotEmpty(),
		field.String("computer_id").NotEmpty(),
		field.Int64("started_unix"),
		field.Int64("ended_unix").Default(0),
		field.Int32("exit_code").Default(0),
		field.String("summary").Default(""),
		field.String("error").Default(""),
		field.Uint32("event_count").Default(0),
	}
}

func (AgentRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("agent_id", "started_unix"),
		index.Fields("computer_id", "started_unix"),
	}
}

// AgentRunEvent is one lifecycle event inside an agent run. Ordinals on
// the sqlite schema come from a native AUTOINCREMENT column; ent keeps
// its id as a string surrogate so migrations stay consistent with the
// rest of the project.
type AgentRunEvent struct {
	ent.Schema
}

func (AgentRunEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("evt") }).Immutable(),
		field.String("run_id").NotEmpty(),
		field.Int64("at_unix_nano"),
		// Phase values mirror AgentRunPhase in proto:
		// start | tool_call | tool_result | error | output | end.
		field.String("phase").NotEmpty(),
		field.String("summary").Default(""),
		// Payload carries runtime-specific extras. Stored as JSON text for
		// easy FTS5 indexing without growing the schema per runtime.
		field.String("payload_json").Default("{}"),
		field.Int32("exit_code").Default(0),
		field.String("error_message").Default(""),
	}
}

func (AgentRunEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id", "at_unix_nano"),
	}
}
