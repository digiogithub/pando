import { create } from 'zustand'
import api from '../services/api'

export interface InstanceInfo {
  instance_id: string
  path: string
  pid: number
  pub_port: number
  rpc_port: number
  started_at: string
  mode: string
  is_primary: boolean
}

export interface RemoteSession {
  id: string
  title: string
  updated_at: string
  message_count: number
}

export interface RemoteMessage {
  id: string
  role: string
  content: string
  created_at: string
}

/** page size for the remote session list */
export const REMOTE_SESSIONS_PAGE_SIZE = 100

interface InstancesStore {
  instances: InstanceInfo[]
  selectedInstanceId: string | null
  remoteSessions: RemoteSession[]
  /** total sessions on the selected remote instance */
  remoteSessionsTotal: number
  remoteSessionsHasMore: boolean
  remoteSessionsLoadingMore: boolean
  loading: boolean
  fetchInstances: () => Promise<void>
  selectInstance: (id: string) => Promise<void>
  /** append the next page of the selected instance's sessions */
  loadMoreRemoteSessions: () => Promise<void>
  fetchRemoteMessages: (instanceId: string, sessionId: string) => Promise<RemoteMessage[]>
  sendRemoteMessage: (instanceId: string, sessionId: string, content: string) => Promise<void>
  cancelRemote: (instanceId: string, sessionId: string) => Promise<void>
}

type RawInstancesResponse = { instances: InstanceInfo[] }
type RawSessionsResponse = { sessions: RemoteSession[]; total?: number; has_more?: boolean }
type RawMessagesResponse = { messages: RemoteMessage[] }

export const useInstancesStore = create<InstancesStore>((set, get) => ({
  instances: [],
  selectedInstanceId: null,
  remoteSessions: [],
  remoteSessionsTotal: 0,
  remoteSessionsHasMore: false,
  remoteSessionsLoadingMore: false,
  loading: false,

  fetchInstances: async () => {
    set({ loading: true })
    try {
      const data = await api.get<RawInstancesResponse>('/api/v1/instances')
      set({ instances: data.instances ?? [] })
    } finally {
      set({ loading: false })
    }
  },

  selectInstance: async (id: string) => {
    set({
      selectedInstanceId: id,
      remoteSessions: [],
      remoteSessionsTotal: 0,
      remoteSessionsHasMore: false,
    })
    try {
      const data = await api.get<RawSessionsResponse>(
        `/api/v1/instances/${id}/sessions?limit=${REMOTE_SESSIONS_PAGE_SIZE}&offset=0`,
      )
      const sessions = data.sessions ?? []
      set({
        remoteSessions: sessions,
        remoteSessionsTotal: data.total ?? sessions.length,
        remoteSessionsHasMore: data.has_more ?? false,
      })
    } catch (err) {
      console.error('[instances] failed to load remote sessions', err)
      set({ remoteSessions: [] })
    }
  },

  loadMoreRemoteSessions: async () => {
    const { selectedInstanceId, remoteSessions, remoteSessionsHasMore, remoteSessionsLoadingMore } = get()
    if (!selectedInstanceId || !remoteSessionsHasMore || remoteSessionsLoadingMore) return
    set({ remoteSessionsLoadingMore: true })
    try {
      const data = await api.get<RawSessionsResponse>(
        `/api/v1/instances/${selectedInstanceId}/sessions?limit=${REMOTE_SESSIONS_PAGE_SIZE}&offset=${remoteSessions.length}`,
      )
      const known = new Set(remoteSessions.map((s) => s.id))
      const merged = [...remoteSessions, ...(data.sessions ?? []).filter((s) => !known.has(s.id))]
      set({
        remoteSessions: merged,
        remoteSessionsTotal: data.total ?? merged.length,
        remoteSessionsHasMore: data.has_more ?? false,
      })
    } catch (err) {
      console.error('[instances] failed to load more remote sessions', err)
    } finally {
      set({ remoteSessionsLoadingMore: false })
    }
  },

  fetchRemoteMessages: async (instanceId: string, sessionId: string) => {
    const data = await api.get<RawMessagesResponse>(
      `/api/v1/instances/${instanceId}/sessions/${sessionId}/messages`,
    )
    return data.messages ?? []
  },

  sendRemoteMessage: async (instanceId: string, sessionId: string, content: string) => {
    await api.post(`/api/v1/instances/${instanceId}/sessions/${sessionId}/message`, { content })
  },

  cancelRemote: async (instanceId: string, sessionId: string) => {
    await api.delete(`/api/v1/instances/${instanceId}/sessions/${sessionId}/cancel`)
  },
}))
