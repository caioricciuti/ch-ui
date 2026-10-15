// SPDX-License-Identifier: BUSL-1.1
import type { RbacAuthMethod, RbacHostKind, RbacHostSpec, RbacNewUser, RbacUserAuth } from '../../../types/rbac'

/**
 * Sign-in for users an import creates. The server never sees a password:
 * a generated password is hashed here (sha256 of password + salt, the way
 * ClickHouse checks sha256_hash ... SALT), only the hash and salt are sent,
 * and the password is shown once after the apply.
 */

export type SignIn = 'generate' | RbacAuthMethod

export const SIGN_IN_OPTIONS: { value: SignIn; label: string }[] = [
  { value: 'generate', label: 'Generate a password' },
  { value: 'sha256_hash', label: 'Paste a sha256 hash' },
  { value: 'double_sha1_hash', label: 'Paste a double SHA-1 hash' },
  { value: 'bcrypt_hash', label: 'Paste a bcrypt hash' },
  { value: 'ldap', label: 'LDAP server' },
  { value: 'kerberos', label: 'Kerberos' },
  { value: 'ssl_certificate', label: 'SSL certificate' },
  { value: 'no_password', label: 'No password' },
]

const METHOD_LABELS: Record<RbacAuthMethod, string> = {
  sha256_hash: 'sha256 hash',
  double_sha1_hash: 'double SHA-1 hash',
  bcrypt_hash: 'bcrypt hash',
  ldap: 'LDAP',
  kerberos: 'Kerberos',
  ssl_certificate: 'SSL certificate',
  no_password: 'no password',
}

export function authLabel(method: RbacAuthMethod): string {
  return METHOD_LABELS[method] ?? method
}

/** The sign-in to preselect for a ClickHouse auth type from the file. */
export function signInFromHint(auth: string | undefined): SignIn {
  switch (auth) {
    case 'double_sha1_password':
      return 'double_sha1_hash'
    case 'bcrypt_password':
      return 'bcrypt_hash'
    case 'ldap':
    case 'kerberos':
    case 'ssl_certificate':
    case 'no_password':
      return auth
    default:
      // sha256_password, plaintext_password and anything else.
      return 'generate'
  }
}

/** Per-user form state. hosts is the text form ("local, ip 10.0.0.0/8"). */
export interface NewUserConfig {
  create: boolean
  signIn: SignIn
  hash: string
  salt: string
  server: string
  realm: string
  cn: string
  hosts: string
  profile: string
}

export function configFromHint(u: RbacNewUser): NewUserConfig {
  return {
    create: false,
    signIn: signInFromHint(u.auth),
    hash: '',
    salt: '',
    server: '',
    realm: '',
    cn: '',
    hosts: formatHosts(u.hosts ?? []),
    profile: '',
  }
}

const HOST_KINDS: ReadonlySet<string> = new Set<RbacHostKind>(['any', 'local', 'ip', 'name', 'regexp', 'like'])

/** Host specs from a server response; anything malformed is dropped. */
export function hostsFrom(v: unknown): RbacHostSpec[] {
  if (!Array.isArray(v)) return []
  const out: RbacHostSpec[] = []
  for (const h of v) {
    if (typeof h !== 'object' || h === null) continue
    const kind = (h as { kind?: unknown }).kind
    const value = (h as { value?: unknown }).value
    if (typeof kind !== 'string' || !HOST_KINDS.has(kind)) continue
    out.push(typeof value === 'string' && value ? { kind: kind as RbacHostKind, value } : { kind: kind as RbacHostKind })
  }
  return out
}

export function formatHosts(hosts: RbacHostSpec[]): string {
  return hosts.map((h) => (h.value ? `${h.kind} ${h.value}` : h.kind)).join(', ')
}

/**
 * Reads "any", "local" or "ip 10.0.0.0/8, like %.corp". Empty is any host.
 * The server checks the values again.
 */
export function parseHosts(text: string): { hosts: RbacHostSpec[]; error: string } {
  const hosts: RbacHostSpec[] = []
  for (const part of text.split(',').map((p) => p.trim()).filter(Boolean)) {
    const space = part.indexOf(' ')
    const kind = (space < 0 ? part : part.slice(0, space)).toLowerCase()
    const value = space < 0 ? '' : part.slice(space + 1).trim()
    if (!HOST_KINDS.has(kind)) return { hosts: [], error: `Host "${part}" should start with any, local, ip, name, regexp or like.` }
    if ((kind === 'any' || kind === 'local') && value) return { hosts: [], error: `Host ${kind} takes no value.` }
    if (kind !== 'any' && kind !== 'local' && !value) return { hosts: [], error: `Host ${kind} needs a value, as in "${kind} ${kind === 'ip' ? '10.0.0.0/8' : 'host.example.com'}".` }
    hosts.push(value ? { kind: kind as RbacHostKind, value } : { kind: kind as RbacHostKind })
  }
  if (hosts.length > 1 && hosts.some((h) => h.kind === 'any')) return { hosts: [], error: 'Host any cannot be combined with other hosts.' }
  return { hosts, error: '' }
}

const PASSWORD_ALPHABET = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'

/** A random password from an unambiguous alphabet, without modulo bias. */
export function generatePassword(length = 24): string {
  const limit = 256 - (256 % PASSWORD_ALPHABET.length)
  let out = ''
  const buf = new Uint8Array(64)
  while (out.length < length) {
    crypto.getRandomValues(buf)
    for (const b of buf) {
      if (b < limit && out.length < length) out += PASSWORD_ALPHABET[b % PASSWORD_ALPHABET.length]
    }
  }
  return out
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

export function randomSalt(): string {
  return toHex(crypto.getRandomValues(new Uint8Array(16)))
}

/** Lowercase hex sha256 of text as UTF-8. */
export async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))
  return toHex(new Uint8Array(digest))
}

/** The hash ClickHouse checks for IDENTIFIED WITH sha256_hash BY hash SALT salt. */
export function saltedSha256(password: string, salt: string): Promise<string> {
  return sha256Hex(password + salt)
}

const HEX = /^[0-9a-fA-F]+$/

/**
 * The auth of a create_user op from a user's form, and the generated
 * password when there is one. Throws with a message naming the user.
 */
export async function buildAuth(name: string, cfg: NewUserConfig): Promise<{ auth: RbacUserAuth; password?: string }> {
  const fail = (msg: string): never => {
    throw new Error(`New user ${name}: ${msg}`)
  }
  const hash = cfg.hash.trim()
  switch (cfg.signIn) {
    case 'generate': {
      const password = generatePassword()
      const salt = randomSalt()
      return { auth: { method: 'sha256_hash', hash: await saltedSha256(password, salt), salt }, password }
    }
    case 'sha256_hash': {
      if (hash.length !== 64 || !HEX.test(hash)) fail('a sha256 hash is 64 hex characters.')
      const salt = cfg.salt.trim()
      return { auth: salt ? { method: 'sha256_hash', hash, salt } : { method: 'sha256_hash', hash } }
    }
    case 'double_sha1_hash':
      if (hash.length !== 40 || !HEX.test(hash)) fail('a double SHA-1 hash is 40 hex characters.')
      return { auth: { method: 'double_sha1_hash', hash } }
    case 'bcrypt_hash':
      if (!/^\$2[aby]?\$\d\d\$[./A-Za-z0-9]{53}$/.test(hash)) fail('a bcrypt hash looks like $2b$12$ followed by 53 characters.')
      return { auth: { method: 'bcrypt_hash', hash } }
    case 'ldap':
      if (!cfg.server.trim()) fail('name the LDAP server.')
      return { auth: { method: 'ldap', server: cfg.server.trim() } }
    case 'kerberos':
      return { auth: cfg.realm.trim() ? { method: 'kerberos', realm: cfg.realm.trim() } : { method: 'kerberos' } }
    case 'ssl_certificate':
      if (!cfg.cn.trim()) fail('give the certificate common name.')
      return { auth: { method: 'ssl_certificate', cn: cfg.cn.trim() } }
    case 'no_password':
      return { auth: { method: 'no_password' } }
  }
  return fail('choose how the user signs in.')
}
