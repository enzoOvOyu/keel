package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func writeBOMMaterial(ctx context.Context, tx *sql.Tx, projectID, packageID string, m *bomMaterial, now time.Time) error {
	v := m.row.Values
	status := v["status"]
	if status == "" {
		status = "planned"
	}
	subType := v["sub_type"]
	if m.category == "port" && subType == "" {
		subType = "port:" + v["port_index"]
	}
	if m.category == "switch" && subType == "" {
		if err := tx.QueryRowContext(ctx, `SELECT m.name FROM device_model_versions v JOIN device_models m ON m.id=v.model_id WHERE v.id=$1`, v["model_ref"]).Scan(&subType); err != nil {
			return err
		}
	}
	positionJSON, err := json.Marshal(m.position)
	if err != nil {
		return err
	}
	macs := []string{}
	if raw := v["mac"]; raw != "" {
		for _, mac := range strings.Split(raw, ",") {
			mac = strings.TrimSpace(mac)
			if mac != "" {
				if normalized, ok := normalizeMAC(mac); ok {
					mac = normalized
				}
				macs = append(macs, mac)
			}
		}
	}
	macJSON, err := json.Marshal(macs)
	if err != nil {
		return err
	}
	expect := map[string]string{}
	if v["nos_intent"] != "" {
		expect["nos_intent"] = v["nos_intent"]
	}
	expectJSON, err := json.Marshal(expect)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bom_items(item_uid,category,sub_type,model_version_id,vendor,serial,position,parent_uid,endpoint_a_uid,endpoint_b_uid,port_index,port_name,port_role,rail,pod,role,mac_list,credential_batch_ref,expect_version,source_of_record,status,first_project_id,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$23)
 ON CONFLICT(item_uid) DO UPDATE SET sub_type=EXCLUDED.sub_type,model_version_id=EXCLUDED.model_version_id,vendor=EXCLUDED.vendor,serial=EXCLUDED.serial,position=EXCLUDED.position,parent_uid=EXCLUDED.parent_uid,endpoint_a_uid=EXCLUDED.endpoint_a_uid,endpoint_b_uid=EXCLUDED.endpoint_b_uid,port_index=EXCLUDED.port_index,port_name=EXCLUDED.port_name,port_role=EXCLUDED.port_role,rail=EXCLUDED.rail,pod=EXCLUDED.pod,role=EXCLUDED.role,mac_list=EXCLUDED.mac_list,credential_batch_ref=EXCLUDED.credential_batch_ref,expect_version=EXCLUDED.expect_version,source_of_record=EXCLUDED.source_of_record,status=CASE WHEN bom_items.status='planned' THEN EXCLUDED.status ELSE bom_items.status END,updated_at=EXCLUDED.updated_at`, m.uid, m.category, subType, nullBOMString(v["model_ref"]), v["vendor"], nullBOMString(m.serial), positionJSON, nullBOMString(m.parentUID), nullBOMString(m.endpointA), nullBOMString(m.endpointB), nullBOMString(v["port_index"]), nullBOMString(v["port_name"]), nullBOMString(v["port_role"]), nullBOMString(v["rail"]), nullBOMString(v["pod"]), nullBOMString(v["role"]), macJSON, nullBOMString(v["credential_batch_ref"]), expectJSON, m.source, status, projectID, now)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(map[string]any{"file": m.row.File, "values": v})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,$5,$6)`, packageID, m.uid, m.row.Line, projectID, snapshot, now)
	if err != nil {
		return err
	}
	switch m.category {
	case "server":
		_, err = tx.ExecContext(ctx, `INSERT INTO machines(id,serial_number,rack,unit_position,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5) ON CONFLICT(id) DO UPDATE SET rack=EXCLUDED.rack,unit_position=EXCLUDED.unit_position,updated_at=EXCLUDED.updated_at`, m.uid, m.serial, v["rack"], v["u"], now)
		if err != nil {
			return err
		}
		var modelID string
		err = tx.QueryRowContext(ctx, `SELECT model_id FROM device_model_versions WHERE id=$1`, v["model_ref"]).Scan(&modelID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,model_id,model_version_id,hostname,ip_reference,state,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'planned',1,$8,$8) ON CONFLICT(project_id,machine_id) DO UPDATE SET model_id=EXCLUDED.model_id,model_version_id=EXCLUDED.model_version_id,hostname=EXCLUDED.hostname,ip_reference=EXCLUDED.ip_reference,version=CASE WHEN project_assets.model_version_id IS DISTINCT FROM EXCLUDED.model_version_id OR project_assets.hostname IS DISTINCT FROM EXCLUDED.hostname OR project_assets.ip_reference IS DISTINCT FROM EXCLUDED.ip_reference THEN project_assets.version+1 ELSE project_assets.version END,updated_at=CASE WHEN project_assets.model_version_id IS DISTINCT FROM EXCLUDED.model_version_id OR project_assets.hostname IS DISTINCT FROM EXCLUDED.hostname OR project_assets.ip_reference IS DISTINCT FROM EXCLUDED.ip_reference THEN EXCLUDED.updated_at ELSE project_assets.updated_at END`, RandomID(), projectID, m.uid, modelID, v["model_ref"], v["hostname"], v["ip_hint"], now)
	case "switch":
		_, err = tx.ExecContext(ctx, `INSERT INTO switches(id,serial_number,hostname,model_version_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5) ON CONFLICT(id) DO UPDATE SET hostname=EXCLUDED.hostname,model_version_id=EXCLUDED.model_version_id,updated_at=EXCLUDED.updated_at`, m.uid, m.serial, v["hostname"], v["model_ref"], now)
	case "nic", "gpu", "psu", "bmc":
		_, err = tx.ExecContext(ctx, `INSERT INTO components(id,machine_id,category,serial_number,slot,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET machine_id=EXCLUDED.machine_id,category=EXCLUDED.category,serial_number=EXCLUDED.serial_number,slot=EXCLUDED.slot`, m.uid, m.machineUID, m.category, nullBOMString(m.serial), nullBOMString(v["slot"]), now)
	case "port":
		name := v["port_name"]
		if name == "" {
			name = m.parentUID[:8] + ":" + v["port_index"]
		}
		if len(name) > 64 {
			return apiError(409, "BOM_PORT_NAME_TOO_LONG", "Port name exceeds projection limit")
		}
		rail := 0
		if parsed, parseErr := strconv.Atoi(v["rail"]); parseErr == nil && parsed >= 0 && parsed <= 32767 {
			rail = parsed
		}
		var machineID, switchID any
		if m.machineUID != "" {
			machineID = m.machineUID
		} else {
			var owner string
			err = tx.QueryRowContext(ctx, `SELECT CASE WHEN category='switch' THEN item_uid ELSE parent_uid END FROM bom_items WHERE item_uid=$1`, m.parentUID).Scan(&owner)
			if err != nil {
				return err
			}
			switchID = owner
		}
		var mac any
		if len(macs) > 0 {
			mac = macs[0]
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO physical_ports(id,machine_id,switch_id,parent_uid,port_index,mac_address,port_name,rail,rail_label,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET machine_id=EXCLUDED.machine_id,switch_id=EXCLUDED.switch_id,parent_uid=EXCLUDED.parent_uid,port_index=EXCLUDED.port_index,mac_address=EXCLUDED.mac_address,port_name=EXCLUDED.port_name,rail=EXCLUDED.rail,rail_label=EXCLUDED.rail_label`, m.uid, machineID, switchID, m.parentUID, v["port_index"], mac, name, rail, nullBOMString(v["rail"]), now)
	}
	return err
}
