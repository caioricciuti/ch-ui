import { describe, expect, it } from 'vitest'
import { grantStatements } from './clusterHealth'

describe('grantStatements', () => {
  it('writes one GRANT per table for the user', () => {
    expect(grantStatements(['system.disks', 'system.replicas'], 'restricted')).toBe(
      'GRANT SELECT ON system.disks TO `restricted`;\nGRANT SELECT ON system.replicas TO `restricted`;',
    )
  })

  it('escapes backticks and backslashes in the user name', () => {
    expect(grantStatements(['system.parts'], 'a`b\\c')).toBe('GRANT SELECT ON system.parts TO `a\\`b\\\\c`;')
  })
})
