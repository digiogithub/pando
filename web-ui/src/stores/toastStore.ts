import { create } from 'zustand'

export type ToastType = 'success' | 'error' | 'warning' | 'info'

export interface Toast {
  id: string
  message: string
  type: ToastType
}

interface ToastStore {
  toasts: Toast[]
  addToast: (message: string, type: ToastType, ttlMs?: number) => void
  removeToast: (id: string) => void
}

export const useToastStore = create<ToastStore>((set) => ({
  toasts: [],

  addToast: (message, type, ttlMs = 4000) => {
    const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2)}`
    set((s) => ({ toasts: [...s.toasts, { id, message, type }] }))

    if (ttlMs > 0) {
      setTimeout(() => {
        set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }))
      }, ttlMs)
    }
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
