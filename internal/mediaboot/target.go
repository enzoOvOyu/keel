package mediaboot

import (
	"errors"
	"regexp"
)

const TargetRecipeSchema = "keel.boot-recipe/v2"
const TargetBootSchema = "keel.target-boot/v1"
const TargetUKIPath = "usr/lib/keel/target.efi"
const TargetCommandLine = "root=PARTLABEL=keel-root rootfstype=ext4 rw rootwait"
const MaxTargetUKIBytes int64 = 384 << 20

var kernelRelease = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,127}$`)

// TargetBoot describes installed-OS content inside the approved squashfs. It
// does not refer to a future installer or confuse the iPXE firmware with an OS
// bootloader. Actual UKI bytes and the staged root must be checked before use.
type TargetBoot struct {
	Schema        string `json:"schema"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	KernelRelease string `json:"kernel_release"`
	CommandLine   string `json:"command_line"`
}

func (t *TargetBoot) Validate() error {
	if t == nil || t.Schema != TargetBootSchema || t.Path != TargetUKIPath || !digest.MatchString(t.SHA256) || t.Size < 1024 || t.Size > MaxTargetUKIBytes || !kernelRelease.MatchString(t.KernelRelease) || t.CommandLine != TargetCommandLine {
		return errors.New("explicit pinned target UKI and fixed local-root command line required")
	}
	return nil
}

func TargetEFIFallback(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "EFI/BOOT/BOOTX64.EFI", nil
	case "arm64":
		return "EFI/BOOT/BOOTAA64.EFI", nil
	default:
		return "", errors.New("unsupported target EFI architecture")
	}
}

// ValidateWritable adds installed-system content requirements to the existing
// boot/preflight admission. Success remains metadata, never a write permit.
func (p Plan) ValidateWritable() error {
	if p.ValidateInstallable() != nil || p.Profile.Recipe.Schema != TargetRecipeSchema || p.Profile.Recipe.TargetBoot.Validate() != nil {
		return errors.New("approved v2 target-system boot contract required before writing")
	}
	return nil
}
