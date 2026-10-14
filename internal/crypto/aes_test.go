package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

const testSecret = "test-secret"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	for _, plaintext := range []string{"", "p", "pässwörd with unicode ✓", strings.Repeat("x", 4096)} {
		enc, err := Encrypt(plaintext, testSecret)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		got, err := Decrypt(enc, testSecret)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", plaintext, err)
		}
		if got != plaintext {
			t.Fatalf("round trip: got %q, want %q", got, plaintext)
		}
	}
}

func TestEncryptUsesFreshIV(t *testing.T) {
	a, err := Encrypt("same", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("same", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two encryptions of the same plaintext produced identical output")
	}
	if !IsEncrypted(a) {
		t.Fatalf("IsEncrypted(%q) = false", a)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	enc, err := Encrypt("secret", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(enc, "other-secret"); err == nil {
		t.Fatalf("wrong key decrypted to %q", got)
	}
}

// flipHex flips the low bit of the first byte of a hex string.
func flipHex(t *testing.T, s string) string {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil || len(b) == 0 {
		t.Fatalf("flipHex(%q): %v", s, err)
	}
	b[0] ^= 1
	return hex.EncodeToString(b)
}

func TestDecryptTampered(t *testing.T) {
	enc, err := Encrypt("secret value", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(enc, ":")
	for i, name := range []string{"iv", "tag", "ciphertext"} {
		tampered := append([]string(nil), parts...)
		tampered[i] = flipHex(t, tampered[i])
		if got, err := Decrypt(strings.Join(tampered, ":"), testSecret); err == nil {
			t.Fatalf("tampered %s decrypted to %q", name, got)
		}
	}
}

func TestDecryptMalformed(t *testing.T) {
	enc, err := Encrypt("secret", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(enc, ":")
	cases := map[string]string{
		"empty":            "",
		"plain text":       "not-encrypted",
		"two parts":        parts[0] + ":" + parts[1],
		"four parts":       enc + ":00",
		"bad iv hex":       "zz:" + parts[1] + ":" + parts[2],
		"bad tag hex":      parts[0] + ":zz:" + parts[2],
		"bad data hex":     parts[0] + ":" + parts[1] + ":zz",
		"short iv":         parts[0][:8] + ":" + parts[1] + ":" + parts[2],
		"empty iv":         ":" + parts[1] + ":" + parts[2],
		"long iv":          parts[0] + "00:" + parts[1] + ":" + parts[2],
		"short tag":        parts[0] + ":" + parts[1][:8] + ":" + parts[2],
		"empty tag":        parts[0] + "::" + parts[2],
		"empty everything": "::",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decrypt panicked on %q: %v", input, r)
				}
			}()
			if got, err := Decrypt(input, testSecret); err == nil {
				t.Fatalf("Decrypt(%q) = %q, want error", input, got)
			}
		})
	}
}

func TestDecryptFallsBackToPreviousKey(t *testing.T) {
	t.Cleanup(func() { _ = SetPreviousKey("") })

	old, err := Encrypt("rotated", "old-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(old, testSecret); err == nil {
		t.Fatal("value under the old key decrypted without a previous key set")
	}

	if err := SetPreviousKey("old-secret"); err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(old, testSecret)
	if err != nil || got != "rotated" {
		t.Fatalf("fallback decrypt = %q, %v; want %q", got, err, "rotated")
	}

	// The current key still wins for values it wrote.
	cur, err := Encrypt("current", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(cur, testSecret); err != nil || got != "current" {
		t.Fatalf("current decrypt = %q, %v", got, err)
	}

	// A third key is still rejected.
	other, err := Encrypt("x", "unrelated-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(other, testSecret); err == nil {
		t.Fatalf("unrelated key decrypted to %q", got)
	}

	// Key.Decrypt never falls back.
	k, err := NewKey(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := k.Decrypt(old); err == nil {
		t.Fatalf("Key.Decrypt fell back to the previous key: %q", got)
	}

	if err := SetPreviousKey(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(old, testSecret); err == nil {
		t.Fatal("cleared previous key still used")
	}
}
