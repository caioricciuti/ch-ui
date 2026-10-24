// Passwords made and hashed in the browser, so CH-UI's server never sees
// one: Admin → Users and the access control import send only the hash.

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
