//go:build linux

package provisionfirstboot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"regexp"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"golang.org/x/sys/unix"
)

var regexpDisk = regexp.MustCompile(`^(sd[a-z]+|nvme[0-9]+n[0-9]+|vd[a-z]+|xvd[a-z]+)$`)

func readProtected(path string, limit int64, private bool) ([]byte, unix.Stat_t, error) {
	var stat unix.Stat_t
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, stat, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "protected-first-boot-input")
	defer f.Close()
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Nlink != 1 || stat.Mode&0022 != 0 || (private && stat.Mode&0077 != 0) || stat.Size < 1 || stat.Size > limit {
		return nil, stat, ErrDenied
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) != stat.Size {
		clear(raw)
		return nil, stat, ErrDenied
	}
	var after unix.Stat_t
	if unix.Fstat(fd, &after) != nil || after.Size != stat.Size || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim || after.Ino != stat.Ino {
		clear(raw)
		return nil, stat, ErrDenied
	}
	return raw, stat, nil
}

func hashInstalledUKI(path, want string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return ErrDenied
	}
	f := os.NewFile(uintptr(fd), "installed-target-uki")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size < 1024 || st.Size > mediaboot.MaxTargetUKIBytes || st.Uid != 0 || st.Mode&0022 != 0 {
		return ErrDenied
	}
	h := sha256.New()
	if n, err := io.CopyBuffer(h, io.LimitReader(f, st.Size+1), make([]byte, 128<<10)); err != nil || n != st.Size || hex.EncodeToString(h.Sum(nil)) != want {
		return ErrDenied
	}
	return nil
}

// FindNetwork selects only the unique BOM boot MAC and a unique assigned
// address in its explicitly reserved scope. It does not configure networking,
// discover a controller, query DNS or fall back to a default route/interface.
func FindNetwork(b Binding) (net.Interface, netip.Addr, error) {
	var selected net.Interface
	var address netip.Addr
	faces, err := net.Interfaces()
	if err != nil {
		return selected, address, ErrDenied
	}
	matches := 0
	for _, face := range faces {
		if face.HardwareAddr.String() != b.StoragePlan.Spec.Expected.BootMAC {
			continue
		}
		matches++
		if face.Flags&net.FlagUp == 0 || face.Flags&net.FlagLoopback != 0 || !ifaceName.MatchString(face.Name) {
			return selected, address, ErrDenied
		}
		addresses, err := face.Addrs()
		if err != nil {
			return selected, address, ErrDenied
		}
		for _, a := range addresses {
			p, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := p.Addr().Unmap()
			if !ip.Is4() || !b.Clients.Contains(ip) || ip == b.Server || ip == b.Clients.Addr() || ip == scopeBroadcast(b.Clients) || !ip.IsGlobalUnicast() {
				continue
			}
			if address.IsValid() && address != ip {
				return selected, netip.Addr{}, ErrDenied
			}
			selected, address = face, ip
		}
	}
	if matches != 1 || !address.IsValid() {
		return selected, address, ErrDenied
	}
	return selected, address, nil
}

func scopeBroadcast(prefix netip.Prefix) netip.Addr {
	if !prefix.IsValid() || !prefix.Addr().Is4() {
		return netip.Addr{}
	}
	b := prefix.Masked().Addr().As4()
	for bit := prefix.Bits(); bit < 32; bit++ {
		b[bit/8] |= 1 << uint(7-bit%8)
	}
	return netip.AddrFrom4(b)
}

func RunInstalled(ctx context.Context) (*Result, error) {
	if os.Geteuid() != 0 {
		return nil, ErrDenied
	}
	// No listener, key generation, command, disk IO or network access precedes
	// validation of the independently installed protected binding and CA bytes.
	raw, _, err := readProtected(BindingPath, 192<<10, true)
	if err != nil {
		return nil, ErrDenied
	}
	secret, err := DecodeSecret(raw, time.Now())
	clear(raw)
	if err != nil {
		return nil, ErrDenied
	}
	defer func() { secret.PrivateKey = "" }()
	ca, _, err := readProtected(CAPath, 1<<20, true)
	if err != nil {
		return nil, ErrDenied
	}
	h := sha256.Sum256(ca)
	if hex.EncodeToString(h[:]) != secret.Binding.BootCASHA256 {
		return nil, ErrDenied
	}
	roots, err := provisionimage.ParseBootRoots(ca, time.Now())
	if err != nil {
		return nil, ErrDenied
	}
	face, address, err := FindNetwork(secret.Binding)
	if err != nil {
		return nil, ErrDenied
	}
	transport, err := NewHTTPTransport(HTTPConfig{Binding: secret.Binding, RootCAs: roots, Interface: face.Name, LocalAddress: address})
	if err != nil {
		return nil, ErrDenied
	}
	defer transport.Close()
	journal, err := newLinuxJournal()
	if err != nil {
		return nil, ErrDenied
	}
	defer journal.Close()
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	return RunOnce(ctx, secret, LinuxCollector{}, transport, journal, time.Now)
}

type linuxJournal struct {
	dir   *os.File
	begun bool
}

func newLinuxJournal() (*linuxJournal, error) {
	fd, err := unix.Open(StateDirectory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "private-first-boot-journal")
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0077 != 0 {
		f.Close()
		return nil, ErrDenied
	}
	return &linuxJournal{dir: f}, nil
}
func (j *linuxJournal) Close() error {
	if j == nil || j.dir == nil {
		return nil
	}
	return j.dir.Close()
}
func (j *linuxJournal) Begin(ctx context.Context, binding string) error {
	if j == nil || j.dir == nil || j.begun || !digest.MatchString(binding) || ctx.Err() != nil {
		return ErrDenied
	}
	raw, _ := json.Marshal(map[string]any{"binding_sha256": binding, "status": "unknown", "started_at": time.Now().UTC().Truncate(time.Microsecond)})
	if j.write(ctx, "attempt.json", raw) != nil {
		return ErrDenied
	}
	j.begun = true
	return nil
}
func (j *linuxJournal) SaveSubmission(ctx context.Context, raw []byte) error {
	if j == nil || !j.begun || len(raw) < 1 || len(raw) > 32<<10 {
		return ErrDenied
	}
	return j.write(ctx, "submission.json", raw)
}
func (j *linuxJournal) SaveAcknowledgment(ctx context.Context, raw []byte) error {
	if j == nil || !j.begun || len(raw) < 1 || len(raw) > 8192 {
		return ErrDenied
	}
	if j.write(ctx, "acknowledgment.json", raw) != nil {
		return ErrDenied
	}
	// Once the nonce-bound ACK is durable, this one-use private identity is no
	// longer needed. Unlink only its fixed file, never the OS SSH host key. A
	// lost ACK retains the binding and intent for explicit reconciliation.
	if unix.Unlinkat(int(j.dir.Fd()), "binding.json", 0) != nil || j.dir.Sync() != nil {
		return ErrDenied
	}
	return nil
}
func (j *linuxJournal) write(ctx context.Context, name string, raw []byte) error {
	if j == nil || j.dir == nil || ctx.Err() != nil {
		return ErrDenied
	}
	fd, err := unix.Openat(int(j.dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrDenied
	}
	f := os.NewFile(uintptr(fd), "private-first-boot-evidence")
	n, err := f.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil || j.dir.Sync() != nil || ctx.Err() != nil {
		return ErrDenied
	}
	return nil
}

// Diagnostic returns only a bounded status, never a private binding or key.
func Diagnostic(result *Result) string {
	if result == nil {
		return "Keel first boot: unavailable"
	}
	if result.Status == "pending_ssh" {
		return fmt.Sprintf("Keel first boot: identity acknowledged for %s; SSH verification pending", result.InstallationID)
	}
	return "Keel first boot: unknown; retained evidence requires reconciliation"
}
