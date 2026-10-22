package database

import "testing"

// TestMigrateRemovedChannelTypes turns off Resend and Brevo channels on
// every start, keeps their rows, type and config, and leaves the
// supported channel types alone.
func TestMigrateRemovedChannelTypes(t *testing.T) {
	db := openTestDB(t)
	ids := map[string]string{}
	for name, kind := range map[string]string{"r": "resend", "b": "Brevo", "s": "smtp", "w": "webhook"} {
		id, err := db.CreateAlertChannel(name, kind, "enc-"+name, true, "admin")
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	for i := 0; i < 2; i++ {
		if err := db.migrateRemovedChannelTypes(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := db.runMigrations(); err != nil {
		t.Fatalf("full rerun: %v", err)
	}
	for name, wantActive := range map[string]bool{"r": false, "b": false, "s": true, "w": true} {
		ch, err := db.GetAlertChannelByID(ids[name])
		if err != nil || ch == nil {
			t.Fatalf("%s: row lost: %v", name, err)
		}
		if ch.IsActive != wantActive {
			t.Errorf("%s (%s): is_active = %v, want %v", name, ch.ChannelType, ch.IsActive, wantActive)
		}
		if ch.ConfigEncrypted != "enc-"+name {
			t.Errorf("%s: config changed to %q", name, ch.ConfigEncrypted)
		}
	}
	if ch, _ := db.GetAlertChannelByID(ids["b"]); ch.ChannelType != "Brevo" {
		t.Errorf("type rewritten to %q", ch.ChannelType)
	}
}

// The share invite mail only goes out through an SMTP channel: a webhook
// or a turned-off legacy channel is not an email channel.
func TestGetFirstActiveAlertChannelIsSMTP(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.CreateAlertChannel("hook", "webhook", "enc", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAlertChannel("old", "resend", "enc", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if ch, err := db.GetFirstActiveAlertChannel(); err != nil || ch != nil {
		t.Fatalf("got %+v, %v; want no channel", ch, err)
	}
	id, err := db.CreateAlertChannel("relay", "smtp", "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if ch, err := db.GetFirstActiveAlertChannel(); err != nil || ch == nil || ch.ID != id {
		t.Fatalf("got %+v, %v; want %s", ch, err, id)
	}
}
