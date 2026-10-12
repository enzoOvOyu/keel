package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type User struct{ ent.Schema }

func (User) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("username").MaxLen(128).Unique(), field.String("display_name").MaxLen(120), field.String("password_hash").Sensitive(), field.Bool("is_admin").Default(false), field.Bool("enabled").Default(true), field.Int("security_version").Default(1), field.Time("created_at").Default(time.Now).Immutable()}
}
