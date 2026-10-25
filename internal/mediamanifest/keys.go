package mediamanifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func GenerateKeyFiles(privatePath, publicPath string) (string, error) {
	if privatePath == "" || publicPath == "" || filepath.Clean(privatePath) == filepath.Clean(publicPath) {
		return "", errors.New("distinct private and public key file paths are required")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", errors.New("could not generate signing key")
	}
	secret, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return "", err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", err
	}
	priv, err := os.OpenFile(privatePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", errors.New("cannot create private key file; existing files are never replaced")
	}
	defer priv.Close()
	pub, err := os.OpenFile(publicPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		os.Remove(privatePath)
		return "", errors.New("cannot create public key file; existing files are never replaced")
	}
	defer pub.Close()
	if err = pem.Encode(priv, &pem.Block{Type: "PRIVATE KEY", Bytes: secret}); err == nil {
		err = pem.Encode(pub, &pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	}
	if err == nil {
		err = priv.Sync()
	}
	if err == nil {
		err = pub.Sync()
	}
	if err != nil {
		os.Remove(privatePath)
		os.Remove(publicPath)
		return "", errors.New("could not persist signing key pair")
	}
	return keyID(public), nil
}
func LoadSigner(path string) (*Signer, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot open private signing key file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.New("cannot inspect private signing key file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || st.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("private signing key requires an owner-only regular file owned by the API user")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errors.New("invalid private signing key file")
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return nil, errors.New("expected one PKCS8 private key PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid PKCS8 private key")
	}
	private, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(private) != ed25519.PrivateKeySize {
		return nil, errors.New("manifest signing requires Ed25519")
	}
	return &Signer{private: private}, nil
}
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot open trusted public key file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("trusted public key must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errors.New("invalid trusted public key file")
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" || len(rest) != 0 {
		return nil, errors.New("expected one PKIX public key PEM block")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid trusted public key")
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("trusted key must be Ed25519")
	}
	return public, nil
}

// VerifyFiles checks the exact fixed filenames; no URI or path from the manifest
// is executed or opened, and links/non-regular files are rejected.
func VerifyFiles(payload Payload, directory string) error {
	if err := validate(payload); err != nil {
		return err
	}
	if directory == "" {
		return errors.New("artifact directory is required for byte verification")
	}
	for _, node := range payload.Artifacts {
		path := filepath.Join(directory, node.MediaID+".bin")
		file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return identityError(node.MediaID)
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != node.Size {
			file.Close()
			return identityError(node.MediaID)
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(file, node.Size+1))
		file.Close()
		if err != nil || size != node.Size || hex.EncodeToString(hash.Sum(nil)) != node.SHA256 {
			return identityError(node.MediaID)
		}
	}
	return nil
}
