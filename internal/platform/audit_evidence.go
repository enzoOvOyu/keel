package platform

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/auditevidence"
)

// The versioned projection hashes every stored audit field. Only ID+hash leave
// this module; neither raw metadata nor operator/account fields are exported.
func auditEvidenceEntry(rows *sql.Rows) (auditevidence.Entry, error) {
	var record struct {
		ID              string  `json:"id"`
		Actor           *string `json:"actor_id"`
		Project         *string `json:"project_id"`
		Action          string  `json:"action"`
		Target          *string `json:"target_id"`
		Trace           *string `json:"trace_id"`
		Metadata        any     `json:"metadata"`
		MetadataPresent bool    `json:"metadata_present"`
		CreatedAt       string  `json:"created_at"`
	}
	var actor, project, target, trace sql.NullString
	var raw []byte
	var at time.Time
	if err := rows.Scan(&record.ID, &actor, &project, &record.Action, &target, &trace, &raw, &at); err != nil {
		return auditevidence.Entry{}, err
	}
	optional := func(value sql.NullString) *string {
		if !value.Valid {
			return nil
		}
		copy := value.String
		return &copy
	}
	record.Actor, record.Project, record.Target, record.Trace = optional(actor), optional(project), optional(target), optional(trace)
	if len(raw) > 1<<20 {
		return auditevidence.Entry{}, apiError(422, "AUDIT_EVIDENCE_RECORD_LIMIT", "Audit metadata exceeds the bounded evidence projection")
	}
	record.MetadataPresent = raw != nil
	if len(raw) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&record.Metadata); err != nil {
			return auditevidence.Entry{}, err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return auditevidence.Entry{}, apiError(422, "AUDIT_EVIDENCE_INVALID", "Invalid stored audit metadata")
		}
	}
	record.CreatedAt = at.UTC().Format(time.RFC3339Nano)
	canonical, err := json.Marshal(record)
	if err != nil {
		return auditevidence.Entry{}, err
	}
	digest := sha256.Sum256(append([]byte("Keel audit record v1\x00"), canonical...))
	return auditevidence.Entry{ID: record.ID, SHA256: hex.EncodeToString(digest[:])}, nil
}

const auditEvidenceQuery = `SELECT id,actor_id,project_id,action,target_id,trace_id,metadata,created_at FROM audits ORDER BY id COLLATE "C"`

func (s *Store) beginAuditEvidence(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	if err = requireDatabaseOwner(ctx, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// ExportAuditEvidence is an owner-only consistent snapshot, not a database
// mutation. External custody of the resulting signed index is essential.
func (s *Store) ExportAuditEvidence(ctx context.Context, signer *auditevidence.Signer, index io.Writer) (*auditevidence.Envelope, error) {
	tx, err := s.beginAuditEvidence(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, auditEvidenceQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	writer := auditevidence.NewIndexWriter(index)
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		entry, e := auditEvidenceEntry(rows)
		if e != nil {
			return nil, e
		}
		if err = writer.Add(entry); err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	seal, err := writer.Seal(signer, RandomID(), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return seal, nil
}

type AuditEvidenceDifference struct {
	ID    string `json:"id"`
	Issue string `json:"issue"`
}
type AuditEvidenceComparison struct {
	SnapshotID   string                    `json:"snapshot_id"`
	EvidenceRows int                       `json:"evidence_rows"`
	CurrentRows  int                       `json:"current_rows"`
	Matched      int                       `json:"matched"`
	Changed      int                       `json:"changed"`
	Missing      int                       `json:"missing"`
	Additional   int                       `json:"additional"`
	Unchanged    bool                      `json:"archived_rows_unchanged"`
	Differences  []AuditEvidenceDifference `json:"differences"`
}

// CompareAuditEvidence verifies independent evidence and merges it against one
// current DB snapshot. New unarchived rows are reported separately, never used
// to hide changed/deleted archived rows. No payload or raw metadata is returned.
func (s *Store) CompareAuditEvidence(ctx context.Context, index io.Reader, seal []byte, trusted ed25519.PublicKey) (*AuditEvidenceComparison, error) {
	tx, err := s.beginAuditEvidence(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, auditEvidenceQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &AuditEvidenceComparison{Differences: []AuditEvidenceDifference{}}
	var current auditevidence.Entry
	var hasCurrent bool
	advance := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		hasCurrent = rows.Next()
		if !hasCurrent {
			return rows.Err()
		}
		var e error
		current, e = auditEvidenceEntry(rows)
		if e != nil {
			return e
		}
		out.CurrentRows++
		if out.CurrentRows > auditevidence.MaxRows {
			return apiError(422, "AUDIT_EVIDENCE_LIMIT", "Current audit snapshot exceeds the bounded v1 comparison limit")
		}
		return nil
	}
	if err = advance(); err != nil {
		return nil, err
	}
	difference := func(id, issue string) {
		if len(out.Differences) < 20 {
			out.Differences = append(out.Differences, AuditEvidenceDifference{ID: id, Issue: issue})
		}
	}
	header, err := auditevidence.Verify(index, seal, trusted, func(entry auditevidence.Entry) error {
		for hasCurrent && current.ID < entry.ID {
			out.Additional++
			if e := advance(); e != nil {
				return e
			}
		}
		if !hasCurrent || current.ID > entry.ID {
			out.Missing++
			difference(entry.ID, "missing")
			return nil
		}
		if current.SHA256 != entry.SHA256 {
			out.Changed++
			difference(entry.ID, "changed")
		} else {
			out.Matched++
		}
		return advance()
	})
	if err != nil {
		return nil, err
	}
	for hasCurrent {
		out.Additional++
		if err = advance(); err != nil {
			return nil, err
		}
	}
	out.SnapshotID = header.SnapshotID
	out.EvidenceRows = header.Rows
	out.Unchanged = out.Changed == 0 && out.Missing == 0
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
