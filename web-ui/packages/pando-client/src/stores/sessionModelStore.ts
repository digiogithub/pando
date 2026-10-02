import { create } from 'zustand'
import api from '../services/api'
import { AUTO_MODEL_ID } from './modelAutoModeStore'
import { useSessionStore } from './sessionStore'

/**
 * Model selection of the active chat session. It is scoped to the session:
 * picking a model in the switcher never changes the coder model persisted in
 * the configuration (`default_model`), which stays the default of every session.
 */
export interface SessionModelSelection {
  /** Model picked for the session; null means the configured default model. */
  model: string | null
  /** True when the session runs in model auto mode. */
  auto: boolean
}

interface SessionModelResponse {
  model: string
  override: boolean
  autoSelected: boolean
}

interface SessionModelStore {
  /** Session the selection belongs to; null while the chat has no session yet. */
  sessionId: string | null
  /** null means "no session-scoped choice": the configured defaults apply. */
  selection: SessionModelSelection | null
  /** Records a selection made in the switcher for the active session. */
  select: (modelId: string) => void
  /** Loads the selection the server holds for a session. */
  load: (sessionId: string) => Promise<void>
}

export const useSessionModelStore = create<SessionModelStore>((set, get) => ({
  sessionId: null,
  selection: null,

  select: (modelId) =>
    set({ selection: modelId === AUTO_MODEL_ID ? { model: null, auto: true } : { model: modelId, auto: false } }),

  load: async (sessionId) => {
    try {
      const r = await api.get<SessionModelResponse>(`/api/v1/sessions/${sessionId}/model`)
      if (get().sessionId !== sessionId) return
      set({ selection: { model: r.override ? r.model : null, auto: !!r.autoSelected } })
    } catch {
      // Older backend without session-scoped models: keep what is shown.
    }
  },
}))

/**
 * Value sent as `model` with the prompt that creates a session, so a selection
 * made on an empty chat is applied to the new session by the server.
 */
export function pendingSessionModel(): string | undefined {
  const { sessionId, selection } = useSessionModelStore.getState()
  if (sessionId !== null || !selection) return undefined
  if (selection.auto) return AUTO_MODEL_ID
  return selection.model ?? undefined
}

// Follow the active session. A selection made before the session existed is
// kept across its creation (the server received it with the first prompt);
// any other switch starts from the configured defaults until the server answers.
useSessionStore.subscribe((state, prev) => {
  const id = state.activeSessionId
  if (id === prev.activeSessionId) return
  const store = useSessionModelStore.getState()
  if (id === null) {
    useSessionModelStore.setState({ sessionId: null, selection: null })
    return
  }
  const adoptPending = prev.activeSessionId === null && store.sessionId === null
  useSessionModelStore.setState({ sessionId: id, selection: adoptPending ? store.selection : null })
  void store.load(id)
})
