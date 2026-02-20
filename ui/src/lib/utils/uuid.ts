function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

export function createUUID(): string {
  const cryptoObject = globalThis.crypto

  if (cryptoObject?.randomUUID) {
    return cryptoObject.randomUUID()
  }

  if (cryptoObject?.getRandomValues) {
    const bytes = new Uint8Array(16)
    cryptoObject.getRandomValues(bytes)
    // RFC 4122 v4 bits.
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    const hex = bytesToHex(bytes)
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }

  // Last-resort fallback for very restricted environments.
  const seed = `${Date.now()}-${Math.random()}-${Math.random()}`
  let hash = 0
  for (let i = 0; i < seed.length; i++) {
    hash = ((hash << 5) - hash) + seed.charCodeAt(i)
    hash |= 0
  }
  const base = Math.abs(hash).toString(16).padStart(8, '0')
  return `${base.slice(0, 8)}-${base.slice(0, 4)}-4${base.slice(0, 3)}-a${base.slice(0, 3)}-${base}${base.slice(0, 4)}`
}
