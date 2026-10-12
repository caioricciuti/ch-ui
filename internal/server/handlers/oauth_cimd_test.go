package handlers

import (
	"errors"
	"testing"
)

func TestCIMDDialControlRefusesNonPublicAddresses(t *testing.T) {
	// The Control func sees the address actually dialed, after DNS. A name
	// that resolved public during any earlier check but private at dial time
	// still lands here with the private IP.
	blocked := []string{
		"127.0.0.1:443", "10.1.2.3:443", "172.16.0.1:443", "192.168.1.1:443",
		"169.254.169.254:443", "0.0.0.0:443", "224.0.0.1:443",
		"[::1]:443", "[fe80::1]:443", "[fc00::1]:443", "[::ffff:127.0.0.1]:443", "[::]:443",
	}
	for _, addr := range blocked {
		if err := cimdDialControl("tcp", addr, nil); !errors.Is(err, errCIMDPrivateAddress) {
			t.Errorf("%s: got %v, want errCIMDPrivateAddress", addr, err)
		}
	}
	allowed := []string{"93.184.215.14:443", "[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443"}
	for _, addr := range allowed {
		if err := cimdDialControl("tcp", addr, nil); err != nil {
			t.Errorf("%s: got %v, want nil", addr, err)
		}
	}
	if err := cimdDialControl("tcp", "not-an-address", nil); err == nil {
		t.Error("a malformed address must be refused")
	}
}

func TestFetchClientMetadataRefusesLoopbackAtDial(t *testing.T) {
	// localhost resolves to a loopback address; the refusal must come from
	// the dialer, before any connection is made.
	_, err := fetchClientMetadata("https://localhost:1/client.json")
	if !errors.Is(err, errCIMDPrivateAddress) {
		t.Fatalf("got %v, want errCIMDPrivateAddress", err)
	}
	if _, err := fetchClientMetadata("https://127.0.0.1:1/client.json"); !errors.Is(err, errCIMDPrivateAddress) {
		t.Fatalf("literal IP: got %v, want errCIMDPrivateAddress", err)
	}
}
