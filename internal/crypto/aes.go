package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"

	"golang.org/x/crypto/scrypt"
)

const (
	ivSize  = 16 // nonce size used by Encrypt
	tagSize = 16 // GCM authentication tag size
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

// Key is an AES-256-GCM key derived from an app secret. Deriving runs scrypt,
// so code that handles many values (secret rotation) derives a Key once and
// reuses it instead of calling Encrypt/Decrypt per value.
type Key struct {
	gcm cipher.AEAD
}

// NewKey derives the AES key for secret.
func NewKey(secret string) (*Key, error) {
	key, err := deriveKey(secret)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher creation failed: %w", err)
	}

	// Use 16-byte nonce to match Node.js createCipheriv('aes-256-gcm', key, iv) with 16-byte IV
	gcm, err := cipher.NewGCMWithNonceSize(block, ivSize)
	if err != nil {
		return nil, fmt.Errorf("GCM creation failed: %w", err)
	}
	return &Key{gcm: gcm}, nil
}

// Encrypt encrypts plaintext with k. Returns format: iv:authTag:encrypted (hex-encoded).
func (k *Key) Encrypt(plaintext string) (string, error) {
	iv := make([]byte, ivSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("random IV generation failed: %w", err)
	}

	// Seal appends ciphertext + tag
	sealed := k.gcm.Seal(nil, iv, []byte(plaintext), nil)

	// Split sealed into encrypted data and auth tag (last 16 bytes)
	overhead := k.gcm.Overhead()
	encrypted := sealed[:len(sealed)-overhead]
	authTag := sealed[len(sealed)-overhead:]

	return fmt.Sprintf("%s:%s:%s",
		hex.EncodeToString(iv),
		hex.EncodeToString(authTag),
		hex.EncodeToString(encrypted),
	), nil
}

// Decrypt decrypts a string in format iv:authTag:encrypted with k only. It
// never tries the previous key; see the package-level Decrypt for that.
func (k *Key) Decrypt(encryptedStr string) (string, error) {
	iv, ciphertext, err := parseEncrypted(encryptedStr)
	if err != nil {
		return "", err
	}
	return k.open(iv, ciphertext)
}

func (k *Key) open(iv, ciphertext []byte) (string, error) {
	plaintext, err := k.gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}
	return string(plaintext), nil
}

// parseEncrypted splits iv:authTag:encrypted into the nonce and the
// ciphertext+tag that GCM expects.
func parseEncrypted(encryptedStr string) (iv, ciphertext []byte, err error) {
	parts := strings.Split(encryptedStr, ":")
	if len(parts) != 3 {
		return nil, nil, fmt.Errorf("invalid encrypted format: expected 3 parts, got %d", len(parts))
	}

	iv, err = hex.DecodeString(parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid IV hex: %w", err)
	}

	authTag, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid auth tag hex: %w", err)
	}

	encrypted, err := hex.DecodeString(parts[2])
	if err != nil {
		return nil, nil, fmt.Errorf("invalid encrypted data hex: %w", err)
	}

	// gcm.Open panics on a nonce of the wrong size, so check both fixed-size
	// parts before touching the cipher.
	if len(iv) != ivSize {
		return nil, nil, fmt.Errorf("invalid IV length: expected %d bytes, got %d", ivSize, len(iv))
	}
	if len(authTag) != tagSize {
		return nil, nil, fmt.Errorf("invalid auth tag length: expected %d bytes, got %d", tagSize, len(authTag))
	}

	// Go expects ciphertext + tag concatenated
	return iv, append(encrypted, authTag...), nil
}

// previousKey is the key derived from APP_SECRET_KEY_PREVIOUS, set once at
// startup. Decrypt falls back to it so secrets written before a key rotation
// stay readable until they are re-encrypted with the current key.
var previousKey atomic.Pointer[Key]

// SetPreviousKey sets the fallback key Decrypt tries after the current one.
// An empty secret clears it.
func SetPreviousKey(secret string) error {
	if secret == "" {
		previousKey.Store(nil)
		return nil
	}
	k, err := NewKey(secret)
	if err != nil {
		return err
	}
	previousKey.Store(k)
	return nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a 16-byte nonce.
// Returns format: iv:authTag:encrypted (hex-encoded).
func Encrypt(plaintext, secret string) (string, error) {
	k, err := NewKey(secret)
	if err != nil {
		return "", err
	}
	return k.Encrypt(plaintext)
}

// Decrypt decrypts a string in format iv:authTag:encrypted using AES-256-GCM.
// When the current secret fails to authenticate the value and a previous key
// is set (SetPreviousKey), the previous key is tried before giving up.
func Decrypt(encryptedStr, secret string) (string, error) {
	iv, ciphertext, err := parseEncrypted(encryptedStr)
	if err != nil {
		return "", err
	}

	k, err := NewKey(secret)
	if err != nil {
		return "", err
	}

	plaintext, err := k.open(iv, ciphertext)
	if err != nil {
		if prev := previousKey.Load(); prev != nil {
			if p, perr := prev.open(iv, ciphertext); perr == nil {
				return p, nil
			}
		}
		return "", err
	}
	return plaintext, nil
}

// IsEncrypted checks if a string appears to be in the encrypted format (iv:authTag:encrypted).
func IsEncrypted(value string) bool {
	parts := strings.Split(value, ":")
	return len(parts) == 3 &&
		len(parts[0]) == 32 && // IV: 16 bytes = 32 hex chars
		len(parts[1]) == 32 && // Auth tag: 16 bytes = 32 hex chars
		len(parts[2]) > 0 // Encrypted data exists
}
