package license

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// GenerateTunnelToken generates a tunnel token with prefix 'cht_'
func GenerateTunnelToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "cht_" + hex.EncodeToString(b)
}

// GenerateSessionToken generates a session token
func GenerateSessionToken() string {
	u1 := uuid.New().String()
	u2 := strings.ReplaceAll(uuid.New().String(), "-", "")
	return u1 + u2
}

var tunnelTokenRegex = regexp.MustCompile(`^cht_[a-f0-9]{32}$`)

// IsValidTunnelToken validates tunnel token format
func IsValidTunnelToken(token string) bool {
	return tunnelTokenRegex.MatchString(token)
}
