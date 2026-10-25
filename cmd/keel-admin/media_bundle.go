package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"

	"github.com/enzoOvOyu/keel/internal/mediamanifest"
)

func runMediaBundle(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	public := flags.String("public-key-file", "", "independently trusted PKIX public key file")
	var manifestPath, directory, output, bundle *string
	if command == "media-bundle-create" {
		manifestPath = flags.String("manifest-file", "", "signed platform manifest JSON")
		directory = flags.String("artifact-directory", "", "directory with every exact <media_id>.bin")
		output = flags.String("output-file", "", "new archive path; existing output is never replaced")
	} else {
		bundle = flags.String("bundle-file", "", "regular Keel offline artifact USTAR file")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *public == "" {
		return errors.New("independently trusted --public-key-file is required; no positional arguments")
	}
	key, err := mediamanifest.LoadPublicKey(*public)
	if err != nil {
		return err
	}
	var verified *mediamanifest.Envelope
	if command == "media-bundle-create" {
		if *manifestPath == "" || *directory == "" || *output == "" {
			return errors.New("--manifest-file, --artifact-directory and new --output-file are required")
		}
		raw, readErr := readManifestFile(*manifestPath)
		if readErr != nil {
			return readErr
		}
		verified, err = mediamanifest.CreateBundle(raw, key, *directory, *output)
	} else {
		if *bundle == "" {
			return errors.New("--bundle-file is required")
		}
		verified, err = mediamanifest.VerifyBundle(*bundle, key)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "operation": command, "key_id": verified.Payload.KeyID, "root_id": verified.Payload.RootID, "artifact_count": len(verified.Payload.Artifacts), "artifact_bytes_verified": true, "attestation": "platform approval metadata at export time; not vendor signing, installation or live authorization"})
}
