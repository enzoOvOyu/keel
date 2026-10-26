package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
)

func runProvisionInitrd(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	keyFile := flags.String("public-key-file", "", "independently trusted platform signing public key")
	var manifestFile, directory, output, archive *string
	if command == "provision-initrd-create" {
		manifestFile = flags.String("manifest-file", "", "signed independent runtime-set input manifest")
		directory = flags.String("artifact-directory", "", "directory containing exact <media_id>.bin inputs")
		output = flags.String("output-file", "", "new initrd gzip/newc in a protected operator-owned directory")
	} else {
		archive = flags.String("initrd-file", "", "regular read-only installer initrd archive")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *keyFile == "" {
		return errors.New("independently trusted --public-key-file is required; no positional arguments")
	}
	key, err := mediamanifest.LoadPublicKey(*keyFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var result *provisionimage.InitrdResult
	if command == "provision-initrd-create" {
		if *manifestFile == "" || *directory == "" || *output == "" {
			return errors.New("--manifest-file, --artifact-directory and new --output-file are required")
		}
		raw, err := readManifestFile(*manifestFile)
		if err != nil {
			return err
		}
		result, err = mediamanifest.CreateInstallerInitrd(ctx, raw, key, *directory, *output)
		if err != nil {
			return err
		}
	} else {
		if *archive == "" {
			return errors.New("--initrd-file is required")
		}
		result, err = mediamanifest.VerifyInstallerInitrd(ctx, *archive, key)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "operation": command, "sha256": result.SHA256, "size": result.Size, "metadata": result.Metadata, "runtime_input_bytes_verified": true, "image_boot_verified": false, "installation_performed": false, "attestation": "offline read-only runtime archive; independent final-media approval, kernel/driver acceptance and live installation permission remain required"})
}
