package provisionboot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
	"unicode"

	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

const WritePermitSchema = "keel.install-write-permit/v1"

// These fields bind exact prepared bytes, not just a caller's chosen disk name.
// A fresh signed report accompanies the request and is verified independently.
type WritePreparation struct {
	Nonce              string                       `json:"nonce"`
	StoragePlanSHA256  string                       `json:"storage_plan_sha256"`
	GPTIdentity        provisionstorage.GPTIdentity `json:"gpt_identity"`
	GPTSHA256          string                       `json:"gpt_sha256"`
	RootFSSHA256       string                       `json:"rootfs_sha256"`
	TargetUKISHA256    string                       `json:"target_uki_sha256"`
	RuntimeInputSHA256 string                       `json:"runtime_input_sha256"`
	FirstBootBinding   *provisionfirstboot.Binding  `json:"first_boot_binding,omitempty"`
}
type WritePermitSubmission struct {
	Preparation WritePreparation `json:"preparation"`
	Report      []byte           `json:"report"`
	Proof       BootProof        `json:"proof"`
}
type WritePermit struct {
	Schema                 string    `json:"schema"`
	ID                     string    `json:"id"`
	InstallationID         string    `json:"installation_id"`
	BootID                 string    `json:"boot_id"`
	Sequence               int64     `json:"sequence"`
	Nonce                  string    `json:"nonce"`
	StoragePlanSHA256      string    `json:"storage_plan_sha256"`
	GPTSHA256              string    `json:"gpt_sha256"`
	RootFSSHA256           string    `json:"rootfs_sha256"`
	TargetUKISHA256        string    `json:"target_uki_sha256"`
	RuntimeInputSHA256     string    `json:"runtime_input_sha256"`
	IssuedAt               time.Time `json:"issued_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	FirstBootBindingSHA256 string    `json:"first_boot_binding_sha256,omitempty"`
}
type WriteCheckQuery struct {
	PermitID string `json:"permit_id"`
	Nonce    string `json:"nonce"`
	Stage    string `json:"stage"`
}
type WriteCheckSubmission struct {
	Query WriteCheckQuery `json:"query"`
	Proof BootProof       `json:"proof"`
}

// Authorization is a short observation of current authority, not an enduring
// lease. Every operation must recheck, and an in-flight operation must stop if
// checks fail or ValidUntil passes. A lost response never grants more time.
type WriteAuthorization struct {
	PermitID          string    `json:"permit_id"`
	InstallationID    string    `json:"installation_id"`
	BootID            string    `json:"boot_id"`
	Sequence          int64     `json:"sequence"`
	Nonce             string    `json:"nonce"`
	Stage             string    `json:"stage"`
	StoragePlanSHA256 string    `json:"storage_plan_sha256"`
	GPTSHA256         string    `json:"gpt_sha256"`
	CheckedAt         time.Time `json:"checked_at"`
	ValidUntil        time.Time `json:"valid_until"`
}
type WriteReceipt struct {
	PermitID          string `json:"permit_id"`
	StoragePlanSHA256 string `json:"storage_plan_sha256"`
	GPTSHA256         string `json:"gpt_sha256"`
	RootFSSHA256      string `json:"rootfs_sha256"`
	TargetUKISHA256   string `json:"target_uki_sha256"`
	Outcome           string `json:"outcome"`
	ReadbackSHA256    string `json:"readback_sha256"`
	Reason            string `json:"reason"`
}
type WriteReceiptSubmission struct {
	Receipt WriteReceipt `json:"receipt"`
	Proof   BootProof    `json:"proof"`
}

// WriteReadback records which exact expected content the authenticated writer
// claims to have checked. The controller can compare this commitment, but cannot
// remotely observe disk bytes or infer first boot/SSH success from it.
type WriteReadback struct {
	GPTSHA256       string `json:"gpt_sha256"`
	RootFSSHA256    string `json:"rootfs_sha256"`
	TargetUKISHA256 string `json:"target_uki_sha256"`
	SSHHelperSHA256 string `json:"ssh_helper_sha256"`
	UserDataSHA256  string `json:"user_data_sha256"`
	MetaDataSHA256  string `json:"meta_data_sha256"`
}

func (r WriteReadback) SHA256() string {
	for _, hash := range []string{r.GPTSHA256, r.RootFSSHA256, r.TargetUKISHA256, r.SSHHelperSHA256, r.UserDataSHA256, r.MetaDataSHA256} {
		if !sha256ID.MatchString(hash) {
			return ""
		}
	}
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(append([]byte("Keel installation authenticated readback v1\x00"), raw...))
	return hex.EncodeToString(sum[:])
}

func WritePermitPayload(in WritePermitSubmission) ([]byte, error) {
	if !opaqueID.MatchString(in.Preparation.Nonce) || len(in.Report) == 0 || len(in.Report) > 2<<20 {
		return nil, ErrDenied
	}
	for _, h := range []string{in.Preparation.StoragePlanSHA256, in.Preparation.GPTSHA256, in.Preparation.RootFSSHA256, in.Preparation.TargetUKISHA256, in.Preparation.RuntimeInputSHA256} {
		if !sha256ID.MatchString(h) {
			return nil, ErrDenied
		}
	}
	p, err := json.Marshal(in.Preparation)
	if err != nil || len(p) > 128<<10 {
		return nil, ErrDenied
	}
	out := append([]byte("Keel installation write preparation v1\x00"), p...)
	out = append(out, 0)
	out = append(out, in.Report...)
	return out, nil
}

func ValidWriteStage(stage string) bool {
	switch stage {
	case "gpt.write", "efi.format", "root.format", "root.extract", "boot.install", "seed.install", "final.sync", "final.verify":
		return true
	}
	return false
}
func ValidateWritePermit(p *WritePermit, proof BootProof, preparation WritePreparation, now time.Time) error {
	if p == nil || p.Schema != WritePermitSchema || !opaqueID.MatchString(p.ID) || p.InstallationID != proof.InstallationID || p.BootID != proof.BootID || p.Sequence != proof.Sequence || !opaqueID.MatchString(preparation.Nonce) || p.Nonce != preparation.Nonce || p.StoragePlanSHA256 != preparation.StoragePlanSHA256 || p.GPTSHA256 != preparation.GPTSHA256 || p.RootFSSHA256 != preparation.RootFSSHA256 || p.TargetUKISHA256 != preparation.TargetUKISHA256 || p.RuntimeInputSHA256 != preparation.RuntimeInputSHA256 || p.IssuedAt.After(now.Add(time.Second)) || !p.ExpiresAt.After(now) || !p.ExpiresAt.After(p.IssuedAt) || p.ExpiresAt.Sub(p.IssuedAt) > 30*time.Minute {
		return ErrDenied
	}
	if preparation.FirstBootBinding != nil {
		if preparation.FirstBootBinding.Validate(now) != nil || p.FirstBootBindingSHA256 != preparation.FirstBootBinding.SHA256() {
			return ErrDenied
		}
	} else if p.FirstBootBindingSHA256 != "" {
		return ErrDenied
	}
	return nil
}
func ValidateWriteAuthorization(a *WriteAuthorization, permit *WritePermit, query WriteCheckQuery, now time.Time) error {
	if a == nil || permit == nil || !opaqueID.MatchString(query.Nonce) || query.PermitID != permit.ID || !ValidWriteStage(query.Stage) || a.PermitID != permit.ID || a.InstallationID != permit.InstallationID || a.BootID != permit.BootID || a.Sequence != permit.Sequence || a.Nonce != query.Nonce || a.Stage != query.Stage || a.StoragePlanSHA256 != permit.StoragePlanSHA256 || a.GPTSHA256 != permit.GPTSHA256 || a.CheckedAt.After(now.Add(time.Second)) || a.CheckedAt.Before(now.Add(-2*time.Second)) || !a.ValidUntil.After(now) || a.ValidUntil.After(a.CheckedAt.Add(2*time.Second)) || a.ValidUntil.After(permit.ExpiresAt) {
		return ErrDenied
	}
	return nil
}
func ValidateWriteReceipt(r WriteReceipt, permit *WritePermit) error {
	if permit == nil || r.PermitID != permit.ID || r.StoragePlanSHA256 != permit.StoragePlanSHA256 || r.GPTSHA256 != permit.GPTSHA256 || r.RootFSSHA256 != permit.RootFSSHA256 || r.TargetUKISHA256 != permit.TargetUKISHA256 || len(r.Reason) < 1 || len(r.Reason) > 120 {
		return ErrDenied
	}
	for _, c := range r.Reason {
		if unicode.IsControl(c) {
			return ErrDenied
		}
	}
	switch r.Outcome {
	case "written":
		if !sha256ID.MatchString(r.ReadbackSHA256) {
			return ErrDenied
		}
	case "unknown":
		if r.ReadbackSHA256 != "" {
			return ErrDenied
		}
	default:
		return ErrDenied
	}
	return nil
}
func WriteReceiptSHA256(r WriteReceipt) string {
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
