// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Creating users. The server never sees a password: a create_user op
// carries a hash made in the browser (or pasted by the admin), or an
// external authenticator. The hash and salt are secrets all the same, so
// they only ever reach the statement Apply runs. Everything shown, hashed,
// audited or stored renders them as '***'.

// Authentication methods a create_user op accepts. Plaintext passwords are
// never one of them.
const (
	AuthSHA256Hash     = "sha256_hash"
	AuthDoubleSHA1Hash = "double_sha1_hash"
	AuthBcryptHash     = "bcrypt_hash"
	AuthLDAP           = "ldap"
	AuthKerberos       = "kerberos"
	AuthSSLCertificate = "ssl_certificate"
	AuthNoPassword     = "no_password"
)

// UserAuth is how a created user signs in. Hash and Salt are secrets.
type UserAuth struct {
	Method string `json:"method"`
	Hash   string `json:"hash,omitempty"`
	Salt   string `json:"salt,omitempty"`
	Server string `json:"server,omitempty"` // ldap
	Realm  string `json:"realm,omitempty"`  // kerberos, optional
	CN     string `json:"cn,omitempty"`     // ssl_certificate
	// Redacted marks an op whose hash and salt were removed (access
	// history). It can never run again.
	Redacted bool `json:"redacted,omitempty"`
}

// Host kinds of a HOST clause.
const (
	HostAny    = "any"
	HostLocal  = "local"
	HostIP     = "ip"
	HostName   = "name"
	HostRegexp = "regexp"
	HostLike   = "like"
)

// HostSpec is one element of a user's HOST clause. Value is empty for any
// and local.
type HostSpec struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

// UserLogin is how an existing user signs in, without any secret: the
// authentication type ClickHouse reports and the hosts it may connect from.
type UserLogin struct {
	Auth  string     `json:"auth,omitempty"`
	Hosts []HostSpec `json:"hosts,omitempty"`
}

var (
	hexRe     = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	saltRe    = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{1,128}$`)
	bcryptRe  = regexp.MustCompile(`^\$2[aby]?\$[0-9]{2}\$[./A-Za-z0-9]{53}$`)
	authTypRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
)

// secretMask replaces a hash or salt everywhere a statement is shown.
const secretMask = "'***'"

func validateUserAuth(a *UserAuth) error {
	if a == nil {
		return fmt.Errorf("choose how the user signs in")
	}
	if a.Redacted {
		return fmt.Errorf("this user was created with a secret CH-UI does not keep, so the change cannot run again; create the user from Import instead")
	}
	noExtra := func(fields map[string]string) error {
		for name, v := range fields {
			if v != "" {
				return fmt.Errorf("%s does not apply to %s", name, a.Method)
			}
		}
		return nil
	}
	switch a.Method {
	case AuthSHA256Hash:
		if len(a.Hash) != 64 || !hexRe.MatchString(a.Hash) {
			return fmt.Errorf("a sha256 hash is 64 hex characters")
		}
		if a.Salt != "" && !saltRe.MatchString(a.Salt) {
			return fmt.Errorf("the salt may hold up to 128 letters, digits and . _ ~ + / = -")
		}
		return noExtra(map[string]string{"server": a.Server, "realm": a.Realm, "cn": a.CN})
	case AuthDoubleSHA1Hash:
		if len(a.Hash) != 40 || !hexRe.MatchString(a.Hash) {
			return fmt.Errorf("a double_sha1 hash is 40 hex characters")
		}
		return noExtra(map[string]string{"salt": a.Salt, "server": a.Server, "realm": a.Realm, "cn": a.CN})
	case AuthBcryptHash:
		if !bcryptRe.MatchString(a.Hash) {
			return fmt.Errorf("a bcrypt hash looks like $2b$12$ followed by 53 characters")
		}
		return noExtra(map[string]string{"salt": a.Salt, "server": a.Server, "realm": a.Realm, "cn": a.CN})
	case AuthLDAP:
		if err := ValidateName("LDAP server", a.Server); err != nil {
			return err
		}
		return noExtra(map[string]string{"hash": a.Hash, "salt": a.Salt, "realm": a.Realm, "cn": a.CN})
	case AuthKerberos:
		if a.Realm != "" {
			if err := ValidateName("Kerberos realm", a.Realm); err != nil {
				return err
			}
		}
		return noExtra(map[string]string{"hash": a.Hash, "salt": a.Salt, "server": a.Server, "cn": a.CN})
	case AuthSSLCertificate:
		if err := ValidateName("certificate common name", a.CN); err != nil {
			return err
		}
		return noExtra(map[string]string{"hash": a.Hash, "salt": a.Salt, "server": a.Server, "realm": a.Realm})
	case AuthNoPassword:
		return noExtra(map[string]string{"hash": a.Hash, "salt": a.Salt, "server": a.Server, "realm": a.Realm, "cn": a.CN})
	}
	return fmt.Errorf("authentication method %q is not supported", a.Method)
}

// ValidateHosts checks a HOST clause: any alone, or local and valued
// entries. An IP entry is an address or a CIDR range.
func ValidateHosts(hosts []HostSpec) error {
	for _, h := range hosts {
		switch h.Kind {
		case HostAny:
			if len(hosts) > 1 {
				return fmt.Errorf("host any cannot be combined with other hosts")
			}
			fallthrough
		case HostLocal:
			if h.Value != "" {
				return fmt.Errorf("host %s takes no value", h.Kind)
			}
		case HostIP:
			if net.ParseIP(h.Value) == nil {
				if _, _, err := net.ParseCIDR(h.Value); err != nil {
					return fmt.Errorf("host ip %q is not an address or a CIDR range", h.Value)
				}
			}
		case HostName, HostRegexp, HostLike:
			if err := ValidateName("host "+h.Kind, h.Value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("host kind %q is not supported (any, local, ip, name, regexp or like)", h.Kind)
		}
	}
	return nil
}

func validateCreateUser(op Op) error {
	if err := ValidateName("user", op.Name); err != nil {
		return err
	}
	if op.Profile != "" {
		if err := ValidateName("settings profile", op.Profile); err != nil {
			return err
		}
	}
	if err := ValidateHosts(op.Hosts); err != nil {
		return err
	}
	return validateUserAuth(op.Auth)
}

// hostClause renders the HOST clause; no hosts means ClickHouse's default,
// any host.
func hostClause(hosts []HostSpec) string {
	if len(hosts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(hosts))
	for _, h := range hosts {
		switch h.Kind {
		case HostAny:
			return " HOST ANY"
		case HostLocal:
			parts = append(parts, "LOCAL")
		default:
			parts = append(parts, strings.ToUpper(h.Kind)+" "+quoteLit(h.Value))
		}
	}
	return " HOST " + strings.Join(parts, ", ")
}

// renderCreateUser builds CREATE USER. With reveal false the hash and salt
// are masked; only Apply ever asks for reveal.
func renderCreateUser(op Op, on string, reveal bool) string {
	secret := func(v string) string {
		if reveal {
			return quoteLit(v)
		}
		return secretMask
	}
	s := "CREATE USER " + quoteIdent(op.Name) + on
	if a := op.Auth; a != nil {
		switch a.Method {
		case AuthSHA256Hash, AuthDoubleSHA1Hash, AuthBcryptHash:
			s += " IDENTIFIED WITH " + a.Method + " BY " + secret(a.Hash)
			if a.Salt != "" {
				s += " SALT " + secret(a.Salt)
			}
		case AuthLDAP:
			s += " IDENTIFIED WITH ldap SERVER " + quoteLit(a.Server)
		case AuthKerberos:
			s += " IDENTIFIED WITH kerberos"
			if a.Realm != "" {
				s += " REALM " + quoteLit(a.Realm)
			}
		case AuthSSLCertificate:
			s += " IDENTIFIED WITH ssl_certificate CN " + quoteLit(a.CN)
		case AuthNoPassword:
			s += " IDENTIFIED WITH no_password"
		}
	}
	s += hostClause(op.Hosts)
	if op.Profile != "" {
		s += " SETTINGS PROFILE " + quoteLit(op.Profile)
	}
	return s
}

// opSecrets returns the secret values an op carries, for scrubbing them
// out of anything that is shown or stored.
func opSecrets(op Op) []string {
	if op.Kind != OpCreateUser || op.Auth == nil {
		return nil
	}
	var out []string
	for _, v := range []string{op.Auth.Hash, op.Auth.Salt} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// scrub replaces every secret in s with ***. ClickHouse may quote part of
// a statement in an error, so errors go through this before anyone sees
// them.
func scrub(s string, secrets []string) string {
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "***")
	}
	return s
}

// RedactOps returns a copy of ops without hashes or salts, for anything
// that keeps ops after the request: a create_user keeps its method and is
// marked redacted so it can never run again.
func RedactOps(ops []Op) []Op {
	out := make([]Op, len(ops))
	for i, op := range ops {
		if op.Auth != nil {
			a := *op.Auth
			a.Hash, a.Salt, a.Redacted = "", "", true
			op.Auth = &a
		}
		out[i] = op
	}
	return out
}

// writableUserStorages are the access storages CREATE USER can write to.
var writableUserStorages = map[string]bool{"local_directory": true, "replicated": true, "memory": true}

// NoWritableUserStorage reports whether the server is known to keep users
// only in read-only storage (users.xml, LDAP), so CREATE USER would fail.
// No storages read means unknown, not none.
func (s *Snapshot) NoWritableUserStorage() bool {
	if len(s.UserStorages) == 0 {
		return false
	}
	for _, t := range s.UserStorages {
		if writableUserStorages[t] {
			return false
		}
	}
	return true
}

// readUserLogins loads each user's authentication type and hosts, and the
// access storages. Neither is a secret. Failures leave them unknown, so
// older servers and limited logins still load the rest of the snapshot.
func readUserLogins(ctx context.Context, ex Executor, s *Snapshot) {
	if rows, err := ex.Query(ctx, `SELECT name, toString(auth_type) AS auth, host_ip, host_names, host_names_regexp, host_names_like FROM system.users ORDER BY name`, nil); err == nil {
		s.UserLogins = map[string]UserLogin{}
		for _, r := range rows {
			name := str(r, "name")
			if name == "" {
				continue
			}
			s.UserLogins[name] = UserLogin{Auth: firstAuthType(str(r, "auth")), Hosts: hostsFromRow(r)}
		}
	}
	if rows, err := ex.Query(ctx, `SELECT type FROM system.user_directories ORDER BY precedence`, nil); err == nil {
		s.UserStorages = []string{}
		for _, r := range rows {
			s.UserStorages = append(s.UserStorages, str(r, "type"))
		}
	}
}

// firstAuthType reads auth_type, an Array(Enum) on recent servers
// ("['sha256_password']") and an Enum before.
func firstAuthType(v string) string {
	v = strings.Trim(strings.TrimSpace(v), "[]")
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	v = strings.Trim(strings.TrimSpace(v), "'")
	if !authTypRe.MatchString(v) {
		return ""
	}
	return v
}

// hostsFromRow turns the host columns of system.users into a HOST clause.
// ClickHouse reports ANY as the ::/0 range and LOCAL as host localhost.
func hostsFromRow(r map[string]any) []HostSpec {
	ips := strs(r, "host_ip")
	for _, ip := range ips {
		if ip == "::/0" {
			return []HostSpec{{Kind: HostAny}}
		}
	}
	var out []HostSpec
	for _, n := range strs(r, "host_names") {
		if n == "localhost" {
			out = append(out, HostSpec{Kind: HostLocal})
		} else {
			out = append(out, HostSpec{Kind: HostName, Value: n})
		}
	}
	for _, ip := range ips {
		out = append(out, HostSpec{Kind: HostIP, Value: ip})
	}
	for _, v := range strs(r, "host_names_regexp") {
		out = append(out, HostSpec{Kind: HostRegexp, Value: v})
	}
	for _, v := range strs(r, "host_names_like") {
		out = append(out, HostSpec{Kind: HostLike, Value: v})
	}
	return out
}

// FormatHost writes a host for an access file: "any", "local" or
// "<kind> <value>".
func FormatHost(h HostSpec) string {
	if h.Value == "" {
		return h.Kind
	}
	return h.Kind + " " + h.Value
}

// ParseHost reads FormatHost's form.
func ParseHost(s string) (HostSpec, error) {
	s = strings.TrimSpace(s)
	kind, value, _ := strings.Cut(s, " ")
	h := HostSpec{Kind: strings.ToLower(kind), Value: strings.TrimSpace(value)}
	if err := ValidateHosts([]HostSpec{h}); err != nil {
		return HostSpec{}, err
	}
	return h, nil
}

// NewUser is a user an access file lists that the target does not have,
// with the login hints the file carries. Import creates it only when asked.
type NewUser struct {
	Name  string     `json:"name"`
	Auth  string     `json:"auth,omitempty"`
	Hosts []HostSpec `json:"hosts,omitempty"`
}
