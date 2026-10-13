package database

import (
	"errors"
	"testing"
	"time"
)

// TestRotateSession replaces a session in one step and refuses a token that
// is already gone, writing nothing.
func TestRotateSession(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "rotate", TunnelToken: "cht_rotate", Type: ConnectionTypeTunnel})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	params := func(token string) CreateSessionParams {
		return CreateSessionParams{ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "x", Token: token, ExpiresAt: expires, AuthSubject: "alice@example.com"}
	}
	if _, err := db.CreateSession(params("old")); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := db.RotateSession("old", params("new")); err != nil {
		t.Fatalf("RotateSession: %v", err)
	}
	if s, _ := db.GetSession("old"); s != nil {
		t.Fatal("old session still exists after rotation")
	}
	s, err := db.GetSession("new")
	if err != nil || s == nil {
		t.Fatalf("new session missing: %v", err)
	}
	if s.AuthSubject == nil || *s.AuthSubject != "alice@example.com" {
		t.Fatalf("auth subject not kept: %v", s.AuthSubject)
	}

	// The old token is gone now: a second rotation must fail and add nothing.
	if _, err := db.RotateSession("old", params("orphan")); !errors.Is(err, ErrSessionChanged) {
		t.Fatalf("rotating a missing token: err = %v, want ErrSessionChanged", err)
	}
	if s, _ := db.GetSession("orphan"); s != nil {
		t.Fatal("rotation of a missing token created a session")
	}
}
