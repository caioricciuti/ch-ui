package pipelines

import (
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// SecretMask replaces a stored secret in API responses. A client that sends
// it back unchanged keeps the stored value.
const SecretMask = "***"

// encPrefix marks a config value encrypted with the app secret key. Values
// without it are legacy plaintext and are used as is (and encrypted on the
// next save).
const encPrefix = "enc:v1:"

// secretFields are the connector config keys that hold credentials.
var secretFields = map[string]bool{
	"password":          true,
	"sasl_password":     true,
	"secret_key":        true,
	"access_key":        true,
	"connection_string": true,
	"api_key":           true,
	"auth_token":        true,
}

// IsSecretField reports whether a connector config key holds a credential.
func IsSecretField(key string) bool { return secretFields[key] }

// EncryptConfigSecrets returns a copy of cfg with every non-empty secret
// field encrypted. A field sent as SecretMask takes its value from prev (the
// stored config of the same node), so masked GET responses round-trip.
// Values already encrypted are kept.
func EncryptConfigSecrets(cfg, prev map[string]interface{}, key string) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	for k := range secretFields {
		s, ok := out[k].(string)
		if !ok {
			continue
		}
		if s == SecretMask {
			old, _ := prev[k].(string)
			if old == SecretMask {
				old = ""
			}
			s = old
		}
		if s == "" || strings.HasPrefix(s, encPrefix) {
			out[k] = s
			continue
		}
		enc, err := crypto.Encrypt(s, key)
		if err != nil {
			return nil, fmt.Errorf("encrypt %s: %w", k, err)
		}
		out[k] = encPrefix + enc
	}
	return out, nil
}

// DecryptConfigSecrets returns a copy of cfg with encrypted secret fields
// decrypted. Plaintext (legacy) values pass through unchanged.
func DecryptConfigSecrets(cfg map[string]interface{}, key string) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	for k := range secretFields {
		s, ok := out[k].(string)
		if !ok || !strings.HasPrefix(s, encPrefix) {
			continue
		}
		plain, err := crypto.Decrypt(strings.TrimPrefix(s, encPrefix), key)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s: %w", k, err)
		}
		out[k] = plain
	}
	return out, nil
}

// MaskConfigSecrets returns a copy of cfg with every non-empty secret field
// replaced by SecretMask.
func MaskConfigSecrets(cfg map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		if s, ok := v.(string); ok && secretFields[k] && s != "" {
			out[k] = SecretMask
			continue
		}
		out[k] = v
	}
	return out
}
