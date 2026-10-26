//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"gopkg.in/yaml.v3"
)

// NewFirstBootSecret prepares a separate installed-system key before a permit.
// Its public binding is signed by the enrolled boot key in WritePreparation;
// the private key is only passed to the controlled writer's private file path.
// It is never included in seed, public downloads, stdout or retained evidence.
func (s *BootSession) NewFirstBootSecret(ctx context.Context, input Input, files *Files, stage *StagedInstallation, ids provisionstorage.GPTIdentity) (*provisionfirstboot.Secret, error) {
	if RequireInitramfs() != nil || ctx.Err() != nil || stage == nil || files == nil || s == nil || stage.RuntimeSHA256() != s.runtimeInputSHA {
		return nil, ErrUnavailable
	}
	plan, err := s.ApprovedStoragePlan()
	if err != nil {
		return nil, err
	}
	manifest, spec, err := provisionboot.VerifyBootFiles(files.Manifest, input.ManifestSHA256, files.Preflight, files.Seed, time.Now())
	if err != nil || manifest.GrantID != input.GrantID || manifest.MAC != input.BootMAC || manifest.Media.ValidateWritable() != nil || !bytes.Equal(mustJSON(spec), mustJSON(s.spec)) {
		return nil, ErrUnavailable
	}
	var metadata struct {
		InstanceID string `yaml:"instance-id"`
		Hostname   string `yaml:"local-hostname"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(files.Seed.MetaData))
	decoder.KnownFields(true)
	if decoder.Decode(&metadata) != nil || decoder.Decode(new(any)) != io.EOF || metadata.InstanceID != "keel-"+s.spec.RequestID {
		return nil, ErrUnavailable
	}
	u, err := url.Parse(input.ManifestURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, ErrUnavailable
	}
	f, err := os.Open("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, 65))
	f.Close()
	if err != nil || len(raw) != 37 || raw[len(raw)-1] != '\n' {
		return nil, ErrUnavailable
	}
	set := stage.Runtime()
	if set.Writer == nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	binding := provisionfirstboot.Binding{Schema: provisionfirstboot.BindingSchema, InstallationID: s.installation, BootID: s.boot, GrantID: input.GrantID, StoragePlan: *plan, GPTIdentity: ids, Hostname: metadata.Hostname, Origin: u.Scheme + "://" + u.Host, Server: input.Server, Clients: input.Clients, BootCASHA256: set.BootCA.SHA256, TargetUKISHA256: manifest.Media.Profile.Recipe.TargetBoot.SHA256, RuntimeInputSHA256: stage.RuntimeSHA256(), InstallerKernelBootID: string(raw[:36]), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	secret, err := provisionfirstboot.NewSecret(binding, time.Now())
	if err != nil {
		return nil, ErrUnavailable
	}
	// Check canonical serialization here before passing a private value to the
	// writer; never store the encoded secret with the public preparation data.
	encoded, err := json.Marshal(secret)
	if err != nil {
		secret.PrivateKey = ""
		return nil, ErrUnavailable
	}
	clear(encoded)
	return secret, nil
}
