import { create } from 'zustand'

const CHAT_MODE_KEY = 'pando_chat_mode'
const INFO_SIDEBAR_KEY = 'pando_info_sidebar_open'

export type ChatMode = 'simple' | 'advanced'

interface LayoutStore {
  sidebarOpen: boolean
  /** right-hand chat info panel (session usage, plan, modified files) */
  infoSidebarOpen: boolean
  quickMenuOpen: boolean
  modelSwitcherOpen: boolean
  chatMode: ChatMode
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  toggleInfoSidebar: () => void
  setInfoSidebarOpen: (open: boolean) => void
  setQuickMenuOpen: (open: boolean) => void
  setModelSwitcherOpen: (open: boolean) => void
  setChatMode: (mode: ChatMode) => void
}

// The info panel defaults to open on wide viewports only; the stored preference
// wins once the user has toggled it at least once.
const storedInfoSidebar = localStorage.getItem(INFO_SIDEBAR_KEY)
const initialInfoSidebarOpen =
  storedInfoSidebar === null ? window.innerWidth > 1100 : storedInfoSidebar === 'true'

export const useLayoutStore = create<LayoutStore>((set) => ({
  sidebarOpen: window.innerWidth > 768,
  infoSidebarOpen: initialInfoSidebarOpen,
  quickMenuOpen: false,
  modelSwitcherOpen: false,
  chatMode: (localStorage.getItem(CHAT_MODE_KEY) as ChatMode) || 'advanced',
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  toggleInfoSidebar: () =>
    set((s) => {
      const open = !s.infoSidebarOpen
      localStorage.setItem(INFO_SIDEBAR_KEY, String(open))
      return { infoSidebarOpen: open }
    }),
  setInfoSidebarOpen: (open) => {
    localStorage.setItem(INFO_SIDEBAR_KEY, String(open))
    set({ infoSidebarOpen: open })
  },
  setQuickMenuOpen: (open) => set({ quickMenuOpen: open }),
  setModelSwitcherOpen: (open) => set({ modelSwitcherOpen: open }),
  setChatMode: (mode) => {
    localStorage.setItem(CHAT_MODE_KEY, mode)
    set({ chatMode: mode })
  },
}))
