package platform

import (
	"context"
	"database/sql"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"time"
)

// NP-3 consumes confirmed server/switch physical endpoint pairs. Network-side
// N-S5 observations and switch-only links cannot manufacture this assertion.
func (s *Store) wiringNP3Tx(ctx context.Context, tx *sql.Tx, project string, cables []fabricmon.Cable) (NetworkGate, *time.Time, error) {
	out := networkGate("NP-3", "PASS", "")
	type proof struct{ id, port, peer, cable string }
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT ON(p.physical_port_id) g.id,p.physical_port_id,p.switch_port_id,p.cable_id FROM validation_wiring_confirmed_ports p JOIN validation_wiring_confirmations g ON g.id=p.confirmation_id WHERE g.project_id=$1 ORDER BY p.physical_port_id,g.created_at DESC,g.id DESC`, project)
	if e != nil {
		return out, nil, e
	}
	byPort := map[string]proof{}
	for rows.Next() {
		var p proof
		if e = rows.Scan(&p.id, &p.port, &p.peer, &p.cable); e != nil {
			rows.Close()
			return out, nil, e
		}
		byPort[p.port] = p
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, nil, e
	}
	cache := map[string]*WiringConfirmation{}
	var until *time.Time
	required := 0
	for _, cable := range cables {
		if cable.A.Switch && cable.B.Switch {
			continue
		}
		required++
		host, peer := cable.A, cable.B
		if host.Switch {
			host, peer = peer, host
		}
		p, ok := byPort[host.UID]
		if !peer.Switch || !ok || p.peer != peer.UID || p.cable != cable.UID {
			mergeNetworkGate(&out, networkGate(cable.UID, "BLOCKED", "server_three_source_G2_evidence_required"))
			continue
		}
		g, seen := cache[p.id]
		if !seen {
			g, e = s.wiringConfirmationTx(ctx, tx, project, p.id)
			if e != nil {
				return out, nil, e
			}
			cache[p.id] = g
		}
		if g.EffectiveVerdict != "PASS" {
			mergeNetworkGate(&out, networkGate(cable.UID, "BLOCKED", g.InvalidationReason))
			continue
		}
		out.EvidenceRefs = append(out.EvidenceRefs, g.ID)
		deadline := g.ValidUntil
		if until == nil || deadline.Before(*until) {
			until = &deadline
		}
	}
	if required == 0 {
		out = networkGate("NP-3", "BLOCKED", "server_port_scope_empty")
	}
	return out, until, nil
}
