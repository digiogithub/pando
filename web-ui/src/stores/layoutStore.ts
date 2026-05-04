import { create } from 'zustand'

const CHAT_MODE_KEY = 'pando_chat_mode'

export type ChatMode = 'simple' | 'advanced'

interface LayoutStore {
  sidebarOpen: boolean
  quickMenuOpen: boolean
  modelSwitcherOpen: boolean
  chatMode: ChatMode
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  setQuickMenuOpen: (open: boolean) => void
  setModelSwitcherOpen: (open: boolean) => void
  setChatMode: (mode: ChatMode) => void
}

export const useLayoutStore = create<LayoutStore>((set) => ({
  sidebarOpen: window.innerWidth > 768,
  quickMenuOpen: false,
  modelSwitcherOpen: false,
  chatMode: (localStorage.getItem(CHAT_MODE_KEY) as ChatMode) || 'advanced',
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setQuickMenuOpen: (open) => set({ quickMenuOpen: open }),
  setModelSwitcherOpen: (open) => set({ modelSwitcherOpen: open }),
  setChatMode: (mode) => {
    localStorage.setItem(CHAT_MODE_KEY, mode)
    set({ chatMode: mode })
  },
}))
