package provisionimage

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

func writerRuntimeFixture(t *testing.T, arch string) (mediaboot.RuntimeSet, []Input) {
	t.Helper()
	set, inputs := runtimeFixture(t, arch)
	set.Schema = mediaboot.WriterRuntimeSetSchema
	set.Writer = &mediaboot.WriterTools{}
	for i, c := range []*mediaboot.RuntimeComponent{&set.Writer.UnsquashFS, &set.Writer.MkfsExt4, &set.Writer.MkfsVFAT} {
		raw := append(fixtureELF(arch), byte(i+5))
		c.MediaID = strings.Repeat(string(rune('6'+i)), 32)
		c.SHA256 = rawSHA(raw)
		inputs = append(inputs, Input{MediaID: c.MediaID, SHA256: c.SHA256, Size: int64(len(raw)), File: bytes.NewReader(raw)})
	}
	return set, inputs
}

func TestInitrdWriterInputsExactBoundedArchive(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			set, inputs := writerRuntimeFixture(t, arch)
			config, _ := json.Marshal(set)
			var out bytes.Buffer
			result, err := WriteInitrd(context.Background(), &out, set, []byte(`{"signed_closure_verified_by_outer_layer":true}`), config, inputs)
			if err != nil {
				t.Fatal(err)
			}
			verified, err := ReadInitrd(context.Background(), bytes.NewReader(out.Bytes()))
			if err != nil || !verified.Metadata.Runtime.Equal(set) || verified.Metadata.Schema != WriterInitrdSchema || len(verified.Metadata.Components) != 8 {
				t.Fatal("writer runtime roundtrip", err)
			}
			if result.SHA256 != rawSHA(out.Bytes()) {
				t.Fatal("actual archive hash")
			}
			for i, path := range []string{UnsquashFSPath, MkfsExt4Path, MkfsVFATPath} {
				if verified.Metadata.Components[i+5].Path != path || verified.Metadata.Components[i+5].SHA256 != inputs[i+5].SHA256 {
					t.Fatal("tool path/hash changed")
				}
			}
			var repeated bytes.Buffer
			second, err := WriteInitrd(context.Background(), &repeated, set, []byte(`{"signed_closure_verified_by_outer_layer":true}`), config, inputs)
			if err != nil || second.SHA256 != result.SHA256 || !bytes.Equal(out.Bytes(), repeated.Bytes()) {
				t.Fatal("non-deterministic writer-input archive", err)
			}
			if _, err = ReadInitrd(context.Background(), bytes.NewReader(append(out.Bytes(), []byte("unapproved appended archive")...))); err == nil {
				t.Fatal("unapproved appended archive accepted")
			}
		})
	}
}

func TestInitrdWriterToolWrongArchitectureMissingAndChangedBytes(t *testing.T) {
	for _, kind := range []string{"wrong_arch", "missing_tool", "changed_bytes", "v1_downgrade"} {
		t.Run(kind, func(t *testing.T) {
			set, inputs := writerRuntimeFixture(t, "amd64")
			switch kind {
			case "wrong_arch":
				raw := fixtureELF("arm64")
				inputs[5].File = bytes.NewReader(raw)
				inputs[5].Size = int64(len(raw))
				inputs[5].SHA256 = rawSHA(raw)
				set.Writer.UnsquashFS.SHA256 = rawSHA(raw)
			case "missing_tool":
				inputs = inputs[:7]
			case "changed_bytes":
				inputs[6].File = bytes.NewReader([]byte("changed approved executable"))
			case "v1_downgrade":
				set.Schema = mediaboot.RuntimeSetSchema
			}
			config, _ := json.Marshal(set)
			var out bytes.Buffer
			if _, err := WriteInitrd(context.Background(), &out, set, []byte("non-executed manifest format fixture"), config, inputs); err == nil {
				t.Fatal("unapproved writer inputs accepted")
			}
			if out.Len() != 0 {
				t.Fatal("failed input validation emitted an archive")
			}
		})
	}
}
