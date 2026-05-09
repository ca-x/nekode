package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChannelDecision is a governance record attached to a channel. It moves
// through proposed → ratified/rejected/retired. Votes live on the sibling
// ChannelDecisionVote schema.
type ChannelDecision struct {
	ent.Schema
}

func (ChannelDecision) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").DefaultFunc(func() string { return newID("dec") }).Immutable(),
		field.String("target").NotEmpty(),
		field.String("title").NotEmpty(),
		field.String("body").NotEmpty(),
		// Status values: proposed | ratified | rejected | retired.
		field.String("status").NotEmpty(),
		field.String("proposer_id").NotEmpty(),
		field.String("proposer_kind").NotEmpty(),
		field.Int64("created_unix"),
		field.Int64("ratified_unix").Default(0),
		field.Int64("retired_unix").Default(0),
		field.String("retired_by").Default(""),
		field.String("retire_reason").Default(""),
		field.String("supersedes_decision_id").Default(""),
		field.Uint32("approve_count").Default(0),
		field.Uint32("reject_count").Default(0),
		field.Uint32("abstain_count").Default(0),
	}
}

func (ChannelDecision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target", "status", "created_unix"),
		index.Fields("supersedes_decision_id"),
	}
}

// ChannelDecisionVote records one voter's stance on a decision. Primary
// key is (decision_id, voter_id); changing a vote rewrites the row.
type ChannelDecisionVote struct {
	ent.Schema
}

func (ChannelDecisionVote) Fields() []ent.Field {
	return []ent.Field{
		// Surrogate primary key keeps ent's UniqueIndex simple; (decision_id,
		// voter_id) is enforced via a unique index below.
		field.String("id").DefaultFunc(func() string { return newID("vot") }).Immutable(),
		field.String("decision_id").NotEmpty(),
		field.String("voter_id").NotEmpty(),
		field.String("voter_kind").NotEmpty(),
		// Vote values: approve | reject | abstain.
		field.String("decision").NotEmpty(),
		field.Int64("voted_unix"),
		field.String("reason").Default(""),
	}
}

func (ChannelDecisionVote) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("decision_id", "voter_id").Unique(),
		index.Fields("voter_id"),
	}
}
