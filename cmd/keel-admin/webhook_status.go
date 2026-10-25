package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/webhook"
)

func runWebhookStatus(args []string) error {
	flags := flag.NewFlagSet("webhook-delivery-status", flag.ContinueOnError)
	project := flags.String("project", "", "exact project ID")
	limit := flags.Int("limit", 50, "page size, 1..100")
	offset := flags.Int("offset", 0, "row offset, 0..100000")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || len(*project) != 32 || *limit < 1 || *limit > 100 || *offset < 0 || *offset > 100000 {
		return errors.New("valid --project/--limit/--offset required")
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := platform.Open(ctx, dsn)
	if err != nil {
		return errors.New("notification database unavailable")
	}
	defer store.Close()
	if err = store.CheckMigrations(ctx); err != nil {
		return err
	}
	page, err := store.ReadWebhookDeliveries(ctx, *project, *limit, *offset)
	if err != nil {
		return errors.New("notification delivery status rejected")
	}
	return json.NewEncoder(os.Stdout).Encode(page)
}

func runWebhookKeygen(args []string) error {
	flags := flag.NewFlagSet("webhook-signing-keygen", flag.ContinueOnError)
	output := flags.String("output", "", "new absolute protected signing-key path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	keyID, err := webhook.GenerateSigningKey(*output)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok", "key_id": keyID})
}
