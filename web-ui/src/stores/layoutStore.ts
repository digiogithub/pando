import { create } from 'zustand'

interface LayoutStore {
  sidebarOpen: boolean
  quickMenuOpen: boolean
  modelSwitcherOpen: boolean
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  setQuickMenuOpen: (open: boolean) => void
  setModelSwitcherOpen: (open: boolean) => void
}

export const useLayoutStore = create<LayoutStore>((set) => ({
  sidebarOpen: window.innerWidth > 768,
  quickMenuOpen: false,
  modelSwitcherOpen: false,
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  setQuickMenuOpen: (open) => set({ quickMenuOpen: open }),
  setModelSwitcherOpen: (open) => set({ modelSwitcherOpen: open }),
}))
