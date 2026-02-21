package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// deriveKey derives a 32-byte AES key from the secret using scrypt.
// Parameters match Node.js crypto.scryptSync(secret, 'salt', 32) defaults: N=16384, r=8, p=1.
//
// SECURITY NOTE: The static salt weakens key derivation — all installations using
// the same AppSecretKey will produce the same derived key. A future version should
// use a per-installation salt (stored alongside the DB) with a data migration path.
func deriveKey(secret string) ([]byte, error) {
	return scrypt.Key([]byte(secret), []byte("salt"), 16384, 8, 1, 32)
}

// Encrypt encrypts plaintext using AES-256-GCM with a 16-byte nonce.
// Returns format: iv:authTag:encrypted (hex-encoded).
func Encrypt(plaintext, secret string) (string, error) {
	key, err := deriveKey(secret)
	if err != nil {
		return "", fmt.Errorf("key derivation failed: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher creation failed: %w", err)
	}

	// Use 16-byte nonce to match Node.js createCipheriv('aes-256-gcm', key, iv) with 16-byte IV
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return "", fmt.Errorf("GCM creation failed: %w", err)
	}

	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("random IV generation failed: %w", err)
	}

	// Seal appends ciphertext + tag
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)

	// Split sealed into encrypted data and auth tag (last 16 bytes)
	tagSize := gcm.Overhead()
	encrypted := sealed[:len(sealed)-tagSize]
	authTag := sealed[len(sealed)-tagSize:]

	return fmt.Sprintf("%s:%s:%s",
		hex.EncodeToString(iv),
		hex.EncodeToString(authTag),
		hex.EncodeToString(encrypted),
	), nil
}

// Decrypt decrypts a string in format iv:authTag:encrypted using AES-256-GCM.
func Decrypt(encryptedStr, secret string) (string, error) {
	parts := strings.Split(encryptedStr, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid encrypted format: expected 3 parts, got %d", len(parts))
	}

	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid IV hex: %w", err)
	}

	authTag, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid auth tag hex: %w", err)
	}

	encrypted, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("invalid encrypted data hex: %w", err)
	}

	key, err := deriveKey(secret)
	if err != nil {
		return "", fmt.Errorf("key derivation failed: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher creation failed: %w", err)
	}

	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return "", fmt.Errorf("GCM creation failed: %w", err)
	}

	// Go expects ciphertext + tag concatenated
	ciphertext := append(encrypted, authTag...)

	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a string appears to be in the encrypted format (iv:authTag:encrypted).
func IsEncrypted(value string) bool {
	parts := strings.Split(value, ":")
	return len(parts) == 3 &&
		len(parts[0]) == 32 && // IV: 16 bytes = 32 hex chars
		len(parts[1]) == 32 && // Auth tag: 16 bytes = 32 hex chars
		len(parts[2]) > 0 // Encrypted data exists
}
