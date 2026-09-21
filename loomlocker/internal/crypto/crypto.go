package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argon2Memory      = 64 * 1024 // 64 MB
	argon2Iterations  = 3
	argon2Parallelism = 2
	argon2KeyLen      = 32

	saltLen    = 16
	nonceLen   = 12
	bcryptCost = 12
)

// HashPassword returns a bcrypt hash of password for in-memory storage.
func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
}

// VerifyPassword checks password against a bcrypt hash.
func VerifyPassword(password string, hash []byte) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

// RandomKey generates a cryptographically random 32-byte AES key.
func RandomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("random key: %w", err)
	}
	return key, nil
}

// DeriveKey derives a 32-byte AES key from password and salt using Argon2id.
// Returns the key and the salt (caller must store salt for recovery).
func DeriveKey(password string, salt []byte) ([]byte, []byte, error) {
	if salt == nil {
		salt = make([]byte, saltLen)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, nil, fmt.Errorf("derive key salt: %w", err)
		}
	}
	key := argon2.IDKey([]byte(password), salt,
		argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLen)
	return key, salt, nil
}

// Encrypt encrypts plaintext with AES-256-GCM using key.
// Returns nonce + ciphertext concatenated.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts AES-256-GCM ciphertext (nonce prepended) using key.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(ciphertext) < nonceLen {
		return nil, fmt.Errorf("ciphertext too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := ciphertext[:nonceLen]
	return gcm.Open(nil, nonce, ciphertext[nonceLen:], nil)
}

// RandomToken generates a `lk_<16 hex chars>` replacement token.
func RandomToken() string {
	b := make([]byte, 8)
	_, _ = io.ReadFull(rand.Reader, b)
	return "lk_" + hex.EncodeToString(b)
}

// IsToken reports whether s looks like a loomlocker token (already locked).
func IsToken(s string) bool {
	if len(s) != 19 { // "lk_" + 16 hex
		return false
	}
	if s[:3] != "lk_" {
		return false
	}
	_, err := hex.DecodeString(s[3:])
	return err == nil
}
