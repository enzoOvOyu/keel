package provisionimage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

func runtimeFixture(t *testing.T, arch string) (mediaboot.RuntimeSet, []Input) {
	t.Helper()
	set := mediaboot.RuntimeSet{Schema: mediaboot.RuntimeSetSchema, Arch: arch}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []Input
	for i, c := range []*mediaboot.RuntimeComponent{&set.Init, &set.Preflight, &set.SSHHelper, &set.SMART, &set.BootCA} {
		raw := append(fixtureELF(arch), byte(i))
		if i == 4 {
			raw = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		}
		c.MediaID = strings.Repeat(string(rune('1'+i)), 32)
		c.SHA256 = rawSHA(raw)
		inputs = append(inputs, Input{MediaID: c.MediaID, SHA256: c.SHA256, Size: int64(len(raw)), File: bytes.NewReader(raw)})
	}
	return set, inputs
}
func TestInitrdCompleteReadOnlyRuntimeArchive(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			set, inputs := runtimeFixture(t, arch)
			config, _ := json.Marshal(set)
			manifest := []byte(`{"fixture":"authenticated at caller"}`)
			var out, again bytes.Buffer
			result, err := WriteInitrd(context.Background(), &out, set, manifest, config, inputs)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = WriteInitrd(context.Background(), &again, set, manifest, config, inputs); err != nil || !bytes.Equal(out.Bytes(), again.Bytes()) {
				t.Fatal("not reproducible", err)
			}
			verified, err := ReadInitrd(context.Background(), bytes.NewReader(out.Bytes()))
			if err != nil || verified.Metadata.Runtime != set || !bytes.Equal(verified.Manifest, manifest) || result.SHA256 != rawSHA(out.Bytes()) || result.Size != int64(out.Len()) {
				t.Fatal("archive roundtrip", err)
			}
			gz, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(gz)
			gz.Close()
			if err != nil {
				t.Fatal(err)
			}
			files := readNewc(t, raw)
			if len(files) != len(runtimeDirectories)+8 || files[InitPath].mode != 0100555 || files[SMARTPath].mode != 0100555 || files[BootCAPath].mode != 0100444 {
				t.Fatal("missing/unsafe runtime entries")
			}
			for _, bad := range [][]byte{out.Bytes()[:out.Len()-1], append(append([]byte(nil), out.Bytes()...), 1), append(append([]byte(nil), out.Bytes()...), out.Bytes()...)} {
				if _, err := ReadInitrd(context.Background(), bytes.NewReader(bad)); err == nil {
					t.Fatal("truncated/trailing/multistream archive")
				}
			}
		})
	}
}
func TestInitrdRejectsChangedInputAndInvalidCA(t *testing.T) {
	set, inputs := runtimeFixture(t, "amd64")
	config, _ := json.Marshal(set)
	raw := append(fixtureELF("amd64"), byte(0))
	inputs[0].File = &swappingBinary{Reader: bytes.NewReader(raw), original: raw}
	if _, err := WriteInitrd(context.Background(), io.Discard, set, []byte(`{}`), config, inputs); err == nil {
		t.Fatal("changed entry binary")
	}
	set, inputs = runtimeFixture(t, "amd64")
	config, _ = json.Marshal(set)
	bad := []byte("not a public CA")
	set.BootCA.SHA256 = rawSHA(bad)
	inputs[4] = Input{MediaID: set.BootCA.MediaID, SHA256: set.BootCA.SHA256, Size: int64(len(bad)), File: bytes.NewReader(bad)}
	config, _ = json.Marshal(set)
	if _, err := WriteInitrd(context.Background(), io.Discard, set, []byte(`{}`), config, inputs); err == nil {
		t.Fatal("approved text is not a CA")
	}
	set, inputs = runtimeFixture(t, "amd64")
	set.Arch = "arm64"
	config, _ = json.Marshal(set)
	if _, err := WriteInitrd(context.Background(), io.Discard, set, []byte(`{}`), config, inputs); err == nil {
		t.Fatal("wrong ELF architecture")
	}
}
