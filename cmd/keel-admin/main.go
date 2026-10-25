// keel-admin is a server-only operator CLI. It never accepts passwords in argv.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/mediaartifact"
	"github.com/enzoOvOyu/keel/internal/platform"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "keel-admin:", e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: keel-admin audit-signing-keygen|audit-evidence-export|audit-evidence-verify|audit-evidence-compare|agent-task-create|agent-task-status|agent-task-action|agent-task-reconcile|agent-credential-status|agent-ca-init|agent-enrollment-create|agent-enrollment-cancel|agent-revoke|credential-master-keygen|credential-master-inspect|credential-master-rewrap|ssh-key-create|ssh-key-list|ssh-key-revoke|media-init|media-signing-keygen|media-manifest-verify|media-bundle-create|media-bundle-verify|migrate|init-admin|user-create|user-disable|user-unlock|user-password|member-grant|member-revoke [flags]")
	}
	command := args[0]
	if command == "audit-signing-keygen" || command == "audit-evidence-export" || command == "audit-evidence-verify" || command == "audit-evidence-compare" {
		return runAuditEvidence(command, args[1:])
	}
	if command == "agent-task-create" || command == "agent-task-status" || command == "agent-task-action" || command == "agent-task-reconcile" {
		return runAgentTasks(command, args[1:])
	}
	if command == "agent-ca-init" || command == "agent-enrollment-create" || command == "agent-enrollment-cancel" || command == "agent-revoke" || command == "agent-credential-status" {
		return runAgentIdentity(command, args[1:])
	}
	if command == "credential-master-keygen" || command == "credential-master-inspect" || command == "credential-master-rewrap" || command == "ssh-key-create" || command == "ssh-key-list" || command == "ssh-key-revoke" {
		return runSSHCredentials(command, args[1:])
	}
	if command == "media-bundle-create" || command == "media-bundle-verify" {
		return runMediaBundle(command, args[1:])
	}
	if command == "media-signing-keygen" || command == "media-manifest-verify" {
		return runMediaManifest(command, args[1:])
	}
	if command == "media-init" {
		if len(args) != 1 {
			return errors.New("usage: keel-admin media-init")
		}
		objects, e := mediaartifact.Load()
		if e != nil {
			return e
		}
		if objects == nil {
			return errors.New("media storage configuration is required")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if e = objects.Initialize(ctx); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "operation": "media-init"})
	}
	switch command {
	case "migrate", "init-admin", "user-create", "user-disable", "user-unlock", "user-password", "member-grant", "member-revoke":
	default:
		return errors.New("unknown command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	username := flags.String("username", "", "local username")
	display := flags.String("display-name", "", "display name")
	passwordFile := flags.String("password-file", "", "owner-only regular file, mode 0600 or stricter")
	admin := flags.Bool("admin", false, "create a platform administrator (user-create only)")
	ip := flags.String("ip", "", "explicit source IP throttle to unlock (user-unlock only)")
	project := flags.String("project", "", "project id")
	role := flags.String("role", "Viewer", "project role")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	// Do not charge an operator's interactive typing time to the database deadline.
	var password string
	if command == "init-admin" || command == "user-create" || command == "user-password" {
		if strings.TrimSpace(*username) == "" {
			return errors.New("--username is required")
		}
		var e error
		password, e = readPassword(*passwordFile)
		if e != nil {
			return e
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, e := platform.Open(ctx, dsn)
	if e != nil {
		return e
	}
	defer s.Close()
	if command == "migrate" {
		if e = s.Migrate(ctx); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "operation": "migrate"})
	}
	if e = s.CheckMigrations(ctx); e != nil {
		return e
	}
	if strings.TrimSpace(*username) == "" {
		return errors.New("--username is required")
	}
	var result any = map[string]string{"status": "ok", "operation": command}
	switch command {
	case "init-admin", "user-create":
		if *display == "" {
			*display = *username
		}
		result, e = s.CreateUser(ctx, *username, *display, password, *admin, command == "init-admin")
		if e != nil {
			return e
		}
	case "user-disable":
		e = s.DisableUser(ctx, *username)
	case "user-unlock":
		e = s.UnlockUser(ctx, *username)
		if e == nil && *ip != "" {
			e = s.UnlockIP(ctx, *ip)
		}
	case "user-password":
		e = s.ResetPassword(ctx, *username, password)
	case "member-grant", "member-revoke":
		if *project == "" {
			return errors.New("--project is required")
		}
		e = s.SetMembership(ctx, *username, *project, *role, command == "member-revoke")
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
func readPassword(path string) (string, error) {
	if path != "" {
		file, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
		if e != nil {
			return "", errors.New("cannot open password file (symlinks are prohibited)")
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil {
			return "", e
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uint32(os.Geteuid()) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", errors.New("password file must be a regular file owned by the current user with mode 0600 or stricter")
		}
		data, e := io.ReadAll(io.LimitReader(file, 1027))
		if e != nil {
			return "", e
		}
		defer func() {
			for i := range data {
				data[i] = 0
			}
		}()
		password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if len(password) < 12 || len(password) > 1024 {
			return "", errors.New("password must contain 12 to 1024 bytes")
		}
		return password, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("use an interactive terminal or --password-file; passwords in arguments/stdin pipes are not accepted")
	}
	fmt.Fprint(os.Stderr, "Password (12+ bytes): ")
	first, e := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if e != nil {
		return "", e
	}
	defer func() {
		for i := range first {
			first[i] = 0
		}
	}()
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, e := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if e != nil {
		return "", e
	}
	defer func() {
		for i := range second {
			second[i] = 0
		}
	}()
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	if len(first) < 12 || len(first) > 1024 {
		return "", errors.New("password must contain 12 to 1024 bytes")
	}
	return string(first), nil
}
