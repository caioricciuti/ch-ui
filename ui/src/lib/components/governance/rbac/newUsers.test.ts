// SPDX-License-Identifier: BUSL-1.1
import { describe, expect, it } from 'vitest'
import { buildAuth, configFromHint, formatHosts, generatePassword, parseHosts, saltedSha256, signInFromHint } from './newUsers'

describe('saltedSha256', () => {
  // Expected values come from ClickHouse itself:
  // SELECT lower(hex(SHA256(password || salt))), the hash it checks for
  // IDENTIFIED WITH sha256_hash BY 'hash' SALT 'salt'. The Go live test
  // TestCreateUserLive signs in with a password hashed this way.
  it('matches what ClickHouse computes', async () => {
    expect(await saltedSha256('pw1', 'salt1')).toBe('9bf6479634baeda1ad95722672002e2b38f95de4065b631448a2fa93fc5b7203')
    expect(await saltedSha256('pässwörd', 'c0ffee')).toBe('24bf4606e45e0579a2d27e53beec7036a5f2dc1bd6cd621205ca8e774a67be0c')
  })
})

describe('generatePassword', () => {
  it('is long, from the alphabet, and different each time', () => {
    const a = generatePassword()
    expect(a).toMatch(/^[A-HJ-NP-Za-km-z2-9]{24}$/)
    expect(generatePassword()).not.toBe(a)
  })
})

describe('buildAuth', () => {
  const base = configFromHint({ name: 'ann' })

  it('generates a password and sends only its salted hash', async () => {
    const { auth, password } = await buildAuth('ann', { ...base, signIn: 'generate' })
    expect(auth.method).toBe('sha256_hash')
    expect(password).toBeTruthy()
    expect(JSON.stringify(auth)).not.toContain(password!)
    expect(auth.hash).toBe(await saltedSha256(password!, auth.salt!))
  })

  it('checks pasted hashes and external methods', async () => {
    await expect(buildAuth('ann', { ...base, signIn: 'sha256_hash', hash: 'abc' })).rejects.toThrow('New user ann: a sha256 hash is 64 hex characters.')
    await expect(buildAuth('ann', { ...base, signIn: 'ldap' })).rejects.toThrow(/LDAP server/)
    expect((await buildAuth('ann', { ...base, signIn: 'double_sha1_hash', hash: 'a'.repeat(40) })).auth).toEqual({ method: 'double_sha1_hash', hash: 'a'.repeat(40) })
    expect((await buildAuth('ann', { ...base, signIn: 'kerberos' })).auth).toEqual({ method: 'kerberos' })
    expect((await buildAuth('ann', { ...base, signIn: 'no_password' })).password).toBeUndefined()
  })
})

describe('hosts', () => {
  it('round-trips the text form', () => {
    const { hosts, error } = parseHosts('local, IP 10.0.0.0/8 , like %.corp')
    expect(error).toBe('')
    expect(hosts).toEqual([{ kind: 'local' }, { kind: 'ip', value: '10.0.0.0/8' }, { kind: 'like', value: '%.corp' }])
    expect(formatHosts(hosts)).toBe('local, ip 10.0.0.0/8, like %.corp')
    expect(parseHosts('').hosts).toEqual([])
  })

  it('refuses unknown kinds, missing values and any with others', () => {
    expect(parseHosts('somewhere').error).toMatch(/should start with/)
    expect(parseHosts('ip').error).toMatch(/needs a value/)
    expect(parseHosts('any, local').error).toMatch(/cannot be combined/)
  })
})

it('preselects the sign-in from the file hint', () => {
  expect(signInFromHint('sha256_password')).toBe('generate')
  expect(signInFromHint('plaintext_password')).toBe('generate')
  expect(signInFromHint('bcrypt_password')).toBe('bcrypt_hash')
  expect(signInFromHint('ldap')).toBe('ldap')
  expect(configFromHint({ name: 'x', auth: 'ldap', hosts: [{ kind: 'local' }] })).toMatchObject({ signIn: 'ldap', hosts: 'local', create: false })
})
