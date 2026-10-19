package connector

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

// Resolve and validate on every connection, then dial the validated IP itself.
// Private and loopback ClickHouse installations remain supported. Link-local,
// metadata, unspecified and multicast addresses are never ClickHouse targets.
func clickHouseDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return dialClickHouse(ctx, network, address, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

func dialClickHouse(ctx context.Context, network, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "metadata.google.internal" {
		return nil, fmt.Errorf("ClickHouse target is a metadata service")
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("ClickHouse target resolved to no addresses")
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip == netip.MustParseAddr("fd00:ec2::254") {
			return nil, fmt.Errorf("ClickHouse target resolves to a prohibited address")
		}
	}
	// No second DNS lookup: this also prevents rebinding between validation and use.
	var lastErr error
	for _, ip := range ips {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}
