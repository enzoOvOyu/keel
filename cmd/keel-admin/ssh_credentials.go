package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func runSSHCredentials(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	path := flags.String("output", "", "new absolute master-key path (keygen only)")
	project := flags.String("project", "", "project id")
	ref := flags.String("ref", "", "project domain credential reference")
	version := flags.String("version", "", "immutable credential version")
	expires := flags.String("expires-at", "", "RFC3339 expiration")
	id := flags.String("id", "", "domain key id")
	expected := flags.Int64("expected-version", 0, "key state version for revocation")
	reason := flags.String("reason", "", "revocation reason")
	limit := flags.Int("limit", 50, "page size, 1..100")
	offset := flags.Int("offset", 0, "page offset")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	allowed := map[string]map[string]bool{
		"credential-master-keygen": {"output": true},
		"ssh-key-create":           {"project": true, "ref": true, "version": true, "expires-at": true},
		"ssh-key-list":             {"project": true, "limit": true, "offset": true},
		"ssh-key-revoke":           {"project": true, "id": true, "expected-version": true, "reason": true},
	}
	invalid := false
	flags.Visit(func(f *flag.Flag) {
		if !allowed[command][f.Name] {
			invalid = true
		}
	})
	if invalid {
		return errors.New("flag is not supported for this command")
	}
	if command == "credential-master-keygen" {
		if err := credentialvault.GenerateMaster(*path); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "operation": command})
	}
	if *project == "" {
		return errors.New("--project is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	store, err := platform.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	if err = store.CheckMigrations(ctx); err != nil {
		return err
	}
	var result any
	switch command {
	case "ssh-key-create":
		vault, e := credentialvault.Load(os.Getenv("KEEL_CREDENTIAL_MASTER_KEY_FILE"))
		if e != nil {
			return e
		}
		if vault == nil {
			return errors.New("KEEL_CREDENTIAL_MASTER_KEY_FILE is required")
		}
		expiry, e := time.Parse(time.RFC3339Nano, *expires)
		if e != nil {
			return errors.New("--expires-at must be RFC3339")
		}
		result, err = store.CreateSSHDomainKey(ctx, vault, credentialvault.Identity{ProjectID: *project, Ref: *ref, Version: *version, ExpiresAt: expiry})
	case "ssh-key-list":
		result, err = store.ListSSHDomainKeys(ctx, *project, *limit, *offset)
	case "ssh-key-revoke":
		result, err = store.RevokeSSHDomainKey(ctx, *project, *id, *expected, *reason)
	default:
		return errors.New("unknown credential command")
	}
	if err != nil {
		return errors.New("SSH credential operation rejected; check project, version and database authority")
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
