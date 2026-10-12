package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type LoginThrottle struct{ ent.Schema }

func (LoginThrottle) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(64).Immutable(), field.Int("failures").Default(0), field.Time("window_started_at").Default(time.Now), field.Time("locked_until").Optional().Nillable()}
}
