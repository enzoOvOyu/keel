package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type Audit struct{ ent.Schema }

func (Audit) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("actor_id").MaxLen(32).Optional(), field.String("project_id").MaxLen(32).Optional(), field.String("action").MaxLen(64), field.String("target_id").Optional(), field.String("trace_id").MaxLen(64).Optional(), field.JSON("metadata", map[string]any{}).Optional(), field.Time("created_at").Default(time.Now).Immutable()}
}
