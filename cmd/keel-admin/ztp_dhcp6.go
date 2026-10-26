package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"golang.org/x/sys/unix"
)

type dhcp6CandidateInput struct {
	Version  string                           `json:"version"`
	Live     json.RawMessage                  `json:"live_config"`
	Subnet   string                           `json:"subnet"`
	Owner    string                           `json:"project_id"`
	Bindings []fabricboot.KeaDHCP6Reservation `json:"bindings"`
	URIs     []string                         `json:"bootstrap_origins"`
}
type dhcp6ProbeInput struct {
	Version        string                      `json:"version"`
	Config         fabricboot.DHCP6ProbeConfig `json:"config"`
	URIs           []string                    `json:"bootstrap_origins"`
	CanaryReserved bool                        `json:"canary_reserved"`
}

func runZTPDHCP6(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input := flags.String("input-file", "", "private operator input JSON")
	output := flags.String("output-file", "", "new private candidate file (candidate command only)")
	confirmed := flags.Bool("confirm-canary", false, "confirm the configured canary DUID and address are reserved and isolated")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *input == "" {
		return errors.New("an explicit --input-file is required")
	}
	if command == "ztp-dhcp6-candidate" && (*output == "" || *confirmed) || command == "ztp-dhcp6-probe" && (*output != "" || !*confirmed) {
		return errors.New("candidate requires --output-file; probe requires --confirm-canary and no output file")
	}
	raw, err := readDHCP6OperatorInput(*input)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if command == "ztp-dhcp6-candidate" {
		var in dhcp6CandidateInput
		if decoder.Decode(&in) != nil || in.Version != "kea-dhcp6-candidate/v1" || decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid DHCPv6 candidate input")
		}
		candidate, err := fabricboot.MergeBootstrap136(in.Live, in.Subnet, in.Owner, in.Bindings, in.URIs)
		if err != nil {
			return err
		}
		// Full live configs may contain unrelated secrets. Never print them or
		// overwrite an existing candidate; only create an operator-owned 0600 file.
		file, err := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return errors.New("cannot create a new private candidate file")
		}
		_, err = file.Write(candidate)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return errors.New("candidate output incomplete")
		}
		if closeErr != nil {
			return closeErr
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"operation": command, "state": "candidate_only", "before_sha256": fabricboot.KeaConfigDigest(in.Live), "candidate_sha256": fabricboot.KeaConfigDigest(candidate), "binding_count": len(in.Bindings), "config_applied": false, "device_managed": false})
	}
	var in dhcp6ProbeInput
	if decoder.Decode(&in) != nil || in.Version != "dhcp6-canary/v1" || !in.CanaryReserved || decoder.Decode(new(any)) != io.EOF {
		return errors.New("explicit private reserved-canary input is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	proof, err := in.Config.Verify136(ctx, in.URIs)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"operation": command, "state": "observed", "source": "operator_canary", "proof": proof, "device_managed": false, "np_ready": false})
}
func readDHCP6OperatorInput(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("private DHCPv6 input unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("DHCPv6 input must be a private regular file")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || owner.Nlink != 1 {
		return nil, errors.New("DHCPv6 input must have a single operator-owned link")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, errors.New("DHCPv6 input unreadable or too large")
	}
	return raw, nil
}
