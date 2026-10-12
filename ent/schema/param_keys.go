package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

type ParamKey struct{ ent.Schema }

func (ParamKey) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("scope_key").MaxLen(120), field.String("key").MaxLen(120), field.String("value_type").MaxLen(10), field.JSON("options", []string{}).Default([]string{}), field.String("default_value").MaxLen(120).Default(""), field.String("source_media_type").MaxLen(20).Default(""), field.JSON("deps", map[string]any{}).Default(map[string]any{}), field.Enum("state").Values("active", "retired").Default("active"), field.String("created_by").MaxLen(32), field.Time("created_at").Default(time.Now).Immutable(), field.Time("updated_at").Default(time.Now)}
}
func (ParamKey) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope_key", "key").Unique()}
}
