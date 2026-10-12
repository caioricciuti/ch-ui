/** Dashboard grid layout utilities — pure functions, no framework dependency */

export const COLS = 12
export const ROW_H = 60
export const GAP = 16
export const MIN_W = 2
export const MIN_H = 2

export interface LayoutItem {
  id: string
  x: number
  y: number
  w: number
  h: number
}

/** Convert grid coordinates to absolute pixel position */
export function gridToPixel(
  item: { x: number; y: number; w: number; h: number },
  colW: number,
): { left: number; top: number; width: number; height: number } {
  return {
    left: item.x * (colW + GAP),
    top: item.y * (ROW_H + GAP),
    width: item.w * (colW + GAP) - GAP,
    height: item.h * (ROW_H + GAP) - GAP,
  }
}

/** Compute column width from container width */
export function calcColW(containerWidth: number): number {
  return (containerWidth - GAP * (COLS - 1)) / COLS
}

/** AABB overlap test */
export function rectsOverlap(a: LayoutItem, b: LayoutItem): boolean {
  if (a.id === b.id) return false
  return (
    a.x < b.x + b.w &&
    a.x + a.w > b.x &&
    a.y < b.y + b.h &&
    a.y + a.h > b.y
  )
}

/**
 * Compact layout: resolve collisions and apply gravity.
 * The moved panel (movedId) keeps its target position;
 * overlapping panels are pushed below it, then all panels
 * are pulled upward as far as possible.
 */
export function compact(items: LayoutItem[], movedId?: string): LayoutItem[] {
  const result = items.map(i => ({ ...i }))
  const moved = movedId ? result.find(i => i.id === movedId) : undefined

  // Push panels that overlap the moved panel below it
  if (moved) {
    for (const item of result) {
      if (item.id === movedId) continue
      if (rectsOverlap(moved, item)) {
        item.y = moved.y + moved.h
      }
    }
    // Cascade: resolve secondary overlaps caused by pushing
    const others = result.filter(i => i.id !== movedId).sort((a, b) => a.y - b.y || a.x - b.x)
    for (let i = 0; i < others.length; i++) {
      for (let j = i + 1; j < others.length; j++) {
        if (rectsOverlap(others[i], others[j])) {
          others[j].y = others[i].y + others[i].h
        }
      }
    }
  }

  // Sort by y then x for gravity pass (moved item excluded from sorting priority)
  const sorted = [...result].sort((a, b) => a.y - b.y || a.x - b.x)

  // Gravity: pull each non-moved panel upward as far as possible
  let changed = true
  while (changed) {
    changed = false
    for (const item of sorted) {
      if (item.id === movedId) continue
      while (item.y > 0) {
        const test = { ...item, y: item.y - 1 }
        const collides = result.some(
          o => o.id !== item.id && rectsOverlap(test, o),
        )
        if (!collides) {
          item.y--
          changed = true
        } else {
          break
        }
      }
    }
  }

  return result
}

/** Compute the total grid height in pixels from the bottommost panel */
export function containerHeight(items: LayoutItem[]): number {
  if (items.length === 0) return ROW_H
  const maxBottom = Math.max(...items.map(i => i.y + i.h))
  return maxBottom * (ROW_H + GAP)
}

/** Below this container width (px) panels stack in one full-width column. */
export const STACK_BELOW = 640

/**
 * One column layout for narrow screens: panels keep their height and their
 * reading order (top to bottom, then left to right) and take the full width.
 */
export function stackLayout(items: LayoutItem[]): LayoutItem[] {
  const ordered = [...items].sort((a, b) => a.y - b.y || a.x - b.x)
  let y = 0
  return ordered.map(i => {
    const out = { id: i.id, x: 0, y, w: COLS, h: i.h }
    y += i.h
    return out
  })
}

/** The first free row below every panel, where a new panel goes. */
export function nextFreeRow(items: { y: number; h: number }[]): number {
  return items.reduce((max, i) => Math.max(max, i.y + i.h), 0)
}
