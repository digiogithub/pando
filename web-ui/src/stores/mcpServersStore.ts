import { create } from 'zustand'
import api from '@/services/api'
import type { MCPServerConfig } from '@/types'
import { useToastStore } from './toastStore'

interface MCPServersStore {
  servers: MCPServerConfig[]
  loading: boolean
  saving: boolean
  error: string | null
  fetchServers: () => Promise<void>
  saveServer: (server: MCPServerConfig) => Promise<void>
  deleteServer: (name: string) => Promise<void>
  reloadServer: (name: string) => Promise<void>
}

export const useMCPServersStore = create<MCPServersStore>((set, get) => ({
  servers: [],
  loading: false,
  saving: false,
  error: null,

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
    await api.post(`/api/v1/config/mcp-servers/${encodeURIComponent(name)}/reload`, {})
  },
}))
