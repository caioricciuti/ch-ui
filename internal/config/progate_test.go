package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/license"
)

func TestProAccessForEdition(t *testing.T) {
	cases := []struct {
		info *license.LicenseInfo
		want ProAccess
	}{
		{nil, ProNone},
		{license.CommunityLicense(), ProNone},
		{&license.LicenseInfo{Edition: "pro", Valid: true}, ProActive},
		{&license.LicenseInfo{Edition: "enterprise", Valid: true}, ProActive},
		{&license.LicenseInfo{Edition: "Enterprise", Valid: true}, ProActive},
		{&license.LicenseInfo{Edition: "enterprise", InGrace: true}, ProGrace},
		{&license.LicenseInfo{Edition: "enterprise"}, ProNone},
		{&license.LicenseInfo{Edition: "community", Valid: true}, ProNone},
	}
	for _, tc := range cases {
		if got := proAccessFor(tc.info); got != tc.want {
			t.Errorf("proAccessFor(%+v) = %v, want %v", tc.info, got, tc.want)
		}
	}
}

func TestSetProAccessForTest(t *testing.T) {
	cfg := &Config{}
	if cfg.ProAccess() != ProNone || cfg.IsPro() {
		t.Fatal("empty license must be ProNone")
	}
	cfg.SetProAccessForTest(func() ProAccess { return ProGrace })
	if cfg.ProAccess() != ProGrace || cfg.IsPro() {
		t.Fatal("override must be returned; grace is not IsPro")
	}
}

func TestProGateAllowAndTransitionLogging(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	access := ProNone
	gate := NewProGate("Test worker", func() ProAccess { return access })
	steps := []struct {
		access ProAccess
		allow  bool
	}{
		{ProNone, false}, {ProNone, false}, {ProGrace, true}, {ProActive, true},
		{ProActive, true}, {ProNone, false}, {ProNone, false}, {ProActive, true},
	}
	for i, step := range steps {
		access = step.access
		if got := gate.Allow(); got != step.allow {
			t.Fatalf("step %d access=%v: Allow() = %v, want %v", i, step.access, got, step.allow)
		}
	}
	out := buf.String()
	if n := strings.Count(out, "Test worker paused"); n != 2 {
		t.Errorf("paused logged %d times, want 2 (once per transition):\n%s", n, out)
	}
	if n := strings.Count(out, "Test worker resumed"); n != 2 {
		t.Errorf("resumed logged %d times, want 2 (once per transition):\n%s", n, out)
	}
}

func TestProGateNilDenies(t *testing.T) {
	var g *ProGate
	if g.Allow() {
		t.Fatal("nil gate must deny")
	}
	if NewProGate("x", nil).Allow() {
		t.Fatal("gate without access func must deny")
	}
}
