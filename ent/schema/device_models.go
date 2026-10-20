package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"time"
)

type DeviceModel struct{ ent.Schema }

func (DeviceModel) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("scope_key").MaxLen(120), field.String("name").MaxLen(120), field.Enum("kind").Values("rack", "server", "compute-tray", "switch", "nic", "gpu", "psu", "bmc", "optic_module"), field.Enum("state").Values("active", "retired").Default("active"), field.String("created_by").MaxLen(32), field.Time("created_at").Default(time.Now).Immutable(), field.Time("updated_at").Default(time.Now)}
}
func (DeviceModel) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope_key", "name").Unique()}
}

type DeviceModelVersion struct{ ent.Schema }

func (DeviceModelVersion) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("model_id").MaxLen(32), field.Int("version").Min(1), field.JSON("hw_spec", map[string]any{}).Default(map[string]any{}), field.JSON("expected_topology", map[string]any{}).Default(map[string]any{}), field.JSON("capability_matrix", map[string]any{}).Default(map[string]any{}), field.JSON("panel_layout", map[string]any{}).Default(map[string]any{}), field.JSON("bom_expect", map[string]any{}).Default(map[string]any{}), field.JSON("default_role_plan", map[string]any{}).Default(map[string]any{}), field.String("content_hash").MaxLen(64), field.Time("published_at").Default(time.Now).Immutable()}
}
func (DeviceModelVersion) Indexes() []ent.Index {
	return []ent.Index{index.Fields("model_id", "version").Unique()}
}

type DeviceModelSlot struct{ ent.Schema }

func (DeviceModelSlot) Fields() []ent.Field {
	return []ent.Field{field.String("id").MaxLen(32).Immutable(), field.String("model_version_id").MaxLen(32), field.Int("slot_no").Min(1), field.String("pcie_slot").MaxLen(64), field.String("expect_nic_model").MaxLen(120), field.String("panel_port").MaxLen(64), field.Enum("default_role").Values("management", "storage", "compute", "oob", "unused")}
}
func (DeviceModelSlot) Indexes() []ent.Index {
	return []ent.Index{index.Fields("model_version_id", "slot_no").Unique()}
}
