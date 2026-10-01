import { create } from 'zustand'
import api from '../services/api'
import { localBrowserStorage } from '../services/storage'

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
  /** Adopts the chat mode stored server-side (user-level, port independent). */
  hydrateChatMode: () => Promise<void>
}

function readStoredChatMode(): ChatMode {
  return localBrowserStorage.getItem(CHAT_MODE_KEY) === 'simple' ? 'simple' : 'advanced'
}

// Set once the user picks a mode in this page load, so a slower server read
// never overrides that fresher choice.
let chatModeChosen = false

function writeStoredChatMode(mode: ChatMode) {
  localBrowserStorage.setItem(CHAT_MODE_KEY, mode)
}

// The info panel defaults to open on wide viewports only; the stored preference
// wins once the user has toggled it at least once.
const storedInfoSidebar = localBrowserStorage.getItem(INFO_SIDEBAR_KEY)
const initialInfoSidebarOpen =
  storedInfoSidebar === null ? window.innerWidth > 1100 : storedInfoSidebar === 'true'

export const useLayoutStore = create<LayoutStore>((set) => ({
  sidebarOpen: window.innerWidth > 768,
  infoSidebarOpen: initialInfoSidebarOpen,
  quickMenuOpen: false,
  modelSwitcherOpen: false,
  chatMode: readStoredChatMode(),
  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  toggleInfoSidebar: () =>
    set((s) => {
      const open = !s.infoSidebarOpen
      localBrowserStorage.setItem(INFO_SIDEBAR_KEY, String(open))
      return { infoSidebarOpen: open }
    }),
  setInfoSidebarOpen: (open) => {
    localBrowserStorage.setItem(INFO_SIDEBAR_KEY, String(open))
    set({ infoSidebarOpen: open })
  },
  setQuickMenuOpen: (open) => set({ quickMenuOpen: open }),
  setModelSwitcherOpen: (open) => set({ modelSwitcherOpen: open }),
  setChatMode: (mode) => {
    chatModeChosen = true
    writeStoredChatMode(mode)
    set({ chatMode: mode })
    // localStorage is per origin and the desktop app / project instances can
    // come up on another port, so the choice is also kept user-level.
    void api.put('/api/v1/ui/preferences', { chatMode: mode }).catch(() => {})
  },
  hydrateChatMode: async () => {
    try {
      const prefs = await api.get<{ chatMode?: string }>('/api/v1/ui/preferences')
      if (chatModeChosen) return
      if (prefs.chatMode === 'simple' || prefs.chatMode === 'advanced') {
        writeStoredChatMode(prefs.chatMode)
        set({ chatMode: prefs.chatMode })
      }
    } catch {
      // keep the local value when the server cannot answer
    }
  },
}))
