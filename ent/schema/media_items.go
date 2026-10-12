package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

type MediaItem struct{ ent.Schema }

func (MediaItem) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("scope_key").MaxLen(120), field.String("type").MaxLen(20), field.String("name").MaxLen(120), field.String("version").MaxLen(120), field.String("sha256").MaxLen(64), field.Int64("size").Min(0), field.String("arch").MaxLen(8), field.String("os_scope").MaxLen(120).Default(""), field.String("hw_scope").MaxLen(120).Default(""), field.JSON("deps", map[string]any{}).Default(map[string]any{}), field.String("manifest_ref").MaxLen(200).Default(""), field.String("signature_ref").MaxLen(200).Default(""), field.Enum("state").Values("uploaded", "verified", "approved", "retired").Default("uploaded"), field.String("uploaded_by").MaxLen(32), field.String("approved_by").MaxLen(32).Default(""), field.Time("created_at").Default(time.Now).Immutable(), field.Time("updated_at").Default(time.Now)}
}
func (MediaItem) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope_key", "type", "name", "version").Unique(), index.Fields("state")}
}
