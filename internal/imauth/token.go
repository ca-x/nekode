package imauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

func NewToken() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return "nk_" + base64.RawURLEncoding.EncodeToString(buf)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TokenPrefix(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 10 {
		return token
	}
	return token[:10]
}
