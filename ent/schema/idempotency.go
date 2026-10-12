package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type Idempotency struct{ ent.Schema }

func (Idempotency) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(64).Immutable(), field.String("actor_id").MaxLen(32), field.String("request_hash").MaxLen(64), field.JSON("response", map[string]any{}), field.Time("created_at").Default(time.Now).Immutable()}
}
