package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

// RandomCode is a 6-digit one-time code. We never store this as plain text.
func RandomCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 1_000_000
	return fmt.Sprintf("%06d", n), nil
}

// HashCode binds the code to one email and one purpose with the server secret.
func HashCode(secret, email, purpose, code string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	mac.Write([]byte("|"))
	mac.Write([]byte(purpose))
	mac.Write([]byte("|"))
	mac.Write([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

func CheckCode(secret, email, purpose, code, hash string) bool {
	want := HashCode(secret, email, purpose, code)
	return hmac.Equal([]byte(want), []byte(hash))
}
