import { toast } from 'svelte-sonner'
import type { ExternalToast } from 'svelte-sonner'
import { friendlyError } from '../utils/errors'

export type ToastType = 'info' | 'success' | 'error' | 'warning'

const DEFAULT_DURATION: Record<ToastType, number> = {
  success: 3600,
  info: 4400,
  warning: 5600,
  error: 7000,
}

function normalizeMessage(message: string): string {
  return (message ?? '').trim()
}

function resolveToastOptions(
  type: ToastType,
  optionsOrDuration?: number | ExternalToast,
): ExternalToast {
  if (typeof optionsOrDuration === 'number') {
    return { duration: Math.max(0, optionsOrDuration) }
  }
  return {
    duration: optionsOrDuration?.duration ?? DEFAULT_DURATION[type],
    ...optionsOrDuration,
  }
}

export function success(message: string, optionsOrDuration?: number | ExternalToast): void {
  const cleanMessage = normalizeMessage(message)
  if (!cleanMessage) return
  toast.success(cleanMessage, resolveToastOptions('success', optionsOrDuration))
}

export function error(message: string, optionsOrDuration?: number | ExternalToast): void {
  const cleanMessage = normalizeMessage(message)
  if (!cleanMessage) return
  toast.error(cleanMessage, resolveToastOptions('error', optionsOrDuration))
}

/**
 * Toast for a failed call: a human message, with the original server text as
 * the description. action reads after "Could not", e.g. 'save the policy'.
 */
export function errorFrom(e: unknown, action?: string): void {
  const { message, detail } = friendlyError(e, action)
  error(message, detail ? { description: detail } : undefined)
}

export function warning(message: string, optionsOrDuration?: number | ExternalToast): void {
  const cleanMessage = normalizeMessage(message)
  if (!cleanMessage) return
  toast.warning(cleanMessage, resolveToastOptions('warning', optionsOrDuration))
}

export function info(message: string, optionsOrDuration?: number | ExternalToast): void {
  const cleanMessage = normalizeMessage(message)
  if (!cleanMessage) return
  toast.info(cleanMessage, resolveToastOptions('info', optionsOrDuration))
}
