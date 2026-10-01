import { create } from 'zustand'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface Toast {
  id: string
  type: ToastType
  message?: string
  i18nKey?: string
  i18nValues?: Record<string, unknown>
}

interface ToastStore {
  toasts: Toast[]
  addToast: (message: string, type: ToastType, ttlMs?: number) => void
  addToastKey: (i18nKey: string, type: ToastType, i18nValues?: Record<string, unknown>, ttlMs?: number) => void
  removeToast: (id: string) => void
}

function makeToastId(): string {
  return `toast-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function scheduleRemoval(id: string, ttlMs: number, set: (fn: (state: ToastStore) => Partial<ToastStore>) => void) {
  if (ttlMs <= 0) return
  setTimeout(() => {
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
  }, ttlMs)
}

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],

  addToast: (message, type, ttlMs = 4000) => {
    const id = makeToastId()
    set((s) => ({ toasts: [...s.toasts, { id, message, type }] }))
    scheduleRemoval(id, ttlMs, set)
  },

  addToastKey: (i18nKey, type, i18nValues, ttlMs = 4000) => {
    const id = makeToastId()
    set((s) => ({ toasts: [...s.toasts, { id, type, i18nKey, i18nValues }] }))
    scheduleRemoval(id, ttlMs, set)
  },

  removeToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))

export function useToast() {
  const addToast = useToastStore((s) => s.addToast)
  return {
    success: (message: string, ttlMs?: number) => addToast(message, 'success', ttlMs),
    error: (message: string, ttlMs?: number) => addToast(message, 'error', ttlMs),
    warning: (message: string, ttlMs?: number) => addToast(message, 'warning', ttlMs),
    info: (message: string, ttlMs?: number) => addToast(message, 'info', ttlMs),
  }
}
