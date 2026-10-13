package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Network role bindings (WP-3.2, FR-5.9). Configuration templates reference
// logical roles only — management/storage/compute/oob — never a NIC model or
// an interface name. A role binds to a model slot (the approved model
// declares one default role per slot) and the project may override the
// binding per slot, so a staged build (phase two without storage) or a NIC
// substitution is absorbed by an override instead of a template edit: the
// model definition and the template stay untouched. The resolved binding is
// written to PortModel (physical_ports.role) and the panel, the
// connectivity tests and the issue loop all read it from there.
//
// Validation follows the FR-5.9 rule: only roles that are enabled *and*
// required must be present, so disabling storage (or marking a slot unused)
// never raises a missing-binding error. The one exception is compute — a
// test tier may not be bypassed by switching the compute role off, so
// disabling it (or dropping its required flag) is refused.

type RoleSpec struct {
	Enabled  bool `json:"enabled"`
	Required bool `json:"required"`
}

type RolePlanInput struct {
	Roles         map[string]RoleSpec `json:"roles"`
	SlotOverrides map[string]string   `json:"slot_overrides"`
}

type RolePlan struct {
	ProjectID     string              `json:"project_id"`
	Roles         map[string]RoleSpec `json:"roles"`
	SlotOverrides map[string]string   `json:"slot_overrides"`
	Coverage      map[string]int      `json:"coverage"`
	Missing       []string            `json:"missing"`
	Warnings      []string            `json:"warnings"`
	UpdatedBy     string              `json:"updated_by,omitempty"`
	UpdatedAt     *time.Time          `json:"updated_at,omitempty"`
}

type RoleBinding struct {
	PortID         string `json:"port_id"`
	PortName       string `json:"port_name"`
	Rail           int    `json:"rail"`
	SlotNo         int    `json:"slot_no"`
	PcieSlot       string `json:"pcie_slot"`
	ExpectNicModel string `json:"expect_nic_model"`
	Role           string `json:"role"`
	DefaultRole    string `json:"default_role"`
	Overridden     bool   `json:"overridden"`
}

// logicalRoles are the roles a plan may configure; "unused" is a slot
// target, not a plan role.
var logicalRoles = []string{"management", "storage", "compute", "oob"}

func isLogicalRole(role string) bool {
	for _, name := range logicalRoles {
		if name == role {
			return true
		}
	}
	return false
}

func isSlotRole(role string) bool { return role == "unused" || isLogicalRole(role) }

// defaultRoleSpecs is the plan used when neither the model nor the project
// declares one: management and storage are expected present, compute is
// mandatory for the test tiers and oob is optional.
func defaultRoleSpecs() map[string]RoleSpec {
	return map[string]RoleSpec{
		"management": {Enabled: true, Required: true},
		"storage":    {Enabled: true, Required: true},
		"compute":    {Enabled: true, Required: true},
		"oob":        {Enabled: true, Required: false},
	}
}

func (s *Store) GetRolePlan(ctx context.Context, a *AuthSession, project *ProjectSummary) (*RolePlan, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadRolePlan(ctx, tx, project.ID)
	if e != nil {
		return nil, e
	}
	return plan, nil
}

// loadRolePlan reads the stored plan (falling back to the built-in
// defaults), overlays the model-free coverage counts and flags the roles
// that are enabled *and* required yet have no bound port.
func loadRolePlan(ctx context.Context, tx *sql.Tx, projectID string) (*RolePlan, error) {
	plan := &RolePlan{ProjectID: projectID, Roles: defaultRoleSpecs(), SlotOverrides: map[string]string{},
		Coverage: map[string]int{}, Missing: []string{}, Warnings: []string{}}
	var rolesJSON, overridesJSON []byte
	var updatedBy string
	var updatedAt time.Time
	e := tx.QueryRowContext(ctx, "SELECT roles,slot_overrides,updated_by,updated_at FROM project_role_plans WHERE project_id=$1", projectID).
		Scan(&rolesJSON, &overridesJSON, &updatedBy, &updatedAt)
	switch {
	case e == nil:
		if len(rolesJSON) > 0 {
			var saved map[string]RoleSpec
			if e = json.Unmarshal(rolesJSON, &saved); e == nil {
				for name, spec := range saved {
					plan.Roles[name] = spec
				}
			}
		}
		if len(overridesJSON) > 0 {
			var saved map[string]string
			if e = json.Unmarshal(overridesJSON, &saved); e == nil && saved != nil {
				plan.SlotOverrides = saved
			}
		}
		plan.UpdatedBy, plan.UpdatedAt = updatedBy, &updatedAt
	case errors.Is(e, sql.ErrNoRows):
		plan.Warnings = append(plan.Warnings, "role_plan_default")
	default:
		return nil, e
	}
	if e = collectRoleCoverage(ctx, tx, projectID, plan); e != nil {
		return nil, e
	}
	return plan, nil
}

func collectRoleCoverage(ctx context.Context, tx *sql.Tx, projectID string, plan *RolePlan) error {
	rows, e := tx.QueryContext(ctx, `SELECT p.role,count(*) FROM physical_ports p
		JOIN project_assets a ON a.machine_id=p.machine_id WHERE a.project_id=$1 GROUP BY p.role`, projectID)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		var count int
		if e = rows.Scan(&role, &count); e != nil {
			return e
		}
		plan.Coverage[role] = count
	}
	if e = rows.Err(); e != nil {
		return e
	}
	// Only enabled *and* required roles must be bound: a disabled role
	// (phase two storage) and unused slots are not missing bindings.
	for _, name := range logicalRoles {
		spec := plan.Roles[name]
		if spec.Enabled && spec.Required && plan.Coverage[name] == 0 {
			plan.Missing = append(plan.Missing, name)
		}
	}
	if plan.Missing == nil {
		plan.Missing = []string{}
	}
	return nil
}

// SaveRolePlan stores the project plan and re-resolves every port binding
// from the model slots plus the overrides (FR-5.9: the binding result is
// written to PortModel).
func (s *Store) SaveRolePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, input RolePlanInput, trace string) (*RolePlan, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// Authority first: an unauthorized caller learns nothing about the rules.
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	roles := defaultRoleSpecs()
	for name, spec := range input.Roles {
		if !isLogicalRole(name) {
			return nil, apiError(400, "VALIDATION_ERROR", "unknown network role "+name)
		}
		// The compute role carries the test tiers: it may be re-bound to
		// another slot but never disabled or made optional.
		if name == "compute" && (!spec.Enabled || !spec.Required) {
			return nil, apiError(409, "ROLE_POLICY", "the compute role must stay enabled and required: test tiers cannot be bypassed by disabling it")
		}
		roles[name] = spec
	}
	overrides := map[string]string{}
	for key, role := range input.SlotOverrides {
		if !isSlotRole(role) {
			return nil, apiError(400, "VALIDATION_ERROR", "slot "+key+" cannot be bound to "+role)
		}
		overrides[key] = role
	}
	rolesJSON, e := json.Marshal(roles)
	if e != nil {
		return nil, e
	}
	overridesJSON, e := json.Marshal(overrides)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `INSERT INTO project_role_plans(id,project_id,roles,slot_overrides,updated_by,updated_at)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(project_id) DO UPDATE SET roles=$3,slot_overrides=$4,updated_by=$5,updated_at=$6`,
		RandomID(), project.ID, rolesJSON, overridesJSON, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = applyRoleBindings(ctx, tx, project.ID, overrides); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "roleplan.save", project.ID, trace, map[string]any{"overrides": len(overrides)}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.GetRolePlan(ctx, a, project)
}

// ListRoleBindings returns the resolved per-port binding of one machine.
func (s *Store) ListRoleBindings(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string) ([]RoleBinding, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var bound bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project_assets WHERE project_id=$1 AND machine_id=$2)", project.ID, machineID).Scan(&bound); e != nil {
		return nil, e
	}
	if !bound {
		return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
	}
	slots, e := modelSlotsForMachine(ctx, tx, machineID)
	if e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,port_name,rail,role FROM physical_ports WHERE machine_id=$1 ORDER BY port_name", machineID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []RoleBinding{}
	for rows.Next() {
		var item RoleBinding
		if e = rows.Scan(&item.PortID, &item.PortName, &item.Rail, &item.Role); e != nil {
			return nil, e
		}
		item.SlotNo, item.PcieSlot, item.ExpectNicModel, item.DefaultRole = matchSlot(slots, item.PortName, len(out))
		if item.DefaultRole == "" {
			item.DefaultRole, item.Role = "unused", "unused"
		}
		item.Overridden = item.DefaultRole != item.Role
		out = append(out, item)
	}
	return out, rows.Err()
}

// matchSlot resolves the slot that owns a port: the model declares the panel
// port name, and ports without a declared slot stay unused (they are never
// guessed into a role).
func matchSlot(slots []ModelSlot, portName string, index int) (int, string, string, string) {
	for _, slot := range slots {
		if slot.PanelPort != "" && slot.PanelPort == portName {
			return slot.SlotNo, slot.PcieSlot, slot.ExpectNicModel, slot.DefaultRole
		}
	}
	if index >= 0 && index < len(slots) && slots[index].PanelPort == "" {
		slot := slots[index]
		return slot.SlotNo, slot.PcieSlot, slot.ExpectNicModel, slot.DefaultRole
	}
	return 0, "", "", ""
}

func modelSlotsForMachine(ctx context.Context, tx *sql.Tx, machineID string) ([]ModelSlot, error) {
	rows, e := tx.QueryContext(ctx, `SELECT s.slot_no,s.pcie_slot,s.expect_nic_model,s.panel_port,s.default_role
		FROM device_model_slots s
		WHERE s.model_version_id = (SELECT v.id FROM device_model_versions v
			JOIN project_assets a ON a.model_id = v.model_id
			WHERE a.machine_id=$1 ORDER BY v.version DESC LIMIT 1)
		ORDER BY s.slot_no`, machineID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ModelSlot{}
	for rows.Next() {
		var slot ModelSlot
		if e = rows.Scan(&slot.SlotNo, &slot.PcieSlot, &slot.ExpectNicModel, &slot.PanelPort, &slot.DefaultRole); e != nil {
			return nil, e
		}
		out = append(out, slot)
	}
	return out, rows.Err()
}

// applyRoleBindings re-resolves and writes physical_ports.role for every
// port of every project asset. Rows are consumed before any UPDATE is
// issued (PostgreSQL keeps one statement per connection).
func applyRoleBindings(ctx context.Context, tx *sql.Tx, projectID string, overrides map[string]string) error {
	rows, e := tx.QueryContext(ctx, `SELECT a.machine_id,p.id,p.port_name FROM project_assets a
		JOIN physical_ports p ON p.machine_id=a.machine_id
		WHERE a.project_id=$1 ORDER BY a.machine_id,p.port_name`, projectID)
	if e != nil {
		return e
	}
	type portRow struct {
		machineID string
		portID    string
		portName  string
	}
	ports := []portRow{}
	for rows.Next() {
		var row portRow
		if e = rows.Scan(&row.machineID, &row.portID, &row.portName); e != nil {
			rows.Close()
			return e
		}
		ports = append(ports, row)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	slotCache := map[string][]ModelSlot{}
	index := map[string]int{}
	for _, port := range ports {
		slots, ok := slotCache[port.machineID]
		if !ok {
			slots, e = modelSlotsForMachine(ctx, tx, port.machineID)
			if e != nil {
				return e
			}
			slotCache[port.machineID] = slots
		}
		slotNo, _, _, defaultRole := matchSlot(slots, port.portName, index[port.machineID])
		index[port.machineID]++
		role := defaultRole
		if slotNo > 0 {
			// A slot override wins over the model default; the key is the
			// slot number ("3") or, when the panel port is known, its name.
			if value, found := overrides[strconv.Itoa(slotNo)]; found {
				role = value
			} else if value, found := overrides[port.portName]; found {
				role = value
			}
		}
		if role == "" {
			role = "unused"
		}
		if _, e = tx.ExecContext(ctx, "UPDATE physical_ports SET role=$2 WHERE id=$1", port.portID, role); e != nil {
			return e
		}
	}
	return nil
}

// roleParamKey renders the parameter-catalog key of a role's network block,
// used by the renderers and reported in errors.
func roleParamKey(role string) string { return "net." + role }

func stringifyRoleSpec(spec RoleSpec) string {
	return strings.Join([]string{"enabled=" + strconv.FormatBool(spec.Enabled), "required=" + strconv.FormatBool(spec.Required)}, " ")
}
