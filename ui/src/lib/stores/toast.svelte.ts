import { toast } from 'svelte-sonner'
import type { ExternalToast } from 'svelte-sonner'

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

export function addToast(message: string, type: ToastType = 'info', duration?: number): void {
  switch (type) {
    case 'success':
      success(message, duration)
      return
    case 'error':
      error(message, duration)
      return
    case 'warning':
      warning(message, duration)
      return
    case 'info':
    default:
      info(message, duration)
      return
  }
}

export function removeToast(id: number | string): void {
  toast.dismiss(id)
}

export function dismiss(id?: number | string): void {
  toast.dismiss(id)
}

export function getToasts() {
  return toast.getActiveToasts()
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
