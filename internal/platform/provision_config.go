package platform

import "github.com/enzoOvOyu/keel/internal/mediamanifest"

// ConfigureProvisionInstall is independent of source preparation. It loads
// only the out-of-band public trust anchor, never a signing private key.
func (s *Store) ConfigureProvisionInstall(enabled bool, publicKeyFile string) error {
	s.ProvisionInstallEnabled = false
	s.ProvisionImagePublicKey = nil
	if !enabled {
		return nil
	}
	key, err := mediamanifest.LoadPublicKey(publicKeyFile)
	if err != nil {
		return err
	}
	s.ProvisionImagePublicKey = key
	s.ProvisionInstallEnabled = true
	return nil
}
