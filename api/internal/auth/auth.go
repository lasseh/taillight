package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost   = 12
	apiKeyPrefix = "tl_"
	// base62Chars is the alphabet for API key generation.
	base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// apiKeyLen is the number of random base62 chars after the prefix.
	apiKeyLen = 43
	// displayPrefixLen is how many chars of the full key to store for display.
	displayPrefixLen = 10
)

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(password, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// dummyHash is a pre-computed bcrypt hash used to prevent timing-based
// username enumeration. Generated at startup with the same cost factor.
var dummyHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-safe-dummy"), bcryptCost)
	if err != nil {
		panic("failed to generate dummy bcrypt hash: " + err.Error())
	}
	dummyHash = string(h)
}

// DummyCheckPassword performs a bcrypt comparison that burns the same CPU
// time as a real password check, preventing timing-based user enumeration.
func DummyCheckPassword(password string) {
	_ = CheckPassword(password, dummyHash)
}

// GenerateSessionToken returns a cryptographically random session token
// and its SHA-256 hex hash. The raw token is sent to the client; the hash
// is stored in the database.
func GenerateSessionToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	raw = base64.URLEncoding.EncodeToString(b)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 hex digest of a raw token string.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// GenerateAPIKey creates a new API key with the "tl_" prefix followed by
// 43 base62 characters. Returns the full key, its SHA-256 hash, and a
// short display prefix.
func GenerateAPIKey() (fullKey, hash, prefix string, err error) {
	chars := make([]byte, apiKeyLen)
	alphabetSize := big.NewInt(int64(len(base62Chars)))
	for i := range chars {
		n, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", "", "", fmt.Errorf("generate api key: %w", err)
		}
		chars[i] = base62Chars[n.Int64()]
	}

	fullKey = apiKeyPrefix + string(chars)
	hash = HashToken(fullKey)
	prefix = fullKey[:displayPrefixLen]
	return fullKey, hash, prefix, nil
}
