package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type Session struct{ ent.Schema }

func (Session) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(64).Immutable(), field.String("user_id").MaxLen(32).Optional().Nillable(), field.String("csrf_hash").MaxLen(64).Sensitive(), field.Int("security_version").Default(0), field.Time("issued_at").Default(time.Now).Immutable(), field.Time("last_seen_at").Default(time.Now), field.Time("expires_at"), field.Time("revoked_at").Optional().Nillable()}
}
