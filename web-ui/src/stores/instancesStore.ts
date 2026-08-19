import { create } from 'zustand'
import api from '@/services/api'

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

interface InstancesStore {
  instances: InstanceInfo[]
  selectedInstanceId: string | null
  remoteSessions: RemoteSession[]
  loading: boolean
  fetchInstances: () => Promise<void>
  selectInstance: (id: string) => Promise<void>
  fetchRemoteMessages: (instanceId: string, sessionId: string) => Promise<RemoteMessage[]>
  sendRemoteMessage: (instanceId: string, sessionId: string, content: string) => Promise<void>
  cancelRemote: (instanceId: string, sessionId: string) => Promise<void>
}

type RawInstancesResponse = { instances: InstanceInfo[] }
type RawSessionsResponse = { sessions: RemoteSession[] }
type RawMessagesResponse = { messages: RemoteMessage[] }

export const useInstancesStore = create<InstancesStore>((set) => ({
  instances: [],
  selectedInstanceId: null,
  remoteSessions: [],
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
    set({ selectedInstanceId: id, remoteSessions: [] })
    try {
      const data = await api.get<RawSessionsResponse>(`/api/v1/instances/${id}/sessions`)
      set({ remoteSessions: data.sessions ?? [] })
    } catch (err) {
      console.error('[instances] failed to load remote sessions', err)
      set({ remoteSessions: [] })
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
