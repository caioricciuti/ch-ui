// Package ingest serves the push receivers: POST /ingest/v1/logs, /traces
// and /events. A request carries an ingest key that names one pipeline; the
// body is decoded, encoded as JSONEachRow and appended to that pipeline's
// spool stream, and only then answered with 200. The pipeline's
// source_ingest drains the stream into ClickHouse (see pipelines.Hub).
package ingest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
)

// KeyPrefix starts every ingest key.
const KeyPrefix = "chi_"

// keyBodyLen is the number of random characters after KeyPrefix.
const keyBodyLen = 40

// displayPrefixLen is how many characters after KeyPrefix are kept to tell
// keys apart in the UI.
const displayPrefixLen = 8

var keyEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// GenerateKey returns a new key, its SHA-256 hex hash for storage and its
// display prefix (the first 8 characters after chi_).
func GenerateKey() (plaintext, hash, prefix string, err error) {
	var raw [25]byte // 200 bits, exactly 40 base32 characters
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", "", err
	}
	body := keyEncoding.EncodeToString(raw[:])
	plaintext = KeyPrefix + body
	return plaintext, HashKey(plaintext), body[:displayPrefixLen], nil
}

// HashKey returns the SHA-256 hex digest used to store and look up keys.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// wellFormed reports whether s has the shape of a key, so obvious garbage
// never reaches the database.
func wellFormed(s string) bool {
	if len(s) != len(KeyPrefix)+keyBodyLen || !strings.HasPrefix(s, KeyPrefix) {
		return false
	}
	for i := len(KeyPrefix); i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// presentedKey reads the key from the Authorization header or
// X-CHUI-Ingest-Key. Authorization takes the schemes the shippers send:
//
//   - Bearer <key> (OTLP exporters, curl)
//   - Splunk <key> (Splunk HEC clients)
//   - Basic base64(<any user>:<key>) (Elasticsearch and HEC clients)
//   - ApiKey base64(<any id>:<key>), or ApiKey <key> (Elasticsearch clients)
//
// The query string is never consulted: URLs end up in proxy and access logs.
func presentedKey(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get("Authorization")); h != "" {
		scheme, cred, _ := strings.Cut(h, " ")
		cred = strings.TrimSpace(cred)
		switch strings.ToLower(scheme) {
		case "bearer", "splunk":
			return cred
		case "basic":
			return credentialSecret(cred, false)
		case "apikey":
			return credentialSecret(cred, true)
		}
	}
	return strings.TrimSpace(r.Header.Get("X-CHUI-Ingest-Key"))
}

// credentialSecret returns the part after the colon of a base64
// "<id>:<secret>" credential. With raw, a credential that already is a key
// is returned as is.
func credentialSecret(cred string, raw bool) string {
	if raw && strings.HasPrefix(cred, KeyPrefix) {
		return cred
	}
	dec, err := base64.StdEncoding.DecodeString(cred)
	if err != nil {
		return ""
	}
	_, secret, ok := strings.Cut(string(dec), ":")
	if !ok {
		return ""
	}
	return secret
}
