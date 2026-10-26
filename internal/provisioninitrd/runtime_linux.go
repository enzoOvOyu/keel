//go:build linux

package provisioninitrd

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"golang.org/x/sys/unix"
)

const BootCAPath = "/etc/keel/boot-ca.pem"

// MountRuntime mounts only kernel virtual filesystems inside an ephemeral
// PID1 root. No block device, customer filesystem or target root is mounted.
func MountRuntime() error {
	if RequireInitramfs() != nil {
		return ErrUnavailable
	}
	for _, m := range []struct {
		path, kind string
		magic      int64
		flags      uintptr
	}{
		{"/proc", "proc", unix.PROC_SUPER_MAGIC, unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC},
		{"/sys", "sysfs", unix.SYSFS_MAGIC, unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC},
		{"/dev", "devtmpfs", unix.TMPFS_MAGIC, unix.MS_NOSUID | unix.MS_NOEXEC},
		{"/run", "tmpfs", unix.TMPFS_MAGIC, unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC},
	} {
		if err := os.Mkdir(m.path, 0755); err != nil && !os.IsExist(err) {
			return ErrUnavailable
		}
		info, err := os.Lstat(m.path)
		if err != nil || !info.IsDir() {
			return ErrUnavailable
		}
		var before unix.Statfs_t
		if unix.Statfs(m.path, &before) != nil {
			return ErrUnavailable
		}
		if before.Type == m.magic && (m.path == "/proc" || m.path == "/sys") {
			continue
		}
		if err = unix.Mount(m.kind, m.path, m.kind, m.flags, ""); err != nil {
			return ErrUnavailable
		}
		var after unix.Statfs_t
		if unix.Statfs(m.path, &after) != nil || after.Type != m.magic {
			return ErrUnavailable
		}
	}
	// Kernel startup may have had no console device in the initial archive.
	// Open it only after devtmpfs exists; never create a host device node.
	console, err := unix.Open("/dev/console", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrUnavailable
	}
	for target := 0; target < 3; target++ {
		if console != target && unix.Dup3(console, target, 0) != nil {
			unix.Close(console)
			return ErrUnavailable
		}
	}
	if console > 2 {
		unix.Close(console)
	}
	return nil
}

// LoadBootRoots reads public CA certificates baked into the independently
// approved image. DHCP, downloaded files and the ambient OS root store cannot
// supply or replace the first HTTPS trust anchor.
func LoadBootRoots() (*x509.CertPool, error) {
	if RequireInitramfs() != nil {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(BootCAPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), BootCAPath)
	defer f.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Mode&0022 != 0 || stat.Size < 1 || stat.Size > 1<<20 {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || int64(len(raw)) != stat.Size {
		return nil, ErrUnavailable
	}
	return provisionimage.ParseBootRoots(raw, time.Now())
}

func ReadBootInput() (Input, error) {
	if RequireInitramfs() != nil {
		return Input{}, ErrUnavailable
	}
	f, err := os.Open("/proc/cmdline")
	if err != nil {
		return Input{}, ErrUnavailable
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (16<<10)+2))
	if err != nil || len(raw) > 16<<10 {
		return Input{}, ErrUnavailable
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	return ParseCommandLine(string(raw))
}

// RunPreparation is the bounded read-only boot phase. It stops after retaining
// local evidence; it sends no callback and has no disk writer or shell escape.
func RunPreparation(ctx context.Context) (*Preparation, error) {
	if MountRuntime() != nil {
		return nil, ErrUnavailable
	}
	input, err := ReadBootInput()
	if err != nil {
		return nil, ErrUnavailable
	}
	roots, err := LoadBootRoots()
	if err != nil {
		return nil, ErrUnavailable
	}
	lease, err := ConfigureNetwork(ctx, input)
	if err != nil {
		return nil, ErrUnavailable
	}
	var preparation *Preparation
	err = WithNetworkLease(ctx, lease, func(ctx context.Context) error {
		filesClient, err := NewHTTPFiles(HTTPConfig{Input: input, RootCAs: roots, Interface: lease.Interface, LocalAddress: lease.Address})
		if err != nil {
			return err
		}
		defer filesClient.Close()
		files, err := Download(ctx, input, filesClient)
		if err != nil {
			return err
		}
		preparation, err = Prepare(ctx, input, files, LinuxHelpers{}, time.Now)
		return err
	})
	if err != nil || preparation == nil {
		return nil, ErrUnavailable
	}
	if err = storePreparation(preparation); err != nil {
		return nil, ErrUnavailable
	}
	return preparation, nil
}

func storePreparation(p *Preparation) error {
	if RequireInitramfs() != nil || p == nil || len(p.ReportBytes) == 0 || len(p.ReportBytes) > 2<<20 || p.InstallationPerformed || p.Schema != PreparationSchema {
		return ErrUnavailable
	}
	hash := sha256.Sum256(p.ReportBytes)
	if hex.EncodeToString(hash[:]) != p.ReportSHA256 {
		return ErrUnavailable
	}
	const dir = "/run/keel"
	if err := os.Mkdir(dir, 0700); err != nil {
		return ErrUnavailable
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > 3<<20 {
		return ErrUnavailable
	}
	// Exclusive local evidence, never overwrite an earlier boot result.
	for _, item := range []struct {
		name string
		data []byte
	}{{"preflight-report.json", p.ReportBytes}, {"preparation.json", data}} {
		f, err := os.OpenFile(filepath.Join(dir, item.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return ErrUnavailable
		}
		n, writeErr := f.Write(item.data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil || n != len(item.data) {
			return ErrUnavailable
		}
	}
	return nil
}
