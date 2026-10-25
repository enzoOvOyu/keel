package mediamanifest

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateBundleDir(t testing.TB) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func bundleFixture(t *testing.T) ([]byte, ed25519.PublicKey, Payload, string) {
	t.Helper()
	signer, key, payload := fixture(t)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.MarshalIndent(signed, "", "  ")
	directory := privateBundleDir(t)
	for _, node := range signed.Payload.Artifacts {
		if err := os.WriteFile(filepath.Join(directory, node.MediaID+".bin"), []byte(node.Name+" bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return raw, key, signed.Payload, directory
}
func TestMediaBundleCompleteTrustAndBytes(t *testing.T) {
	raw, key, payload, dir := bundleFixture(t)
	path := filepath.Join(privateBundleDir(t), "bundle.tar")
	if _, err := CreateBundle(raw, key, dir, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 || info.Size() != bundleSize(len(raw), payload) {
		t.Fatal("bundle size or permissions invalid")
	}
	verified, err := VerifyBundle(path, key)
	if err != nil || verified.Payload.RootID != payload.RootID {
		t.Fatal("cannot verify complete archive", err)
	}
	original, _ := os.ReadFile(path)
	if _, err = CreateBundle(raw, key, dir, path); err == nil {
		t.Fatal("replaced an existing archive")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("changed existing archive")
	}
	_, wrong, _ := fixture(t)
	if _, err = VerifyBundle(path, wrong); err == nil {
		t.Fatal("trusted a foreign signature")
	}
	if _, err = CreateBundle(raw, wrong, dir, filepath.Join(privateBundleDir(t), "wrong.tar")); err == nil {
		t.Fatal("packaged an untrusted manifest")
	}
	// Every output carries the exact input manifest rather than silently changing it.
	reader := tar.NewReader(bytes.NewReader(original))
	header, err := reader.Next()
	if err != nil || header.Name != bundleManifestName {
		t.Fatal("missing manifest")
	}
	stored, _ := io.ReadAll(reader)
	if !bytes.Equal(stored, raw) {
		t.Fatal("rewrote signed manifest")
	}
}
func TestMediaBundleAtomicFailureAndInputTypes(t *testing.T) {
	raw, key, payload, dir := bundleFixture(t)
	root := privateBundleDir(t)
	output := filepath.Join(root, "bundle.tar")
	source := filepath.Join(dir, payload.Artifacts[1].MediaID+".bin")
	for _, kind := range []string{"corrupt", "short", "long", "missing", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			os.Remove(source)
			good := []byte(payload.Artifacts[1].Name + " bytes")
			switch kind {
			case "corrupt":
				os.WriteFile(source, bytes.Repeat([]byte{'x'}, len(good)), 0600)
			case "short":
				os.WriteFile(source, good[:len(good)-1], 0600)
			case "long":
				os.WriteFile(source, append(good, 'x'), 0600)
			case "symlink":
				os.Symlink(filepath.Join(dir, payload.Artifacts[0].MediaID+".bin"), source)
			case "directory":
				os.Mkdir(source, 0700)
			}
			if _, err := CreateBundle(raw, key, dir, output); err == nil {
				t.Fatal("accepted invalid artifact")
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatal("published failed output")
			}
			staging, _ := filepath.Glob(filepath.Join(root, ".keel-media-bundle-*"))
			if len(staging) != 0 {
				t.Fatal("left a partial private staging file")
			}
			os.Remove(source)
		})
	}
	os.WriteFile(source, []byte(payload.Artifacts[1].Name+" bytes"), 0600)
	target := filepath.Join(root, "existing.txt")
	os.WriteFile(target, []byte("preserve"), 0600)
	os.Symlink(target, output)
	if _, err := CreateBundle(raw, key, dir, output); err == nil {
		t.Fatal("replaced output symlink")
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "preserve" {
		t.Fatal("overwrote symlink target")
	}
}
func TestMediaBundleRejectsMalformedArchives(t *testing.T) {
	raw, key, payload, dir := bundleFixture(t)
	goodPath := filepath.Join(privateBundleDir(t), "good.tar")
	if _, err := CreateBundle(raw, key, dir, goodPath); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(goodPath)
	check := func(t *testing.T, data []byte) {
		t.Helper()
		path := filepath.Join(privateBundleDir(t), "bad.tar")
		os.WriteFile(path, data, 0600)
		if _, err := VerifyBundle(path, key); err == nil {
			t.Fatal("accepted malformed archive")
		}
	}
	for name, data := range map[string][]byte{
		"truncated header": good[:100], "missing final block": good[:len(good)-512], "truncated artifact": good[:len(good)/2], "trailing bytes": append(bytes.Clone(good), 'x'), "concatenated archive": append(bytes.Clone(good), good...),
	} {
		t.Run(name, func(t *testing.T) { check(t, data) })
	}
	for _, kind := range []string{"traversal", "absolute", "link", "hardlink", "directory", "reordered", "duplicate", "extra", "size", "content", "manifest", "pax"} {
		t.Run(kind, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := tar.NewWriter(&buffer)
			reader := tar.NewReader(bytes.NewReader(good))
			index := 0
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(reader)
				header.Format = tar.FormatUSTAR
				if index == 1 {
					switch kind {
					case "traversal":
						header.Name = "../" + header.Name
					case "absolute":
						header.Name = "/" + header.Name
					case "link":
						header.Typeflag = tar.TypeSymlink
						header.Linkname = "/etc/passwd"
						header.Size = 0
						body = nil
					case "hardlink":
						header.Typeflag = tar.TypeLink
						header.Linkname = "manifest.json"
						header.Size = 0
						body = nil
					case "directory":
						header.Typeflag = tar.TypeDir
						header.Name += "/"
						header.Size = 0
						body = nil
					case "reordered", "duplicate":
						header.Name = payload.Artifacts[1].MediaID + ".bin"
					case "size":
						header.Size++
						body = append(body, 'x')
					case "content":
						body[0] ^= 1
					case "pax":
						header.Format = tar.FormatPAX
						header.PAXRecords = map[string]string{"comment": "disallowed extension"}
					}
				}
				if index == 0 && kind == "manifest" {
					body = bytes.Replace(body, []byte(`"algorithm": "ed25519"`), []byte(`"algorithm": "invalid"`), 1)
					header.Size = int64(len(body))
				}
				if err := writer.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write(body); err != nil {
					t.Fatal(err)
				}
				index++
			}
			if kind == "extra" {
				writer.WriteHeader(bundleHeader("extra.bin", 0))
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			check(t, buffer.Bytes())
		})
	}
	// Nonregular bundle inputs cannot block waiting on a FIFO or follow a link.
	link := filepath.Join(privateBundleDir(t), "link.tar")
	os.Symlink(goodPath, link)
	if _, err := VerifyBundle(link, key); err == nil {
		t.Fatal("followed a bundle symlink")
	}
	if _, err := VerifyBundle(dir, key); err == nil {
		t.Fatal("opened directory as bundle")
	}
}
func TestMediaBundleExactSchemaAliasesRejected(t *testing.T) {
	raw, key, _, dir := bundleFixture(t)
	for _, field := range []string{"algorithm", "payload", "artifacts", "dependencies"} {
		altered := strings.Replace(string(raw), `"`+field+`":`, `"`+strings.ToUpper(field)+`":`, 1)
		if _, err := CreateBundle([]byte(altered), key, dir, filepath.Join(privateBundleDir(t), "alias.tar")); err == nil {
			t.Fatal("packaged an aliased schema")
		}
	}
}

func TestMediaBundleEmptyArtifactAndPhysicalHeaders(t *testing.T) {
	signer, key, payload := fixture(t)
	sum := sha256.Sum256(nil)
	payload.Artifacts[0].Size = 0
	payload.Artifacts[0].SHA256 = hex.EncodeToString(sum[:])
	payload.Artifacts[1].Dependencies[0].SHA256 = payload.Artifacts[0].SHA256
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(signed)
	dir := privateBundleDir(t)
	os.WriteFile(filepath.Join(dir, payload.Artifacts[0].MediaID+".bin"), nil, 0600)
	os.WriteFile(filepath.Join(dir, payload.Artifacts[1].MediaID+".bin"), []byte(payload.Artifacts[1].Name+" bytes"), 0600)
	path := filepath.Join(privateBundleDir(t), "empty-artifact.tar")
	if _, err = CreateBundle(raw, key, dir, path); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyBundle(path, key); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(path)
	for name, offset := range map[string]int{"manifest padding": 512 + len(raw), "final markers": len(good) - 1} {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Clone(good)
			changed[offset] = 1
			out := filepath.Join(privateBundleDir(t), "invalid.tar")
			os.WriteFile(out, changed, 0600)
			if _, err := VerifyBundle(out, key); err == nil {
				t.Fatal("accepted hidden data in padding or markers")
			}
		})
	}
	// Reject physical PAX headers before generic archive/tar can hide them.
	var pax bytes.Buffer
	writer := tar.NewWriter(&pax)
	header := bundleHeader("manifest.json", int64(len(raw)))
	header.Format = tar.FormatPAX
	header.PAXRecords = map[string]string{"comment": "hidden metadata"}
	writer.WriteHeader(header)
	writer.Write(raw)
	writer.Close()
	if _, err := readBundleHeader(bytes.NewReader(pax.Bytes())); err == nil {
		t.Fatal("skipped hidden extension header")
	}
	fifo := filepath.Join(privateBundleDir(t), "bundle.fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBundle(fifo, key); err == nil {
		t.Fatal("accepted FIFO")
	}
}

func TestMediaBundleOutputDirectoryTrust(t *testing.T) {
	raw, key, _, source := bundleFixture(t)
	directory := privateBundleDir(t)
	if err := os.Chmod(directory, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBundle(raw, key, source, filepath.Join(directory, "bundle.tar")); err == nil {
		t.Fatal("published through an untrusted writable parent")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(privateBundleDir(t), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateBundle(raw, key, source, filepath.Join(alias, "bundle.tar")); err == nil {
		t.Fatal("published through a symlink output directory")
	}
}
