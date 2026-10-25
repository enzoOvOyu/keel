package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/auditevidence"
	"github.com/enzoOvOyu/keel/internal/platform"
	"golang.org/x/sys/unix"
)

func openAuditIndex(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("absolute canonical audit index path required")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("audit index unavailable")
	}
	file := os.NewFile(uintptr(fd), "audit-index")
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size < 0 || st.Size > auditevidence.MaxIndexBytes {
		file.Close()
		return nil, errors.New("audit index must be a bounded regular file")
	}
	return file, nil
}

func exportAuditFiles(ctx context.Context, store *platform.Store, signer *auditevidence.Signer, directory string) (result *auditevidence.Header, resultErr error) {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, errors.New("new absolute evidence directory required")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, errors.New("evidence directory must not already exist")
	}
	indexPath, sealPath := filepath.Join(directory, "audit-index.jsonl"), filepath.Join(directory, "seal.json")
	var indexCreated, sealCreated bool
	defer func() {
		if resultErr != nil {
			if indexCreated {
				os.Remove(indexPath)
			}
			if sealCreated {
				os.Remove(sealPath)
			}
			os.Remove(directory)
		}
	}()
	index, err := os.OpenFile(indexPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	indexCreated = true
	defer index.Close()
	envelope, err := store.ExportAuditEvidence(ctx, signer, index)
	if err != nil {
		return nil, err
	}
	if err = index.Sync(); err != nil {
		return nil, err
	}
	if err = index.Close(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	seal, err := os.OpenFile(sealPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	sealCreated = true
	defer seal.Close()
	if _, err = seal.Write(raw); err != nil {
		return nil, err
	}
	if err = seal.Sync(); err != nil {
		return nil, err
	}
	if err = seal.Close(); err != nil {
		return nil, err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return nil, err
	}
	parent, err := os.Open(filepath.Dir(directory))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = parent.Sync(); err != nil {
		return nil, err
	}
	return &envelope.Payload, nil
}

func runAuditEvidence(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	private := flags.String("private-key-file", "", "independently held protected signing key")
	public := flags.String("public-key-file", "", "independently trusted public key")
	output := flags.String("output-directory", "", "new private evidence directory")
	indexPath := flags.String("index-file", "", "canonical hash index")
	sealPath := flags.String("seal-file", "", "canonical signed seal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	allowed := map[string]map[string]bool{
		"audit-signing-keygen":   {"private-key-file": true, "public-key-file": true},
		"audit-evidence-export":  {"private-key-file": true, "output-directory": true},
		"audit-evidence-verify":  {"public-key-file": true, "index-file": true, "seal-file": true},
		"audit-evidence-compare": {"public-key-file": true, "index-file": true, "seal-file": true},
	}
	bad := false
	flags.Visit(func(f *flag.Flag) {
		if !allowed[command][f.Name] {
			bad = true
		}
	})
	if bad {
		return errors.New("unsupported audit evidence flag")
	}
	if command == "audit-signing-keygen" {
		if !filepath.IsAbs(*private) || !filepath.IsAbs(*public) {
			return errors.New("absolute private and public key output paths required")
		}
		id, err := auditevidence.GenerateKeyFiles(*private, *public)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ok", "key_id": id})
	}
	var index *os.File
	var seal []byte
	if command == "audit-evidence-verify" || command == "audit-evidence-compare" {
		var err error
		index, err = openAuditIndex(*indexPath)
		if err != nil {
			return err
		}
		defer index.Close()
		seal, err = agentpki.ReadFile(*sealPath, 16<<10, false)
		if err != nil {
			return errors.New("audit seal unavailable")
		}
		trusted, err := auditevidence.LoadPublicKey(*public)
		if err != nil {
			return errors.New("independent audit trust key unavailable")
		}
		if command == "audit-evidence-verify" {
			header, err := auditevidence.Verify(index, seal, trusted, nil)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"signature_and_index_verified": true, "snapshot": header})
		}
		// Fully authenticate the external evidence before opening a database.
		if _, err = auditevidence.Verify(index, seal, trusted, nil); err != nil {
			return err
		}
		if _, err = index.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, err := platform.Open(ctx, dsn)
	if err != nil {
		return errors.New("audit database unavailable")
	}
	defer store.Close()
	if err = store.CheckMigrations(ctx); err != nil {
		return err
	}
	switch command {
	case "audit-evidence-export":
		signer, e := auditevidence.LoadSigner(*private)
		if e != nil {
			return e
		}
		defer signer.Close()
		header, e := exportAuditFiles(ctx, store, signer, *output)
		if e != nil {
			return errors.New("audit evidence export failed; no completed snapshot was published")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"exported": true, "snapshot": header})
	case "audit-evidence-compare":
		trusted, e := auditevidence.LoadPublicKey(*public)
		if e != nil {
			return e
		}
		comparison, e := store.CompareAuditEvidence(ctx, index, seal, trusted)
		if e != nil {
			return errors.New("audit evidence comparison could not be completed")
		}
		if e = json.NewEncoder(os.Stdout).Encode(comparison); e != nil {
			return e
		}
		if !comparison.Unchanged {
			return errors.New("archived audit rows changed or are missing")
		}
		return nil
	default:
		return errors.New("unknown audit evidence command")
	}
}
