package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

var provisionOrigin = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

type ProvisionBootScope struct {
	ID                  string              `json:"id"`
	ProjectID           string              `json:"project_id"`
	Scope               provisionboot.Scope `json:"scope"`
	ScopeSHA256         string              `json:"scope_sha256"`
	SourceRef           string              `json:"source_ref"`
	CreatedBy           string              `json:"created_by"`
	CreatedAt           time.Time           `json:"created_at"`
	State               string              `json:"state"`
	StateVersion        int64               `json:"state_version"`
	ApprovedBy          *string             `json:"approved_by,omitempty"`
	ApprovedAt          *time.Time          `json:"approved_at,omitempty"`
	CustomerApprovalRef *string             `json:"customer_approval_ref,omitempty"`
	NetworkApprovalRef  *string             `json:"network_approval_ref,omitempty"`
	RevokedBy           *string             `json:"revoked_by,omitempty"`
	RevokedAt           *time.Time          `json:"revoked_at,omitempty"`
	RevocationReason    *string             `json:"revocation_reason,omitempty"`
}
type CreateProvisionBootScopeInput struct {
	Interface   string   `json:"interface"`
	ServerIPv4  string   `json:"server_ipv4"`
	ClientCIDR  string   `json:"client_cidr"`
	Origin      string   `json:"origin"`
	KeaNetworks []string `json:"kea_networks"`
	SourceRef   string   `json:"source_ref"`
}
type ProvisionBootScopeActionInput struct {
	ExpectedVersion     int64  `json:"expected_version"`
	CustomerApprovalRef string `json:"customer_approval_ref,omitempty"`
	NetworkApprovalRef  string `json:"network_approval_ref,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

const provisionScopeColumns = `id,project_id,scope,scope_sha256,source_ref,created_by,created_at,state,state_version,approved_by,approved_at,customer_approval_ref,network_approval_ref,revoked_by,revoked_at,revocation_reason`

func scanProvisionScope(row interface{ Scan(...any) error }) (*ProvisionBootScope, error) {
	x := &ProvisionBootScope{}
	var raw []byte
	err := row.Scan(&x.ID, &x.ProjectID, &raw, &x.ScopeSHA256, &x.SourceRef, &x.CreatedBy, &x.CreatedAt, &x.State, &x.StateVersion, &x.ApprovedBy, &x.ApprovedAt, &x.CustomerApprovalRef, &x.NetworkApprovalRef, &x.RevokedBy, &x.RevokedAt, &x.RevocationReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &x.Scope) != nil || validateProvisionScope(x.Scope) != nil || x.Scope.ID != x.ID || x.Scope.ProjectID != x.ProjectID {
		return nil, apiError(409, "PROVISION_SCOPE_INVALID", "Stored boot scope is invalid")
	}
	encoded, _ := json.Marshal(x.Scope)
	if digest(string(encoded)) != x.ScopeSHA256 {
		return nil, apiError(409, "PROVISION_SCOPE_INVALID", "Stored boot scope digest differs")
	}
	return x, nil
}
func validateProvisionScope(scope provisionboot.Scope) error {
	if scope.Validate() != nil || scope.Development || !provisionOrigin.MatchString(scope.Origin) {
		return apiError(400, "PROVISION_SCOPE_INVALID", "Explicit production interface, IPv4 subnet and HTTPS origin required")
	}
	u, err := url.Parse(scope.Origin)
	if err != nil {
		return ErrForbidden
	}
	if port := u.Port(); port != "" {
		n, err := net.LookupPort("tcp", port)
		if err != nil || n < 1 || n > 65535 {
			return apiError(400, "PROVISION_SCOPE_INVALID", "HTTPS port must be 1..65535")
		}
	}
	return nil
}
func requireProvisionScopeAuthority(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, allowClosed bool) error {
	if err := requireProjectMember(ctx, tx, a, project); err != nil {
		return err
	}
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state FROM projects WHERE id=$1 FOR SHARE", project).Scan(&state); err != nil {
		return err
	}
	if !allowClosed && (state == "archived" || state == "delivered") {
		return apiError(409, "PROVISION_PROJECT_CLOSED", "Boot scope requires an open project")
	}
	if a.User.IsAdmin {
		return nil
	}
	var role string
	if err := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE", project, a.User.ID).Scan(&role); err != nil {
		return err
	}
	if role != "Admin" && role != "NDE" {
		return ErrForbidden
	}
	return nil
}

// The supplied external pool inventory requires independent network-team
// approval. Current platform-owned approved management pools are additionally
// re-read at every approval/authorization, never trusted from a request file.
func provisionKeaNetworksTx(ctx context.Context, tx *sql.Tx) ([]netip.Prefix, error) {
	rows, err := tx.QueryContext(ctx, `SELECT engineering_limits->'management'->>'cidr' FROM network_baseline_versions WHERE state='approved' AND COALESCE(engineering_limits->'management','null'::jsonb)<>'null'::jsonb ORDER BY id FOR SHARE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var networks []netip.Prefix
	for rows.Next() {
		var raw sql.NullString
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if !raw.Valid {
			return nil, apiError(409, "PROVISION_NETWORK_UNKNOWN", "An approved management pool is not readable")
		}
		p, e := netip.ParsePrefix(raw.String)
		if e != nil || !p.Addr().Is4() || p != p.Masked() {
			return nil, apiError(409, "PROVISION_NETWORK_UNKNOWN", "An approved management pool is invalid")
		}
		if !seen[p.String()] {
			seen[p.String()] = true
			networks = append(networks, p)
		}
		if len(networks) > 256 {
			return nil, apiError(409, "PROVISION_NETWORK_LIMIT", "Approved pool inventory exceeds supported scope bounds")
		}
	}
	return networks, rows.Err()
}

func provisionScopeAvailableTx(ctx context.Context, tx *sql.Tx, scope provisionboot.Scope) error {
	known, err := provisionKeaNetworksTx(ctx, tx)
	if err != nil {
		return err
	}
	for _, p := range known {
		if p.Overlaps(scope.Clients) {
			return apiError(409, "PROVISION_NETWORK_CONFLICT", "Boot scope overlaps an approved network-team management pool")
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,scope->>'clients' FROM provision_boot_scopes WHERE state='approved' AND id<>$1 ORDER BY id FOR SHARE`, scope.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return err
		}
		p, e := netip.ParsePrefix(raw)
		if e != nil || !p.IsValid() || !p.Addr().Is4() || p != p.Masked() {
			return apiError(409, "PROVISION_NETWORK_UNKNOWN", "Existing boot domain is invalid")
		}
		if p.Overlaps(scope.Clients) {
			return apiError(409, "PROVISION_NETWORK_CONFLICT", "Boot scope overlaps another approved server boot domain")
		}
	}
	return rows.Err()
}

func (s *Store) CreateProvisionBootScope(ctx context.Context, a *AuthSession, project string, in CreateProvisionBootScopeInput, trace string) (*ProvisionBootScope, error) {
	if !validNetworkEvidenceRef(in.SourceRef) || in.KeaNetworks == nil || len(in.KeaNetworks) > 256 {
		return nil, apiError(400, "PROVISION_SCOPE_INVALID", "Bounded network source reference and explicit pool inventory required")
	}
	scope := provisionboot.Scope{ID: RandomID(), ProjectID: project, Interface: in.Interface, Origin: in.Origin, KeaNetworks: []netip.Prefix{}}
	var err error
	scope.Server, err = netip.ParseAddr(in.ServerIPv4)
	if err != nil {
		return nil, apiError(400, "PROVISION_SCOPE_INVALID", "Canonical server IPv4 required")
	}
	scope.Clients, err = netip.ParsePrefix(in.ClientCIDR)
	if err != nil {
		return nil, apiError(400, "PROVISION_SCOPE_INVALID", "Canonical client CIDR required")
	}
	seen := map[string]bool{}
	for _, raw := range in.KeaNetworks {
		p, e := netip.ParsePrefix(raw)
		if e != nil || p.String() != raw || seen[raw] {
			return nil, apiError(400, "PROVISION_SCOPE_INVALID", "Unique canonical external Kea networks required")
		}
		seen[raw] = true
		scope.KeaNetworks = append(scope.KeaNetworks, p)
	}
	sort.Slice(scope.KeaNetworks, func(i, j int) bool { return scope.KeaNetworks[i].String() < scope.KeaNetworks[j].String() })
	if scope.Server.String() != in.ServerIPv4 || scope.Clients.String() != in.ClientCIDR {
		return nil, apiError(400, "PROVISION_SCOPE_INVALID", "Canonical server IPv4 and CIDR required")
	}
	if err = validateProvisionScope(scope); err != nil {
		return nil, err
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionScopeAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "provision.network-ownership"); err != nil {
		return nil, err
	}
	if err = provisionScopeAvailableTx(ctx, tx, scope); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(scope)
	now := time.Now().UTC().Truncate(time.Microsecond)
	x := &ProvisionBootScope{ID: scope.ID, ProjectID: project, Scope: scope, ScopeSHA256: digest(string(raw)), SourceRef: in.SourceRef, CreatedBy: a.User.ID, CreatedAt: now, State: "requested", StateVersion: 1}
	if _, err = tx.ExecContext(ctx, `INSERT INTO provision_boot_scopes(id,project_id,scope,scope_sha256,source_ref,created_by,created_at)VALUES($1,$2,$3,$4,$5,$6,$7)`, x.ID, project, raw, x.ScopeSHA256, x.SourceRef, a.User.ID, now); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.scope.created", x.ID, trace, map[string]any{"scope_sha256": x.ScopeSHA256, "source_ref": x.SourceRef}); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

func (s *Store) ChangeProvisionBootScope(ctx context.Context, a *AuthSession, project, id, action string, in ProvisionBootScopeActionInput, trace string) (*ProvisionBootScope, error) {
	if !modelIDPattern.MatchString(id) || in.ExpectedVersion < 1 || (action != "approve" && action != "revoke") || (action == "approve" && (!validNetworkEvidenceRef(in.CustomerApprovalRef) || !validNetworkEvidenceRef(in.NetworkApprovalRef))) || (action == "revoke" && (strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 500 || strings.ContainsAny(in.Reason, "\x00\r\n"))) {
		return nil, apiError(400, "PROVISION_SCOPE_ACTION_INVALID", "Exact version and approval references or revocation reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionScopeAuthority(ctx, tx, a, project, action == "revoke"); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "provision.network-ownership"); err != nil {
		return nil, err
	}
	x, err := scanProvisionScope(tx.QueryRowContext(ctx, "SELECT "+provisionScopeColumns+" FROM provision_boot_scopes WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
	if err != nil {
		return nil, err
	}
	if x.StateVersion != in.ExpectedVersion || x.State == "revoked" {
		return nil, errVersion
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if action == "approve" {
		if x.State != "requested" || x.CreatedBy == a.User.ID {
			return nil, apiError(409, "PROVISION_INDEPENDENT_APPROVAL_REQUIRED", "Another authorized network reviewer must approve the unconsumed scope")
		}
		if err = provisionScopeAvailableTx(ctx, tx, x.Scope); err != nil {
			return nil, err
		}
		x.State = "approved"
		x.ApprovedBy = &a.User.ID
		x.ApprovedAt = &now
		x.CustomerApprovalRef = &in.CustomerApprovalRef
		x.NetworkApprovalRef = &in.NetworkApprovalRef
	} else {
		x.State = "revoked"
		x.RevokedBy = &a.User.ID
		x.RevokedAt = &now
		x.RevocationReason = &in.Reason
	}
	x.StateVersion++
	if _, err = tx.ExecContext(ctx, `UPDATE provision_boot_scopes SET state=$3,state_version=$4,approved_by=$5,approved_at=$6,customer_approval_ref=$7,network_approval_ref=$8,revoked_by=$9,revoked_at=$10,revocation_reason=$11 WHERE id=$1 AND project_id=$2`, id, project, x.State, x.StateVersion, x.ApprovedBy, x.ApprovedAt, x.CustomerApprovalRef, x.NetworkApprovalRef, x.RevokedBy, x.RevokedAt, x.RevocationReason); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.scope."+action, id, trace, map[string]any{"scope_sha256": x.ScopeSHA256, "state_version": x.StateVersion, "reason": in.Reason}); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

func (s *Store) ListProvisionBootScopes(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[ProvisionBootScope], error) {
	out := NewPage[ProvisionBootScope](p)
	if err := p.Validate(); err != nil {
		return out, err
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM provision_boot_scopes WHERE project_id=$1", project).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+provisionScopeColumns+" FROM provision_boot_scopes WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3", project, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x, e := scanProvisionScope(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *x)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// LoadApprovedProvisionScopeTx is for current live authorization, not a cached
// file. It pins the approved scope row; overlap-changing approval mutations use
// the ownership mutex and must reject this still-approved domain themselves.
// Normal delivery checks must not serialize every device on that mutation lock.
func LoadApprovedProvisionScopeTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionBootScope, error) {
	x, err := scanProvisionScope(tx.QueryRowContext(ctx, "SELECT "+provisionScopeColumns+" FROM provision_boot_scopes WHERE id=$1 AND project_id=$2 FOR SHARE", id, project))
	if err != nil {
		return nil, err
	}
	if x.State != "approved" || x.ApprovedBy == nil {
		return nil, apiError(409, "PROVISION_SCOPE_NOT_APPROVED", "Current independently approved boot scope required")
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN projects p ON p.id=$2 WHERE u.id=$1 AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=p.id AND m.user_id=u.id AND m.role IN('Admin','NDE'))))`, *x.ApprovedBy, project).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, apiError(409, "PROVISION_SCOPE_APPROVAL_REVOKED", "Network approver no longer has authority")
	}
	if err = provisionScopeAvailableTx(ctx, tx, x.Scope); err != nil {
		return nil, err
	}
	return x, nil
}

func (s *Store) ReadProvisionBootScope(ctx context.Context, a *AuthSession, project, id string) (*ProvisionBootScope, error) {
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project); err != nil {
		return nil, err
	}
	x, err := scanProvisionScope(tx.QueryRowContext(ctx, "SELECT "+provisionScopeColumns+" FROM provision_boot_scopes WHERE id=$1 AND project_id=$2", id, project))
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

// A later network-team baseline approval must also respect existing approved
// server domains. The same transaction lock serializes both approval paths.
func rejectProvisionScopeOverlapTx(ctx context.Context, tx *sql.Tx, cidr string) error {
	if err := lockKey(ctx, tx, "provision.network-ownership"); err != nil {
		return err
	}
	pool, err := netip.ParsePrefix(cidr)
	if err != nil {
		return apiError(400, "PROVISION_NETWORK_UNKNOWN", "Canonical management pool required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT scope->>'clients' FROM provision_boot_scopes WHERE state='approved' ORDER BY id FOR SHARE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		p, e := netip.ParsePrefix(raw)
		if e != nil || p != p.Masked() || !p.Addr().Is4() {
			return apiError(409, "PROVISION_NETWORK_UNKNOWN", "Approved server domain is invalid")
		}
		if pool.Overlaps(p) {
			return apiError(409, "PROVISION_NETWORK_CONFLICT", "Management pool overlaps an approved server boot domain")
		}
	}
	return rows.Err()
}
