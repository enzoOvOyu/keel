package platform

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// A preview token is bound to the authenticated session, project and exact
// archive manifest. It proves a read-only preview happened without persisting
// upload bytes or preview state. Commit still revalidates in its transaction.
func bomPreviewToken(a *AuthSession, projectID string, files map[string][]byte) string {
	manifest := sha256.Sum256(files["manifest.json"])
	h := hmac.New(sha256.New, []byte(a.ID))
	h.Write([]byte("keel-bom-preview-v1\x00"))
	h.Write([]byte(projectID))
	h.Write([]byte{0})
	h.Write(manifest[:])
	h.Write([]byte{0})
	h.Write([]byte(BOMContentHash(files)))
	return hex.EncodeToString(h.Sum(nil))
}
func validBOMPreviewToken(a *AuthSession, projectID string, files map[string][]byte, token string) bool {
	provided, err := hex.DecodeString(token)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	expected, _ := hex.DecodeString(bomPreviewToken(a, projectID, files))
	return hmac.Equal(provided, expected)
}
