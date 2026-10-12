package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

type Membership struct{ ent.Schema }

func (Membership) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("user_id").MaxLen(32), field.String("project_id").MaxLen(32), field.Enum("role").Values("Admin", "DE", "DL", "NE", "SRE", "Viewer"), field.Time("created_at").Default(time.Now).Immutable()}
}
func (Membership) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "project_id").Unique()}
}
