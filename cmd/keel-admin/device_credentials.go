package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func runDeviceCredentials(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	project := flags.String("project", "", "project ID")
	batch := flags.String("batch", "", "BOM credential batch reference")
	scope := flags.String("scope", "bmc", "bmc or switch batch scope")
	version := flags.String("version", "", "new exact credential version")
	expires := flags.String("expires-at", "", "RFC3339 expiry (import only)")
	previous := flags.String("expected-current-version", "", "current batch head, empty for first import")
	reason := flags.String("reason", "", "rotation or revocation reason")
	path := flags.String("credentials-file", "", "protected absolute JSON file with username/password (import only)")
	if e := flags.Parse(args); e != nil {
		return e
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *project == "" || *batch == "" || *version == "" || (*scope != "bmc" && *scope != "switch") || *reason == "" {
		return errors.New("project, batch, version, scope and reason required")
	}
	var value platform.DevicePassword
	var expiry time.Time
	if command == "device-credential-import" {
		raw, e := bmc.ReadOwnedFile(*path, 8192)
		if e != nil {
			return errors.New("protected credentials file required")
		}
		defer clear(raw)
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&value) != nil || d.Decode(&extra) != io.EOF {
			return errors.New("credential file requires only username and password")
		}
		expiry, e = time.Parse(time.RFC3339Nano, *expires)
		if e != nil {
			return errors.New("expires-at must be RFC3339")
		}
	} else if command != "device-credential-revoke" {
		return errors.New("unknown device credential operation")
	} else if *path != "" || *expires != "" || *previous != "" {
		return errors.New("import-only flags are not accepted for revocation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL required")
	}
	store, e := platform.Open(ctx, dsn)
	if e != nil {
		return e
	}
	defer store.Close()
	if e = store.CheckMigrations(ctx); e != nil {
		return e
	}
	var result any = map[string]string{"status": "ok", "operation": command, "project_id": *project, "credential_batch_ref": *batch, "credential_version": *version}
	if command == "device-credential-import" {
		vault, e := credentialvault.Load(os.Getenv("KEEL_DEVICE_CREDENTIAL_MASTER_KEY_FILE"))
		if e != nil || vault == nil {
			return errors.New("protected device credential master key required")
		}
		result, e = store.ImportDeviceCredential(ctx, vault, credentialvault.SecretIdentity{ProjectID: *project, Ref: *batch, Version: *version, Scope: *scope, ExpiresAt: expiry}, value, *previous, *reason)
		if e != nil {
			return errors.New("device credential import rejected; check BOM batch, current version, expiry and owner authority")
		}
	} else if e = store.RevokeDeviceCredential(ctx, *project, *batch, *scope, *version, *reason); e != nil {
		return errors.New("device credential revocation rejected")
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
