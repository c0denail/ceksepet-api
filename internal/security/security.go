package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("invalid token")

func HashPIN(pin string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	return string(hash), err
}

func ComparePIN(hash, pin string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)) == nil
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func RandomToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func RandomOTP() string { return randomDigits(6) }

func RandomUsernameSuffix() string { return randomDigits(5) }

func randomDigits(length int) string {
	result := make([]byte, length)
	limit := byte(250)
	for index := range result {
		for {
			candidate := make([]byte, 1)
			if _, err := rand.Read(candidate); err != nil {
				panic("crypto/rand unavailable: " + err.Error())
			}
			if candidate[0] < limit {
				result[index] = '0' + candidate[0]%10
				break
			}
		}
	}
	return string(result)
}

func HashValue(secret, scope, value string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(scope))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}

func ValuesEqual(expectedHex, actualHex string) bool {
	expected, err1 := hex.DecodeString(expectedHex)
	actual, err2 := hex.DecodeString(actualHex)
	return err1 == nil && err2 == nil && hmac.Equal(expected, actual)
}

func StringsEqual(expected, actual string) bool {
	expectedHash := sha256.Sum256([]byte(expected))
	actualHash := sha256.Sum256([]byte(actual))
	return hmac.Equal(expectedHash[:], actualHash[:])
}

func ParseBearer(value string) (string, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", ErrInvalidToken
	}
	return parts[1], nil
}

func IsSixDigits(value string) bool {
	if len(value) != 6 {
		return false
	}
	_, err := strconv.Atoi(value)
	return err == nil
}
