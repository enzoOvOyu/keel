package main

import (
	"encoding/json"
	"errors"
	"flag"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

func runMediaManifest(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if command == "media-signing-keygen" {
		private := flags.String("private-key-file", "", "new owner-only PKCS8 private key file")
		public := flags.String("public-key-file", "", "new PKIX public key file for independent provisioning")
		if err := flags.Parse(args); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		id, err := mediamanifest.GenerateKeyFiles(*private, *public)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "operation": command, "key_id": id})
	}
	manifest := flags.String("manifest-file", "", "signed manifest JSON file")
	public := flags.String("public-key-file", "", "independently trusted PKIX public key file")
	directory := flags.String("artifact-directory", "", "optional directory containing <media_id>.bin for every artifact")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *manifest == "" || *public == "" {
		return errors.New("--manifest-file and --public-key-file are required")
	}
	key, err := mediamanifest.LoadPublicKey(*public)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*manifest, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("cannot open manifest file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("manifest must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, mediamanifest.MaxBytes+1))
	if err != nil {
		return errors.New("cannot read manifest file")
	}
	verified, err := mediamanifest.Verify(raw, key)
	if err != nil {
		return err
	}
	if *directory != "" {
		if err = mediamanifest.VerifyFiles(verified.Payload, *directory); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "operation": command, "key_id": verified.Payload.KeyID, "root_id": verified.Payload.RootID, "artifact_count": len(verified.Payload.Artifacts), "artifact_bytes_verified": *directory != "", "attestation": "platform approval metadata at export time; not vendor signing or live authorization"})
}
