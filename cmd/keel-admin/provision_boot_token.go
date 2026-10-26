package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

// This operator-only path never prints the secret or embeds it in public PXE
// files. The protected file still needs an explicit trusted out-of-band handoff.
func runProvisionBootToken(command string, args []string) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	project := f.String("project", "", "project ID")
	installation := f.String("installation", "", "active installation ID")
	reason := f.String("reason", "", "operator authorization reason")
	var output, tokenID *string
	var ttl *time.Duration
	if command == "provision-boot-token-issue" {
		output = f.String("output-file", "", "new 0600 file in existing owner-only directory; never stdout")
		ttl = f.Duration("expires-in", 5*time.Minute, "single-use enrollment TTL, 1..10 minutes")
	} else {
		tokenID = f.String("token-id", "", "unconsumed token ID to revoke")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *project == "" || *installation == "" || *reason == "" || (output != nil && *output == "") || (tokenID != nil && *tokenID == "") {
		return errors.New("project, installation, reason and operation-specific flags are required; no positional arguments")
	}
	if os.Getenv("KEEL_DATABASE_URL") == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := platform.Open(ctx, os.Getenv("KEEL_DATABASE_URL"))
	if err != nil {
		return errors.New("operator database unavailable")
	}
	defer store.Close()
	if err = store.CheckMigrations(ctx); err != nil {
		return err
	}
	if tokenID != nil {
		if err = store.RevokeProvisionBootToken(ctx, *project, *installation, *tokenID, *reason); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"operation": command, "status": "ok", "token_id": *tokenID})
	}
	metadata, secret, err := store.IssueProvisionBootToken(ctx, *project, *installation, *reason, *ttl)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(provisionboot.Bootstrap{Schema: provisionboot.BootstrapSchema, Token: secret, Binding: *metadata})
	if err == nil {
		err = agentpki.WriteNew(*output, append(raw, '\n'))
	}
	if err != nil {
		revokeCtx, revokeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer revokeCancel()
		if revokeErr := store.RevokeProvisionBootToken(revokeCtx, *project, *installation, metadata.ID, "protected bootstrap file publication failed"); revokeErr != nil {
			return errors.New("protected bootstrap publication failed and token revocation unconfirmed; revoke the latest token for this installation before reissuing")
		}
		return errors.New("protected bootstrap file publication failed; unused token revoked")
	}
	return json.NewEncoder(os.Stdout).Encode(metadata)
}
