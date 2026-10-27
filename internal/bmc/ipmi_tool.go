package bmc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type IPMIConfig struct {
	Address     string `json:"address"`
	ToolPath    string `json:"tool_path,omitempty"`
	ToolSHA256  string `json:"tool_sha256,omitempty"`
	FixtureFile string `json:"fixture_file,omitempty"`
}
type ipmiTool struct {
	config   IPMIConfig
	origin   string
	provider CredentialProvider
	timeout  time.Duration
	fixture  map[string][]byte
}

// The operator opts into this exact address and pinned executable. Passwords
// travel through inherited anonymous descriptors, never argv, environment or disk.
func newIPMITool(config IPMIConfig, origin string, provider CredentialProvider, timeout time.Duration) (*ipmiTool, string, error) {
	out := &ipmiTool{config: config, origin: origin, provider: provider, timeout: timeout}
	if config.FixtureFile != "" {
		if origin != "sim" || config.ToolPath != "" || config.ToolSHA256 != "" || config.Address != "" {
			return nil, "", errors.New("IPMI fixtures require isolated sim binding")
		}
		raw, e := ReadOwnedFile(config.FixtureFile, 1<<20)
		if e != nil {
			return nil, "", e
		}
		var source struct {
			Schema  string            `json:"schema"`
			Replies map[string]string `json:"replies"`
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&source) != nil || d.Decode(&extra) != io.EOF || source.Schema != IPMIParserVersion || len(source.Replies) > 4 {
			return nil, "", errors.New("invalid IPMI fixture")
		}
		out.fixture = map[string][]byte{}
		for operation, encoded := range source.Replies {
			payload, e := hex.DecodeString(encoded)
			if e != nil {
				return nil, "", errors.New("invalid IPMI fixture hex")
			}
			if _, e = decodeIPMI(operation, payload); e != nil {
				return nil, "", e
			}
			out.fixture[operation] = payload
		}
		sum := sha256.Sum256(raw)
		return out, hex.EncodeToString(sum[:]), nil
	}
	if net.ParseIP(config.Address) == nil || !filepath.IsAbs(config.ToolPath) || !sha256Pattern.MatchString(config.ToolSHA256) || provider == nil {
		return nil, "", errors.New("IPMI requires a fixed literal address, pinned tool and credential provider")
	}
	file, e := openIPMITool(config)
	if e != nil {
		return nil, "", e
	}
	file.Close()
	return out, config.ToolSHA256, nil
}
func openIPMITool(config IPMIConfig) (*os.File, error) {
	fd, e := unix.Open(config.ToolPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, errors.New("pinned IPMI executable unavailable")
	}
	file := os.NewFile(uintptr(fd), "ipmi-inventory-tool")
	fail := func() (*os.File, error) {
		file.Close()
		return nil, errors.New("IPMI executable ownership, mode or digest invalid")
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || st.Mode&0111 == 0 || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || st.Size < 1 || st.Size > 64<<20 {
		return fail()
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, io.LimitReader(file, 64<<20+1)); e != nil || hex.EncodeToString(hash.Sum(nil)) != config.ToolSHA256 {
		return fail()
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		return fail()
	}
	return file, nil
}
func (c *ipmiTool) Origin() string { return c.origin }

type boundedIPMIOutput struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (w *boundedIPMIOutput) Write(raw []byte) (int, error) {
	if len(raw) > w.limit-w.Len() {
		w.overflow = true
		return 0, errors.New("IPMI output limit")
	}
	return w.Buffer.Write(raw)
}
func anonymousIPMIFile(name string, body []byte, sealed bool) (*os.File, error) {
	fd, e := unix.MemfdCreate(name, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_NOEXEC_SEAL)
	if e != nil {
		return nil, errors.New("anonymous IPMI descriptor unavailable")
	}
	file := os.NewFile(uintptr(fd), name)
	mode := os.FileMode(0600)
	if sealed {
		mode = 0400
	}
	if e = file.Chmod(mode); e != nil {
		file.Close()
		return nil, e
	}
	if _, e = file.Write(body); e != nil {
		file.Close()
		return nil, e
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		file.Close()
		return nil, e
	}
	if sealed {
		if _, e = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); e != nil {
			file.Close()
			return nil, e
		}
	}
	return file, nil
}
func (c *ipmiTool) ReadIPMI(ctx context.Context, target Target, operation string) ([]byte, error) {
	var command []string
	switch operation {
	case "device-id":
		command = []string{"raw", "0x06", "0x01"}
	case "system-guid":
		command = []string{"raw", "0x06", "0x37"}
	case "chassis-status":
		command = []string{"raw", "0x00", "0x01"}
	case "fru-0":
		command = []string{"fru", "read", "0", "/proc/self/fd/4"}
	default:
		return nil, failure("", "version_unsupported", "ipmi_operation_unsupported", false)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.fixture != nil {
		payload, ok := c.fixture[operation]
		if !ok {
			return nil, failure("", "version_unsupported", "ipmi_fixture_missing", false)
		}
		return append([]byte(nil), payload...), nil
	}
	credential, e := c.provider.Resolve(ctx, target)
	if e != nil {
		if errors.Is(e, ErrCredentialExpired) {
			return nil, failure("", "authentication_failed", "credential_expired", true)
		}
		return nil, failure("", "authentication_failed", "ipmi_credential_unavailable", false)
	}
	if credential.BatchRef != target.CredentialBatchRef || credential.Version != target.CredentialVersion || !credential.ExpiresAt.After(time.Now()) {
		return nil, failure("", "authentication_failed", "credential_expired", true)
	}
	if len(credential.Username) < 1 || len(credential.Username) > 16 || len(credential.Password) < 1 || len(credential.Password) > 20 || strings.ContainsAny(credential.Username, "\r\n\x00") || strings.ContainsAny(credential.Password, "\r\n\x00") {
		return nil, failure("", "version_unsupported", "ipmi_credential_format_unsupported", false)
	}
	password := append([]byte(credential.Password), '\n')
	defer clear(password)
	pass, e := anonymousIPMIFile("keel-ipmi-credential", password, true)
	if e != nil {
		return nil, failure("", "partial_failure", "ipmi_credential_descriptor_failed", false)
	}
	defer pass.Close()
	output, e := anonymousIPMIFile("keel-ipmi-fru", nil, false)
	if e != nil {
		return nil, failure("", "partial_failure", "ipmi_capture_failed", false)
	}
	defer output.Close()
	tool, e := openIPMITool(c.config)
	if e != nil {
		return nil, failure("", "partial_failure", "ipmi_tool_changed", false)
	}
	defer tool.Close()
	work, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	args := []string{"-I", "lanplus", "-C", "17", "-L", "USER", "-H", c.config.Address, "-p", "623", "-U", credential.Username, "-f", "/proc/self/fd/3", "-N", "2", "-R", "0"}
	args = append(args, command...)
	cmd := exec.CommandContext(work, "/proc/self/fd/5", args...)
	cmd.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin"}
	cmd.ExtraFiles = []*os.File{pass, output, tool}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if e == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return e
	}
	stdout := &boundedIPMIOutput{limit: 256 << 10}
	stderr := &boundedIPMIOutput{limit: 32 << 10}
	defer func() { clear(stdout.Bytes()); clear(stderr.Bytes()) }()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if work.Err() != nil {
		return nil, failure("", "timeout", "ipmi_timeout", true)
	}
	if stdout.overflow || stderr.overflow {
		return nil, failure("", "partial_failure", "ipmi_output_limit", false)
	}
	if runErr != nil {
		message := strings.ToLower(stderr.String())
		if strings.Contains(message, "unauthorized") || strings.Contains(message, "password") || strings.Contains(message, "rakp") || strings.Contains(message, "privilege") {
			return nil, failure("", "authentication_failed", "ipmi_authentication_failed", false)
		}
		return nil, failure("", "partial_failure", "ipmi_command_failed", true)
	}
	if operation == "fru-0" {
		if _, e = output.Seek(0, io.SeekStart); e != nil {
			return nil, failure("", "partial_failure", "ipmi_capture_failed", false)
		}
		payload, e := io.ReadAll(io.LimitReader(output, 65536))
		if e != nil || len(payload) > 65535 {
			return nil, failure("", "partial_failure", "ipmi_fru_size_invalid", false)
		}
		// ipmitool fru read may return exit zero for read errors. No evidence is
		// accepted until the binary header and all decoded area checksums validate.
		if _, e = decodeFRU(payload); e != nil {
			return nil, failure("", "partial_failure", "ipmi_fru_invalid", false)
		}
		return payload, nil
	}
	payload, e := readHexPayload(stdout.Bytes())
	if e != nil {
		return nil, failure("", "partial_failure", "ipmi_reply_invalid", false)
	}
	return payload, nil
}
