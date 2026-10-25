package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func runAgentIdentity(command string, args []string) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	directory := f.String("directory", "", "new private CA directory")
	caFile := f.String("ca-certificate", "", "existing public Agent CA certificate")
	project := f.String("project", "", "project ID")
	machine := f.String("machine", "", "machine ID")
	id := f.String("id", "", "enrollment or credential ID")
	reason := f.String("reason", "", "operator authorization reason")
	ttl := f.Duration("expires-in", 5*time.Minute, "one-time enrollment lifetime, 1..15 minutes")
	output := f.String("token-output", "", "new absolute private token file; never stdout")
	version := f.Int64("expected-version", 0, "credential version for revocation")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected Agent positional arguments")
	}
	allowed := map[string]map[string]bool{"agent-ca-init": {"directory": true}, "agent-enrollment-create": {"ca-certificate": true, "project": true, "machine": true, "reason": true, "expires-in": true, "token-output": true}, "agent-enrollment-cancel": {"project": true, "id": true, "reason": true}, "agent-revoke": {"project": true, "id": true, "reason": true, "expected-version": true}, "agent-credential-status": {"project": true, "id": true}}
	invalid := false
	f.Visit(func(field *flag.Flag) {
		if !allowed[command][field.Name] {
			invalid = true
		}
	})
	if invalid {
		return errors.New("unsupported Agent command flag")
	}
	if command == "agent-ca-init" {
		if !filepath.IsAbs(*directory) || filepath.Clean(*directory) != *directory {
			return agentpki.ErrInvalid
		}
		if e := os.Mkdir(*directory, 0700); e != nil {
			return agentpki.ErrInvalid
		}
		success := false
		defer func() {
			if !success {
				_ = os.Remove(filepath.Join(*directory, "ca-key.pem"))
				_ = os.Remove(filepath.Join(*directory, "ca.pem"))
				_ = os.Remove(*directory)
			}
		}()
		public, private, e := agentpki.NewAuthority(time.Now())
		if e != nil {
			return e
		}
		if e = agentpki.WriteNew(filepath.Join(*directory, "ca-key.pem"), private); e != nil {
			return e
		}
		if e = agentpki.WriteNew(filepath.Join(*directory, "ca.pem"), public); e != nil {
			return e
		}
		certificate, e := agentpki.Certificate(public)
		if e != nil {
			return e
		}
		success = true
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"operation": command, "authority_id": agentpki.Hash(certificate.Raw)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	store, e := platform.Open(ctx, dsn)
	if e != nil {
		return e
	}
	defer store.Close()
	if e = store.CheckMigrations(ctx); e != nil {
		return e
	}
	switch command {
	case "agent-credential-status":
		status, e := store.AgentCredentialStatus(ctx, *project, *id)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(status)
	case "agent-enrollment-create":
		raw, e := agentpki.ReadFile(*caFile, 16384, false)
		if e != nil {
			return e
		}
		ca, e := agentpki.Certificate(raw)
		if e != nil || !ca.IsCA || !ca.BasicConstraintsValid || ca.CheckSignatureFrom(ca) != nil || time.Now().Before(ca.NotBefore) || !ca.NotAfter.After(time.Now().Add(time.Hour)) {
			return agentpki.ErrInvalid
		}
		grant, token, e := store.IssueAgentEnrollment(ctx, *project, *machine, agentpki.Hash(ca.Raw), *reason, *ttl)
		if e != nil {
			return e
		}
		if e = agentpki.WriteNew(*output, []byte(token+"\n")); e != nil {
			_ = store.CancelAgentEnrollment(ctx, *project, grant.ID, "protected token handoff failed")
			return agentpki.ErrInvalid
		}
		return json.NewEncoder(os.Stdout).Encode(grant)
	case "agent-enrollment-cancel":
		if e = store.CancelAgentEnrollment(ctx, *project, *id, *reason); e != nil {
			return e
		}
	case "agent-revoke":
		if e = store.RevokeAgentCredential(ctx, *project, *id, *version, *reason); e != nil {
			return e
		}
	default:
		return errors.New("unsupported Agent command")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"operation": command, "status": "ok", "id": *id})
}
