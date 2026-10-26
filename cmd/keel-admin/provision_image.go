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

func runProvisionOverlay(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	publicFile := flags.String("public-key-file", "", "independently trusted platform signing public key")
	var manifestFile, directory, output, overlayFile *string
	if command == "provision-overlay-create" {
		manifestFile = flags.String("manifest-file", "", "signed independently approved helper-set config manifest")
		directory = flags.String("artifact-directory", "", "directory containing exact <config/helper_media_id>.bin files")
		output = flags.String("output-file", "", "new gzip/newc overlay in an operator-owned protected directory")
	} else {
		overlayFile = flags.String("overlay-file", "", "regular installer helper gzip/newc archive")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *publicFile == "" {
		return errors.New("independently trusted --public-key-file is required; no positional arguments")
	}
	key, err := mediamanifest.LoadPublicKey(*publicFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var result *provisionimage.Result
	if command == "provision-overlay-create" {
		if *manifestFile == "" || *directory == "" || *output == "" {
			return errors.New("--manifest-file, --artifact-directory and new --output-file are required")
		}
		raw, readErr := readManifestFile(*manifestFile)
		if readErr != nil {
			return readErr
		}
		result, err = mediamanifest.CreateInstallerOverlay(ctx, raw, key, *directory, *output)
	} else {
		if *overlayFile == "" {
			return errors.New("--overlay-file is required")
		}
		result, err = mediamanifest.VerifyInstallerOverlay(ctx, *overlayFile, key)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "operation": command, "sha256": result.SHA256, "size": result.Size, "metadata": result.Metadata, "helper_set_bytes_verified": true, "helper_bytes_verified": true, "other_image_role_bytes_verified": false, "installation_performed": false, "attestation": "offline helper overlay only; independently accepted bootable image, live approval and installation permission are still required"})
}
