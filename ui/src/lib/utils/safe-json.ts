/**
 * safe-json.ts
 *
 * Precision-safe JSON parsing for ClickHouse results.
 *
 * Problem: JavaScript's JSON.parse() converts all numbers to IEEE 754 Float64,
 * which only has ~15.9 significant digits. ClickHouse UInt64/Int64 values like
 * order IDs (e.g. 816687988383154176) are silently rounded to a phantom value
 * (816687988383154200), making them useless as identifiers.
 *
 * Solution: Intercept large integers before they lose precision:
 *   1. Primary: Use TC39 Stage 4 reviver `context.source` (native, zero-cost).
 *      Supported in Chrome 114+, Firefox 135+, Safari 18.4+ (~86% of users).
 *   2. Fallback: json-bigint with `storeAsString: true` for older browsers.
 *
 * Large integers are returned as strings. Consumer code must handle both
 * `number` (safe integers) and `string` (large integers) for numeric columns.
 */

import JSONbig from 'json-bigint'

// Feature-detect TC39 reviver context.source support
let hasReviverSource = false
try {
  JSON.parse('1', (_key, _value, ctx: any) => {
    if (typeof ctx?.source === 'string') hasReviverSource = true
    return _value
  })
} catch {
  // Older environments may throw on the extra argument; fallback is used
}

// Lazy-initialised fallback parser (json-bigint allocates a parser object)
let _fallbackParser: ReturnType<typeof JSONbig> | null = null
function getFallbackParser() {
  if (!_fallbackParser) {
    _fallbackParser = JSONbig({ storeAsString: true })
  }
  return _fallbackParser
}

/**
 * Parse a JSON string with precision-safe handling of large integers.
 *
 * Safe integers (|n| <= 2^53 - 1) are returned as `number`, exactly as
 * standard JSON.parse does. Large integers are returned as `string` to
 * preserve all digits. Everything else is unchanged.
 */
export function safeParse(text: string): any {
  if (hasReviverSource) {
    return JSON.parse(text, (key, value, ctx: any) => {
      // ctx.source is the raw token string from the original JSON text.
      // If the parsed value rounded (i.e., it's a number that isn't a safe
      // integer) and the raw source was a plain integer literal, keep the
      // raw string so no precision is lost.
      if (
        typeof value === 'number' &&
        !Number.isSafeInteger(value) &&
        /^-?\d+$/.test(ctx.source as string)
      ) {
        return ctx.source as string
      }
      return value
    })
  }

  // Fallback for browsers without reviver context.source support
  return getFallbackParser().parse(text)
}
