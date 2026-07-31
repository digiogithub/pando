import { create } from 'zustand'
import api from '@/services/api'
import type { MCPServerAuthStatus, MCPServerConfig } from '@/types'
import { useToastStore } from './toastStore'

interface MCPLoginStartResponse {
  authorizationUrl: string
  message: string
}

interface MCPServersStore {
  servers: MCPServerConfig[]
  loading: boolean
  saving: boolean
  error: string | null
  // authBusy tracks which server currently has a login/logout request in
  // flight, so the UI can disable just that row's buttons.
  authBusy: string | null
  fetchServers: () => Promise<void>
  saveServer: (server: MCPServerConfig) => Promise<void>
  deleteServer: (name: string) => Promise<void>
  reloadServer: (name: string) => Promise<void>
  loginServer: (name: string) => Promise<void>
  logoutServer: (name: string) => Promise<void>
  fetchAuthStatus: (name: string) => Promise<MCPServerAuthStatus | null>
}

export const useMCPServersStore = create<MCPServersStore>((set, get) => ({
  servers: [],
  loading: false,
  saving: false,
  error: null,
  authBusy: null,

  fetchServers: async () => {
    set({ loading: true, error: null })
    try {
      const data = await api.get<{ mcpServers: MCPServerConfig[] }>('/api/v1/config/mcp-servers')
      set({ servers: data.mcpServers ?? [] })
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Failed to load MCP servers' })
    } finally {
      set({ loading: false })
    }
  },

  saveServer: async (server: MCPServerConfig) => {
    set({ saving: true, error: null })
    try {
      const data = await api.put<{ mcpServers: MCPServerConfig[] }>('/api/v1/config/mcp-servers', server)
      set({ servers: data.mcpServers ?? get().servers })
      useToastStore.getState().addToast(`MCP server "${server.name}" saved`, 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Save failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  deleteServer: async (name: string) => {
    set({ saving: true, error: null })
    try {
      await api.delete(`/api/v1/config/mcp-servers/${encodeURIComponent(name)}`)
      set((s) => ({ servers: s.servers.filter((srv) => srv.name !== name) }))
      useToastStore.getState().addToast(`MCP server "${name}" deleted`, 'success')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Delete failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ saving: false })
    }
  },

  reloadServer: async (name: string) => {
    set({ error: null })
    try {
      const data = await api.post<{ status: string; name: string; tools: number }>(
        `/api/v1/config/mcp-servers/${encodeURIComponent(name)}/reload`,
        {},
      )
      const count = data?.tools ?? 0
      useToastStore
        .getState()
        .addToast(`MCP server "${name}" reloaded — ${count} tool${count === 1 ? '' : 's'} discovered`, count > 0 ? 'success' : 'info')
    } catch (e) {
      // The reload endpoint reports the real connection/handshake error, which
      // is the only place the user can see why a server ends up with 0 tools.
      const msg = e instanceof Error ? e.message : 'Reload failed'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      // Refresh either way: the catalog changed on success, and on failure the
      // (now empty) tool list is the honest state.
      await get().fetchServers()
    }
  },

  loginServer: async (name: string) => {
    set({ authBusy: name, error: null })
    try {
      const data = await api.post<MCPLoginStartResponse>(`/api/v1/mcp/${encodeURIComponent(name)}/login`, {})
      if (data.authorizationUrl) {
        window.open(data.authorizationUrl, '_blank', 'noopener,noreferrer')
      }
      useToastStore.getState().addToast(data.message || `Authorization started for "${name}" — complete it in the opened tab.`, 'info')
      // The OAuth flow completes in the background once the user finishes
      // the browser round trip; give it a moment before polling status so
      // the first refresh isn't a guaranteed "still pending".
      window.setTimeout(() => {
        get().fetchServers()
      }, 5000)
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to start MCP login'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ authBusy: null })
    }
  },

  logoutServer: async (name: string) => {
    set({ authBusy: name, error: null })
    try {
      await api.post(`/api/v1/mcp/${encodeURIComponent(name)}/logout`, {})
      useToastStore.getState().addToast(`Logged out of MCP server "${name}"`, 'success')
      await get().fetchServers()
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to log out of MCP server'
      set({ error: msg })
      useToastStore.getState().addToast(msg, 'error')
    } finally {
      set({ authBusy: null })
    }
  },

  fetchAuthStatus: async (name: string) => {
    try {
      return await api.get<MCPServerAuthStatus>(`/api/v1/mcp/${encodeURIComponent(name)}/status`)
    } catch {
      return null
    }
  },
}))
