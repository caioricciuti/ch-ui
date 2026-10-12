package pipelines

import (
	"strings"
	"testing"
)

func TestConfigSecretsRoundTrip(t *testing.T) {
	const key = "app-secret"
	in := map[string]interface{}{"brokers": "b:9092", "sasl_password": "hunter2", "auth_token": "", "batch_size": float64(10)}
	enc, err := EncryptConfigSecrets(in, nil, key)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := enc["sasl_password"].(string)
	if !strings.HasPrefix(stored, encPrefix) || strings.Contains(stored, "hunter2") {
		t.Fatalf("secret not encrypted: %q", stored)
	}
	if enc["brokers"] != "b:9092" || enc["auth_token"] != "" || enc["batch_size"] != float64(10) {
		t.Fatalf("non-secret fields changed: %v", enc)
	}

	masked := MaskConfigSecrets(enc)
	if masked["sasl_password"] != SecretMask || masked["auth_token"] != "" {
		t.Fatalf("mask: %v", masked)
	}

	// The editor sends the mask back: the stored ciphertext is kept.
	again, err := EncryptConfigSecrets(masked, enc, key)
	if err != nil {
		t.Fatal(err)
	}
	if again["sasl_password"] != stored {
		t.Fatalf("masked value did not keep the stored secret")
	}

	dec, err := DecryptConfigSecrets(again, key)
	if err != nil {
		t.Fatal(err)
	}
	if dec["sasl_password"] != "hunter2" {
		t.Fatalf("decrypt: %v", dec["sasl_password"])
	}
	if _, err := DecryptConfigSecrets(again, "wrong-key"); err == nil {
		t.Fatal("decrypt with the wrong key succeeded")
	}
}

func TestLegacyPlaintextSecretsStillWork(t *testing.T) {
	const key = "app-secret"
	legacy := map[string]interface{}{"secret_key": "plain-old", "connection_string": "postgres://u:p@h/db"}
	dec, err := DecryptConfigSecrets(legacy, key)
	if err != nil || dec["secret_key"] != "plain-old" {
		t.Fatalf("legacy plaintext not passed through: %v %v", dec, err)
	}
	// Saving the masked form of a legacy row encrypts the plaintext value.
	enc, err := EncryptConfigSecrets(MaskConfigSecrets(legacy), legacy, key)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := enc["secret_key"].(string); !strings.HasPrefix(s, encPrefix) {
		t.Fatalf("legacy secret not re-encrypted: %q", s)
	}
	dec, _ = DecryptConfigSecrets(enc, key)
	if dec["secret_key"] != "plain-old" || dec["connection_string"] != "postgres://u:p@h/db" {
		t.Fatalf("re-encrypted values changed: %v", dec)
	}
	// A mask with nothing stored becomes empty, never the literal mask.
	enc, _ = EncryptConfigSecrets(map[string]interface{}{"auth_token": SecretMask}, nil, key)
	if enc["auth_token"] != "" {
		t.Fatalf("mask without stored value = %v", enc["auth_token"])
	}
}
