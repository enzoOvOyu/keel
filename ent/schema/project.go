package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"time"
)

type Project struct{ ent.Schema }

func (Project) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("name").MaxLen(120), field.String("slug").MaxLen(64).Unique(), field.String("description").Default(""), field.Enum("state").Values("planning", "deploying", "verifying", "delivered", "archived").Default("planning"), field.String("created_by").MaxLen(32), field.Int("version").Default(1), field.Time("created_at").Default(time.Now).Immutable(), field.Time("updated_at").Default(time.Now)}
}
