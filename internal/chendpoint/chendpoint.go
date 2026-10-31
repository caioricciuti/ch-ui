// Package chendpoint names the ClickHouse server a URL reaches, so CH-UI can
// tell when a connection starts pointing at a different one.
package chendpoint

import (
	"net/url"
	"strings"
)

// Normalize returns "scheme://host:port/path" for a ClickHouse HTTP URL: the
// scheme and host lower-cased, the default port filled in (8123 for http,
// 8443 for https), and the path without a trailing slash ("/" when empty),
// so a path behind a proxy tells two servers on one host apart. User info,
// query and fragment are dropped, so the result never carries a credential.
// ok is false for a URL without a host or with another scheme.
func Normalize(raw string) (endpoint string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch {
	case scheme != "http" && scheme != "https":
		return "", false
	case port == "" && scheme == "http":
		port = "8123"
	case port == "":
		port = "8443"
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]" // IPv6
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + ":" + port + path, true
}
