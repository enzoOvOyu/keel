// Package provisionfirstboot authenticates one installed-system observation.
// It cannot establish SSH trust or infer successful boot from DHCP identity.
package provisionfirstboot

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/crypto/ssh"
)

const BindingSchema = "keel.first-boot-binding/v1"
const ReportSchema = "keel.first-boot-report/v1"
const BindingPath = "/var/lib/keel/firstboot/binding.json"
const CAPath = "/var/lib/keel/firstboot/boot-ca.pem"
const StateDirectory = "/var/lib/keel/firstboot"
const ExecutablePath = "usr/local/libexec/keel-firstboot"
const UnitPath = "etc/systemd/system/keel-firstboot.service"
const UnitEnablePath = "etc/systemd/system/multi-user.target.wants/keel-firstboot.service"
const UnitEnableTarget = "../keel-firstboot.service"
const Unit = "[Unit]\nDescription=Keel authenticated first boot observation\nRequires=cloud-final.service\nAfter=cloud-final.service network-online.target\nWants=network-online.target\nConditionPathExists=/var/lib/keel/firstboot/binding.json\n\n[Service]\nType=oneshot\nUser=root\nExecStart=/usr/local/libexec/keel-firstboot --once\nRestart=no\nRemainAfterExit=yes\nTimeoutStartSec=90s\nUMask=0077\n\n[Install]\nWantedBy=multi-user.target\n"
const signatureDomain = "Keel provision first boot v1\x00"

var ErrDenied = errors.New("authenticated first boot unavailable")
var id = regexp.MustCompile(`^[a-f0-9]{32}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var hostname = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var bootID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

// Binding is public, frozen before a permit. Its canonical digest and public
// key are signed by the original enrolled boot key and persisted by Authority.
// It deliberately contains no permit ID: the permit is produced afterwards.
type Binding struct {
	Schema                string                       `json:"schema"`
	InstallationID        string                       `json:"installation_id"`
	BootID                string                       `json:"boot_id"`
	GrantID               string                       `json:"grant_id"`
	StoragePlan           provisionstorage.Plan        `json:"storage_plan"`
	GPTIdentity           provisionstorage.GPTIdentity `json:"gpt_identity"`
	Hostname              string                       `json:"hostname"`
	Origin                string                       `json:"origin"`
	Server                netip.Addr                   `json:"server"`
	Clients               netip.Prefix                 `json:"clients"`
	BootCASHA256          string                       `json:"boot_ca_sha256"`
	TargetUKISHA256       string                       `json:"target_uki_sha256"`
	RuntimeInputSHA256    string                       `json:"runtime_input_sha256"`
	FirstBootPublicKey    string                       `json:"first_boot_public_key"`
	InstallerKernelBootID string                       `json:"installer_kernel_boot_id"`
	CreatedAt             time.Time                    `json:"created_at"`
}

func publicKey(value string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(raw) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(raw) != value {
		return nil, ErrDenied
	}
	return ed25519.PublicKey(raw), nil
}

func (b Binding) Validate(now time.Time) error {
	if b.Schema != BindingSchema || !id.MatchString(b.InstallationID) || !id.MatchString(b.BootID) || !id.MatchString(b.GrantID) || !hostname.MatchString(b.Hostname) || b.StoragePlan.Validate(now) != nil || !digest.MatchString(b.BootCASHA256) || !digest.MatchString(b.TargetUKISHA256) || !digest.MatchString(b.RuntimeInputSHA256) || !bootID.MatchString(b.InstallerKernelBootID) || b.CreatedAt.IsZero() || b.CreatedAt.After(now.Add(time.Second)) || b.CreatedAt.Before(now.Add(-30*time.Minute)) || !b.CreatedAt.Before(b.StoragePlan.Spec.ExpiresAt) {
		return ErrDenied
	}
	if _, err := publicKey(b.FirstBootPublicKey); err != nil {
		return err
	}
	if _, err := provisionstorage.PrepareGPT(b.StoragePlan, b.GPTIdentity, now); err != nil {
		return ErrDenied
	}
	u, err := url.Parse(b.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.String() != b.Origin || !b.Server.Is4() || b.Server.IsUnspecified() || b.Server.IsMulticast() || !b.Clients.IsValid() || !b.Clients.Addr().Is4() || b.Clients != b.Clients.Masked() || b.Clients.Bits() < 8 || b.Clients.Bits() > 30 || !b.Clients.Contains(b.Server) {
		return ErrDenied
	}
	return nil
}

func (b Binding) SHA256() string {
	raw, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(append([]byte("Keel first boot binding v1\x00"), raw...))
	return hex.EncodeToString(sum[:])
}

type Secret struct {
	Binding    Binding `json:"binding"`
	PrivateKey string  `json:"private_key"`
}

func NewSecret(binding Binding, now time.Time) (*Secret, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, ErrDenied
	}
	defer clear(private)
	binding.FirstBootPublicKey = base64.StdEncoding.EncodeToString(public)
	if binding.Validate(now) != nil {
		return nil, ErrDenied
	}
	return &Secret{Binding: binding, PrivateKey: base64.StdEncoding.EncodeToString(private)}, nil
}

func (s *Secret) private(now time.Time) (ed25519.PrivateKey, error) {
	if s == nil || s.Binding.Validate(now) != nil {
		return nil, ErrDenied
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(s.PrivateKey)
	if err != nil || len(raw) != ed25519.PrivateKeySize || base64.StdEncoding.EncodeToString(raw) != s.PrivateKey {
		clear(raw)
		return nil, ErrDenied
	}
	key := ed25519.PrivateKey(raw)
	derived := ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize])
	consistent := bytes.Equal(derived, raw)
	clear(derived)
	if !consistent {
		clear(raw)
		return nil, ErrDenied
	}
	if base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != s.Binding.FirstBootPublicKey {
		clear(raw)
		return nil, ErrDenied
	}
	return key, nil
}

func DecodeSecret(raw []byte, now time.Time) (*Secret, error) {
	var secret Secret
	if len(raw) < 1 || len(raw) > 192<<10 || !utf8.Valid(raw) || json.Unmarshal(raw, &secret) != nil {
		return nil, ErrDenied
	}
	canonical, err := json.Marshal(secret)
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, ErrDenied
	}
	key, err := secret.private(now)
	clear(key)
	if err != nil {
		return nil, ErrDenied
	}
	return &secret, nil
}

type Report struct {
	Schema            string    `json:"schema"`
	InstallationID    string    `json:"installation_id"`
	BootID            string    `json:"boot_id"`
	GrantID           string    `json:"grant_id"`
	BindingSHA256     string    `json:"binding_sha256"`
	Sequence          int64     `json:"sequence"`
	Nonce             string    `json:"nonce"`
	ObservedAt        time.Time `json:"observed_at"`
	KernelBootID      string    `json:"kernel_boot_id"`
	Hostname          string    `json:"hostname"`
	Serial            string    `json:"serial_number"`
	BootMAC           string    `json:"boot_mac"`
	Arch              string    `json:"arch"`
	DiskWWID          string    `json:"disk_wwid"`
	DiskSerial        string    `json:"disk_serial"`
	DiskSizeBytes     int64     `json:"disk_size_bytes"`
	LogicalBlockBytes int       `json:"logical_block_bytes"`
	RootPartitionGUID string    `json:"root_partition_guid"`
	EFIPartitionGUID  string    `json:"efi_partition_guid"`
	HostPublicKey     string    `json:"host_public_key"`
	HostFingerprint   string    `json:"host_fingerprint"`
	HostKeyModifiedAt time.Time `json:"host_key_modified_at"`
}

func ValidateReport(b Binding, r Report, now time.Time) error {
	d := b.StoragePlan.Device
	if b.Validate(now) != nil || r.Schema != ReportSchema || r.InstallationID != b.InstallationID || r.BootID != b.BootID || r.GrantID != b.GrantID || r.BindingSHA256 != b.SHA256() || r.Sequence != 1 || !id.MatchString(r.Nonce) || !bootID.MatchString(r.KernelBootID) || r.KernelBootID == b.InstallerKernelBootID || r.ObservedAt.Before(now.Add(-30*time.Second)) || r.ObservedAt.After(now.Add(time.Second)) || r.Hostname != b.Hostname || r.Serial != b.StoragePlan.Spec.Expected.Serial || r.BootMAC != b.StoragePlan.Spec.Expected.BootMAC || r.Arch != b.StoragePlan.Spec.Expected.Arch || r.DiskWWID != d.WWID || r.DiskSerial != d.Serial || r.DiskSizeBytes != d.SizeBytes || r.LogicalBlockBytes != d.LogicalBlockBytes || r.RootPartitionGUID != b.GPTIdentity.Root || r.EFIPartitionGUID != b.GPTIdentity.EFI || r.HostKeyModifiedAt.Before(b.CreatedAt) || r.HostKeyModifiedAt.After(r.ObservedAt.Add(time.Second)) || len(r.HostPublicKey) > 256 {
		return ErrDenied
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(r.HostPublicKey))
	if err != nil || key.Type() != ssh.KeyAlgoED25519 || comment != "" || len(options) != 0 || len(rest) != 0 || strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) != r.HostPublicKey || ssh.FingerprintSHA256(key) != r.HostFingerprint {
		return ErrDenied
	}
	return nil
}

type Submission struct {
	Report    Report `json:"report"`
	Signature string `json:"signature"`
}

func Sign(secret *Secret, report Report, now time.Time) (*Submission, error) {
	key, err := secret.private(now)
	if err != nil {
		return nil, ErrDenied
	}
	defer clear(key)
	if ValidateReport(secret.Binding, report, now) != nil {
		return nil, ErrDenied
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, ErrDenied
	}
	return &Submission{Report: report, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(signatureDomain), raw...)))}, nil
}

// SignObserved preserves an authenticated hostname mismatch for controller
// alerting. Every other binding stays strict; this is not registration success.
func SignObserved(secret *Secret, report Report, now time.Time) (*Submission, error) {
	if secret == nil || !validObservedHostname(report.Hostname) {
		return nil, ErrDenied
	}
	check := report
	check.Hostname = secret.Binding.Hostname
	if ValidateReport(secret.Binding, check, now) != nil {
		return nil, ErrDenied
	}
	key, err := secret.private(now)
	if err != nil {
		return nil, ErrDenied
	}
	defer clear(key)
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, ErrDenied
	}
	return &Submission{Report: report, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(signatureDomain), raw...)))}, nil
}

func validObservedHostname(value string) bool {
	if len(value) < 1 || len(value) > 253 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// VerifySignature authenticates a fully bound observation while permitting
// only hostname mismatch for diagnostic alerting. Registration must additionally
// call ValidateReport or VerifySubmission, both of which reject that mismatch.
func VerifySignature(binding Binding, in Submission, now time.Time) error {
	if !validObservedHostname(in.Report.Hostname) {
		return ErrDenied
	}
	check := in.Report
	check.Hostname = binding.Hostname
	if ValidateReport(binding, check, now) != nil {
		return ErrDenied
	}
	key, err := publicKey(binding.FirstBootPublicKey)
	if err != nil {
		return ErrDenied
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(in.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(signature) != in.Signature {
		return ErrDenied
	}
	raw, err := json.Marshal(in.Report)
	if err != nil || !ed25519.Verify(key, append([]byte(signatureDomain), raw...), signature) {
		return ErrDenied
	}
	return nil
}

func VerifySubmission(binding Binding, in Submission, now time.Time) error {
	if ValidateReport(binding, in.Report, now) != nil {
		return ErrDenied
	}
	key, err := publicKey(binding.FirstBootPublicKey)
	if err != nil {
		return ErrDenied
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(in.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(signature) != in.Signature {
		return ErrDenied
	}
	raw, err := json.Marshal(in.Report)
	if err != nil || !ed25519.Verify(key, append([]byte(signatureDomain), raw...), signature) {
		return ErrDenied
	}
	return nil
}

func DecodeSubmission(raw []byte) (*Submission, error) {
	var out Submission
	if len(raw) < 1 || len(raw) > 32<<10 || !utf8.Valid(raw) || json.Unmarshal(raw, &out) != nil {
		return nil, ErrDenied
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, ErrDenied
	}
	return &out, nil
}

type Acknowledgment struct {
	InstallationID  string    `json:"installation_id"`
	BootID          string    `json:"boot_id"`
	BindingSHA256   string    `json:"binding_sha256"`
	Nonce           string    `json:"nonce"`
	State           string    `json:"state"`
	HostFingerprint string    `json:"host_fingerprint"`
	ReceivedAt      time.Time `json:"received_at"`
}

func ValidateAcknowledgment(ack *Acknowledgment, b Binding, r Report, now time.Time) error {
	if ValidateReport(b, r, now) != nil || ack == nil || ack.InstallationID != b.InstallationID || ack.BootID != b.BootID || ack.BindingSHA256 != b.SHA256() || ack.Nonce != r.Nonce || ack.State != "awaiting_first_boot" || ack.HostFingerprint != r.HostFingerprint || ack.ReceivedAt.Before(now.Add(-30*time.Second)) || ack.ReceivedAt.After(now.Add(time.Second)) {
		return ErrDenied
	}
	return nil
}

func DecodeAcknowledgment(raw []byte) (*Acknowledgment, error) {
	var ack Acknowledgment
	if len(raw) < 1 || len(raw) > 8192 || !utf8.Valid(raw) || json.Unmarshal(raw, &ack) != nil {
		return nil, ErrDenied
	}
	canonical, err := json.Marshal(ack)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return nil, ErrDenied
	}
	return &ack, nil
}
