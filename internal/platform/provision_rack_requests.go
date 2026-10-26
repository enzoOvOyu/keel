package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxProvisionRackRequests = 50

// Each item selects an existing asset, its current version and exact inputs.
// This endpoint always means reinstall intent and requires its full BOM serial.
type ProvisionRackRequestItem struct {
	AssetID string `json:"asset_id"`
	ProvisionPreviewInput
	ConfirmSerial string `json:"confirm_serial"`
}

type CreateProvisionRackRequestsInput struct {
	Rack   string                     `json:"rack"`
	Reason string                     `json:"reason"`
	Items  []ProvisionRackRequestItem `json:"items"`
}

type ProvisionRackRequests struct {
	BatchID       string             `json:"batch_id"`
	ProjectID     string             `json:"project_id"`
	Rack          string             `json:"rack"`
	RequestSHA256 string             `json:"request_sha256"`
	CreatedAt     time.Time          `json:"created_at"`
	Requests      []ProvisionRequest `json:"requests"`
}

func canonicalProvisionRackInput(in CreateProvisionRackRequestsInput) (CreateProvisionRackRequestsInput, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !utf8.ValidString(in.Rack) || strings.TrimSpace(in.Rack) == "" || utf8.RuneCountInString(in.Rack) > 120 || !utf8.ValidString(in.Reason) || in.Reason == "" || utf8.RuneCountInString(in.Reason) > 500 || len(in.Items) < 1 || len(in.Items) > MaxProvisionRackRequests {
		return in, apiError(400, "VALIDATION_ERROR", "An exact BOM rack, bounded reason and 1-50 explicit assets are required")
	}
	for _, r := range in.Rack {
		if unicode.IsControl(r) {
			return in, apiError(400, "VALIDATION_ERROR", "Invalid rack value")
		}
	}
	// Do not normalize rack case/whitespace or serial: those must match the
	// current BOM exactly. Clone before sorting; never mutate caller inputs.
	in.Items = append([]ProvisionRackRequestItem(nil), in.Items...)
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].AssetID < in.Items[j].AssetID })
	for i, item := range in.Items {
		if !modelIDPattern.MatchString(item.AssetID) || item.AssetVersion < 1 || !modelIDPattern.MatchString(item.BootPortID) || !modelIDPattern.MatchString(item.MediaID) || !modelIDPattern.MatchString(item.DomainKeyID) || !snPattern.MatchString(item.ConfirmSerial) {
			return in, apiError(400, "VALIDATION_ERROR", "Each selected asset requires an exact version, full serial, boot port, OS media and domain key")
		}
		if i > 0 && item.AssetID == in.Items[i-1].AssetID {
			return in, apiError(400, "PROVISION_RACK_DUPLICATE", "A rack request cannot select the same asset twice")
		}
	}
	return in, nil
}

// CreateProvisionRackRequests atomically creates ordinary unapproved requests
// for an explicit selection from one current BOM rack. It never approves,
// schedules, starts an installation, changes asset/trust state or chooses disks.
func (s *Store) CreateProvisionRackRequests(ctx context.Context, a *AuthSession, project *ProjectSummary, in CreateProvisionRackRequestsInput, key, trace string) (out *ProvisionRackRequests, resultErr error) {
	if project == nil {
		return nil, ErrNotFound
	}
	in, err := canonicalProvisionRackInput(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	if err = requireProvisionAuthority(ctx, tx, a, project.ID, false); err != nil {
		return nil, err
	}
	idem, hash, prior, err := provisionRackMutationTx(ctx, tx, a, project.ID, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, nil
	}
	// Pin each selected asset and physical machine before consulting its rack.
	// The same machine advisory locks used by individual requests are acquired
	// in sorted physical identity order, independent of client selection order.
	machines := make([]string, 0, len(in.Items))
	seen := make(map[string]bool, len(in.Items))
	for _, item := range in.Items {
		target, e := provisionTargetTx(ctx, tx, a, project, item.AssetID)
		if e != nil {
			return nil, e
		}
		if target.AssetVersion != item.AssetVersion {
			return nil, errVersion
		}
		if seen[target.MachineID] {
			return nil, apiError(400, "PROVISION_RACK_DUPLICATE", "A physical machine may only occur once in a rack request")
		}
		seen[target.MachineID] = true
		var rack string
		if err = tx.QueryRowContext(ctx, "SELECT rack FROM machines WHERE id=$1 FOR SHARE", target.MachineID).Scan(&rack); err != nil {
			return nil, err
		}
		if rack != in.Rack {
			return nil, apiError(409, "PROVISION_RACK_CHANGED", "Every selected asset must currently belong to the exact requested BOM rack")
		}
		if item.ConfirmSerial != target.Serial {
			return nil, apiError(422, "PROVISION_SERIAL_CONFIRM_REQUIRED", "Confirm each selected machine's complete current BOM serial")
		}
		machines = append(machines, target.MachineID)
	}
	sort.Strings(machines)
	for _, machine := range machines {
		if err = LockProvisionMachineTx(ctx, tx, machine); err != nil {
			return nil, err
		}
	}
	out = &ProvisionRackRequests{BatchID: RandomID(), ProjectID: project.ID, Rack: in.Rack, RequestSHA256: hash, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Requests: make([]ProvisionRequest, 0, len(in.Items))}
	requestIDs := make([]string, 0, len(in.Items))
	for _, item := range in.Items {
		request, e := s.createProvisionRequestTx(ctx, tx, a, project, item.AssetID, CreateProvisionRequestInput{ProvisionPreviewInput: item.ProvisionPreviewInput, Reason: in.Reason, Reinstall: true, ConfirmSerial: item.ConfirmSerial}, trace)
		if e != nil {
			return nil, e
		}
		out.Requests = append(out.Requests, *request)
		requestIDs = append(requestIDs, request.ID)
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "provision.rack_requests.created", out.BatchID, trace, map[string]any{"rack": out.Rack, "request_sha256": out.RequestSHA256, "request_ids": requestIDs, "count": len(requestIDs), "reinstall": true}); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,now())", idem, a.User.ID, hash, raw); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func provisionRackMutationTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, key string, in CreateProvisionRackRequestsInput) (string, string, *ProvisionRackRequests, error) {
	if len(key) < 1 || len(key) > 128 {
		return "", "", nil, apiError(400, "VALIDATION_ERROR", "Idempotency-Key required")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", "", nil, apiError(400, "VALIDATION_ERROR", "Invalid idempotency key")
		}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return "", "", nil, err
	}
	hash := digest(string(raw))
	// One actor/project key cannot silently change racks or any selected input.
	id := digest("provision-rack:" + project + ":" + a.User.ID + ":" + key)
	if err = lockKey(ctx, tx, id); err != nil {
		return "", "", nil, err
	}
	var priorHash string
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT request_hash,response FROM idempotencies WHERE id=$1", id).Scan(&priorHash, &prior)
	if errors.Is(err, sql.ErrNoRows) {
		return id, hash, nil, nil
	}
	if err != nil {
		return "", "", nil, err
	}
	if priorHash != hash {
		return "", "", nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Key belongs to a different rack request or selection")
	}
	var out ProvisionRackRequests
	if err = json.Unmarshal(prior, &out); err != nil {
		return "", "", nil, err
	}
	return id, hash, &out, nil
}
