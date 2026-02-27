package hlskey

import (
	"crypto/hmac"
	"crypto/sha256"
)

func DevAES128Key(masterKey []byte, assetID string) [16]byte {
	mac := hmac.New(sha256.New, masterKey)
	_, _ = mac.Write([]byte("hls-key|" + assetID))
	sum := mac.Sum(nil)
	var out [16]byte
	copy(out[:], sum[:16])
	return out
}
